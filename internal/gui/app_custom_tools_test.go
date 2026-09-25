package gui

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/plugin"
)

func init() {
	// Suppress standard log output during tests
	log.SetOutput(io.Discard)
}

// newTestAppWithDirs creates an App with the given temp dir as XMDevDir.
func newTestAppWithDirs(t *testing.T, tmpDir string) *App {
	t.Helper()

	cfg := &config.Config{
		Core: config.CoreConfig{
			XMDevDir:    tmpDir,
			DownloadDir: filepath.Join(tmpDir, "download"),
			InstallDir:  filepath.Join(tmpDir, "install"),
			ShimDir:     filepath.Join(tmpDir, "shims"),
			CacheDir:    filepath.Join(tmpDir, "cache"),
			EnvDir:      filepath.Join(tmpDir, "env"),
			ManifestDir: filepath.Join(tmpDir, "manifests"),
		},
		Path: config.PathConfig{
			Mode: "shim",
		},
		Mirror: map[string]string{},
	}
	cat := &manifest.Catalog{}
	reg := plugin.NewRegistry()
	return NewApp(cfg, cat, reg)
}

// TestAddCustomTool_InvalidNamePath verifies that a tool name containing
// "../" is rejected (T1.5.4).
func TestAddCustomTool_InvalidNamePath(t *testing.T) {
	tmpDir := t.TempDir()
	app := newTestAppWithDirs(t, tmpDir)

	result := app.AddCustomTool("../etc/passwd", "Bad", "https://example.com", "html", `(\d+\.\d+)`, "", "")
	if result.Success {
		t.Fatal("expected AddCustomTool to fail for tool name with ../")
	}
	if result.Message == "" {
		t.Fatal("expected non-empty error message")
	}
	t.Logf("Got expected error: %s", result.Message)

	// Verify no manifest was written
	manifestDir := filepath.Join(tmpDir, "manifests")
	if entries, _ := os.ReadDir(manifestDir); len(entries) > 0 {
		t.Error("no manifest files should exist after failed AddCustomTool")
	}
}

// TestAddCustomTool_InvalidNameUpper verifies that a tool name containing
// uppercase letters is rejected (T1.5.5).
func TestAddCustomTool_InvalidNameUpper(t *testing.T) {
	tmpDir := t.TempDir()
	app := newTestAppWithDirs(t, tmpDir)

	result := app.AddCustomTool("MyTool", "My Tool", "https://example.com", "html", `(\d+\.\d+)`, "", "")
	if result.Success {
		t.Fatal("expected AddCustomTool to fail for uppercase tool name")
	}
	t.Logf("Got expected error: %s", result.Message)
}

// TestAddCustomTool_Valid uses an httptest.Server to mock an HTML source
// and verifies that the manifest is written and config is saved (T1.5.6).
func TestAddCustomTool_Valid(t *testing.T) {
	tmpDir := t.TempDir()
	// Create required subdirs
	for _, d := range []string{"manifests", "cache"} {
		if err := os.MkdirAll(filepath.Join(tmpDir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// Mock HTML server returning version links
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body>
			<a href="tool-2.0.0.zip">2.0.0</a>
			<a href="tool-1.0.0.zip">1.0.0</a>
		</body></html>`))
	}))
	defer server.Close()

	app := newTestAppWithDirs(t, tmpDir)
	app.cfg.Core.ManifestDir = filepath.Join(tmpDir, "manifests")

	// Use pattern that matches the mock HTML
	pattern := `(\d+\.\d+\.\d+)`
	result := app.AddCustomTool("mytool", "My Tool", server.URL, "html", pattern, "", "")
	if !result.Success {
		t.Fatalf("AddCustomTool failed: %s", result.Message)
	}
	if len(result.Versions) == 0 {
		t.Fatal("expected versions to be discovered")
	}
	t.Logf("Discovered versions: %v", result.Versions)

	// Verify manifest file was created
	manifestPath := filepath.Join(tmpDir, "manifests", "mytool.yaml")
	if _, err := os.Stat(manifestPath); err != nil {
		t.Fatalf("manifest not created at %s: %v", manifestPath, err)
	}

	// Verify config was updated
	cfg := app.getCfg()
	if len(cfg.CustomTools) != 1 {
		t.Fatalf("expected 1 custom tool in config, got %d", len(cfg.CustomTools))
	}
	if cfg.CustomTools[0].Name != "mytool" {
		t.Errorf("config CustomTools[0].Name = %q, want %q", cfg.CustomTools[0].Name, "mytool")
	}
	if cfg.CustomTools[0].SourceURL != server.URL {
		t.Errorf("config CustomTools[0].SourceURL = %q, want %q", cfg.CustomTools[0].SourceURL, server.URL)
	}
}

// TestAddCustomTool_InvalidHTTPSourceURL verifies that HTTP (non-HTTPS)
// source URLs are rejected for HTML type when not localhost.
func TestAddCustomTool_InvalidHTTPSourceURL(t *testing.T) {
	tmpDir := t.TempDir()
	app := newTestAppWithDirs(t, tmpDir)

	result := app.AddCustomTool("mytool", "My Tool", "http://insecure.example.com", "html", `(\d+\.\d+)`, "", "")
	if result.Success {
		t.Fatal("expected AddCustomTool to fail for HTTP source URL to non-localhost")
	}
	t.Logf("Got expected error: %s", result.Message)
}

// TestAddCustomTool_EmptySourceURL verifies rejection of empty source URL.
func TestAddCustomTool_EmptySourceURL(t *testing.T) {
	tmpDir := t.TempDir()
	app := newTestAppWithDirs(t, tmpDir)

	result := app.AddCustomTool("mytool", "My Tool", "", "html", `(\d+\.\d+)`, "", "")
	if result.Success {
		t.Fatal("expected AddCustomTool to fail for empty source URL")
	}
	t.Logf("Got expected error: %s", result.Message)
}

// TestRemoveCustomTool_Existing tests removing an existing custom tool
// (T1.5.7).
func TestRemoveCustomTool_Existing(t *testing.T) {
	tmpDir := t.TempDir()
	manifestsDir := filepath.Join(tmpDir, "manifests")
	if err := os.MkdirAll(manifestsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	app := newTestAppWithDirs(t, tmpDir)
	app.cfg.Core.ManifestDir = manifestsDir

	// Pre-populate config with a custom tool
	app.cfgMu.Lock()
	app.cfg.CustomTools = append(app.cfg.CustomTools, config.CustomToolConfig{
		Name:        "testtool",
		DisplayName: "Test Tool",
		SourceURL:   "https://example.com",
		SourceType:  "html",
	})
	app.cfgMu.Unlock()

	// Create a manifest file on disk
	manifestContent := `name: testtool
display_name: Test Tool
versions:
  - version: "1.0.0"
`
	manifestPath := filepath.Join(manifestsDir, "testtool.yaml")
	if err := os.WriteFile(manifestPath, []byte(manifestContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Remove the tool
	err := app.RemoveCustomTool("testtool")
	if err != nil {
		t.Fatalf("RemoveCustomTool failed: %v", err)
	}

	// Verify manifest file was deleted
	if _, err := os.Stat(manifestPath); !os.IsNotExist(err) {
		t.Errorf("manifest should be deleted, got err: %v", err)
	}

	// Verify config entry was removed
	cfg := app.getCfg()
	if len(cfg.CustomTools) != 0 {
		t.Errorf("expected 0 custom tools after removal, got %d", len(cfg.CustomTools))
	}
}

// TestRemoveCustomTool_NonExisting tests that removing a non-existing tool
// returns an error (T1.5.8).
func TestRemoveCustomTool_NonExisting(t *testing.T) {
	tmpDir := t.TempDir()
	app := newTestAppWithDirs(t, tmpDir)

	err := app.RemoveCustomTool("nonexistent")
	if err == nil {
		t.Fatal("expected error when removing non-existing tool")
	}
	t.Logf("Got expected error: %v", err)
}

// TestRemoveCustomTool_InvalidName verifies that invalid names are rejected.
func TestRemoveCustomTool_InvalidName(t *testing.T) {
	tmpDir := t.TempDir()
	app := newTestAppWithDirs(t, tmpDir)

	err := app.RemoveCustomTool("../evil")
	if err == nil {
		t.Fatal("expected error for invalid tool name")
	}
	t.Logf("Got expected error: %v", err)
}

// TestListCustomTools verifies that ListCustomTools returns the correct
// entries from config (T1.5.9).
func TestListCustomTools(t *testing.T) {
	tmpDir := t.TempDir()
	app := newTestAppWithDirs(t, tmpDir)

	// Pre-populate config
	app.cfgMu.Lock()
	app.cfg.CustomTools = []config.CustomToolConfig{
		{
			Name:        "toolalpha",
			DisplayName: "Tool Alpha",
			SourceURL:   "https://alpha.example.com",
			SourceType:  "html",
		},
		{
			Name:        "toolbeta",
			DisplayName: "Tool Beta",
			SourceURL:   "owner/beta-repo",
			SourceType:  "github",
		},
	}
	app.cfgMu.Unlock()

	// Save the config so it persists
	if err := app.cfg.Save(); err != nil {
		t.Fatalf("save config: %v", err)
	}

	tools := app.ListCustomTools()
	if len(tools) != 2 {
		t.Fatalf("expected 2 custom tools, got %d", len(tools))
	}

	// Check first tool
	if tools[0].Name != "toolalpha" {
		t.Errorf("tools[0].Name = %q, want %q", tools[0].Name, "toolalpha")
	}
	if tools[0].SourceType != "html" {
		t.Errorf("tools[0].SourceType = %q, want %q", tools[0].SourceType, "html")
	}

	// Check second tool
	if tools[1].Name != "toolbeta" {
		t.Errorf("tools[1].Name = %q, want %q", tools[1].Name, "toolbeta")
	}
	if tools[1].SourceType != "github" {
		t.Errorf("tools[1].SourceType = %q, want %q", tools[1].SourceType, "github")
	}
}

// TestListCustomTools_Empty verifies that an empty list returns no entries.
func TestListCustomTools_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	app := newTestAppWithDirs(t, tmpDir)

	tools := app.ListCustomTools()
	if len(tools) != 0 {
		t.Errorf("expected empty list, got %d entries", len(tools))
	}
}

// TestPreviewCustomToolVersions_HTML uses an httptest.Server to mock
// an HTML source and verifies version discovery without writing a
// manifest (T1.5.10).
func TestPreviewCustomToolVersions_HTML(t *testing.T) {
	tmpDir := t.TempDir()
	app := newTestAppWithDirs(t, tmpDir)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body>
			<a href="tool-3.0.0.zip">3.0.0</a>
			<a href="tool-2.0.0.zip">2.0.0</a>
			<a href="tool-1.0.0.zip">1.0.0</a>
		</body></html>`))
	}))
	defer server.Close()

	result := app.PreviewCustomToolVersions(server.URL, "html", `(\d+\.\d+\.\d+)`)
	if !result.Success {
		t.Fatalf("PreviewCustomToolVersions failed: %s", result.Message)
	}
	versions := result.Versions
	if len(versions) == 0 {
		t.Fatal("expected versions from PreviewCustomToolVersions")
	}
	t.Logf("Preview returned versions: %v", versions)

	// Verify no manifest was written
	manifestsDir := filepath.Join(tmpDir, "manifests")
	if entries, _ := os.ReadDir(manifestsDir); len(entries) > 0 {
		t.Errorf("no manifest files should exist after preview, found %d", len(entries))
		for _, e := range entries {
			t.Logf("  found: %s", e.Name())
		}
	}
}

// TestPreviewCustomToolVersions_EmptySource verifies a failure result for
// an empty URL (F-2: failures must be observable, not silent empty lists).
func TestPreviewCustomToolVersions_EmptySource(t *testing.T) {
	tmpDir := t.TempDir()
	app := newTestAppWithDirs(t, tmpDir)

	result := app.PreviewCustomToolVersions("", "html", `(\d+\.\d+)`)
	if result == nil || result.Success || result.Versions != nil {
		t.Errorf("expected failure result for empty URL, got %+v", result)
	}
	if result != nil && result.Message == "" {
		t.Error("failure result must carry a reason in Message")
	}
}

// TestConfigCustomToolsRoundTrip verifies that custom tools survive a
// config save/load cycle.
func TestConfigCustomToolsRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	// Create the XMDevDir (config save needs it)
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		t.Fatal(err)
	}

	app := newTestAppWithDirs(t, tmpDir)
	app.cfg.Core.XMDevDir = tmpDir

	// Add a custom tool to config
	app.cfgMu.Lock()
	app.cfg.CustomTools = []config.CustomToolConfig{
		{
			Name:        "persisttest",
			DisplayName: "Persist Test",
			SourceURL:   "https://example.com/tool",
			SourceType:  "html",
			Pattern:     `v(\d+\.\d+\.\d+)`,
		},
	}
	if err := app.cfg.Save(); err != nil {
		t.Fatalf("save config: %v", err)
	}
	app.cfgMu.Unlock()

	// Load the config fresh and verify
	loadedCfg, err := config.Load(tmpDir)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if len(loadedCfg.CustomTools) != 1 {
		t.Fatalf("expected 1 custom tool after reload, got %d", len(loadedCfg.CustomTools))
	}
	if loadedCfg.CustomTools[0].Name != "persisttest" {
		t.Errorf("Name = %q, want %q", loadedCfg.CustomTools[0].Name, "persisttest")
	}
	if loadedCfg.CustomTools[0].SourceType != "html" {
		t.Errorf("SourceType = %q, want %q", loadedCfg.CustomTools[0].SourceType, "html")
	}
}

// TestRemoveCustomTool_FileOnly verifies removal works even when config entry
// exists but manifest file doesn't.
func TestRemoveCustomTool_FileOnly(t *testing.T) {
	tmpDir := t.TempDir()
	manifestsDir := filepath.Join(tmpDir, "manifests")
	if err := os.MkdirAll(manifestsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	app := newTestAppWithDirs(t, tmpDir)
	app.cfg.Core.ManifestDir = manifestsDir

	// Pre-populate config
	app.cfgMu.Lock()
	app.cfg.CustomTools = []config.CustomToolConfig{
		{
			Name:        "orphantool",
			DisplayName: "Orphan Tool",
			SourceURL:   "https://example.com",
			SourceType:  "html",
		},
	}
	if err := app.cfg.Save(); err != nil {
		t.Fatal(err)
	}
	app.cfgMu.Unlock()

	// Remove - should succeed even without manifest file
	err := app.RemoveCustomTool("orphantool")
	if err != nil {
		t.Fatalf("RemoveCustomTool failed: %v", err)
	}

	// Verify config entry was removed
	cfg := app.getCfg()
	if len(cfg.CustomTools) != 0 {
		t.Errorf("expected 0 custom tools, got %d", len(cfg.CustomTools))
	}
}

// TestAddCustomTool_ProtectedEnvVarRejected verifies a custom tool cannot
// declare a protected system env var (F-09 / D12) — validation happens
// before any network/discovery call.
func TestAddCustomTool_ProtectedEnvVarRejected(t *testing.T) {
	tmpDir := t.TempDir()
	app := newTestAppWithDirs(t, tmpDir)

	for _, bad := range []string{"PATH", "Path", "SystemRoot", "TEMP", "COMSPEC"} {
		result := app.AddCustomTool("mytool", "My Tool", "https://example.com", "html", `(\d+\.\d+)`, "", bad)
		if result.Success {
			t.Errorf("AddCustomTool with envVar %q should be rejected (protected system var)", bad)
		}
	}
}

// TestAddCustomTool_InvalidEnvVarSyntaxRejected verifies syntactically
// invalid env var names (shell-injectable) are rejected (F-09 / D12).
func TestAddCustomTool_InvalidEnvVarSyntaxRejected(t *testing.T) {
	tmpDir := t.TempDir()
	app := newTestAppWithDirs(t, tmpDir)

	for _, bad := range []string{"FOO; rm -rf /", "1HOME", "A B", "HOME-"} {
		result := app.AddCustomTool("mytool", "My Tool", "https://example.com", "html", `(\d+\.\d+)`, "", bad)
		if result.Success {
			t.Errorf("AddCustomTool with envVar %q should be rejected (invalid syntax)", bad)
		}
	}
}

// TestAddCustomTool_InvalidShimRejected verifies shim names that would
// escape the shims directory are rejected (F-08 / D11).
func TestAddCustomTool_InvalidShimRejected(t *testing.T) {
	tmpDir := t.TempDir()
	app := newTestAppWithDirs(t, tmpDir)

	for _, bad := range []string{`..\outside`, "a/b", ".", ".."} {
		result := app.AddCustomTool("mytool", "My Tool", "https://example.com", "html", `(\d+\.\d+)`, bad, "")
		if result.Success {
			t.Errorf("AddCustomTool with shim %q should be rejected", bad)
		}
	}
}

// TestAddCustomTool_ValidCustomEnvVarPassesValidation verifies arbitrary
// valid non-protected env var names (e.g. MYTOOL_HOME) pass the LOCAL
// validation stage — the failure (if any) must come from later stages
// (network discovery), not from validation.
func TestAddCustomTool_ValidCustomEnvVarPassesValidation(t *testing.T) {
	tmpDir := t.TempDir()
	app := newTestAppWithDirs(t, tmpDir)

	result := app.AddCustomTool("mytool", "My Tool", "https://example.com", "html", `(\d+\.\d+)`, "mt", "MYTOOL_HOME")
	if strings.Contains(result.Message, "invalid env var") || strings.Contains(result.Message, "invalid shim") {
		t.Fatalf("valid envVar/shim should pass validation, got: %s", result.Message)
	}
}
