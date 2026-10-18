package util

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
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
	// Fail closed on DNS errors: an unresolvable or attacker-controlled
	// name must not skip the private-IP check (SSRF).
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("URL host %s could not be resolved: %w", host, err)
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
		return fmt.Errorf("URL host %s could not be resolved: %w", u.Hostname(), err)
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
// Returns the input unchanged when it has no scheme (avoids re-encoding
// arbitrary non-URL text). Functional URLs used for actual requests must NOT
// go through this function.
//
// 第九轮审计 P1-1（P1）：**解析失败时不能再原样返回**。
//
// 旧实现在 `url.Parse` 失败时 `return raw`。而「解析失败」恰恰是最需要脱敏的
// 场景，也是**同一条错误链里必然出现**的场景：
//   - `IsSafeExternalURL` 的 "invalid URL" 错误本身就是 `url.Parse` 的
//     `*url.Error`，其文本内嵌**整段原始 URL**（含 userinfo 与 query）；
//   - 调用方把该 URL 用 `%q` 插进错误后，`url.Parse` 再失败 → 脱敏整体失效。
//
// 实测（Round 9）：`https://user:hunter2@example.com/x%zz?token=TOPSECRET123`
// 的错误链里 userinfo 与 `?token=` 原样出现；坏端口（`:bad`）与控制字符同样泄漏。
// 对照：可解析但被策略拒绝的 URL 已被正确脱敏（只剩 scheme+host+path）。
//
// 修复：解析失败时退化为**正则脱敏** —— 先抹掉 `//user:pass@`，再截断
// `?...` / `#...`。这是纯文本兜底，不追求还原 URL 结构，只保证「不泄漏凭据」。
func SanitizeURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return sanitizeURLTextually(raw)
	}
	if parsed.Scheme == "" {
		// 无 scheme：可能是任意非 URL 文本（错误消息片段等）。仍做一次文本兜底，
		// 因为「无 scheme 但含 //user:pass@host」或「含 ?token=」的文本同样会泄漏。
		return sanitizeURLTextually(raw)
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

// urlTextUserinfoRe 匹配 `//<userinfo>@`（userinfo 不含 `/`、`@`、空白）。
// 与 downloader.redactProxyCredentials 的判据一致，但这里作用于**任意文本**。
var urlTextUserinfoRe = regexp.MustCompile(`//[^/@\s]*@`)

// sanitizeURLTextually 在无法结构化解析时按文本脱敏：
//  1. 抹掉 userinfo：`//user:pass@host` → `//<redacted>@host`
//  2. 截断查询串/片段：`?token=…` / `#…` 一律丢弃
//
// 顺序很重要：先抹 userinfo 再截 query，避免 userinfo 里含 `?` 时残留。
func sanitizeURLTextually(raw string) string {
	if raw == "" {
		return raw
	}
	out := urlTextUserinfoRe.ReplaceAllString(raw, "//<redacted>@")
	if idx := strings.IndexAny(out, "?#"); idx >= 0 {
		out = out[:idx]
	}
	return out
}

// ValidateProxyURL validates a user-supplied HTTP/HTTPS/SOCKS proxy URL.
// Empty values are allowed (proxy field not configured). Non-empty values
// must parse as an absolute URL with scheme http, https, socks5, or socks5h
// and a non-empty host. Userinfo is permitted (authenticated proxies) but
// credentials must never be logged — callers should use SanitizeURL.
func ValidateProxyURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid proxy URL: %w", err)
	}
	switch u.Scheme {
	case "http", "https", "socks5", "socks5h":
	default:
		return fmt.Errorf("proxy URL scheme %q is not allowed (use http, https, socks5, or socks5h)", u.Scheme)
	}
	if u.Hostname() == "" {
		return fmt.Errorf("proxy URL has no host")
	}
	return nil
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
