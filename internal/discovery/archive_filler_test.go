package discovery

import (
	"testing"

	"github.com/xmdemt/xdev/internal/manifest"
)

func TestFillArchivePlaceholders(t *testing.T) {
	tool := &manifest.Tool{Name: "cmake"}
	template := []manifest.Archive{
		{
			OS:         "windows",
			Arch:       "amd64",
			Format:     "zip",
			URL:        "https://github.com/Kitware/CMake/releases/download/v{version}/cmake-{version}-windows-x86_64.zip",
			MirrorURL:  "https://mirror.example.com/cmake-{version}-windows-x86_64.zip",
			MirrorURLs: []string{"https://mirror2.example.com/cmake-{version}-windows-x86_64.zip"},
			Strip:      1,
		},
		{
			OS:     "linux",
			Arch:   "amd64",
			Format: "tar.gz",
			URL:    "https://github.com/Kitware/CMake/releases/download/v{version}/cmake-{version}-linux-x86_64.tar.gz",
		},
	}

	result := FillArchivePlaceholders(tool, "3.29.0", template)
	if len(result) != 2 {
		t.Fatalf("expected 2 archives, got %d", len(result))
	}

	// Check URL replacement
	expectedURL := "https://github.com/Kitware/CMake/releases/download/v3.29.0/cmake-3.29.0-windows-x86_64.zip"
	if result[0].URL != expectedURL {
		t.Errorf("URL = %q, want %q", result[0].URL, expectedURL)
	}

	// Check sha256 placeholder
	if result[0].SHA256 != zeroSHA256 {
		t.Errorf("SHA256 should be zero placeholder, got %q", result[0].SHA256)
	}

	// Check size placeholder
	if result[0].Size != 0 {
		t.Errorf("Size should be 0, got %d", result[0].Size)
	}

	// Check mirror URL replacement
	expectedMirror := "https://mirror.example.com/cmake-3.29.0-windows-x86_64.zip"
	if result[0].MirrorURL != expectedMirror {
		t.Errorf("MirrorURL = %q, want %q", result[0].MirrorURL, expectedMirror)
	}

	// Check mirror_urls replacement
	if len(result[0].MirrorURLs) != 1 {
		t.Fatalf("expected 1 mirror URL, got %d", len(result[0].MirrorURLs))
	}
	expectedMirror2 := "https://mirror2.example.com/cmake-3.29.0-windows-x86_64.zip"
	if result[0].MirrorURLs[0] != expectedMirror2 {
		t.Errorf("MirrorURLs[0] = %q, want %q", result[0].MirrorURLs[0], expectedMirror2)
	}

	// Strip preserved
	if result[0].Strip != 1 {
		t.Errorf("Strip should be 1, got %d", result[0].Strip)
	}

	// Second archive
	expectedURL2 := "https://github.com/Kitware/CMake/releases/download/v3.29.0/cmake-3.29.0-linux-x86_64.tar.gz"
	if result[1].URL != expectedURL2 {
		t.Errorf("URL2 = %q, want %q", result[1].URL, expectedURL2)
	}
}

func TestFillArchivePlaceholders_NilTemplate(t *testing.T) {
	tool := &manifest.Tool{Name: "test"}
	result := FillArchivePlaceholders(tool, "1.0.0", nil)
	if result != nil {
		t.Errorf("expected nil for nil template, got %v", result)
	}
}

func TestFillArchivePlaceholders_EmptyTemplate(t *testing.T) {
	tool := &manifest.Tool{Name: "test"}
	result := FillArchivePlaceholders(tool, "1.0.0", []manifest.Archive{})
	if result != nil {
		t.Errorf("expected nil for empty template, got %v", result)
	}
}

func TestFillArchivePlaceholders_NoPlaceholderInURL(t *testing.T) {
	tool := &manifest.Tool{Name: "test"}
	template := []manifest.Archive{
		{
			URL: "https://static.example.com/tool-latest.zip",
		},
	}
	result := FillArchivePlaceholders(tool, "1.0.0", template)
	if len(result) != 1 {
		t.Fatalf("expected 1 archive, got %d", len(result))
	}
	// URL should remain unchanged (no placeholder to replace).
	if result[0].URL != "https://static.example.com/tool-latest.zip" {
		t.Errorf("URL should remain unchanged, got %q", result[0].URL)
	}
	// SHA256 should still be zero placeholder.
	if result[0].SHA256 != zeroSHA256 {
		t.Errorf("SHA256 should be zero placeholder, got %q", result[0].SHA256)
	}
}

// TestFillArchivePlaceholders_RustRollingInstaller — rust 类滚动安装器：
// URL 恒定（不含版本），版本通过 install_args {{version}} 注入。
// buildArchiveTemplate 应认可该模板，FillArchivePlaceholders 应保留
// URL 原样 + install_args 模板，供安装器注入版本（Spec 修复）。
func TestFillArchivePlaceholders_RustRollingInstaller(t *testing.T) {
	existing := manifest.Archive{
		OS:          "windows",
		Arch:        "amd64",
		Format:      "exe",
		URL:         "https://static.rust-lang.org/rustup/dist/x86_64-pc-windows-msvc/rustup-init.exe",
		InstallArgs: []string{"-y", "--no-modify-path", "--default-toolchain", "{{version}}"},
		Strip:       0,
	}

	template, ok := buildArchiveTemplate("1.97.1", []manifest.Archive{existing})
	if !ok {
		t.Fatal("rust rolling-installer template should be usable (install_args carries {{version}})")
	}
	if template[0].URL != existing.URL {
		t.Errorf("URL changed for rolling installer: %q", template[0].URL)
	}

	filled := FillArchivePlaceholders(&manifest.Tool{Name: "rust"}, "1.98.0", template)
	if len(filled) != 1 {
		t.Fatalf("expected 1 filled archive, got %d", len(filled))
	}
	if filled[0].URL != existing.URL {
		t.Errorf("filled URL = %q, want unchanged %q", filled[0].URL, existing.URL)
	}
	if filled[0].SHA256 != zeroSHA256 {
		t.Errorf("SHA256 = %q, want zero placeholder", filled[0].SHA256)
	}
	if len(filled[0].InstallArgs) != 4 || filled[0].InstallArgs[3] != "{{version}}" {
		t.Errorf("InstallArgs not preserved with version injection: %v", filled[0].InstallArgs)
	}
}

// TestBuildArchiveTemplate_ConstantURLNoArgs — URL 恒定且无 {{version}}
// 注入参数：模板不可用（保持原语义，防误用固定 URL）。
func TestBuildArchiveTemplate_ConstantURLNoArgs(t *testing.T) {
	existing := manifest.Archive{
		URL: "https://static.example.com/tool-latest.exe",
	}
	if _, ok := buildArchiveTemplate("1.0.0", []manifest.Archive{existing}); ok {
		t.Error("constant URL without install_args injection should NOT be a usable template")
	}
}

// TestBuildArchiveTemplate_PlainStillWorks — 常规 URL 占位模板回归
func TestBuildArchiveTemplate_PlainStillWorks(t *testing.T) {
	existing := manifest.Archive{
		URL: "https://example.com/download/1.0.0/tool-1.0.0.zip",
	}
	template, ok := buildArchiveTemplate("1.0.0", []manifest.Archive{existing})
	if !ok {
		t.Fatal("versioned URL template should be usable")
	}
	if template[0].URL != "https://example.com/download/{version}/tool-{version}.zip" {
		t.Errorf("template URL = %q", template[0].URL)
	}
}
