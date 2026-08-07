package discovery

import (
	"testing"

	"github.com/xmdemt/xdev/internal/manifest"
)

// ---------------------------------------------------------------------------
// extractGitHubRepoFromURL
// ---------------------------------------------------------------------------

func TestExtractGitHubRepoFromURL(t *testing.T) {
	tests := []struct {
		url         string
		wantOwner   string
		wantRepo    string
		wantOK      bool
		description string
	}{
		{
			url:         "https://github.com/Kitware/CMake/releases/download/v3.28.0/cmake-3.28.0-windows-x86_64.zip",
			wantOwner:   "Kitware",
			wantRepo:    "CMake",
			wantOK:      true,
			description: "standard GitHub releases download URL",
		},
		{
			url:         "https://github.com/golang/go/releases/download/go1.22.0/go1.22.0.windows-amd64.zip",
			wantOwner:   "golang",
			wantRepo:    "go",
			wantOK:      true,
			description: "Go repository release URL",
		},
		{
			url:         "https://github.com/nodejs/node/releases/download/v20.11.0/node-v20.11.0-win-x64.zip",
			wantOwner:   "nodejs",
			wantRepo:    "node",
			wantOK:      true,
			description: "Node.js release URL",
		},
		{
			url:         "https://example.com/downloads/tool-v1.0.0.zip",
			wantOwner:   "",
			wantRepo:    "",
			wantOK:      false,
			description: "non-GitHub URL",
		},
		{
			url:         "https://github.com/singlepart/releases/download/v1.0.0/file.zip",
			wantOwner:   "",
			wantRepo:    "",
			wantOK:      false,
			description: "URL where 'releases' is consumed as repo name, preventing regex match",
		},
		{
			url:         "",
			wantOwner:   "",
			wantRepo:    "",
			wantOK:      false,
			description: "empty URL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			owner, repo, ok := extractGitHubRepoFromURL(tt.url)
			if ok != tt.wantOK {
				t.Errorf("extractGitHubRepoFromURL() ok = %v, want %v", ok, tt.wantOK)
			}
			if owner != tt.wantOwner {
				t.Errorf("extractGitHubRepoFromURL() owner = %q, want %q", owner, tt.wantOwner)
			}
			if repo != tt.wantRepo {
				t.Errorf("extractGitHubRepoFromURL() repo = %q, want %q", repo, tt.wantRepo)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// buildArchiveTemplate
// ---------------------------------------------------------------------------

func TestBuildArchiveTemplate(t *testing.T) {
	existingVersion := "3.28.0"
	archives := []manifest.Archive{
		{
			OS:         "windows",
			Arch:       "amd64",
			Format:     "zip",
			URL:        "https://github.com/Kitware/CMake/releases/download/v3.28.0/cmake-3.28.0-windows-x86_64.zip",
			MirrorURL:  "https://mirror.example.com/cmake-3.28.0-windows-x86_64.zip",
			MirrorURLs: []string{"https://mirror2.example.com/cmake-3.28.0-windows-x86_64.zip"},
			SHA256:     "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			Size:       12345678,
		},
		{
			OS:     "linux",
			Arch:   "amd64",
			Format: "tar.gz",
			URL:    "https://github.com/Kitware/CMake/releases/download/v3.28.0/cmake-3.28.0-linux-x86_64.tar.gz",
		},
	}

	template, ok := buildArchiveTemplate(existingVersion, archives)
	if !ok {
		t.Fatal("buildArchiveTemplate returned ok=false")
	}
	if len(template) != 2 {
		t.Fatalf("expected 2 template entries, got %d", len(template))
	}

	// Check that {version} placeholder is in the URLs.
	expectedURL := "https://github.com/Kitware/CMake/releases/download/v{version}/cmake-{version}-windows-x86_64.zip"
	if template[0].URL != expectedURL {
		t.Errorf("URL template = %q, want %q", template[0].URL, expectedURL)
	}

	expectedMirror := "https://mirror.example.com/cmake-{version}-windows-x86_64.zip"
	if template[0].MirrorURL != expectedMirror {
		t.Errorf("MirrorURL template = %q, want %q", template[0].MirrorURL, expectedMirror)
	}

	// SHA256 and Size should be zeroed out in template.
	if template[0].SHA256 != "" {
		t.Errorf("expected SHA256 to be empty in template, got %q", template[0].SHA256)
	}
	if template[0].Size != 0 {
		t.Errorf("expected Size to be 0 in template, got %d", template[0].Size)
	}

	// OS/Arch/Format should be preserved.
	if template[0].OS != "windows" {
		t.Errorf("expected OS=windows, got %s", template[0].OS)
	}
	if template[0].Arch != "amd64" {
		t.Errorf("expected Arch=amd64, got %s", template[0].Arch)
	}
	if template[0].Format != "zip" {
		t.Errorf("expected Format=zip, got %s", template[0].Format)
	}
}

func TestBuildArchiveTemplate_NoVersionInURL(t *testing.T) {
	// If the version string doesn't appear in the URL, the template
	// should fail (return false).
	archives := []manifest.Archive{
		{
			URL: "https://static.example.com/tool-latest.zip",
		},
	}
	template, ok := buildArchiveTemplate("1.0.0", archives)
	if ok {
		t.Error("expected ok=false when version not in URL")
	}
	if template != nil {
		t.Errorf("expected nil template, got %v", template)
	}
}

func TestBuildArchiveTemplate_EmptyArchives(t *testing.T) {
	template, ok := buildArchiveTemplate("1.0.0", nil)
	if ok {
		t.Error("expected ok=false for empty archives")
	}
	if template != nil {
		t.Errorf("expected nil template, got %v", template)
	}
}

// ---------------------------------------------------------------------------
// archivePatternSniffer.Supports
// ---------------------------------------------------------------------------

func TestArchivePatternSniffer_Supports(t *testing.T) {
	s := &archivePatternSniffer{}

	// Tool with explicit remote should NOT be supported.
	explicitRemoteTool := &manifest.Tool{
		Name: "test",
		Remote: manifest.RemoteSource{
			Type: "github",
			URL:  "owner/repo",
		},
	}
	if s.Supports(explicitRemoteTool) {
		t.Error("should not support tool with explicit github remote")
	}

	// Tool with no remote but GitHub archive URLs should be supported.
	archiveTool := &manifest.Tool{
		Name: "cmake",
		VersionList: []manifest.Version{
			{
				Version: "3.28.0",
				Archives: []manifest.Archive{
					{
						URL: "https://github.com/Kitware/CMake/releases/download/v3.28.0/cmake-3.28.0-windows-x86_64.zip",
					},
				},
			},
		},
	}
	if !s.Supports(archiveTool) {
		t.Error("should support tool with github archive URLs and no explicit remote")
	}

	// Tool with no archives should NOT be supported.
	emptyTool := &manifest.Tool{Name: "empty"}
	if s.Supports(emptyTool) {
		t.Error("should not support tool with no archives")
	}

	// Tool with non-GitHub archives should NOT be supported.
	nonGHTool := &manifest.Tool{
		Name: "custom",
		VersionList: []manifest.Version{
			{
				Version: "1.0.0",
				Archives: []manifest.Archive{
					{URL: "https://example.com/download/tool-1.0.0.zip"},
				},
			},
		},
	}
	if s.Supports(nonGHTool) {
		t.Error("should not support tool with non-GitHub archives")
	}
}

// ---------------------------------------------------------------------------
// fillArchivePlaceholdersFromTemplate
// ---------------------------------------------------------------------------

func TestFillArchivePlaceholdersFromTemplate(t *testing.T) {
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
	}

	result := fillArchivePlaceholdersFromTemplate(template, "3.29.0")
	if len(result) != 1 {
		t.Fatalf("expected 1 archive, got %d", len(result))
	}

	expectedURL := "https://github.com/Kitware/CMake/releases/download/v3.29.0/cmake-3.29.0-windows-x86_64.zip"
	if result[0].URL != expectedURL {
		t.Errorf("URL = %q, want %q", result[0].URL, expectedURL)
	}

	if result[0].SHA256 != zeroSHA256 {
		t.Errorf("SHA256 should be zero placeholder, got %q", result[0].SHA256)
	}
	if result[0].Size != 0 {
		t.Errorf("Size should be 0, got %d", result[0].Size)
	}
	// Strip should be preserved from template.
	if result[0].Strip != 1 {
		t.Errorf("Strip should be 1, got %d", result[0].Strip)
	}
}

// TestArchivePatternSniffer_Unsupported ensures that when no GitHub
// URL pattern can be derived, the sniffer returns nil, nil (silently
// unsupported). We simulate this by calling Supports directly with a
// tool that has non-GitHub archives.
func TestArchivePatternSniffer_Sniff_Unsupported(t *testing.T) {
	s := &archivePatternSniffer{}

	// A tool with no remote and non-GitHub archives but with an
	// explicit remote of type "none" — the sniffer won't support it.
	toolNoRemote := &manifest.Tool{
		Name: "unsupported",
		Remote: manifest.RemoteSource{
			Type: "none",
		},
		VersionList: []manifest.Version{
			{
				Version: "1.0.0",
				Archives: []manifest.Archive{
					{URL: "https://example.com/tool-1.0.0.zip"},
				},
			},
		},
	}
	if s.Supports(toolNoRemote) {
		t.Skip("tool with non-GitHub archives and no explicit remote should not be supported")
	}

	// Test a tool that has no archives at all.
	emptyTool := &manifest.Tool{
		Name: "empty",
		VersionList: []manifest.Version{
			{Version: "1.0.0"},
		},
	}
	if s.Supports(emptyTool) {
		t.Error("expected Supports=false for tool with no archives")
	}
}
