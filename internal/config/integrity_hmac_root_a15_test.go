// integrity_hmac_root_a15_test.go — 第九轮审计 A-15（P2）复现守卫。
//
// 缺陷原形（config.go computeIntegrityHMAC）：H-12 的 HMAC 覆盖
// install/shim/manifest/download + mirror + path_mode，**唯独不含
// xmdevenv_dir**。而 xmdevenv_dir 是**重定向类**字段——它决定全部派生子目录
// 的归属。篡改它既不触发告警、也不被重置，H-12 的「防重定向」对配置根失效；
// 与 A-14 叠加时（改根后派生子目录仍是旧值）放大成「根被换掉但工具/PATH
// 仍写旧目录」的静默错配。
//
// 升级路径（本守卫的第二个契约）：纳入 xmdevenv_dir 会**改变 HMAC 值**，
// 既有配置文件在升级后必然验签失败。Load 的既有行为是「告警 + 重置敏感字段
// + 清空 Integrity」，**不拒绝启动**（config.go:366-382）——必须保持这一点，
// 否则用户会被自己的配置文件锁死。本守卫用「旧格式 HMAC」显式构造升级场景
// 并断言：Load 返回 nil error、根回到安全默认、Integrity 被清空（下次 Save
// 重新生成）。
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/xmdemt/xdev/internal/util"
)

// writeTamperedConfig 读出 root/config.yaml、按 mutate 修改内存对象后原样
// 写回（**保留旧 Integrity**），模拟「文件被外部编辑/篡改」。
func writeTamperedConfig(t *testing.T, root string, mutate func(*Config)) {
	t.Helper()
	path := filepath.Join(root, "config.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	if cfg.Integrity == "" {
		t.Fatal("前置条件失败：落盘配置必须带 integrity")
	}
	mutate(&cfg)
	out, err := yaml.Marshal(&cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

// TestIntegrityHMAC_CoversXMDevenvDir 是本项的红→绿守卫：
// 篡改 xmdevenv_dir 必须触发 H-12 的验签失败路径（此处体现为「不被采信」）。
func TestIntegrityHMAC_CoversXMDevenvDir(t *testing.T) {
	root := t.TempDir()
	evil := filepath.Join(t.TempDir(), "evil-root")

	cfg := defaults(root)
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	writeTamperedConfig(t, root, func(c *Config) { c.Core.XMDevDir = evil })

	loaded, err := Load(root)
	if err != nil {
		t.Fatalf("Load 不得因验签失败而硬失败（会锁死用户）: %v", err)
	}
	if loaded.Core.XMDevDir == evil {
		t.Errorf("A-15：被篡改的 xmdevenv_dir=%q 被采信了（HMAC 未覆盖该字段）", evil)
	}
	if loaded.Core.XMDevDir != root {
		t.Errorf("验签失败后根应回到安全默认 %q, got %q", root, loaded.Core.XMDevDir)
	}
	if loaded.Integrity != "" {
		t.Errorf("验签失败后 Integrity 必须被清空（下次 Save 重新生成）, got %q", loaded.Integrity)
	}
	// 重置根之后，派生子目录必须跟着新根（不得留下 A-14 的混合状态）。
	for field, ptr := range derivedDirFields(&loaded.Core) {
		want := filepath.Join(root, derivedDirNames[field])
		if *ptr != want {
			t.Errorf("A-15 重置路径留下混合状态：%s = %q, want %q", field, *ptr, want)
		}
	}
}

// TestIntegrityHMAC_ResetEmitsWarning：重置必须**告警**（静默重置会让用户
// 莫名其妙地发现配置变了）。
func TestIntegrityHMAC_ResetEmitsWarning(t *testing.T) {
	root := t.TempDir()
	evil := filepath.Join(t.TempDir(), "evil-root")

	cfg := defaults(root)
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	writeTamperedConfig(t, root, func(c *Config) { c.Core.XMDevDir = evil })

	captured, restore := captureStderr(t)
	if _, err := Load(root); err != nil {
		t.Fatalf("Load: %v", err)
	}
	restore()

	if !strings.Contains(captured(), "integrity check FAILED") {
		t.Errorf("验签失败必须向 stderr 告警，实际输出:\n%s", captured())
	}
}

// TestIntegrityHMAC_TamperedDerivedDirStillReset 既有覆盖不得回归：
// install_dir 被篡改时，根**保持**（未被篡改），派生子目录全部回到根下。
func TestIntegrityHMAC_TamperedDerivedDirStillReset(t *testing.T) {
	root := t.TempDir()
	evil := filepath.Join(t.TempDir(), "evil-install")

	cfg := defaults(root)
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	writeTamperedConfig(t, root, func(c *Config) { c.Core.InstallDir = evil })

	loaded, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Core.XMDevDir != root {
		t.Errorf("XMDevDir = %q, want %q（根未被篡改，不得被重置）", loaded.Core.XMDevDir, root)
	}
	if loaded.Core.InstallDir == evil {
		t.Errorf("被篡改的 install_dir 被采信: %q", evil)
	}
	if loaded.Core.InstallDir != filepath.Join(root, "install") {
		t.Errorf("InstallDir = %q, want %q", loaded.Core.InstallDir, filepath.Join(root, "install"))
	}
	for field, ptr := range derivedDirFields(&loaded.Core) {
		want := filepath.Join(root, derivedDirNames[field])
		if *ptr != want {
			t.Errorf("重置后 %s = %q, want %q（不得留下混合状态）", field, *ptr, want)
		}
	}
}

// TestIntegrityHMAC_SaveLoadRoundTripStillValid：正常保存/加载往返必须**不**
// 触发重置（防「一律重置」的假修复），自定义派生目录也不得被抹掉。
func TestIntegrityHMAC_SaveLoadRoundTripStillValid(t *testing.T) {
	root := t.TempDir()
	customInstall := filepath.Join(root, "custom-install")

	cfg := defaults(root)
	cfg.Core.InstallDir = customInstall
	cfg.Mirror["python"] = "https://example.com/python"
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Core.XMDevDir != root {
		t.Errorf("XMDevDir = %q, want %q", loaded.Core.XMDevDir, root)
	}
	if loaded.Core.InstallDir != customInstall {
		t.Errorf("合法的自定义 install_dir 被误重置: got %q, want %q", loaded.Core.InstallDir, customInstall)
	}
	if loaded.Integrity == "" {
		t.Error("往返后 Integrity 必须仍然存在（验签通过）")
	}
	if loaded.Mirror["python"] != "https://example.com/python" {
		t.Errorf("mirror 丢失: %v", loaded.Mirror)
	}
}

// legacyIntegrityHMAC 复算**升级前**格式的 HMAC（不含 xmdevenv_dir）。
// 这是 A-15 升级路径的关键：旧文件必须被判为「验签失败但不锁死」。
func legacyIntegrityHMAC(c *Config) string {
	type legacyIntegrityData struct {
		InstallDir  string            `json:"install_dir"`
		ShimDir     string            `json:"shim_dir"`
		ManifestDir string            `json:"manifest_dir"`
		DownloadDir string            `json:"download_dir"`
		Mirror      map[string]string `json:"mirror"`
		PathMode    string            `json:"path_mode"`
	}
	snapshot := legacyIntegrityData{
		InstallDir:  c.Core.InstallDir,
		ShimDir:     c.Core.ShimDir,
		ManifestDir: c.Core.ManifestDir,
		DownloadDir: c.Core.DownloadDir,
		Mirror:      make(map[string]string),
		PathMode:    c.Path.Mode,
	}
	for k, v := range c.Mirror {
		snapshot.Mirror[k] = v
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return ""
	}
	return util.ComputeHMAC(data)
}

// TestIntegrityHMAC_LegacyConfigUpgradeIsNotFatal：升级路径契约 —— 升级前
// 写下的配置文件（HMAC 不含 xmdevenv_dir）在新版本里必须
//   - Load 成功（err == nil，不锁死）；
//   - 根回到**安全默认**（即配置实际所在的目录），而不是被拒绝启动；
//   - Integrity 清空，下一次 Save 自动按新格式重新生成。
//
// 代价（如实记录）：被自定义过的派生目录会在这一次重置中回到 <root>/<name>，
// 用户按提示重跑 `xdev config set` 即可。这是「不锁死」与「防重定向」之间的
// 取舍：若为了平滑升级而接受旧格式 HMAC，则**恰好**放过「只篡改
// xmdevenv_dir」这一攻击（旧 HMAC 本来就不覆盖它），A-15 的修复将完全失效。
func TestIntegrityHMAC_LegacyConfigUpgradeIsNotFatal(t *testing.T) {
	root := t.TempDir()
	customInstall := filepath.Join(root, "custom-install")

	// 构造「旧版本写下」的配置：Integrity = 旧格式 HMAC。
	legacy := defaults(root)
	legacy.Core.InstallDir = customInstall
	legacy.Integrity = legacyIntegrityHMAC(legacy)
	data, err := yaml.Marshal(legacy)
	if err != nil {
		t.Fatalf("marshal legacy config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "config.yaml"), data, 0o644); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}

	captured, restore := captureStderr(t)
	loaded, err := Load(root)
	restore()
	if err != nil {
		t.Fatalf("升级后的旧配置必须仍能加载（不得锁死用户）: %v", err)
	}
	if !strings.Contains(captured(), "integrity check FAILED") {
		t.Errorf("旧格式配置升级应告警一次，实际输出:\n%s", captured())
	}
	if loaded.Core.XMDevDir != root {
		t.Errorf("升级后根应回到安全默认 %q, got %q", root, loaded.Core.XMDevDir)
	}
	if loaded.Integrity != "" {
		t.Errorf("升级路径必须清空 Integrity 以便重新生成, got %q", loaded.Integrity)
	}
	for field, ptr := range derivedDirFields(&loaded.Core) {
		want := filepath.Join(root, derivedDirNames[field])
		if *ptr != want {
			t.Errorf("升级后 %s = %q, want %q", field, *ptr, want)
		}
	}

	// 用户重新保存一次后，后续 Load 必须干净通过（不再告警、不再重置）。
	if err := loaded.Save(); err != nil {
		t.Fatalf("Save after upgrade: %v", err)
	}
	captured2, restore2 := captureStderr(t)
	reloaded, err := Load(root)
	restore2()
	if err != nil {
		t.Fatalf("Load after re-save: %v", err)
	}
	if strings.Contains(captured2(), "integrity check FAILED") {
		t.Errorf("重新保存后不得再报验签失败:\n%s", captured2())
	}
	if reloaded.Integrity == "" {
		t.Error("重新保存后应带新格式 Integrity")
	}
}

// captureStderr 把 os.Stderr 换成管道，返回内容读取器与还原函数。
// 仅捕获 fmt.Fprintln(os.Stderr, ...)（log 包在 init 时已捕获原始 os.Stderr）。
func captureStderr(t *testing.T) (func() string, func()) {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stderr = w

	done := make(chan string, 1)
	go func() {
		var sb strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				sb.Write(buf[:n])
			}
			if err != nil {
				break
			}
		}
		done <- sb.String()
	}()

	restore := func() {
		os.Stderr = old
		_ = w.Close()
		_ = r.Close()
	}
	return func() string { return <-done }, restore
}
