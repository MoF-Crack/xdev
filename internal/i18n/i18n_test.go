package i18n

import (
	"os"
	"testing"
)

func TestInit(t *testing.T) {
	// Save original state
	origLang := currentLang
	defer func() {
		mu.Lock()
		currentLang = origLang
		mu.Unlock()
	}()

	tests := []struct {
		name     string
		lcAll    string
		lang     string
		expected string
	}{
		{
			name:     "detects Chinese from LC_ALL",
			lcAll:    "zh_CN.UTF-8",
			lang:     "",
			expected: "zh",
		},
		{
			name:     "detects English from LANG",
			lcAll:    "",
			lang:     "en_US.UTF-8",
			expected: "en",
		},
		{
			name:     "LC_ALL takes precedence over LANG",
			lcAll:    "zh_TW.UTF-8",
			lang:     "en_US.UTF-8",
			expected: "zh",
		},
		{
			name:     "defaults to English when no env set",
			lcAll:    "",
			lang:     "",
			expected: "en",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment
			origLCAll := os.Getenv("LC_ALL")
			origLANG := os.Getenv("LANG")
			os.Setenv("LC_ALL", tt.lcAll)
			os.Setenv("LANG", tt.lang)
			defer func() {
				os.Setenv("LC_ALL", origLCAll)
				os.Setenv("LANG", origLANG)
			}()

			Init()
			if Language() != tt.expected {
				t.Errorf("Language() = %q, want %q", Language(), tt.expected)
			}
		})
	}
}

func TestSetLanguage(t *testing.T) {
	// Save original state
	origLang := currentLang
	defer func() {
		mu.Lock()
		currentLang = origLang
		mu.Unlock()
	}()

	tests := []struct {
		input    string
		expected string
	}{
		{"en", "en"},
		{"zh", "zh"},
		{"zh_CN", "zh"},
		{"en_US", "en"},
		{"ZH_TW.UTF-8", "zh"},
		{"EN_US.UTF-8", "en"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			SetLanguage(tt.input)
			if Language() != tt.expected {
				t.Errorf("SetLanguage(%q): Language() = %q, want %q", tt.input, Language(), tt.expected)
			}
		})
	}
}

func TestT_KnownKeys(t *testing.T) {
	// Save original state
	origLang := currentLang
	defer func() {
		mu.Lock()
		currentLang = origLang
		mu.Unlock()
	}()

	// Test English translations
	SetLanguage("en")
	tests := []struct {
		key      string
		expected string
	}{
		{"installed", "installed"},
		{"activated", "activated"},
		{"uninstalled", "uninstalled"},
		{"error", "error"},
		{"success", "success"},
		{"not_installed", "not installed"},
		{"active", "active"},
	}
	for _, tt := range tests {
		got := T(tt.key)
		if got != tt.expected {
			t.Errorf("T(%q) = %q, want %q", tt.key, got, tt.expected)
		}
	}

	// Test Chinese translations
	SetLanguage("zh")
	zhTests := []struct {
		key      string
		expected string
	}{
		{"installed", "已安装"},
		{"activated", "已激活"},
		{"uninstalled", "已卸载"},
		{"error", "错误"},
		{"success", "成功"},
		{"not_installed", "未安装"},
		{"active", "活跃"},
	}
	for _, tt := range zhTests {
		got := T(tt.key)
		if got != tt.expected {
			t.Errorf("T(%q) = %q, want %q", tt.key, got, tt.expected)
		}
	}
}

func TestT_UnknownKey(t *testing.T) {
	// Save original state
	origLang := currentLang
	defer func() {
		mu.Lock()
		currentLang = origLang
		mu.Unlock()
	}()

	SetLanguage("en")
	got := T("nonexistent.key.xyz")
	if got != "nonexistent.key.xyz" {
		t.Errorf("T(unknown key) = %q, want %q", got, "nonexistent.key.xyz")
	}

	SetLanguage("zh")
	got = T("another.missing.key")
	if got != "another.missing.key" {
		t.Errorf("T(unknown key) = %q, want %q", got, "another.missing.key")
	}
}

func TestT_WithFormatArgs(t *testing.T) {
	// Save original state
	origLang := currentLang
	defer func() {
		mu.Lock()
		currentLang = origLang
		mu.Unlock()
	}()

	// English with format arguments
	SetLanguage("en")
	got := T("cli.installed_to", "python", "3.12.0", "/opt/xdev/install/python/3.12.0")
	expected := "installed python@3.12.0 to /opt/xdev/install/python/3.12.0"
	if got != expected {
		t.Errorf("T with args = %q, want %q", got, expected)
	}

	got = T("cli.removed", "node", "18.0.0")
	expected = "removed node@18.0.0"
	if got != expected {
		t.Errorf("T with args = %q, want %q", got, expected)
	}

	// Chinese with format arguments
	SetLanguage("zh")
	got = T("cli.installed_to", "python", "3.12.0", "/opt/xdev/install/python/3.12.0")
	expected = "已安装 python@3.12.0 到 /opt/xdev/install/python/3.12.0"
	if got != expected {
		t.Errorf("T with args = %q, want %q", got, expected)
	}

	got = T("cli.removed", "node", "18.0.0")
	expected = "已移除 node@18.0.0"
	if got != expected {
		t.Errorf("T with args = %q, want %q", got, expected)
	}
}
