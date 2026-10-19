package installer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/plugin"
)

// 第十轮审计 P2-3 端到端守卫：**4 段版本必须能被判定为「有更新」**。
//
// 审计对 `util.CompareSemver` 的 4 段截断只做了单元级复现
// （`CompareSemver("2026.2.0.1", "2026.2") == 0`），**没有**端到端复现「装不上」。
// 本文件补上这一条：已装 `2026.2`、manifest 默认版本 `2026.2.0.1` 时，
// `IsUpgradeAvailable` 必须返回 true。
//
// 修复前：CompareSemver 返回 0 → `> 0` 为假 → 报「已是最新」→ 四段构建永远装不上。
// 这正是 Round 8 R8-2（`refresh` 写回门）同一失败形态的又一处 —— R8-2 只修了
// `refresh.go`，`IsUpgradeAvailable` 没被覆盖。

// versionListPlugin 是带真实版本清单的测试插件（`simplePlugin` 只有固定
// DefaultVersion，覆盖不到 4 段比较）。
type versionListPlugin struct {
	name     string
	latest   string
	versions []string
}

func (p *versionListPlugin) Name() string                              { return p.name }
func (p *versionListPlugin) DisplayName() string                       { return p.name }
func (p *versionListPlugin) Description() string                       { return "test tool " + p.name }
func (p *versionListPlugin) Homepage() string                          { return "" }
func (p *versionListPlugin) Category() string                          { return "language" }
func (p *versionListPlugin) Shims() []string                           { return []string{p.name} }
func (p *versionListPlugin) RequiredPlugin() []string                  { return nil }
func (p *versionListPlugin) EnvVars(string) map[string]string          { return nil }
func (p *versionListPlugin) PostInstall(context.Context, string) error { return nil }
func (p *versionListPlugin) Deactivate(string) error                   { return nil }

func (p *versionListPlugin) Manifest() *manifest.Tool {
	t := &manifest.Tool{Name: p.name, Latest: p.latest}
	for _, v := range p.versions {
		t.VersionList = append(t.VersionList, manifest.Version{Version: v})
	}
	return t
}

func (p *versionListPlugin) DefaultVersion() string { return p.latest }

func (p *versionListPlugin) ArchiveFor(version string) (*manifest.Archive, error) {
	return &manifest.Archive{URL: "https://example.test/" + p.name + "-" + version + ".zip"}, nil
}

func (p *versionListPlugin) InstallDir(root, version string) string {
	return filepath.Join(root, p.name, version)
}

func (p *versionListPlugin) ResolveVersion(spec string) (string, error) {
	if spec == "" || spec == "latest" {
		return p.latest, nil
	}
	return spec, nil
}

// activateInstalledVersion 在 install/<tool>/<ver> 建目录并激活（current 链接）。
func activateInstalledVersion(t *testing.T, cfg *configForTest, tool, ver string) {
	t.Helper()
	installed := filepath.Join(cfg.installDir, tool, ver)
	if err := os.MkdirAll(installed, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := makeLink(installed, filepath.Join(cfg.xmdevDir, "current", tool)); err != nil {
		t.Fatalf("link current/%s: %v", tool, err)
	}
}

// TestIsUpgradeAvailable_FourSegmentLatestIsDetected 是核心端到端断言。
func TestIsUpgradeAvailable_FourSegmentLatestIsDetected(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)
	const tool = "fourseg"

	activateInstalledVersion(t, &configForTest{
		installDir: cfg.Core.InstallDir,
		xmdevDir:   cfg.Core.XMDevDir,
	}, tool, "2026.2")

	ins.reg.Register(&versionListPlugin{
		name: tool, latest: "2026.2.0.1", versions: []string{"2026.2", "2026.2.0.1"},
	})

	latest, ok := ins.IsUpgradeAvailable(tool)
	if !ok {
		t.Fatalf("P2-3 复现：已装 2026.2、manifest latest=2026.2.0.1 时 IsUpgradeAvailable 报「无更新」" +
			"（CompareSemver 把 4 段截断成 [2026 2 0] 并返回 0）—— 四段构建永远装不上")
	}
	if latest != "2026.2.0.1" {
		t.Errorf("latest = %q, want 2026.2.0.1", latest)
	}
}

// 对照：已装的就是最新时不得误报有更新。
func TestIsUpgradeAvailable_SameFourSegmentVersionIsNotAnUpgrade(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)
	const tool = "fourseg-same"

	activateInstalledVersion(t, &configForTest{
		installDir: cfg.Core.InstallDir, xmdevDir: cfg.Core.XMDevDir,
	}, tool, "2026.2.0.1")

	ins.reg.Register(&versionListPlugin{
		name: tool, latest: "2026.2.0.1", versions: []string{"2026.2.0.1"},
	})

	if latest, ok := ins.IsUpgradeAvailable(tool); ok {
		t.Errorf("已是最新（2026.2.0.1）时不应报有更新，got %q", latest)
	}
}

// 对照：4 段的第 4 段递增必须被检出。
func TestIsUpgradeAvailable_FourSegmentMinorBumpDetected(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)
	const tool = "fourseg-bump"

	activateInstalledVersion(t, &configForTest{
		installDir: cfg.Core.InstallDir, xmdevDir: cfg.Core.XMDevDir,
	}, tool, "2025.2.6.3")

	ins.reg.Register(&versionListPlugin{
		name: tool, latest: "2025.2.6.4", versions: []string{"2025.2.6.3", "2025.2.6.4"},
	})

	if _, ok := ins.IsUpgradeAvailable(tool); !ok {
		t.Errorf("2025.2.6.3 → 2025.2.6.4 应报有更新（第 4 段参与比较）")
	}
}

// 对照：降级不得被当成升级。
func TestIsUpgradeAvailable_FourSegmentDowngradeIsNotAnUpgrade(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)
	const tool = "fourseg-down"

	activateInstalledVersion(t, &configForTest{
		installDir: cfg.Core.InstallDir, xmdevDir: cfg.Core.XMDevDir,
	}, tool, "2026.2.0.9")

	ins.reg.Register(&versionListPlugin{
		name: tool, latest: "2026.2.0.1", versions: []string{"2026.2.0.1", "2026.2.0.9"},
	})

	if latest, ok := ins.IsUpgradeAvailable(tool); ok {
		t.Errorf("已装 2026.2.0.9、latest 2026.2.0.1 是降级，不应报有更新，got %q", latest)
	}
}

// findOtherVersion 也走 CompareSemver（installer.go:2007）：
// 4 段版本之间必须能选出更大的那个。
func TestFindOtherVersion_PicksHighestFourSegmentVersion(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)
	const tool = "fourseg-other"

	for _, v := range []string{"2026.2", "2026.2.0.1", "2026.2.0.2"} {
		if err := os.MkdirAll(filepath.Join(cfg.Core.InstallDir, tool, v), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	p := &versionListPlugin{name: tool, latest: "2026.2.0.2"}
	if got := ins.findOtherVersion(p); got != "2026.2.0.2" {
		t.Errorf("findOtherVersion = %q, want 2026.2.0.2（4 段必须参与比较）", got)
	}
	_ = plugin.NewRegistry // 保持 import 一致性（Registry 由 newTestInstaller 构造）
}

// configForTest 只是把 cfg 里两个用到的字段收拢，避免测试里反复写长路径。
type configForTest struct {
	installDir string
	xmdevDir   string
}
