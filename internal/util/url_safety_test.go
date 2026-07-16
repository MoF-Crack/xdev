package util

import "testing"

func TestIsSafeExternalURL_HTTPS_OK(t *testing.T) {
	urls := []string{
		"https://example.com/file",
		"https://github.com/org/repo",
		"https://cdn.npmmirror.com/pkg.tgz",
	}
	for _, u := range urls {
		if err := IsSafeExternalURL(u); err != nil {
			t.Errorf("IsSafeExternalURL(%q) should be OK: %v", u, err)
		}
	}
}

func TestIsSafeExternalURL_HTTP_Localhost_OK(t *testing.T) {
	urls := []string{
		"http://localhost:11434",
		"http://127.0.0.1:8080",
		"http://localhost",
	}
	for _, u := range urls {
		if err := IsSafeExternalURL(u); err != nil {
			t.Errorf("IsSafeExternalURL(%q) should be OK (localhost): %v", u, err)
		}
	}
}

func TestIsSafeExternalURL_RejectsNonHTTPS(t *testing.T) {
	urls := []string{
		"ftp://example.com/file",
		"file:///etc/passwd",
		"gopher://internal",
	}
	for _, u := range urls {
		if err := IsSafeExternalURL(u); err == nil {
			t.Errorf("IsSafeExternalURL(%q) should be rejected (bad scheme)", u)
		}
	}
}

func TestIsSafeExternalURL_RejectsHTTP_External(t *testing.T) {
	urls := []string{
		"http://example.com",
		"http://evil.com/malware",
	}
	for _, u := range urls {
		if err := IsSafeExternalURL(u); err == nil {
			t.Errorf("IsSafeExternalURL(%q) should be rejected (no HTTPS)", u)
		}
	}
}

func TestIsSafeExternalURL_RejectsEmpty(t *testing.T) {
	if err := IsSafeExternalURL(""); err == nil {
		t.Error("IsSafeExternalURL('') should be rejected")
	}
}

func TestIsProtectedSystemEnvVar(t *testing.T) {
	protected := []string{"PATH", "Path", "PATHEXT", "SystemRoot", "COMSPEC", "TEMP"}
	for _, name := range protected {
		if !IsProtectedSystemEnvVar(name) {
			t.Errorf("IsProtectedSystemEnvVar(%q) = false, want true", name)
		}
	}
}

func TestIsProtectedSystemEnvVar_NonProtected(t *testing.T) {
	nonProtected := []string{"JAVA_HOME", "GOROOT", "MY_VAR", ""}
	for _, name := range nonProtected {
		if IsProtectedSystemEnvVar(name) {
			t.Errorf("IsProtectedSystemEnvVar(%q) = true, want false", name)
		}
	}
}

func TestIsLocalhost(t *testing.T) {
	locals := []string{"localhost", "127.0.0.1", "::1", ""}
	for _, h := range locals {
		if !IsLocalhost(h) {
			t.Errorf("IsLocalhost(%q) = false, want true", h)
		}
	}
	nonLocals := []string{"example.com", "192.168.1.1", "10.0.0.1"}
	for _, h := range nonLocals {
		if IsLocalhost(h) {
			t.Errorf("IsLocalhost(%q) = true, want false", h)
		}
	}
}
