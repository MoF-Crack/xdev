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
