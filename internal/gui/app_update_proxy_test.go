package gui

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/remote"
	"github.com/xmdemt/xdev/internal/update"
)

// Actual CONNECT requests prove the GUI forwards an operation's network
// snapshot. The gateway replies immediately; an origin can never be dialed.
func guiUpdateProxyGateway(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	calls := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect || r.Host != "api.github.com:443" || r.Header.Get("Authorization") != "" {
			t.Errorf("unexpected CONNECT: method=%s host=%s bearer=%v", r.Method, r.Host, r.Header.Get("Authorization") != "")
		}
		calls.Add(1)
		http.Error(w, "fixture proxy reached", http.StatusBadGateway)
	}))
	t.Cleanup(server.Close)
	return server, calls
}

func guiUpdateProxyTransport(t *testing.T, gateways ...*httptest.Server) {
	t.Helper()
	allowed := make(map[string]bool)
	for _, gateway := range gateways {
		u, _ := url.Parse(gateway.URL)
		allowed[u.Host] = true
	}
	original := http.DefaultTransport
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		if !allowed[address] {
			return nil, fmt.Errorf("direct update connection forbidden")
		}
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}}
	http.DefaultTransport = transport
	t.Cleanup(func() { transport.CloseIdleConnections(); http.DefaultTransport = original })
}

func probeGUIUpdateProxy(t *testing.T, ctx context.Context) {
	t.Helper()
	_, err := update.Check(ctx, "1.0.0-beta.1")
	if err == nil || !strings.Contains(err.Error(), "Bad Gateway") {
		t.Fatalf("GUI update context did not use configured proxy: %v", err)
	}
}

func TestGUIUpdateProxyChangesApplyToNextOperation(t *testing.T) {
	first, firstCalls := guiUpdateProxyGateway(t)
	second, secondCalls := guiUpdateProxyGateway(t)
	other, otherCalls := guiUpdateProxyGateway(t)
	guiUpdateProxyTransport(t, first, second, other)
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Proxy = config.ProxyConfig{Enabled: true, HTTPS: first.URL}
	app := &App{cfg: cfg}
	t.Cleanup(func() { remote.ConfigureHTTPClient("") })
	check := func(ctx context.Context, _ string) (*update.CheckResult, error) {
		probeGUIUpdateProxy(t, ctx)
		if err := app.SetProxyConfig("", second.URL, "", true); err != nil {
			t.Fatal(err)
		}
		return guiUpdateFixture(), nil
	}
	result := app.applyUpdateWith(check, func() string { return "owned-temporary-target" }, func(ctx context.Context, _ *update.CheckResult, _ string, _ func(update.Progress)) error {
		probeGUIUpdateProxy(t, ctx)
		return nil
	})
	if !result.Applied || result.Error != "" || firstCalls.Load() != 2 || secondCalls.Load() != 0 {
		t.Fatalf("refetch and apply split network snapshot: result=%+v first=%d second=%d", result, firstCalls.Load(), secondCalls.Load())
	}
	app.checkForUpdateWith(func(ctx context.Context, _ string) (*update.CheckResult, error) {
		probeGUIUpdateProxy(t, ctx)
		return guiUpdateFixture(), nil
	})
	if secondCalls.Load() != 1 {
		t.Fatal("subsequent manual check did not adopt saved proxy")
	}
	otherApp := &App{cfg: &config.Config{Proxy: config.ProxyConfig{Enabled: true, HTTPS: other.URL}}}
	otherApp.checkForUpdateWith(func(ctx context.Context, _ string) (*update.CheckResult, error) {
		probeGUIUpdateProxy(t, ctx)
		return guiUpdateFixture(), nil
	})
	if otherCalls.Load() != 1 || secondCalls.Load() != 1 {
		t.Fatal("proxy state crossed Apps")
	}
}

func TestGUIStartupProxyUsesPolicyAndApplySnapshot(t *testing.T) {
	for _, mode := range []string{"auto", "prompt"} {
		t.Run(mode, func(t *testing.T) {
			first, firstCalls := guiUpdateProxyGateway(t)
			second, secondCalls := guiUpdateProxyGateway(t)
			guiUpdateProxyTransport(t, first, second)
			app, desktop, policy := startupUpdateFixture(t, mode)
			app.cfg.Proxy = config.ProxyConfig{Enabled: true, HTTPS: first.URL}
			policy.Check = func(ctx context.Context, _ string) (*update.CheckResult, error) {
				probeGUIUpdateProxy(t, ctx)
				app.cfgMu.Lock()
				app.cfg.Proxy.HTTPS = second.URL
				app.cfgMu.Unlock()
				return guiUpdateFixture(), nil
			}
			app.runStartupSelfCheckWith(policy, func() string { return "owned-temporary-target" }, func(ctx context.Context, _ *update.CheckResult, _ string, _ func(update.Progress)) error {
				probeGUIUpdateProxy(t, ctx)
				return nil
			})
			wantCalls := int32(1)
			if mode == "auto" {
				wantCalls++
			}
			if firstCalls.Load() != wantCalls || secondCalls.Load() != 0 || len(desktop.snapshot()) != 1 {
				t.Fatalf("startup %s proxy snapshot: first=%d second=%d events=%d", mode, firstCalls.Load(), secondCalls.Load(), len(desktop.snapshot()))
			}
		})
	}
}

func TestGUIUpdateProxySnapshotConcurrentConfigWrites(t *testing.T) {
	first, _ := guiUpdateProxyGateway(t)
	second, _ := guiUpdateProxyGateway(t)
	guiUpdateProxyTransport(t, first, second)
	app := &App{cfg: &config.Config{Proxy: config.ProxyConfig{Enabled: true, HTTPS: first.URL}}}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 100 {
			app.cfgMu.Lock()
			app.cfg.Proxy.HTTPS = second.URL
			app.cfgMu.Unlock()
			app.cfgMu.Lock()
			app.cfg.Proxy.HTTPS = first.URL
			app.cfgMu.Unlock()
		}
	}()
	for range 20 {
		ctx, cleanup := app.selfUpdateContext(context.Background())
		probeGUIUpdateProxy(t, ctx)
		cleanup()
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("config writer did not complete")
	}
}
