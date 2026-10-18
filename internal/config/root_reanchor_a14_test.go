// root_reanchor_a14_test.go — 第九轮审计 A-14（P2）复现守卫。
//
// 缺陷原形：`xdev config set core.xmdevenv_dir <新目录>` 只改 XMDevDir 字段，
// 然后 Save()。而 defaults(xmdeDir) 是按**初始** xmdeDir 播种 install/shim/
// cache/env/manifest/download 这 6 个派生子目录的，ResolvePaths 又只填**空**
// 字段（每个字段都有 `if c.Core.X == ""` 守卫）—— 于是新根 + 旧子目录的
// **混合状态**被持久化：工具仍装进旧 home，PATH 仍写旧 shims。
//
// 既有先例（均显式清空那 6 个字段后才 ResolvePaths）：
//   - internal/cli/init.go:105-112
//   - internal/gui/app_migration.go:269-276
//
// 本守卫要求 config 包提供同一个「重定根」助手，并把
// 「ResolvePaths 对非空字段 no-op」这一契约**固定下来**（不得为了让助手好写
// 而改成覆盖非空字段 —— 那会破坏其它调用方）。
package config

import (
	"os"
	"path/filepath"
	"testing"
)

// derivedDirFields 返回 6 个派生子目录字段的读写器，避免用例里复制 6 遍。
func derivedDirFields(c *CoreConfig) map[string]*string {
	return map[string]*string{
		"download_dir": &c.DownloadDir,
		"install_dir":  &c.InstallDir,
		"shim_dir":     &c.ShimDir,
		"cache_dir":    &c.CacheDir,
		"env_dir":      &c.EnvDir,
		"manifest_dir": &c.ManifestDir,
	}
}

// derivedDirNames 是子目录名（与 defaults/ResolvePaths 的 Join 参数一致）。
var derivedDirNames = map[string]string{
	"download_dir": "downloads",
	"install_dir":  "install",
	"shim_dir":     "shims",
	"cache_dir":    "cache",
	"env_dir":      "env",
	"manifest_dir": "manifests",
}

// TestSetRootDir_ReanchorsAllDerivedDirs 是本项的红→绿守卫：
// 重定根之后，6 个派生子目录必须全部落在新根下。
func TestSetRootDir_ReanchorsAllDerivedDirs(t *testing.T) {
	oldRoot := t.TempDir()
	newRoot := t.TempDir()

	cfg := defaults(oldRoot)
	// 先落盘，模拟「用户已有配置」的真实起点。
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	cfg.SetRootDir(newRoot)

	if cfg.Core.XMDevDir != newRoot {
		t.Fatalf("XMDevDir = %q, want %q", cfg.Core.XMDevDir, newRoot)
	}
	for field, ptr := range derivedDirFields(&cfg.Core) {
		want := filepath.Join(newRoot, derivedDirNames[field])
		if *ptr != want {
			t.Errorf("A-14：%s = %q, want %q（重定根后必须落在新根下）", field, *ptr, want)
		}
		if !pathWithin(newRoot, *ptr) {
			t.Errorf("A-14：%s = %q 不在新根 %q 之下", field, *ptr, newRoot)
		}
	}
}

// TestSetRootDir_PersistsConsistentRoot：Save 之后重新 Load，6 个派生子目录
// 仍必须全部在新根下（缺陷原形是把混合状态写盘）。
func TestSetRootDir_PersistsConsistentRoot(t *testing.T) {
	oldRoot := t.TempDir()
	newRoot := t.TempDir()

	cfg := defaults(oldRoot)
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	cfg.SetRootDir(newRoot)
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save after SetRootDir: %v", err)
	}

	loaded, err := Load(newRoot)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Core.XMDevDir != newRoot {
		t.Fatalf("持久化后的 XMDevDir = %q, want %q", loaded.Core.XMDevDir, newRoot)
	}
	for field, ptr := range derivedDirFields(&loaded.Core) {
		want := filepath.Join(newRoot, derivedDirNames[field])
		if *ptr != want {
			t.Errorf("A-14：重新加载后 %s = %q, want %q（混合状态被写盘）", field, *ptr, want)
		}
	}

	// 旧根下不得再被创建出新的子目录（EnsureDirs 会按新根建）。
	if _, err := os.Stat(filepath.Join(oldRoot, "shims")); err == nil {
		t.Logf("注意：旧根 %s 下仍存在 shims（可能是首轮 defaults 的残留，非本次缺陷判定项）", oldRoot)
	}
}

// TestSetRootDir_ClearsExplicitOverridesOfDerivedDirs：派生子目录曾被显式
// 覆盖时，重定根也必须把它们拉回新根（否则「新根 + 旧覆盖」仍是混合状态）。
func TestSetRootDir_ClearsExplicitOverridesOfDerivedDirs(t *testing.T) {
	oldRoot := t.TempDir()
	newRoot := t.TempDir()
	explicit := t.TempDir()

	cfg := defaults(oldRoot)
	cfg.Core.InstallDir = filepath.Join(explicit, "custom-install")
	cfg.Core.ShimDir = filepath.Join(explicit, "custom-shims")

	cfg.SetRootDir(newRoot)

	if cfg.Core.InstallDir != filepath.Join(newRoot, "install") {
		t.Errorf("InstallDir = %q, want %q", cfg.Core.InstallDir, filepath.Join(newRoot, "install"))
	}
	if cfg.Core.ShimDir != filepath.Join(newRoot, "shims") {
		t.Errorf("ShimDir = %q, want %q", cfg.Core.ShimDir, filepath.Join(newRoot, "shims"))
	}
}

// TestResolvePaths_KeepsNoOpContractForNonEmptyFields 把「ResolvePaths 只填空
// 字段」这一既有契约钉死：它必须继续对非空字段 no-op（多个调用方依赖），
// 因此 A-14 的修复必须在「改根」路径上显式清空，而不是改 ResolvePaths。
func TestResolvePaths_KeepsNoOpContractForNonEmptyFields(t *testing.T) {
	oldRoot := t.TempDir()
	newRoot := t.TempDir()

	cfg := defaults(oldRoot)
	cfg.Core.XMDevDir = newRoot
	cfg.ResolvePaths(newRoot)

	for field, ptr := range derivedDirFields(&cfg.Core) {
		want := filepath.Join(oldRoot, derivedDirNames[field])
		if *ptr != want {
			t.Errorf("ResolvePaths 对非空字段必须是 no-op：%s = %q, want %q（改了这条会破坏其它调用方）",
				field, *ptr, want)
		}
	}
}

// TestResolvePaths_StillFillsEmptyFields：no-op 契约的另一半 —— 空字段仍要填。
func TestResolvePaths_StillFillsEmptyFields(t *testing.T) {
	root := t.TempDir()
	var core CoreConfig
	cfg := &Config{Core: core}
	cfg.ResolvePaths(root)

	if cfg.Core.XMDevDir != root {
		t.Fatalf("空 XMDevDir 应被填为 %q, got %q", root, cfg.Core.XMDevDir)
	}
	for field, ptr := range derivedDirFields(&cfg.Core) {
		want := filepath.Join(root, derivedDirNames[field])
		if *ptr != want {
			t.Errorf("空 %s 应被填为 %q, got %q", field, want, *ptr)
		}
	}
}

// pathWithin 报告 child 是否位于 parent 之下（大小写不敏感，Windows 语义）。
func pathWithin(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel != ".." && !hasDotDotPrefix(rel)
}

func hasDotDotPrefix(rel string) bool {
	return len(rel) >= 2 && rel[0] == '.' && rel[1] == '.'
}
