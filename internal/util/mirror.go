package util

import "strings"

// ShouldApplyMirror checks whether a URL should be rewritten by the
// mirror table. Fallback sources that have their own CDN and should
// not be rewritten are excluded.
func ShouldApplyMirror(rawURL string) bool {
	skipHosts := []string{
		"api.adoptium.net",
	}
	for _, h := range skipHosts {
		if strings.Contains(rawURL, h) {
			return false
		}
	}
	return true
}

// CookieFromInstallArgs extracts a cookie value from InstallArgs.
// InstallArgs may contain entries like "cookie:oraclelicense=accept-securebackup-cookie".
func CookieFromInstallArgs(args []string) string {
	for _, a := range args {
		if strings.HasPrefix(a, "cookie:") {
			return strings.TrimPrefix(a, "cookie:")
		}
	}
	return ""
}
