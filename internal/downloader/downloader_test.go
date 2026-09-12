package downloader

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xmdemt/xdev/internal/util"
)

func TestDownloadAggregatesSanitizedFallbackFailures(t *testing.T) {
	server := httptest.NewTLSServer(http.NotFoundHandler())
	defer server.Close()
	err := Download(context.Background(), Options{
		URL:          server.URL + "/primary.zip?token=secret",
		FallbackURLs: []string{server.URL + "/fallback.zip?key=secret"},
		Dest:         filepath.Join(t.TempDir(), "archive.zip"),
		Client:       server.Client(),
		MaxRetries:   1,
	})
	if err == nil {
		t.Fatal("expected all candidates to fail")
	}
	message := err.Error()
	for _, want := range []string{"/primary.zip", "/fallback.zip", "HTTP 404"} {
		if !strings.Contains(message, want) {
			t.Errorf("error %q does not contain %q", message, want)
		}
	}
	if strings.Contains(message, "secret") {
		t.Fatalf("error leaked URL credentials: %s", message)
	}
}

// TestDownloadOnStatusNotifiesRetriesAndFallbacks verifies that the
// OnStatus callback is invoked for retry attempts and fallback switches.
// (404s are non-retryable by design, so only attempt #1 fires before the
// download moves to the fallback URL.)
func TestDownloadOnStatusNotifiesRetriesAndFallbacks(t *testing.T) {
	server := httptest.NewTLSServer(http.NotFoundHandler())
	defer server.Close()

	var statuses []string
	err := Download(context.Background(), Options{
		URL:          server.URL + "/primary.zip",
		FallbackURLs: []string{server.URL + "/fallback.zip"},
		Dest:         filepath.Join(t.TempDir(), "archive.zip"),
		Client:       server.Client(),
		MaxRetries:   2,
		OnStatus: func(message string) {
			statuses = append(statuses, message)
		},
	})
	if err == nil {
		t.Fatal("expected all candidates to fail")
	}
	joined := strings.Join(statuses, "\n")
	for _, want := range []string{"Attempt 1/2", "Trying fallback URL #1"} {
		if !strings.Contains(joined, want) {
			t.Errorf("statuses missing %q; got:\n%s", want, joined)
		}
	}
	// Status messages should not leak query secrets.
	if strings.Contains(joined, "secret") {
		t.Errorf("status leaked credentials: %s", joined)
	}
}

func TestShortHost(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://github.com/xmdemt/xdev/releases/download/v1/archive.zip", "github.com"},
		{"https://mirrors.tuna.tsinghua.edu.cn/go/archive.zip", "mirrors.tuna.tsinghua.edu.cn"},
		{"not a url", "not a url"},
		{"", ""},
	}
	for _, c := range cases {
		if got := shortHost(c.in); got != c.want {
			t.Errorf("shortHost(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestIsProxyError(t *testing.T) {
	cases := []struct {
		in   error
		want bool
	}{
		{nil, false},
		{fmt.Errorf("proxyconnect tcp: dial tcp 127.0.0.1:7890: connection refused"), true},
		{fmt.Errorf("proxyconnect tcp: dial tcp 127.0.0.1:7890: connectex: No connection could be made because the target machine actively refused it."), true},
		{fmt.Errorf("dial tcp 127.0.0.1:7890: connection refused"), true},
		{fmt.Errorf("dial tcp 127.0.0.1:7890: connectex: No connection could be made because the target machine actively refused it."), true},
		{fmt.Errorf("dial tcp 127.0.0.1:7890: i/o timeout"), false},
		{fmt.Errorf("HTTP 404 for https://example.test/archive.zip"), false},
		{fmt.Errorf("sha256 mismatch: expected abc, got def"), false},
		{fmt.Errorf("context deadline exceeded"), false},
	}
	for _, c := range cases {
		if got := isProxyError(c.in); got != c.want {
			t.Errorf("isProxyError(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// deadProxyClient returns an http.Client whose transport routes through a
// proxy on a closed port (127.0.0.1:1), with TLS verification disabled so
// it can talk to an httptest TLS server once the proxy is bypassed.
func deadProxyClient(t *testing.T, proxyURL string) *http.Client {
	t.Helper()
	pu, err := url.Parse(proxyURL)
	if err != nil {
		t.Fatalf("parse proxy URL: %v", err)
	}
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			Proxy:           http.ProxyURL(pu),
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // test cert only
		},
	}
}

// TestDownloadProxyFallbackToDirect verifies that a dead proxy fails fast
// (no retry burn) and the download succeeds via an automatic direct
// (no-proxy) retry.
func TestDownloadProxyFallbackToDirect(t *testing.T) {
	const payload = "proxy fallback test payload"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(payload))
	}))
	defer server.Close()

	var statuses []string
	dest := filepath.Join(t.TempDir(), "archive.zip")
	err := Download(context.Background(), Options{
		URL:        server.URL + "/archive.zip",
		Dest:       dest,
		Client:     deadProxyClient(t, "http://127.0.0.1:1"), // closed port
		MaxRetries: 3,
		OnStatus: func(message string) {
			statuses = append(statuses, message)
		},
	})
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if string(data) != payload {
		t.Errorf("downloaded content = %q, want %q", string(data), payload)
	}

	joined := strings.Join(statuses, "\n")
	if !strings.Contains(joined, "Proxy unreachable") {
		t.Errorf("statuses missing proxy-fallback message; got:\n%s", joined)
	}
	// Fail-fast: the dead proxy must not burn MaxRetries attempts.
	for _, burn := range []string{"Attempt 2/3", "Attempt 3/3"} {
		if strings.Contains(joined, burn) {
			t.Errorf("fail-fast violated: statuses contain %q (dead proxy should not be retried); got:\n%s", burn, joined)
		}
	}
}

// TestDownloadProxyBothFailActionableError verifies that when a configured
// proxy and the direct fallback both fail, the error tells the user to
// start the proxy or disable proxy.enabled.
func TestDownloadProxyBothFailActionableError(t *testing.T) {
	err := Download(context.Background(), Options{
		URL:        "https://127.0.0.1:1/archive.zip", // closed port, unreachable directly
		Dest:       filepath.Join(t.TempDir(), "archive.zip"),
		ProxyURL:   "http://127.0.0.1:1",
		MaxRetries: 1,
	})
	if err == nil {
		t.Fatal("expected download to fail")
	}
	msg := err.Error()
	for _, want := range []string{
		"proxy http://127.0.0.1:1 unreachable",
		"direct also failed",
		"start the proxy service or disable proxy.enabled",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not contain %q", msg, want)
		}
	}
}

// TestDownloadProxyFallbackNotUsedOnRealErrors verifies that non-proxy
// errors (e.g. 404 through a working proxy) do not produce the actionable
// "proxy unreachable" message — the generic failure is returned instead.
func TestDownloadProxyFallbackNotUsedOnRealErrors(t *testing.T) {
	server := httptest.NewTLSServer(http.NotFoundHandler())
	defer server.Close()

	err := Download(context.Background(), Options{
		URL:        server.URL + "/missing.zip",
		Dest:       filepath.Join(t.TempDir(), "archive.zip"),
		Client:     deadProxyClient(t, "http://127.0.0.1:1"),
		MaxRetries: 1,
	})
	if err == nil {
		t.Fatal("expected download to fail")
	}
	msg := err.Error()
	if strings.Contains(msg, "start the proxy service") {
		t.Errorf("404 via dead proxy should not produce the actionable proxy message: %s", msg)
	}
	if !strings.Contains(msg, "HTTP 404") {
		t.Errorf("error should contain HTTP 404: %s", msg)
	}
}

func TestIsAllZeroHex(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", false}, // empty is not a placeholder
		{"0000000000000000000000000000000000000000000000000000000000000000", true},
		// Ticket 39 (P2-12): "0x"-prefixed 64 zeros stays a valid placeholder
		// (TrimPrefix leaves exactly 64 zeros, matching the signing side).
		{"0x0000000000000000000000000000000000000000000000000000000000000000", true},
		{"0000000000000000000000000000000000000000000000000000000000000001", false},
		{"f5cea43414cc33024bbe5867f208d1c9c915d6a38e92abeee07ed9e5563662297", false},
		{"abc", false},
		// Ticket 39 (P2-12) — the red line: a single "0" is NOT the canonical
		// placeholder. The old implementation treated ANY all-zero string as a
		// placeholder, so a truncated/malformed hash silently skipped
		// verification instead of failing loudly.
		{"0", false},
		{"00", false},
		// A truncated zero string is likewise not a placeholder.
		{"00000000000000000000000000000000000000000000000000000000000000", false},
	}
	for _, c := range cases {
		if got := util.IsAllZeroHash(c.in); got != c.want {
			t.Errorf("IsAllZeroHash(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestSanitizedURL covers userinfo/query/fragment stripping for logs,
// audit entries, and error messages (F-15).
func TestSanitizedURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://github.com/xmdemt/xdev/releases/download/v1/a.zip", "https://github.com/xmdemt/xdev/releases/download/v1/a.zip"},
		{"https://user:secret@host/x?k=v#frag", "https://host/x"},
		{"https://host/path?token=abc123", "https://host/path"},
		{"https://host/path?token=abc123&b=2", "https://host/path"},
		{"https://host/a?x=1#f", "https://host/a"},
		{"not a url", "not a url"},
		{"", ""},
	}
	for _, c := range cases {
		if got := util.SanitizeURL(c.in); got != c.want {
			t.Errorf("SanitizeURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestDownloadAllZeroSHA_SidecarPinEnforced verifies the all-zero-SHA (TOFU)
// branch never silently overwrites an established sidecar pin (F-04 / D15):
// matching pin → success with pin unchanged; mismatching pin → error and
// .part cleaned up.
func TestDownloadAllZeroSHA_SidecarPinEnforced(t *testing.T) {
	const payload = "sidecar pin test payload"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(payload))
	}))
	defer server.Close()

	sum := sha256.Sum256([]byte(payload))
	goodHash := hex.EncodeToString(sum[:])
	badHash := strings.Repeat("1", 64)
	allZero := strings.Repeat("0", 64)

	// Case 1: matching pin → success, pin not overwritten.
	dest := filepath.Join(t.TempDir(), "archive.zip")
	if err := os.WriteFile(dest+".sha256", []byte(goodHash+"  archive.zip\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Download(context.Background(), Options{
		URL: server.URL + "/archive.zip", Dest: dest, Client: server.Client(),
		ExpectedSHA: allZero, IsThirdParty: true,
	}); err != nil {
		t.Fatalf("download with matching pin failed: %v", err)
	}
	pin, err := os.ReadFile(dest + ".sha256")
	if err != nil {
		t.Fatalf("sidecar pin missing after download: %v", err)
	}
	if !strings.HasPrefix(string(pin), goodHash) {
		t.Errorf("sidecar pin was overwritten: %s", pin)
	}

	// Case 2: mismatching pin → error, no final file, .part removed.
	dest2 := filepath.Join(t.TempDir(), "archive2.zip")
	if err := os.WriteFile(dest2+".sha256", []byte(badHash+"  archive2.zip\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err = Download(context.Background(), Options{
		URL: server.URL + "/archive2.zip", Dest: dest2, Client: server.Client(),
		ExpectedSHA: allZero, IsThirdParty: true,
	})
	if err == nil {
		t.Fatal("expected mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "mismatch with cached sidecar pin") {
		t.Errorf("error = %v, want sidecar pin mismatch", err)
	}
	if _, statErr := os.Stat(dest2 + ".part"); statErr == nil {
		t.Error(".part file should have been removed on pin mismatch")
	}
	if _, statErr := os.Stat(dest2); statErr == nil {
		t.Error("final file should not exist after pin mismatch")
	}
}

func TestVerifyManifestSignature_PlaceholderSHAIsNotTampering(t *testing.T) {
	zero := strings.Repeat("0", 64)
	sigJSON := []byte(`{"tool":"uv","sha256_map":"{\"0.7.6\":{\"windows/amd64\":\"abc\"}}","timestamp":"2026-07-08T12:35:12Z","signature":"AAAA"}`)
	err := verifyManifestSignature(Options{
		ExpectedSHA:       zero,
		ManifestSignature: sigJSON,
	})
	if err != nil {
		t.Fatalf("placeholder SHA should skip signed-map membership check when no root key is embedded: %v", err)
	}
}

func TestVerifySHAInMap_DoesNotMatchZeroPlaceholder(t *testing.T) {
	zero := strings.Repeat("0", 64)
	// Ticket 13: exact-key semantics — the version slot is mandatory.
	if verifySHAInMapForVersion(`{"0.7.6":{"windows/amd64":"abc123"}}`, "0.7.6", "windows/amd64", zero) {
		t.Fatal("all-zero placeholder must not match a signed real hash")
	}
	if !verifySHAInMapForVersion(`{"0.7.6":{"windows/amd64":"abc123"}}`, "0.7.6", "windows/amd64", "abc123") {
		t.Fatal("real hash should be found in the signed map")
	}
	// Same-version cross-os/arch fallback still works (old-signature layout).
	if !verifySHAInMapForVersion(`{"0.7.6":{"linux/amd64":"abc123"}}`, "0.7.6", "windows/amd64", "abc123") {
		t.Fatal("same-version fallback should accept a hash signed under another os/arch key")
	}
	// Cross-version matches are rejected (ticket 13 / G2 red line).
	if verifySHAInMapForVersion(`{"0.7.6":{"windows/amd64":"abc123"}}`, "3.0.0", "windows/amd64", "abc123") {
		t.Fatal("a hash signed for another version must NOT authorise this version")
	}
	// Missing version context fails closed.
	if verifySHAInMapForVersion(`{"0.7.6":{"windows/amd64":"abc123"}}`, "", "windows/amd64", "abc123") {
		t.Fatal("no version context must fail closed")
	}
}
