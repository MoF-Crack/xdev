package discovery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/remote"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

func testTool(name string, remoteType string) *manifest.Tool {
	t := &manifest.Tool{
		Name:    name,
		VersionList: []manifest.Version{},
	}
	t.Remote.Type = remoteType
	return t
}

func testToolWithRemote(name, remoteType, remoteURL, pattern string) *manifest.Tool {
	t := &manifest.Tool{
		Name:    name,
		VersionList: []manifest.Version{},
	}
	t.Remote.Type = remoteType
	t.Remote.URL = remoteURL
	t.Remote.Pattern = pattern
	return t
}

// TestSnifferRegistrySmoke ensures the registry initialises with 5
// sniffers and that Sniff returns an error for an unsupported tool.
func TestSnifferRegistrySmoke(t *testing.T) {
	reg := newSnifferRegistry()
	if reg == nil {
		t.Fatal("newSnifferRegistry returned nil")
	}
	if len(reg.sniffers) != 5 {
		t.Fatalf("expected 5 sniffers, got %d", len(reg.sniffers))
	}

	// Unsupported tool — should return nil, nil.
	tool := testTool("unsupported", "none")
	ctx := context.Background()
	versions, err := reg.Sniff(ctx, tool)
	if err != nil {
		t.Fatalf("unexpected error for unsupported tool: %v", err)
	}
	if versions != nil {
		t.Fatalf("expected nil for unsupported tool, got %d versions", len(versions))
	}
}

// ---------------------------------------------------------------------------
// GitHub sniffer
// ---------------------------------------------------------------------------

func TestGitHubSniffer_Supports(t *testing.T) {
	s := &gitHubSniffer{}
	if !s.Supports(testTool("go", "github")) {
		t.Error("expected Supports=true for github remote type")
	}
	if s.Supports(testTool("go", "json")) {
		t.Error("expected Supports=false for non-github remote type")
	}
}

func TestGitHubSniffer_Sniff(t *testing.T) {
	// This test requires access to the real GitHub API.
	// For isolated unit tests, we skip unless -tags=integration is used.
	// The remote package's httpClient is unexported, so we can't inject
	// an httptest server from here.  Integration tests live in
	// the remote package itself.
	t.Skip("Skipping GitHub sniffer test (requires real network; use go test -tags=integration for full tests)")
}

// ---------------------------------------------------------------------------
// JSON sniffer
// ---------------------------------------------------------------------------

func TestJSONSniffer_Supports(t *testing.T) {
	s := &jsonSniffer{}
	if !s.Supports(testTool("test", "json")) {
		t.Error("expected Supports=true for json remote type")
	}
	if s.Supports(testTool("test", "html")) {
		t.Error("expected Supports=false for non-json remote type")
	}
}

func TestJSONSniffer_Sniff(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"data": []string{"1.2.3", "1.2.2", "1.2.1"},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	tool := testToolWithRemote("testjson", "json", "", "")
	tool.Remote.Type = "json"
	tool.Remote.URL = server.URL + "/api"
	tool.Remote.Path = "data"

	s := &jsonSniffer{}
	ctx := context.Background()
	versions, err := s.Sniff(ctx, tool)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(versions) != 3 {
		t.Fatalf("expected 3 versions, got %d", len(versions))
	}
	if versions[0].Version != "1.2.3" {
		t.Errorf("expected first version 1.2.3, got %s", versions[0].Version)
	}
	if !versions[0].Unverified {
		t.Error("expected Unverified=true for all discovered versions (FR-009)")
	}
	if versions[0].Source != "json" {
		t.Errorf("expected source=json, got %s", versions[0].Source)
	}
}

// ---------------------------------------------------------------------------
// HTML sniffer
// ---------------------------------------------------------------------------

func TestHTMLSniffer_Supports(t *testing.T) {
	s := &htmlSniffer{}
	if !s.Supports(testTool("test", "html")) {
		t.Error("expected Supports=true for html remote type")
	}
	if s.Supports(testTool("test", "json")) {
		t.Error("expected Supports=false for non-html remote type")
	}
}

func TestHTMLSniffer_Sniff(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := `<html><body>
			<a href="/download/v1.0.0">v1.0.0</a>
			<a href="/download/v1.1.0">v1.1.0</a>
		</body></html>`
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(html))
	}))
	defer server.Close()

	tool := testToolWithRemote("testhtml", "html", "", "")
	tool.Remote.Type = "html"
	tool.Remote.URL = server.URL + "/page"
	tool.Remote.Pattern = `v(\d+\.\d+\.\d+)`

	s := &htmlSniffer{}
	ctx := context.Background()
	versions, err := s.Sniff(ctx, tool)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("expected 2 versions, got %d", len(versions))
	}
	if versions[0].Version != "1.0.0" {
		t.Errorf("expected first version 1.0.0, got %s", versions[0].Version)
	}
}

// ---------------------------------------------------------------------------
// npm sniffer
// ---------------------------------------------------------------------------

func TestNPMsSniffer_Supports(t *testing.T) {
	s := &npmSniffer{}
	if !s.Supports(testTool("typescript", "npm")) {
		t.Error("expected Supports=true for npm remote type")
	}
	if s.Supports(testTool("typescript", "json")) {
		t.Error("expected Supports=false for non-npm remote type")
	}
}

// TestNPMsSniffer_Sniff is the regression test for 023-A2-01: an npm
// remote tool manifest must route through the npm sniffer and return
// discovered versions. The npm registry base is stubbed via the remote
// package test seam (SetNPMRegistryBaseForTest) so no real network is
// needed (023-R3: RemoteSource.URL is never honored).
func TestNPMsSniffer_Sniff(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"name": "typescript",
			"dist-tags": map[string]string{
				"latest": "7.0.2",
			},
			"versions": map[string]interface{}{
				"7.0.2": map[string]interface{}{"version": "7.0.2"},
				"5.7.3": map[string]interface{}{"version": "5.7.3"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	old := remote.SetNPMRegistryBaseForTest(server.URL)
	defer remote.SetNPMRegistryBaseForTest(old)

	tool := testToolWithRemote("typescript", "npm", "", "")
	tool.Remote.Type = "npm"
	tool.Remote.Package = "typescript"

	s := &npmSniffer{}
	ctx := context.Background()
	versions, err := s.Sniff(ctx, tool)
	if err != nil {
		t.Fatalf("npm sniffer: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("expected 2 versions, got %d", len(versions))
	}
	if versions[0].Version != "7.0.2" {
		t.Errorf("expected latest 7.0.2 first, got %s", versions[0].Version)
	}
	if !versions[0].Unverified {
		t.Error("expected Unverified=true for all discovered versions (FR-009)")
	}
	if versions[0].Source != "npm" {
		t.Errorf("expected source=npm, got %s", versions[0].Source)
	}
}

// TestSnifferRegistryRoutesNPMToNPMSniffer verifies the scheduler registry
// picks the npm sniffer for an npm-remote tool (not archive_pattern).
func TestSnifferRegistryRoutesNPMToNPMSniffer(t *testing.T) {
	reg := newSnifferRegistry()
	tool := testTool("typescript", "npm")
	// 找到 npm 之前的 sniffer 都不应支持；npm sniffer 应支持。
	matched := false
	for i, s := range reg.sniffers {
		if s.Supports(tool) {
			if _, ok := s.(*npmSniffer); !ok {
				t.Fatalf("sniffer[%d] (%T) matched npm tool; want npmSniffer", i, s)
			}
			matched = true
			break
		}
	}
	if !matched {
		t.Fatal("no sniffer matched an npm-remote tool — npm sniffer not registered")
	}
}

// ---------------------------------------------------------------------------
// SnifferRegistry priority
// ---------------------------------------------------------------------------

func TestSnifferRegistryPriority(t *testing.T) {
	// A tool with explicit github remote should use github sniffer,
	// not archive_pattern.
	tool := testToolWithRemote("test", "github", "owner/repo", "")
	// Also add an archive (to tempt archive_pattern).
	tool.VersionList = []manifest.Version{
		{
			Version: "1.0.0",
			Archives: []manifest.Archive{
				{
					URL: "https://github.com/owner/repo/releases/download/v1.0.0/file.zip",
				},
			},
		},
	}

	reg := newSnifferRegistry()
	// The first sniffer is github, which should match.
	firstSniffer := reg.sniffers[0]
	if _, ok := firstSniffer.(*gitHubSniffer); !ok {
		t.Fatal("expected first sniffer to be gitHubSniffer")
	}

	// Verify the github sniffer supports this tool.
	if !firstSniffer.Supports(tool) {
		t.Error("expected gitHubSniffer to support tool with github remote")
	}

	// Without remote, archive_pattern should match.
	noRemoteTool := &manifest.Tool{
		Name: "test",
		VersionList: []manifest.Version{
			{
				Version: "1.0.0",
				Archives: []manifest.Archive{
					{
						URL: "https://github.com/owner/repo/releases/download/v1.0.0/file.zip",
					},
				},
			},
		},
	}
	// The last sniffer is archive_pattern.
	lastSniffer := reg.sniffers[4]
	if _, ok := lastSniffer.(*archivePatternSniffer); !ok {
		t.Fatal("expected last sniffer to be archivePatternSniffer")
	}
	if !lastSniffer.Supports(noRemoteTool) {
		t.Error("expected archivePatternSniffer to support tool with github archive URLs")
	}
}
