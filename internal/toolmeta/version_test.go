package toolmeta

import (
	"strings"
	"testing"
)

// TC-V1 ExtractVersion env 前缀并集（java / Python / node）
func TestExtractVersion_EnvPrefixes(t *testing.T) {
	cases := map[string]string{
		"java 25.0.3":   "25.0.3",
		"Python 3.12.7": "3.12.7",
		"node 20.18.0":  "20.18.0",
		"go1.22.0":      "1.22.0",
		// gui 锁定行为：多 token 输出保留带 go 前缀的原始 token
		"go version go1.26.5 windows/amd64": "go1.26.5",
	}
	for in, want := range cases {
		if got := ExtractVersion(in); got != want {
			t.Errorf("ExtractVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

// TC-V2 ExtractVersion gui 前缀并集
func TestExtractVersion_GuiPrefixes(t *testing.T) {
	cases := map[string]string{
		"yarn 9.1.2":                   "9.1.2",
		"gradle 9.4.1":                 "9.4.1",
		"cmake version 3.30.1":         "3.30.1",
		"git version 2.47.0.windows.1": "2.47.0",
		"GNU Make 4.4.1":               "4.4.1",
		"Apache Maven 3.9.9":           "3.9.9",
		"Dart SDK version: 3.5.4":      "3.5.4",
		"go1.22.0":                     "1.22.0",
		"v18.0.0":                      "18.0.0",
	}
	for in, want := range cases {
		if got := ExtractVersion(in); got != want {
			t.Errorf("ExtractVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

// TC-V3 ExtractVersion 无版本
func TestExtractVersion_NoVersion(t *testing.T) {
	for _, in := range []string{"foo bar", "", "   ", "not-a-version"} {
		if got := ExtractVersion(in); got != "" {
			t.Errorf("ExtractVersion(%q) = %q, want empty", in, got)
		}
	}
}

// TC-V4 ExtractVersionFromOutput 单行
func TestExtractVersionFromOutput_SingleLine(t *testing.T) {
	cases := map[string]string{
		"v20.18.0":        "20.18.0",
		"Python 3.12.7\n": "3.12.7",
		"gradle 9.4.1":    "9.4.1",
		"":                "",
	}
	for in, want := range cases {
		if got := ExtractVersionFromOutput(in); got != want {
			t.Errorf("ExtractVersionFromOutput(%q) = %q, want %q", in, got, want)
		}
	}
}

// TC-V5 ExtractVersionFromOutput 逐行 banner（gradle 先打分隔线）
func TestExtractVersionFromOutput_BannerLines(t *testing.T) {
	in := "------------------------------------------------------------\nGradle 9.4.1\n------------------------------------------------------------\n"
	if got := ExtractVersionFromOutput(in); got != "9.4.1" {
		t.Errorf("banner output = %q, want 9.4.1", got)
	}
}

// TC-V6 ExtractVersionFromOutput 回退截断 60
func TestExtractVersionFromOutput_FallbackTruncated(t *testing.T) {
	longLine := "  " + strings.Repeat("x", 80)
	got := ExtractVersionFromOutput(longLine)
	if len(got) != 63 || !strings.HasSuffix(got, "...") {
		t.Errorf("fallback = %q (len %d), want 60 chars + ...", got, len(got))
	}
}

// TC-V1b 多 token + v 前缀 token 的行为记录（与 gui 版一致：不 trim token v）
func TestExtractVersion_MultiTokenVPrefix(t *testing.T) {
	got := ExtractVersion("node v20.18.0")
	// gui 版行为：前缀 "node " 因 v 非数字不剥离，fields 命中 v20.18.0 原样返回。
	// env 版会返回 "20.18.0"；统一为 gui 版（spec 2.2 取 gui 版逻辑）。
	if got != "v20.18.0" {
		t.Errorf("ExtractVersion(node v20.18.0) = %q, want %q (gui-version behavior)", got, "v20.18.0")
	}
}

// TC-V7 IsVersionLike
func TestIsVersionLike(t *testing.T) {
	valid := []string{"1.22", "1.22.3", "v1.2.3", "go1.22", "2.47.0.windows.1", "1.2.3.4-windows", "1."}
	invalid := []string{"abc", "1", "", "v"}
	for _, s := range valid {
		if !IsVersionLike(s) {
			t.Errorf("IsVersionLike(%q) = false, want true (legacy behavior: 1. is version-like)", s)
		}
	}
	for _, s := range invalid {
		if IsVersionLike(s) {
			t.Errorf("IsVersionLike(%q) = true, want false", s)
		}
	}
}

// TC-V8 StripWindowsSuffix
func TestStripWindowsSuffix(t *testing.T) {
	cases := map[string]string{
		"2.47.0.windows.1": "2.47.0",
		"1.2.3":            "1.2.3",
		"7.1.0.windows.2":  "7.1.0",
	}
	for in, want := range cases {
		if got := StripWindowsSuffix(in); got != want {
			t.Errorf("StripWindowsSuffix(%q) = %q, want %q", in, got, want)
		}
	}
}
