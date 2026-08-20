package gui

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestIsLocalInstallForScan(t *testing.T) {
	xdevDirs := []string{`C:\XDev`, `C:\XDev\install`, `C:\XDev\shims`}
	tests := []struct {
		name string
		info localInfo
		want bool
	}{
		{"empty path excluded", localInfo{Path: "", Source: "local"}, false},
		{"xdev managed source excluded", localInfo{Path: `C:\Program Files\Go\bin\go.exe`, Source: "xdev"}, false},
		{"path inside XMDevDir excluded", localInfo{Path: filepath.Join(`C:\XDev`, "current", "go"), Source: "local"}, false},
		{"path inside InstallDir excluded", localInfo{Path: filepath.Join(`C:\XDev`, "install", "go", "1.22.0", "bin", "go.exe"), Source: "local"}, false},
		{"path inside ShimDir excluded", localInfo{Path: filepath.Join(`C:\XDev`, "shims", "go.exe"), Source: "local"}, false},
		{"genuine local install kept", localInfo{Path: filepath.Join(`D:\Env`, "Go", "bin", "go.exe"), Source: "local"}, true},
		{"unix local install kept", localInfo{Path: `/usr/local/go/bin/go`, Source: "local"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isLocalInstallForScan(tt.info, xdevDirs)
			if got != tt.want {
				t.Errorf("isLocalInstallForScan(%+v) = %v, want %v", tt.info, got, tt.want)
			}
		})
	}

	if !isLocalInstallForScan(localInfo{Path: `C:\XDev\install\go\bin\go.exe`, Source: "local"}, nil) {
		t.Error("empty xdev dirs must not exclude a local install")
	}
}

// TestDetectLocalToolPath_NpmInstalledCodex is a regression test for the
// "codex local scan" report: codex is now embedded in the ChatGPT desktop
// app, which provides NO CLI. Local detection is generic (shim name lookup
// on PATH / npm global bin) — there is no hardcoded "codex" keyword. The
// scan must find a standalone codex CLI installed via npm (@openai/codex),
// and must keep working when the npm global bin is not on PATH.
func TestDetectLocalToolPath_NpmInstalledCodex(t *testing.T) {
	tmp := t.TempDir()
	origPATH := os.Getenv("PATH")

	// PATH-based detection: npm global bin on PATH. npm install -g
	// @openai/codex creates %APPDATA%\npm\codex.cmd on Windows (LookPath
	// resolves it via PATHEXT) and a plain "codex" file on Unix. tmp is
	// prepended to PATH so this wins even if a real codex exists.
	shimName := "codex"
	if runtime.GOOS == "windows" {
		shimName = "codex.cmd"
		t.Setenv("PATHEXT", ".COM;.EXE;.BAT;.CMD")
	}
	shimFile := filepath.Join(tmp, shimName)
	if err := os.WriteFile(shimFile, []byte("shim"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+origPATH)
	if got := detectLocalToolPath("codex", []string{"codex"}, ""); got != shimFile {
		t.Errorf("PATH-based: detectLocalToolPath = %q, want %q", got, shimFile)
	}

	// npm-global-bin fallback: npm bin dir NOT on PATH. Uses a unique
	// name so a real codex on the machine PATH cannot shadow the check.
	t.Run("npm-global-bin fallback", func(t *testing.T) {
		name := fmt.Sprintf("codex-fb%d", time.Now().UnixNano())
		var npmBin, file string
		if runtime.GOOS == "windows" {
			npmBin = filepath.Join(tmp, "npm")
			file = filepath.Join(npmBin, name+".cmd") // npm shim on Windows
			t.Setenv("APPDATA", tmp)
		} else {
			npmBin = filepath.Join(tmp, ".npm-global", "bin")
			file = filepath.Join(npmBin, name)
			t.Setenv("HOME", tmp)
		}
		if err := os.MkdirAll(npmBin, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte("shim"), 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", origPATH)
		if got := detectLocalToolPath(name, []string{name}, ""); got != file {
			t.Errorf("npm-bin fallback: detectLocalToolPath = %q, want %q", got, file)
		}
	})
}
