// Package util provides common utility functions for the xdev project.
//
// This file (http.go) implements shared HTTP helpers: proxy connection
// error detection and the secure redirect policy used by every HTTP
// client in the codebase (Spec 021 D4 — single source replacing five
// near-identical local implementations).
package util

import (
	"fmt"
	"net/http"
	"strings"
)

// IsProxyError reports whether err indicates a proxy connection failure
// (proxy refused, unreachable, or proxyconnect error). It also matches
// the Windows connectex message ("no connection could be made").
func IsProxyError(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "proxyconnect") ||
		strings.Contains(lower, "connection refused") ||
		strings.Contains(lower, "actively refused") ||
		strings.Contains(lower, "no connection could be made")
}

// SecureRedirectCheck returns an http.Client CheckRedirect callback that
// limits the number of redirects and blocks HTTPS→HTTP downgrades.
//
// Security (H-8 / N-6 / N-9): an HTTPS URL that redirects to HTTP would
// expose the request to MITM attacks even though the original URL was
// secure, so the downgrade is rejected.
func SecureRedirectCheck(maxRedirects int) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return fmt.Errorf("too many redirects")
		}
		if len(via) > 0 && via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
			return fmt.Errorf("insecure redirect: %s → %s (HTTPS downgrade blocked)", via[0].URL, req.URL)
		}
		return nil
	}
}
