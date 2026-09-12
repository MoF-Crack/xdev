package cli

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/config"
)

// ---------------------------------------------------------------------------
// 票 40（P2-16 / E-12）：`config proxy` 必须复用 cfg.SetProxy
// ---------------------------------------------------------------------------
//
// 旧实现逐字段改 cfg.Proxy 后直接 cfg.Save()，绕过了 config.SetProxy 里的
// util.ValidateProxyURL —— 非法 scheme（file://、javascript: 等）会被原样
// 写进 config.yaml，运行期 ConfigureHTTPClient 解析失败后静默退化为直连。
//
// HOST SAFETY：这些用例只用 &config.Config{} 直接构造配置并把 XMDevDir 指向
// t.TempDir()，从不调用 Cfg()/构造 cobra 命令——进程级 OnInitialize 会把
// 配置解析回真实 home（E:\XDev），是本会话已发生过的事故来源。

// newProxyGuardCfg 返回一个落盘目录位于 t.TempDir() 的配置。
func newProxyGuardCfg(t *testing.T) (*config.Config, string) {
	t.Helper()
	home := t.TempDir()
	return &config.Config{Core: config.CoreConfig{XMDevDir: home}}, filepath.Join(home, "config.yaml")
}

// TestConfigProxy_RejectsInvalidScheme 非法 scheme / 无 host 的代理 URL 必须
// 被拒：既不改内存配置，也不落盘。
func TestConfigProxy_RejectsInvalidScheme(t *testing.T) {
	rejected := []string{
		"file:///etc/passwd",
		"javascript:alert(1)",
		"ftp://proxy.local:21",
		"not-a-url",
		"http://", // 无 host
	}
	for _, bad := range rejected {
		cfg, cfgPath := newProxyGuardCfg(t)
		state := proxyFlagState{HTTP: bad, HTTPSet: true}

		err := state.apply(cfg)
		if err == nil {
			t.Errorf("apply(--http %q) = nil, want rejection", bad)
		}
		if cfg.Proxy.HTTP != "" {
			t.Errorf("apply(--http %q) 写入了被拒值 %q", bad, cfg.Proxy.HTTP)
		}
		if _, statErr := os.Stat(cfgPath); statErr == nil {
			t.Errorf("apply(--http %q) 在校验失败时仍然落盘了 %s", bad, cfgPath)
		}
	}
}

// TestConfigProxy_AcceptsValidSchemeAndKeepsPartialUpdate 合法 URL 被接受，
// 且未显式给出的字段保持现值（部分更新语义与旧实现一致）。
func TestConfigProxy_AcceptsValidSchemeAndKeepsPartialUpdate(t *testing.T) {
	cfg, cfgPath := newProxyGuardCfg(t)
	cfg.Proxy = config.ProxyConfig{
		HTTP:    "http://old-http:8080",
		HTTPS:   "http://old-https:8443",
		NoProxy: "localhost,127.0.0.1",
		Enabled: true,
	}

	state := proxyFlagState{HTTP: "socks5://user:pass@127.0.0.1:1080", HTTPSet: true}
	if err := state.apply(cfg); err != nil {
		t.Fatalf("apply(socks5) = %v, want nil", err)
	}
	if cfg.Proxy.HTTP != "socks5://user:pass@127.0.0.1:1080" {
		t.Errorf("HTTP = %q, want the new socks5 URL", cfg.Proxy.HTTP)
	}
	if cfg.Proxy.HTTPS != "http://old-https:8443" {
		t.Errorf("HTTPS = %q, want unchanged (partial update)", cfg.Proxy.HTTPS)
	}
	if cfg.Proxy.NoProxy != "localhost,127.0.0.1" || !cfg.Proxy.Enabled {
		t.Errorf("NoProxy/Enabled = %q/%v, want unchanged", cfg.Proxy.NoProxy, cfg.Proxy.Enabled)
	}
	if _, err := os.Stat(cfgPath); err != nil {
		t.Errorf("config.yaml 未落盘: %v", err)
	}

	// --disable 与 --enable 同时出现时 disable 优先（旧行为）。
	cfg2, _ := newProxyGuardCfg(t)
	both := proxyFlagState{Enable: true, EnableSet: true, DisableSet: true}
	if err := both.apply(cfg2); err != nil {
		t.Fatalf("apply(enable+disable) = %v, want nil", err)
	}
	if cfg2.Proxy.Enabled {
		t.Error("Enabled = true, want false（--disable 优先）")
	}
}

// TestConfigProxy_TouchedSemantics 无写 flag 时只显示（不落盘）。
func TestConfigProxy_TouchedSemantics(t *testing.T) {
	if (proxyFlagState{}).touched() {
		t.Error("空 flag 集合 touched() = true, want false（只读展示路径）")
	}
	for _, s := range []proxyFlagState{
		{HTTPSet: true}, {HTTPSSet: true}, {NoProxySet: true}, {EnableSet: true}, {DisableSet: true},
	} {
		if !s.touched() {
			t.Errorf("%+v touched() = false, want true", s)
		}
	}
}

// TestConfigProxy_ValidationFailureIsLoggedAndRedacted 校验失败必须留下
// 日志（不静默），且日志里不得出现代理凭据。
func TestConfigProxy_ValidationFailureIsLoggedAndRedacted(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	cfg, _ := newProxyGuardCfg(t)
	// 畸形到 url.Parse 失败、且带凭据的 URL：错误文本会回显原始 URL。
	state := proxyFlagState{HTTP: "http://user:s3cr3t@[::1", HTTPSet: true}
	if err := state.apply(cfg); err == nil {
		t.Fatal("apply(malformed credential URL) = nil, want rejection")
	}

	out := buf.String()
	if !strings.Contains(out, "proxy update rejected") {
		t.Errorf("校验失败没有留下日志（静默失败）: %q", out)
	}
	if strings.Contains(out, "s3cr3t") {
		t.Errorf("日志泄露了代理凭据: %q", out)
	}
	if !strings.Contains(out, "<redacted>@") {
		t.Errorf("日志未标记脱敏位置: %q", out)
	}
}
