package gui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreviewCustomToolVersions_RejectsUnsafeURL(t *testing.T) {
	app := newTestAppWithDirs(t, t.TempDir())
	for _, raw := range []string{
		"file:///etc/passwd",
		"gopher://internal",
		"ftp://example.com/list",
	} {
		got := app.PreviewCustomToolVersions(raw, "html", `(\d+\.\d+)`)
		if got == nil || got.Success || got.Versions != nil {
			t.Errorf("PreviewCustomToolVersions(%q) = %+v, want failure result", raw, got)
		}
		if got != nil && got.Message == "" {
			t.Errorf("unsafe URL rejection must carry a reason, got empty Message for %q", raw)
		}
	}
}

func TestPreviewCustomToolVersions_RejectsGitHubURLForm(t *testing.T) {
	app := newTestAppWithDirs(t, t.TempDir())
	got := app.PreviewCustomToolVersions("https://evil.example/x", "github", "")
	if got == nil || got.Success || got.Versions != nil {
		t.Errorf("github preview with full URL should be rejected, got %+v", got)
	}
}

func TestValidateIsolateCommand(t *testing.T) {
	ok := [][]string{
		{"node", "--version"},
		{"python", "--version"},
		{"go", "version"},
		{"java", "-version"},
		{"g++", "--version"},
		{"git", "--help"},
	}
	for _, cmd := range ok {
		if err := validateIsolateCommand(cmd); err != nil {
			t.Errorf("validateIsolateCommand(%v) should be OK: %v", cmd, err)
		}
	}

	denied := [][]string{
		{},
		{""},
		{"cmd", "/c", "whoami"},
		{"powershell", "-Command", "Get-Process"},
		{"bash", "-c", "id"},
		{"python", "-c", "print(1)"},
		{"node", "-e", "process.exit(0)"},
		{`C:\Windows\System32\cmd.exe`},
		{"../evil"},
		{"sh"},
		{"wsl"},
		{"osascript"},
	}
	for _, cmd := range denied {
		if err := validateIsolateCommand(cmd); err == nil {
			t.Errorf("validateIsolateCommand(%v) should be rejected", cmd)
		}
	}
}

func TestIsolateExec_RejectsArbitraryCommandWithoutSetup(t *testing.T) {
	app := newTestAppWithDirs(t, t.TempDir())
	if _, err := app.IsolateExec([]string{"cmd", "/c", "whoami"}); err == nil {
		t.Fatal("IsolateExec should reject cmd /c")
	}
}

func TestExecRemoteInstance_RejectsArbitraryCommand(t *testing.T) {
	app := newTestAppWithDirs(t, t.TempDir())
	if _, err := app.ExecRemoteInstance("docker", "id", []string{"sh", "-c", "id"}); err == nil {
		t.Fatal("ExecRemoteInstance should reject sh -c")
	}
}

func TestImportProfile_RejectsProtectedEnvVars(t *testing.T) {
	tmp := t.TempDir()
	app := newTestAppWithDirs(t, tmp)
	profilePath := filepath.Join(tmp, "profile.json")
	payload := map[string]any{
		"pathMode": "shim",
		"proxy": map[string]any{
			"http":    "",
			"https":   "",
			"noProxy": "",
			"enabled": false,
		},
		"tools": []any{},
		"envVars": []map[string]string{
			{"name": "PATH", "value": "C:\\evil", "level": "user"},
			{"name": "FOO;rm", "value": "x", "level": "user"},
			{"name": "NOT_IN_WHITELIST_HOME", "value": tmp, "level": "user"},
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(profilePath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	result := app.ImportProfile(profilePath)
	if !result.Success {
		t.Fatalf("import should succeed overall, got %+v", result)
	}
	joined := strings.Join(result.Errors, "\n")
	if !strings.Contains(joined, "PATH") {
		t.Errorf("expected PATH rejection in errors, got %v", result.Errors)
	}
	if !strings.Contains(joined, "FOO;rm") && !strings.Contains(joined, "invalid") {
		t.Errorf("expected invalid env name rejection, got %v", result.Errors)
	}
}

func TestSetProxyConfig_RejectsFileScheme(t *testing.T) {
	app := newTestAppWithDirs(t, t.TempDir())
	if err := app.SetProxyConfig("file:///c:/windows/win.ini", "", "", true); err == nil {
		t.Fatal("SetProxyConfig should reject file://")
	}
}
