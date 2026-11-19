package update

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/util"
)

type networkContextKey struct{}

type updateNetwork struct {
	transport http.RoundTripper
}

// WithProxy freezes an application's proxy settings for one update operation.
// Pass the returned context to both Check and Apply, then call cleanup. Changes
// to the application config affect the next operation, never an in-flight one.
// Disabled or empty settings retain net/http's environment proxy behavior and
// the existing DefaultTransport seam. No process-wide transport is modified.
func WithProxy(ctx context.Context, proxy config.ProxyConfig) (context.Context, func()) {
	transport := http.DefaultTransport
	cleanup := func() {}
	if proxy.Enabled && (proxy.HTTP != "" || proxy.HTTPS != "") {
		var owned *http.Transport
		if base, ok := transport.(*http.Transport); ok {
			owned = base.Clone()
		} else {
			// An embedding application may replace DefaultTransport with another
			// RoundTripper. An explicit proxy still needs a real HTTP transport.
			owned = &http.Transport{
				DialContext:       (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
				ForceAttemptHTTP2: true, MaxIdleConns: 100, IdleConnTimeout: 90 * time.Second,
				TLSHandshakeTimeout: 10 * time.Second, ExpectContinueTimeout: time.Second,
			}
		}
		owned.Proxy = configuredProxy(proxy)
		transport = owned
		cleanup = owned.CloseIdleConnections
	}
	return context.WithValue(ctx, networkContextKey{}, updateNetwork{transport: transport}), cleanup
}

func updateTransport(ctx context.Context) http.RoundTripper {
	if network, ok := ctx.Value(networkContextKey{}).(updateNetwork); ok {
		return network.transport
	}
	return nil
}

func configuredProxy(proxy config.ProxyConfig) func(*http.Request) (*url.URL, error) {
	// Parse once; the returned closure contains only operation-owned values.
	cfg := config.Config{Proxy: proxy}
	httpProxy, httpErr := parseUpdateProxy(cfg.GetProxyURL("http"))
	httpsProxy, httpsErr := parseUpdateProxy(cfg.GetProxyURL("https"))
	bypass := strings.Split(proxy.NoProxy, ",")
	return func(req *http.Request) (*url.URL, error) {
		if httpErr != nil {
			return nil, httpErr
		}
		if httpsErr != nil {
			return nil, httpsErr
		}
		if bypassUpdateProxy(req.URL, bypass) {
			return nil, nil
		}
		if req.URL.Scheme == "https" {
			return httpsProxy, httpsErr
		}
		return httpProxy, httpErr
	}
}

func parseUpdateProxy(raw string) (*url.URL, error) {
	// Use the same validation as config writes, including SOCKS5 support.
	// Do not wrap a URL parse error: it may contain proxy userinfo.
	raw = strings.TrimSpace(raw)
	if err := util.ValidateProxyURL(raw); err != nil {
		return nil, fmt.Errorf("invalid self-update proxy (expected an absolute HTTP, HTTPS or SOCKS5 URL)")
	}
	return url.Parse(raw)
}

// bypassUpdateProxy implements the NO_PROXY host/domain/IP/CIDR and optional
// port rules. A leading dot (or *.) matches subdomains only; a bare domain also
// matches itself. Ports use the destination's effective HTTP(S) port.
func bypassUpdateProxy(u *url.URL, entries []string) bool {
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	ip, _ := netip.ParseAddr(host)
	for _, entry := range entries {
		entry = strings.ToLower(strings.TrimSpace(entry))
		if entry == "*" {
			return true
		}
		if entry == "" {
			continue
		}
		if prefix, err := netip.ParsePrefix(entry); err == nil {
			if ip.IsValid() && prefix.Contains(ip.Unmap()) {
				return true
			}
			continue
		}
		name, onlyPort, err := net.SplitHostPort(entry)
		if err == nil {
			if port != onlyPort {
				continue
			}
			entry = name
		}
		entry = strings.Trim(entry, "[]")
		if candidate, err := netip.ParseAddr(entry); err == nil {
			if ip.IsValid() && candidate.Unmap() == ip.Unmap() {
				return true
			}
			continue
		}
		entry = strings.TrimPrefix(entry, "*")
		if strings.HasPrefix(entry, ".") {
			if strings.HasSuffix(host, entry) {
				return true
			}
		} else if host == entry || strings.HasSuffix(host, "."+entry) {
			return true
		}
	}
	return false
}
