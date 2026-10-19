// integrity_schema_r10_test.go — 第十轮审计 P2-4 / P2-5 复现守卫。
//
// P2-4（可运维性缺口，**不是**安全漏洞）：H-12 的校验只在 `Integrity` 字段
// **非空**时进行（config.go:366 `if cfg.Integrity != ""`）。因此把 config.yaml
// 里的 `integrity:` 那一行删掉，后续所有敏感字段篡改都被**静默接受**——
// 运维无法区分「从未配置过 integrity」与「integrity 被删掉了」。
//
// 与 internal/util/hmac_key.go 的既有威胁模型**自洽**：那里的注释（P2-22 /
// F-14）已如实声明 HMAC 只防「误改 / 意外损坏」，不防有意的本地攻击者（密钥
// 就在本机，任何能写文件的进程都能重算）。所以本项的定性是**可运维性缺口**：
// 「缺失 = 通过」让「被删掉」这一**误改/脚本重写**场景失去唯一的暴露点。
//
// 修法（见 config.go 的 integritySchemaVersion / integrityFieldPresent）：
// 落盘时写一个显式的 schema 标记 `integrity_schema`。于是「本文件由 xdev
// 写出、本应有 integrity，但这一行不见了」可被识别 → **告警**并按既有的
// 「重置敏感字段 + 清空 Integrity」路径处理，但**不拒绝启动**。
//
// 为什么不硬失败：全新安装的配置本来就没有 integrity（首次 Save 才写入），
// 硬失败会让所有新用户无法启动。
//
// P2-5：computeIntegrityHMAC 的覆盖残留 —— `Proxy.*`、
// `Core.MirrorPreference`、`CustomTools[].SourceURL`。前者是最高优先级下载源
// （installer.go:1087），后两者分别决定下载走哪个代理、自定义工具从哪里取版本。
//
// 注意：captureStderr 返回的读取器是**一次性**的（内部 <-done），每次捕获
// 只能调用一次，故一律先落到 out := captured() 再断言。
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

// integritySchemaMarker 是当前版本落盘时使用的 schema 标记键名。
const integritySchemaMarker = "integrity_schema"

// deleteIntegrityLine 从 root/config.yaml 里**物理删掉** `integrity:` 那一行，
// 忠实复现审计的攻击/手滑场景（其余内容——含 schema 标记——原样保留）。
func deleteIntegrityLine(t *testing.T, root string) {
	t.Helper()
	path := filepath.Join(root, "config.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	kept := make([]string, 0, 64)
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "integrity:") {
			continue
		}
		kept = append(kept, line)
	}
	out := strings.Join(kept, "\n")
	if strings.Contains(out, "integrity:") {
		t.Fatal("前置条件失败：integrity 行未被完全删除")
	}
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

// writeTamperedConfigNoIntegrity 与 writeTamperedConfig 同形，但**不要求**
// 落盘配置带 integrity（本项场景正是 integrity 已被删除）。
func writeTamperedConfigNoIntegrity(t *testing.T, root string, mutate func(*Config)) {
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
	mutate(&cfg)
	out, err := yaml.Marshal(&cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

// ---------------------------------------------------------------------------
// P2-4：删掉 integrity 行必须被识别
// ---------------------------------------------------------------------------

// TestIntegrity_DeletedIntegrityFieldIsDetected 是本项的红→绿守卫：
// 文件由当前版本写出（含 schema 标记）、敏感字段被改成非默认值、`integrity:`
// 那一行被删掉 —— 必须**告警**，且**不拒绝启动**，并按既有路径重置敏感字段。
func TestIntegrity_DeletedIntegrityFieldIsDetected(t *testing.T) {
	root := t.TempDir()
	evilShim := filepath.Join(t.TempDir(), "evil-shims")

	cfg := defaults(root)
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// 前置条件：落盘文件带 schema 标记（本项修复的判据来源）。
	raw, err := os.ReadFile(filepath.Join(root, "config.yaml"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(raw), integritySchemaMarker) {
		t.Fatalf("前置条件失败：落盘配置缺少 schema 标记 %q", integritySchemaMarker)
	}

	// 攻击/手滑：删掉 integrity 行，并篡改敏感字段（shim_dir + path.mode）。
	deleteIntegrityLine(t, root)
	writeTamperedConfigNoIntegrity(t, root, func(c *Config) {
		c.Core.ShimDir = evilShim
		c.Path.Mode = "direct"
	})

	captured, restore := captureStderr(t)
	loaded, err := Load(root)
	restore()
	out := captured()
	if err != nil {
		t.Fatalf("Load 不得因 integrity 缺失而拒绝启动（会锁死新用户）: %v", err)
	}
	if !strings.Contains(out, "integrity check FAILED") {
		t.Errorf("integrity 行被删除必须告警（否则「缺失 = 通过」静默放行篡改），实际输出:\n%s", out)
	}
	if loaded.Core.ShimDir == evilShim {
		t.Errorf("P2-4：integrity 缺失时被篡改的 shim_dir=%q 被采信了", evilShim)
	}
	if loaded.Core.ShimDir != filepath.Join(root, "shims") {
		t.Errorf("ShimDir = %q, want %q（应重置为安全默认）", loaded.Core.ShimDir, filepath.Join(root, "shims"))
	}
	if loaded.Path.Mode == "direct" {
		t.Errorf("P2-4：integrity 缺失时被篡改的 path.mode=%q 被采信了", "direct")
	}
	if loaded.Integrity != "" {
		t.Errorf("重置后 Integrity 必须为空（下次 Save 重新生成）, got %q", loaded.Integrity)
	}
	// 重置根之后，派生子目录必须跟着根（不得留下 A-14 的混合状态）。
	for field, ptr := range derivedDirFields(&loaded.Core) {
		want := filepath.Join(root, derivedDirNames[field])
		if *ptr != want {
			t.Errorf("P2-4 重置路径留下混合状态：%s = %q, want %q", field, *ptr, want)
		}
	}
}

// TestIntegrity_FreshConfigDoesNotWarn 防「一律告警」的假修复：
//   - 全新安装（config.yaml 尚未存在）不得告警；
//   - 首次 Save 之后（带 integrity）不得告警；
//   - 手工写下的极简配置（无 schema 标记、无 integrity）不得告警 ——
//     这正是既有 config_test.go 的用例形态，也保护「用户手写 config」的契约。
func TestIntegrity_FreshConfigDoesNotWarn(t *testing.T) {
	t.Run("file_absent", func(t *testing.T) {
		root := t.TempDir()
		captured, restore := captureStderr(t)
		loaded, err := Load(root)
		restore()
		out := captured()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if strings.Contains(out, "integrity check FAILED") {
			t.Errorf("全新安装（无 config.yaml）不得告警:\n%s", out)
		}
		if loaded.Integrity != "" {
			t.Errorf("全新配置不应带 Integrity, got %q", loaded.Integrity)
		}
	})

	t.Run("after_first_save", func(t *testing.T) {
		root := t.TempDir()
		cfg := defaults(root)
		if err := cfg.Save(); err != nil {
			t.Fatalf("Save: %v", err)
		}
		captured, restore := captureStderr(t)
		loaded, err := Load(root)
		restore()
		out := captured()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if strings.Contains(out, "integrity check FAILED") {
			t.Errorf("首次 Save 之后的正常配置不得告警:\n%s", out)
		}
		if loaded.Integrity == "" {
			t.Error("首次 Save 之后 Integrity 必须存在")
		}
	})

	t.Run("handwritten_minimal_config", func(t *testing.T) {
		root := t.TempDir()
		// 用户手工写下的极简配置：没有 schema 标记，也没有 integrity。
		yamlText := "core:\n  xmdevenv_dir: " + root + "\n"
		if err := os.WriteFile(filepath.Join(root, "config.yaml"), []byte(yamlText), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}
		captured, restore := captureStderr(t)
		loaded, err := Load(root)
		restore()
		out := captured()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if strings.Contains(out, "integrity check FAILED") {
			t.Errorf("手工极简配置（从未有 integrity）不得告警:\n%s", out)
		}
		if loaded.Core.XMDevDir != root {
			t.Errorf("XMDevDir = %q, want %q（未被篡改，不得重置）", loaded.Core.XMDevDir, root)
		}
	})
}

// ---------------------------------------------------------------------------
// P2-5：HMAC 覆盖 MirrorPreference / Proxy / CustomTools[].SourceURL
// ---------------------------------------------------------------------------

// TestIntegrityHMAC_CoversMirrorPreference：mirror_preference 是**最高优先级
// 下载源**（installer.go:1087），篡改必须触发验签失败并重置。
func TestIntegrityHMAC_CoversMirrorPreference(t *testing.T) {
	root := t.TempDir()
	evil := "https://evil-mirror.example/xdev"

	cfg := defaults(root)
	cfg.Core.MirrorPreference = []string{"https://good-mirror.example"}
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	writeTamperedConfig(t, root, func(c *Config) {
		c.Core.MirrorPreference = []string{evil}
	})

	captured, restore := captureStderr(t)
	loaded, err := Load(root)
	restore()
	out := captured()
	if err != nil {
		t.Fatalf("Load 不得因验签失败而硬失败: %v", err)
	}
	if !strings.Contains(out, "integrity check FAILED") {
		t.Errorf("篡改 mirror_preference 必须告警，实际输出:\n%s", out)
	}
	for _, u := range loaded.Core.MirrorPreference {
		if u == evil {
			t.Errorf("P2-5：被篡改的 mirror_preference=%q 被采信了", evil)
		}
	}
	if len(loaded.Core.MirrorPreference) != 0 {
		t.Errorf("验签失败后 MirrorPreference 应被重置为空, got %v", loaded.Core.MirrorPreference)
	}
	if loaded.Integrity != "" {
		t.Errorf("验签失败后 Integrity 必须被清空, got %q", loaded.Integrity)
	}
}

// TestIntegrityHMAC_CoversProxy：Proxy 决定下载走哪个代理（installer.go:744
// 的 ProxyURL），篡改必须触发验签失败并重置。
func TestIntegrityHMAC_CoversProxy(t *testing.T) {
	root := t.TempDir()
	evil := "http://evil-proxy.example:8080"

	cfg := defaults(root)
	cfg.Proxy.HTTP = "http://good-proxy.example:7890"
	cfg.Proxy.HTTPS = "http://good-proxy.example:7890"
	cfg.Proxy.Enabled = true
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	writeTamperedConfig(t, root, func(c *Config) {
		c.Proxy.HTTP = evil
		c.Proxy.HTTPS = evil
		c.Proxy.Enabled = true
	})

	captured, restore := captureStderr(t)
	loaded, err := Load(root)
	restore()
	out := captured()
	if err != nil {
		t.Fatalf("Load 不得因验签失败而硬失败: %v", err)
	}
	if !strings.Contains(out, "integrity check FAILED") {
		t.Errorf("篡改 proxy 必须告警，实际输出:\n%s", out)
	}
	if loaded.Proxy.HTTP == evil || loaded.Proxy.HTTPS == evil {
		t.Errorf("P2-5：被篡改的 proxy=%q 被采信了 (http=%q https=%q)", evil, loaded.Proxy.HTTP, loaded.Proxy.HTTPS)
	}
	if loaded.Proxy.Enabled {
		t.Errorf("验签失败后 Proxy.Enabled 应回到安全默认 false, got true")
	}
	if loaded.Integrity != "" {
		t.Errorf("验签失败后 Integrity 必须被清空, got %q", loaded.Integrity)
	}
}

// TestIntegrityHMAC_CoversCustomToolSourceURL：自定义工具的 SourceURL 决定
// 「从哪里取版本清单」，篡改必须触发验签失败并重置。
func TestIntegrityHMAC_CoversCustomToolSourceURL(t *testing.T) {
	root := t.TempDir()
	evil := "https://evil.example/tool.zip"

	cfg := defaults(root)
	cfg.CustomTools = []CustomToolConfig{{
		Name:       "mytool",
		SourceURL:  "https://good.example/tool.zip",
		SourceType: "html",
	}}
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	writeTamperedConfig(t, root, func(c *Config) {
		if len(c.CustomTools) != 1 {
			t.Fatalf("前置条件失败：CustomTools 应含 1 条, got %d", len(c.CustomTools))
		}
		c.CustomTools[0].SourceURL = evil
	})

	captured, restore := captureStderr(t)
	loaded, err := Load(root)
	restore()
	out := captured()
	if err != nil {
		t.Fatalf("Load 不得因验签失败而硬失败: %v", err)
	}
	if !strings.Contains(out, "integrity check FAILED") {
		t.Errorf("篡改 custom_tools[].source_url 必须告警，实际输出:\n%s", out)
	}
	for _, ct := range loaded.CustomTools {
		if ct.SourceURL == evil {
			t.Errorf("P2-5：被篡改的 custom_tools source_url=%q 被采信了", evil)
		}
	}
	if len(loaded.CustomTools) != 0 {
		t.Errorf("验签失败后 CustomTools 应被重置为空, got %v", loaded.CustomTools)
	}
	if loaded.Integrity != "" {
		t.Errorf("验签失败后 Integrity 必须被清空, got %q", loaded.Integrity)
	}
}

// TestIntegrityHMAC_NilVsEmptyRoundTripIsStable 钉死一个**极易回归**的陷阱：
// yaml 把 nil 与空切片都写成 `key: []`，回读时得到非 nil 空切片；而
// encoding/json 对二者的编码不同（`null` vs `[]`）。若 computeIntegrityHMAC
// 不做归一，「Save 写 nil → Load 读空切片」就会在**没有任何篡改**的情况下
// 验签失败，把正常往返误判成篡改（= 每次启动都重置用户配置）。
//
// 本用例覆盖两种形态：nil（全新 defaults）与空切片（Load 回来的形态）。
func TestIntegrityHMAC_NilVsEmptyRoundTripIsStable(t *testing.T) {
	t.Run("nil_mirror_preference", func(t *testing.T) {
		root := t.TempDir()
		cfg := defaults(root)
		if cfg.Core.MirrorPreference != nil {
			t.Fatalf("前置条件：defaults 的 MirrorPreference 应为 nil, got %v", cfg.Core.MirrorPreference)
		}
		if err := cfg.Save(); err != nil {
			t.Fatalf("Save: %v", err)
		}
		captured, restore := captureStderr(t)
		loaded, err := Load(root)
		restore()
		out := captured()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if strings.Contains(out, "integrity check FAILED") {
			t.Errorf("未篡改的往返不得验签失败（nil/空切片未归一）:\n%s", out)
		}
		if loaded.Integrity == "" {
			t.Error("未篡改的往返应保留 Integrity")
		}
	})

	t.Run("empty_slice_mirror_preference", func(t *testing.T) {
		root := t.TempDir()
		cfg := defaults(root)
		cfg.Core.MirrorPreference = []string{}
		cfg.CustomTools = []CustomToolConfig{}
		if err := cfg.Save(); err != nil {
			t.Fatalf("Save: %v", err)
		}
		captured, restore := captureStderr(t)
		loaded, err := Load(root)
		restore()
		out := captured()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if strings.Contains(out, "integrity check FAILED") {
			t.Errorf("空切片配置的正常往返不得验签失败:\n%s", out)
		}
		if loaded.Integrity == "" {
			t.Error("空切片配置的正常往返应保留 Integrity")
		}
	})

	t.Run("populated_round_trip", func(t *testing.T) {
		root := t.TempDir()
		cfg := defaults(root)
		cfg.Core.MirrorPreference = []string{"https://m1.example", "https://m2.example"}
		cfg.Proxy.HTTP = "http://p.example:7890"
		cfg.Proxy.Enabled = true
		cfg.CustomTools = []CustomToolConfig{
			{Name: "a", SourceURL: "https://a.example/t.zip", SourceType: "html", Shim: "a", EnvVar: "A_HOME"},
			{Name: "b", SourceURL: "https://b.example/t.zip", SourceType: "npm"},
		}
		if err := cfg.Save(); err != nil {
			t.Fatalf("Save: %v", err)
		}
		captured, restore := captureStderr(t)
		loaded, err := Load(root)
		restore()
		out := captured()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if strings.Contains(out, "integrity check FAILED") {
			t.Errorf("有值的正常往返不得验签失败:\n%s", out)
		}
		// 值必须原样保留（不得被误重置）。
		if len(loaded.Core.MirrorPreference) != 2 {
			t.Errorf("MirrorPreference 丢失: %v", loaded.Core.MirrorPreference)
		}
		if !loaded.Proxy.Enabled || loaded.Proxy.HTTP != "http://p.example:7890" {
			t.Errorf("Proxy 丢失: %+v", loaded.Proxy)
		}
		if len(loaded.CustomTools) != 2 {
			t.Errorf("CustomTools 丢失: %v", loaded.CustomTools)
		}
		// 顺序敏感：mirror_preference 的顺序即候选优先级。
		if len(loaded.Core.MirrorPreference) == 2 &&
			(loaded.Core.MirrorPreference[0] != "https://m1.example" || loaded.Core.MirrorPreference[1] != "https://m2.example") {
			t.Errorf("MirrorPreference 顺序被改变（优先级语义丢失）: %v", loaded.Core.MirrorPreference)
		}
	})
}

// round9IntegrityHMAC 复算**第九轮（A-15）格式**的 HMAC：含 xmdevenv_dir，
// 但不含本轮新增的 mirror_preference / proxy / custom_tools。
//
// 这是 P2-5 升级路径的关键：把这三项纳入覆盖会**改变 HMAC 值**，上一版本
// 写下的配置文件在本版本必然验签失败一次。契约必须与 A-15 一致 ——
// 告警 + 重置 + 清空 Integrity，**不拒绝启动**。
func round9IntegrityHMAC(c *Config) string {
	type round9IntegrityData struct {
		XMDevDir    string            `json:"xmdevenv_dir"`
		InstallDir  string            `json:"install_dir"`
		ShimDir     string            `json:"shim_dir"`
		ManifestDir string            `json:"manifest_dir"`
		DownloadDir string            `json:"download_dir"`
		Mirror      map[string]string `json:"mirror"`
		PathMode    string            `json:"path_mode"`
	}
	snapshot := round9IntegrityData{
		XMDevDir:    c.Core.XMDevDir,
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

// TestIntegrityHMAC_Round9FormatUpgradeIsNotFatal：P2-5 的升级路径契约。
// 上一版本（A-15）写下的配置文件必须仍能加载，只是告警一次并重置。
func TestIntegrityHMAC_Round9FormatUpgradeIsNotFatal(t *testing.T) {
	root := t.TempDir()
	customInstall := filepath.Join(root, "custom-install")

	prev := defaults(root)
	prev.Core.InstallDir = customInstall
	prev.Proxy.HTTP = "http://prev-proxy.example:7890"
	prev.Proxy.Enabled = true
	prev.Core.MirrorPreference = []string{"https://prev-mirror.example"}
	prev.CustomTools = []CustomToolConfig{{
		Name: "prevtool", SourceURL: "https://prev.example/t.zip", SourceType: "html",
	}}
	prev.Integrity = round9IntegrityHMAC(prev)
	data, err := yaml.Marshal(prev)
	if err != nil {
		t.Fatalf("marshal legacy config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "config.yaml"), data, 0o644); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}

	captured, restore := captureStderr(t)
	loaded, err := Load(root)
	restore()
	out := captured()
	if err != nil {
		t.Fatalf("上一版本格式的配置必须仍能加载（不得锁死用户）: %v", err)
	}
	if !strings.Contains(out, "integrity check FAILED") {
		t.Errorf("旧格式配置升级应告警一次，实际输出:\n%s", out)
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
	out2 := captured2()
	if err != nil {
		t.Fatalf("Load after re-save: %v", err)
	}
	if strings.Contains(out2, "integrity check FAILED") {
		t.Errorf("重新保存后不得再报验签失败:\n%s", out2)
	}
	if reloaded.Integrity == "" {
		t.Error("重新保存后应带新格式 Integrity")
	}
}
