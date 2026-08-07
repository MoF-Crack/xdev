package gui

import (
	"fmt"
	"strings"
	"testing"
)

// TestProxy_DeadProxyActionableMessage verifies that TestProxy returns an
// actionable Chinese hint (Clash Verge mixed-port 7897) instead of a raw
// dial error when the configured proxy port is closed.
func TestProxy_DeadProxyActionableMessage(t *testing.T) {
	app := newTestApp()
	// 127.0.0.1:1 is a closed port — the proxy dial fails immediately.
	res := app.TestProxy("http://127.0.0.1:1", "https://api.github.com")
	if res.Success {
		t.Fatal("Success = true, want false for dead proxy")
	}
	for _, want := range []string{"不可达", "7897", "探测本地代理"} {
		if !strings.Contains(res.Message, want) {
			t.Errorf("message %q does not contain %q", res.Message, want)
		}
	}
	if !strings.Contains(res.Message, "127.0.0.1:1") {
		t.Errorf("message %q should mention the probed proxy address", res.Message)
	}
}

// TestProxy_DeadProxyMessageRawWhenNotConnRefused verifies that non-proxy
// errors keep the raw message (no misleading hint).
func TestProxy_DeadProxyMessageRawWhenNotConnRefused(t *testing.T) {
	app := newTestApp()
	res := app.TestProxy("http://invalid proxy url with spaces", "https://api.github.com")
	if res.Success {
		t.Fatal("Success = true, want false")
	}
	if strings.Contains(res.Message, "7897") {
		t.Errorf("message %q should not contain the 7897 hint for a parse error", res.Message)
	}
}

// TestProbeLocalProxy_ClosedPort verifies the probe reports a closed port
// as not OK without touching the network beyond an immediate local dial.
func TestProbeLocalProxy_ClosedPort(t *testing.T) {
	res := probeLocalProxy(1) // closed port, immediate refusal
	if res.OK {
		t.Error("OK = true, want false for closed port")
	}
	if res.URL == "" || res.Port != 1 {
		t.Errorf("unexpected probe result fields: %+v", res)
	}
	if res.Message == "" {
		t.Error("Message should be non-empty for closed port")
	}
}

// TestProbeLocalProxies_Shape verifies the candidate list shape and that
// every result carries a URL and a message (network outcome is not
// asserted — the probe is offline-friendly for closed ports).
func TestProbeLocalProxies_Shape(t *testing.T) {
	app := newTestApp()
	results := app.ProbeLocalProxies()
	if len(results) < 5 {
		t.Fatalf("len(results) = %d, want >= 5 candidates", len(results))
	}
	seen := map[int]bool{}
	for _, r := range results {
		if r.Port <= 0 || r.Port > 65535 {
			t.Errorf("invalid port %d", r.Port)
		}
		if r.URL == "" {
			t.Error("URL should be non-empty")
		}
		if r.Message == "" {
			t.Errorf("Message should be non-empty for port %d", r.Port)
		}
		if seen[r.Port] {
			t.Errorf("duplicate port %d", r.Port)
		}
		seen[r.Port] = true
	}
}

// TestIsProxyConnError covers the connection-refused / proxyconnect /
// Windows connectex family used for the actionable hint.
func TestIsProxyConnError(t *testing.T) {
	cases := []struct {
		in   error
		want bool
	}{
		{nil, false},
		{fmt.Errorf("proxyconnect tcp: dial tcp 127.0.0.1:7890: connection refused"), true},
		{fmt.Errorf("dial tcp 127.0.0.1:7890: connectex: No connection could be made because the target machine actively refused it."), true},
		{fmt.Errorf("dial tcp 127.0.0.1:7890: i/o timeout"), false},
		{fmt.Errorf("Get \"https://api.github.com\": context deadline exceeded"), false},
		{fmt.Errorf("HTTP 404"), false},
	}
	for _, c := range cases {
		if got := isProxyConnError(c.in); got != c.want {
			t.Errorf("isProxyConnError(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
