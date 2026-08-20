package util

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestSecureRedirectCheck_BlocksHTTPSDowngrade(t *testing.T) {
	check := SecureRedirectCheck(3)
	err := check(
		&http.Request{URL: &url.URL{Scheme: "http", Host: "example.com"}},
		[]*http.Request{{URL: &url.URL{Scheme: "https", Host: "example.com"}}},
	)
	if err == nil {
		t.Fatal("expected HTTPS→HTTP downgrade to be blocked")
	}
}

func TestSecureRedirectCheck_BlocksPrivateHop(t *testing.T) {
	check := SecureRedirectCheck(3)
	via := []*http.Request{{URL: &url.URL{Scheme: "https", Host: "example.com"}}}

	err := check(&http.Request{URL: &url.URL{Scheme: "https", Host: "127.0.0.1"}}, via)
	if err == nil {
		t.Fatal("expected redirect to loopback to be blocked")
	}
	if !strings.Contains(err.Error(), "redirect target blocked") {
		t.Errorf("unexpected error: %v", err)
	}

	err = check(&http.Request{URL: &url.URL{Scheme: "https", Host: "10.0.0.1"}}, via)
	if err == nil {
		t.Fatal("expected redirect to RFC1918 address to be blocked")
	}
}

func TestSecureRedirectCheck_BlocksFileSchemeHop(t *testing.T) {
	check := SecureRedirectCheck(3)
	err := check(
		&http.Request{URL: &url.URL{Scheme: "file", Path: "/etc/passwd"}},
		[]*http.Request{{URL: &url.URL{Scheme: "https", Host: "example.com"}}},
	)
	if err == nil {
		t.Fatal("expected file:// redirect to be blocked")
	}
}

func TestSecureRedirectCheck_AllowsHTTPSPublicHop(t *testing.T) {
	check := SecureRedirectCheck(3)
	err := check(
		&http.Request{URL: &url.URL{Scheme: "https", Host: "example.com", Path: "/pkg"}},
		[]*http.Request{{URL: &url.URL{Scheme: "https", Host: "github.com"}}},
	)
	if err != nil {
		t.Fatalf("public HTTPS hop should be allowed: %v", err)
	}
}

func TestSecureRedirectCheck_TooManyRedirects(t *testing.T) {
	check := SecureRedirectCheck(1)
	via := []*http.Request{{URL: &url.URL{Scheme: "https", Host: "example.com"}}}
	err := check(&http.Request{URL: &url.URL{Scheme: "https", Host: "cdn.example.com"}}, via)
	if err == nil {
		t.Fatal("expected too-many-redirects error")
	}
}
