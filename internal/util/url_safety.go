package util

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// IsSafeExternalURL validates that a URL is safe for the application to
// fetch on behalf of a user. It enforces HTTPS (or http to localhost) and
// rejects private/reserved IP ranges to prevent Server-Side Request
// Forgery (SSRF) attacks.
//
// Use this for any URL supplied by the frontend or from third-party
// manifests/plugins before making HTTP requests.
func IsSafeExternalURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	// Allow only http/https schemes.
	if u.Scheme != "https" && u.Scheme != "http" {
		return fmt.Errorf("URL scheme %q is not allowed, use https://", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("URL has no host")
	}
	// Allow localhost over http for local services (e.g., Ollama).
	if IsLocalhost(host) {
		return nil
	}
	// Non-localhost http is insecure — reject.
	if u.Scheme == "http" {
		return fmt.Errorf("non-localhost http URL is not allowed, use https://")
	}
	// Resolve the host and reject private/reserved IP ranges.
	ips, err := net.LookupIP(host)
	if err != nil {
		// DNS resolution failed — allow the request to proceed and
		// fail naturally; we cannot block what we cannot resolve.
		return nil
	}
	for _, ip := range ips {
		if isPrivateOrReservedIP(ip) {
			return fmt.Errorf("URL host %s resolves to private/reserved IP %s — blocked (SSRF protection)", host, ip)
		}
	}
	return nil
}

// IsSafeHTTPSExternalURL validates download and mirror URLs, where local
// services and plaintext HTTP are never valid targets.
func IsSafeHTTPSExternalURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" {
		return fmt.Errorf("URL must be a valid https URL")
	}
	if IsLocalhost(u.Hostname()) {
		return fmt.Errorf("URL host must not be localhost")
	}
	ips, err := net.LookupIP(u.Hostname())
	if err != nil {
		return nil
	}
	for _, ip := range ips {
		if isPrivateOrReservedIP(ip) {
			return fmt.Errorf("URL host resolves to private/reserved IP %s", ip)
		}
	}
	return nil
}

// IsLocalhost reports whether host refers to the local machine.
func IsLocalhost(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1" ||
		host == "" || strings.HasSuffix(host, ".localhost")
}

// IsPrivateOrReservedIP reports whether ip is in a private, loopback,
// link-local, multicast, or unspecified range that should not be the
// target of an outbound request initiated from user input.
func IsPrivateOrReservedIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() ||
		ip.IsInterfaceLocalMulticast()
}

// isPrivateOrReservedIP is an unexported alias for IsPrivateOrReservedIP
// used internally by IsSafeExternalURL. Kept unexported to preserve the
// original internal API while also exposing the check for reuse.
func isPrivateOrReservedIP(ip net.IP) bool {
	return IsPrivateOrReservedIP(ip)
}

// SanitizeURL strips userinfo, query and fragment from a URL string for
// safe inclusion in logs, audit entries, and error messages (F-15).
// Returns the input unchanged when it cannot be parsed or has no scheme
// (avoids re-encoding arbitrary non-URL text). Functional URLs used for
// actual requests must NOT go through this function.
func SanitizeURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if parsed.Scheme == "" {
		return raw
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

// IsProtectedSystemEnvVar reports whether name is a Windows system
// environment variable that must never be modified or deleted by xdev.
// This is the single source of truth used by the GUI env-var operations
// (SetEnvVar, DeleteEnvVar, DeleteEnvVarEntry, CleanEnvVarPath) to
// ensure consistent protection across all entry points.
func IsProtectedSystemEnvVar(name string) bool {
	protected := []string{
		"PATH", "Path", "PATHEXT", "SystemRoot", "SystemDrive", "windir",
		"OS", "PROCESSOR_ARCHITECTURE", "PROCESSOR_IDENTIFIER",
		"NUMBER_OF_PROCESSORS", "COMPUTERNAME", "USERNAME", "USERPROFILE",
		"HOMEDRIVE", "HOMEPATH", "TEMP", "TMP", "COMSPEC", "PSModulePath",
	}
	for _, p := range protected {
		if strings.EqualFold(name, p) {
			return true
		}
	}
	return false
}
