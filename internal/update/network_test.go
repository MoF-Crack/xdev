package update

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/remote"
)

// The proxy is the only route to the named TLS origins. These tests never
// rewrite request URLs or turn off certificate verification.
type updateProxyFixture struct {
	tls       *httptest.Server
	proxy     *httptest.Server
	roots     *x509.CertPool
	mu        sync.Mutex
	connects  []string
	requests  []string
	direct    atomic.Int32
	directFor map[string]bool
}

func newUpdateProxyFixture(t *testing.T, handler http.HandlerFunc, directFor ...string) *updateProxyFixture {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "update proxy fixture"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		DNSNames:    []string{"api.github.com", "github.com", "bypass.update.test"},
		IPAddresses: []net.IP{net.ParseIP("203.0.113.1")},
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true, IsCA: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, public, private)
	if err != nil {
		t.Fatal(err)
	}
	key, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := tls.X509KeyPair(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	fixture := &updateProxyFixture{roots: x509.NewCertPool(), directFor: make(map[string]bool)}
	fixture.roots.AddCert(parsed)
	for _, host := range directFor {
		fixture.directFor[host] = true
	}
	fixture.tls = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fixture.mu.Lock()
		fixture.requests = append(fixture.requests, r.Host+r.URL.Path)
		fixture.mu.Unlock()
		if r.Header.Get("Proxy-Authorization") != "" {
			t.Error("proxy credentials leaked to TLS origin")
		}
		handler(w, r)
	}))
	fixture.tls.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	fixture.tls.StartTLS()
	t.Cleanup(fixture.tls.Close)
	fixture.proxy = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			t.Errorf("expected CONNECT, got %s", r.Method)
			http.Error(w, "CONNECT required", http.StatusBadRequest)
			return
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("GitHub bearer token leaked to CONNECT proxy")
		}
		fixture.mu.Lock()
		fixture.connects = append(fixture.connects, r.Host)
		fixture.mu.Unlock()
		switch r.Host {
		case "api.github.com:443", "github.com:443", "203.0.113.1:443", "bypass.update.test:443":
		default:
			t.Errorf("unexpected proxy target %q", r.Host)
			http.Error(w, "unexpected host", http.StatusBadGateway)
			return
		}
		upstream, err := net.DialTimeout("tcp", fixture.tls.Listener.Addr().String(), time.Second)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		client, buffer, err := w.(http.Hijacker).Hijack()
		if err != nil {
			upstream.Close()
			return
		}
		_, _ = buffer.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		_ = buffer.Flush()
		go func() { _, _ = io.Copy(upstream, buffer); upstream.Close() }()
		_, _ = io.Copy(client, upstream)
		client.Close()
		upstream.Close()
	}))
	t.Cleanup(fixture.proxy.Close)
	return fixture
}

func (f *updateProxyFixture) installTransport(t *testing.T) *http.Transport {
	t.Helper()
	original := http.DefaultTransport
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: f.roots}}
	proxyURL, _ := url.Parse(f.proxy.URL)
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address == proxyURL.Host {
			return (&net.Dialer{}).DialContext(ctx, network, address)
		}
		host, _, _ := net.SplitHostPort(address)
		if f.directFor[host] {
			f.direct.Add(1)
			return (&net.Dialer{}).DialContext(ctx, network, f.tls.Listener.Addr().String())
		}
		return nil, fmt.Errorf("direct connection forbidden: %s", address)
	}
	http.DefaultTransport = transport
	t.Cleanup(func() { transport.CloseIdleConnections(); http.DefaultTransport = original })
	return transport
}

func (f *updateProxyFixture) connectCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.connects)
}

func TestWithProxyCheckAndApplyCONNECT(t *testing.T) {
	exe := updateTargetFixture(t)
	payload := []byte("binary installed through configured CONNECT proxy")
	archive := updateArchiveFixture(t, payload, binaryNameInArchive())
	result := updateResultFixture("https://github.com", archive)
	result.Release.Assets[1].Size = 100
	withFallbackAssetMetadata(t, result)
	remote.ConfigureGitHubToken("fixture-github-token")
	t.Cleanup(func() { remote.ConfigureGitHubToken("") })
	fixture := newUpdateProxyFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Host == "api.github.com" {
			if r.Header.Get("Authorization") != "Bearer fixture-github-token" {
				t.Error("metadata lost API token")
			}
			switch r.URL.Path {
			case "/repos/MoF-Crack/xdev/releases/assets/101":
				http.Redirect(w, r, "https://203.0.113.1/archive", http.StatusFound)
			case "/repos/MoF-Crack/xdev/releases/assets/102":
				_, _ = fmt.Fprintf(w, "%x  %s\n", sha256.Sum256(archive), result.Release.Assets[0].Name)
			default:
				_ = json.NewEncoder(w).Encode([]*ReleaseInfo{result.Release})
			}
			return
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("GitHub token leaked to archive/CDN")
		}
		switch r.Host + r.URL.Path {
		case "github.com/archive", "github.com/sums":
			http.Error(w, "controlled CDN failure", http.StatusServiceUnavailable)
		case "203.0.113.1/archive":
			_, _ = w.Write(archive)
		default:
			t.Errorf("unexpected path %s%s", r.Host, r.URL.Path)
			http.NotFound(w, r)
		}
	})
	base := fixture.installTransport(t)
	proxy := config.ProxyConfig{Enabled: true, HTTPS: strings.Replace(fixture.proxy.URL, "://", "://proxy-user:proxy-password@", 1)}
	ctx, cleanup := WithProxy(context.Background(), proxy)
	defer cleanup()
	proxy.HTTPS = "http://127.0.0.1:1" // Already captured; caller's config can change.
	checked, err := Check(ctx, "1.0.0-beta.1")
	if err != nil || !checked.HasUpdate {
		t.Fatalf("proxied release discovery: %+v %v", checked, err)
	}
	var events []Progress
	if err := ApplyToWithProgress(ctx, checked, exe, func(p Progress) { events = append(events, p) }); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(exe)
	if err != nil || string(got) != string(payload) || fixture.connectCount() < 3 {
		t.Fatalf("full proxied install: bytes=%q err=%v CONNECT=%d", got, err, fixture.connectCount())
	}
	if base.Proxy != nil || base.TLSClientConfig.InsecureSkipVerify || http.DefaultTransport != base {
		t.Fatal("application snapshot mutated global transport or TLS verification")
	}
	if events[len(events)-1].Phase != "completed" {
		t.Fatal("proxied install lost real completion progress")
	}
	// No direct route exists: this exact operation fails without explicit proxy.
	if _, err := Check(context.Background(), "1.0.0-beta.1"); err == nil || !strings.Contains(err.Error(), "direct connection forbidden") {
		t.Fatalf("test accidentally permits direct metadata: %v", err)
	}
}

func TestWithProxyNO_PROXYUsesTargetHost(t *testing.T) {
	fixture := newUpdateProxyFixture(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("trusted")) }, "bypass.update.test")
	fixture.installTransport(t)
	ctx, cleanup := WithProxy(context.Background(), config.ProxyConfig{Enabled: true, HTTPS: fixture.proxy.URL, NoProxy: ".update.test:443"})
	defer cleanup()
	if err := downloadAsset(ctx, "https://bypass.update.test/archive", io.Discard); err != nil {
		t.Fatal(err)
	}
	if fixture.connectCount() != 0 || fixture.direct.Load() != 1 {
		t.Fatalf("NO_PROXY not applied: CONNECT=%d direct=%d", fixture.connectCount(), fixture.direct.Load())
	}
	if err := downloadAsset(ctx, "https://github.com/archive", io.Discard); err != nil || fixture.connectCount() != 1 {
		t.Fatalf("nonmatching target bypassed proxy: %v CONNECT=%d", err, fixture.connectCount())
	}
}

func TestWithProxySnapshotsAreIndependent(t *testing.T) {
	first := newUpdateProxyFixture(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("first")) })
	second := newUpdateProxyFixture(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("second")) })
	transport := first.installTransport(t)
	transport.TLSClientConfig.RootCAs.AddCert(second.tls.Certificate())
	// Allow both proxy listeners, but still forbid direct origin access.
	firstDial := transport.DialContext
	secondURL, _ := url.Parse(second.proxy.URL)
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address == secondURL.Host {
			return (&net.Dialer{}).DialContext(ctx, network, address)
		}
		return firstDial(ctx, network, address)
	}
	cfg := config.ProxyConfig{Enabled: true, HTTP: first.proxy.URL}
	old, closeOld := WithProxy(context.Background(), cfg)
	defer closeOld()
	cfg.HTTP = second.proxy.URL
	current, closeCurrent := WithProxy(context.Background(), cfg)
	defer closeCurrent()
	for _, tc := range []struct {
		ctx  context.Context
		want string
	}{{old, "first"}, {current, "second"}, {old, "first"}} {
		var body strings.Builder
		if err := downloadAsset(tc.ctx, "https://github.com/archive", &body); err != nil || body.String() != tc.want {
			t.Fatalf("snapshot crossed applications: got=%q want=%q err=%v", body.String(), tc.want, err)
		}
	}
}

func TestWithProxyReadCancellation(t *testing.T) {
	exe := updateTargetFixture(t)
	started := make(chan struct{})
	fixture := newUpdateProxyFixture(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("partial archive"))
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
	})
	fixture.installTransport(t)
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx, cleanup := WithProxy(parent, config.ProxyConfig{Enabled: true, HTTPS: fixture.proxy.URL})
	defer cleanup()
	done := make(chan error, 1)
	result := updateResultFixture("https://github.com", nil)
	go func() { done <- ApplyToWithProgress(ctx, result, exe, nil) }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("proxied archive read never started")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation identity lost: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("root cancellation did not stop proxied read")
	}
	assertOldUpdateTarget(t, exe)
}

func TestWithProxyStillVerifiesTLSCertificates(t *testing.T) {
	fixture := newUpdateProxyFixture(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("untrusted proxy tunnel reached HTTP handler")
	})
	base := fixture.installTransport(t)
	base.TLSClientConfig.RootCAs = x509.NewCertPool()
	ctx, cleanup := WithProxy(context.Background(), config.ProxyConfig{Enabled: true, HTTPS: fixture.proxy.URL})
	defer cleanup()
	if _, err := Check(ctx, "1.0.0"); err == nil || !strings.Contains(err.Error(), "x509") {
		t.Fatalf("explicit proxy disabled metadata TLS verification: %v", err)
	}
	if err := downloadAsset(ctx, "https://github.com/archive", io.Discard); err == nil || !strings.Contains(err.Error(), "x509") {
		t.Fatalf("explicit proxy disabled archive TLS verification: %v", err)
	}
}

func TestWithProxyInvalidConfigFailsClosed(t *testing.T) {
	for _, raw := range []string{"file:///proxy", "http://user:secret@[::1", "http://"} {
		ctx, cleanup := WithProxy(context.Background(), config.ProxyConfig{Enabled: true, HTTPS: raw, NoProxy: "*"})
		_, err := Check(ctx, "1.0.0")
		cleanup()
		if err == nil || !strings.Contains(err.Error(), "invalid self-update proxy") || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "user:") {
			t.Fatalf("invalid proxy failed open or leaked credentials: %v", err)
		}
	}
}

func TestBypassUpdateProxyRules(t *testing.T) {
	for _, tc := range []struct {
		url, bypass string
		want        bool
	}{
		{"https://example.com", "example.com", true}, {"https://a.example.com", "example.com", true},
		{"https://badexample.com", "example.com", false}, {"https://example.com", ".example.com", false},
		{"https://a.example.com", "*.example.com", true}, {"https://a.example.com", "EXAMPLE.COM:443", true},
		{"https://example.com:444", "example.com:443", false}, {"https://10.2.3.4", "10.0.0.0/8", true},
		{"https://11.2.3.4", "10.0.0.0/8", false}, {"https://[2001:db8::2]", "2001:db8::/32", true},
		{"https://[::1]:8443", "[::1]:8443", true}, {"https://[::1]", "[::1]:8443", false},
		{"https://example.com", "*", true}, {"https://example.com", " , ", false},
	} {
		u, err := url.Parse(tc.url)
		if err != nil {
			t.Fatal(err)
		}
		if got := bypassUpdateProxy(u, strings.Split(tc.bypass, ",")); got != tc.want {
			t.Errorf("NO_PROXY=%q URL=%q: got=%v want=%v", tc.bypass, tc.url, got, tc.want)
		}
	}
}

// ProxyFromEnvironment caches its process environment. Use a subprocess to
// verify net/http's actual fallback without interfering with other tests.
func TestWithProxyEnvironmentFallback(t *testing.T) {
	var calls atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodConnect || r.Host != "api.github.com:443" {
			t.Errorf("environment proxy request: %s %s", r.Method, r.Host)
		}
		// This test only needs to prove proxy routing; return a controlled
		// response through the child test's trusted HTTP transport below.
		http.Error(w, "environment proxy reached", http.StatusBadGateway)
	}))
	defer proxy.Close()
	// A child checks the proxy choice without making certificate exceptions.
	cmd := exec.Command(os.Args[0], "-test.run=^TestWithProxyEnvironmentChoiceChild$")
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.EqualFold(key, "HTTP_PROXY") && !strings.EqualFold(key, "HTTPS_PROXY") && !strings.EqualFold(key, "NO_PROXY") && key != "XDEV_UPDATE_PROXY_CHILD" {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "HTTPS_PROXY="+proxy.URL, "NO_PROXY=", "XDEV_UPDATE_PROXY_CHILD=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("environment proxy child: %v\n%s", err, output)
	}
	if calls.Load() != 3 {
		t.Fatalf("environment fallback requests=%d, want 3", calls.Load())
	}
}

func TestWithProxyEnvironmentChoiceChild(t *testing.T) {
	if os.Getenv("XDEV_UPDATE_PROXY_CHILD") != "1" {
		t.Skip("subprocess only")
	}
	for _, cfg := range []config.ProxyConfig{{}, {Enabled: true}, {Enabled: false, HTTPS: "http://ignored.invalid:1", NoProxy: "*"}} {
		ctx, cleanup := WithProxy(context.Background(), cfg)
		_, err := Check(ctx, "1.0.0")
		cleanup()
		if err == nil || !strings.Contains(err.Error(), "Bad Gateway") {
			t.Fatalf("environment fallback: %v", err)
		}
	}
}
