package cli

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

	"github.com/xmdemt/xdev/internal/config"
)

func TestCLISelfUpdateAndStartupCheckUseApplicationProxy(t *testing.T) {
	var calls atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect || r.Host != "api.github.com:443" || r.Header.Get("Authorization") != "" {
			t.Errorf("unexpected CLI proxy request: %s %s bearer=%v", r.Method, r.Host, r.Header.Get("Authorization") != "")
		}
		calls.Add(1)
		http.Error(w, "controlled proxy reached", http.StatusBadGateway)
	}))
	defer proxy.Close()
	u, _ := url.Parse(proxy.URL)
	base := http.DefaultTransport
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != u.Host {
			return nil, fmt.Errorf("direct update connection forbidden")
		}
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}}
	http.DefaultTransport = transport
	t.Cleanup(func() { transport.CloseIdleConnections(); http.DefaultTransport = base })
	old := globalCtx
	t.Cleanup(func() { globalCtx = old })
	globalCtx = runCtx{initialized: true, cfg: &config.Config{Proxy: config.ProxyConfig{Enabled: true, HTTP: proxy.URL}}}
	if err := runSelfUpdate(); err == nil || !strings.Contains(err.Error(), "Bad Gateway") || calls.Load() != 1 {
		t.Fatalf("manual CLI update did not use application proxy: %v calls=%d", err, calls.Load())
	}
	for _, mode := range []string{"auto", "prompt", "notify"} {
		globalCtx.cfg.Core = config.CoreConfig{SelfUpdate: true, SelfUpdateMode: mode, CacheDir: t.TempDir()}
		before := calls.Load()
		if err := runSelfCheck(false); err != nil || calls.Load() != before+1 {
			t.Fatalf("startup %s did not pass full config proxy to Policy.Check: %v calls=%d", mode, err, calls.Load())
		}
		if err := runSelfCheck(false); err != nil || calls.Load() != before+1 {
			t.Fatal("network injection changed policy's 24h failure throttle")
		}
	}
}
