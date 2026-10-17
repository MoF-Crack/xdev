// dependency_integrity_test.go — F-27 guards.
//
// Pre-fix the required-plugin check only did os.Stat + IsDir, so a dependency
// whose install directory existed but was EMPTY (or missing its binaries —
// e.g. a truncated extraction) was treated as satisfied. The dependent tool
// then installed against a broken prerequisite. The rest of the installer
// already decides "installed" via VerifyInstallIntegrity (InstallWithProgress,
// InstallMany), so the dependency gate was the odd one out.
package installer

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/manifest"
)

// TestInstall_DepDirExistsButEmptyFails asserts an empty dependency directory
// no longer satisfies the requirement.
func TestInstall_DepDirExistsButEmptyFails(t *testing.T) {
	flutter := &depPlugin{name: "flutter", requires: []string{"java"}}
	java := &depPlugin{name: "java"}
	ins := newDepTestInstaller(t, flutter, java)

	// The dependency directory EXISTS but is empty (a truncated extraction
	// leaves exactly this shape).
	emptyPath := java.InstallDir(ins.cfg.Core.InstallDir, java.DefaultVersion())
	if err := os.MkdirAll(emptyPath, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := ins.InstallWithProgress(context.Background(), flutter, "1.0.0", nil, InstallOptions{NoActivate: true})
	if err == nil {
		t.Fatal("install proceeded although the required dependency is an empty directory")
	}
	if !strings.Contains(err.Error(), "not installed") {
		t.Errorf("error = %v, want a 'required plugin ... is not installed' error", err)
	}
}

// TestInstall_DepDirExistsButNoBinaryFails asserts a dependency directory that
// is non-empty but lacks the declared shim binary also fails.
func TestInstall_DepDirExistsButNoBinaryFails(t *testing.T) {
	flutter := &depPlugin{name: "flutter", requires: []string{"java"}}
	java := &depPlugin{name: "java"}
	ins := newDepTestInstaller(t, flutter, java)

	depPath := java.InstallDir(ins.cfg.Core.InstallDir, java.DefaultVersion())
	// Non-empty, but no "java" binary anywhere the integrity check looks.
	if err := os.MkdirAll(filepath.Join(depPath, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(depPath, "docs", "README.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := ins.InstallWithProgress(context.Background(), flutter, "1.0.0", nil, InstallOptions{NoActivate: true})
	if err == nil {
		t.Fatal("install proceeded although the dependency has no declared binary")
	}
	if !strings.Contains(err.Error(), "not installed") {
		t.Errorf("error = %v, want a 'required plugin ... is not installed' error", err)
	}
}

// TestInstall_DepWithBinarySucceeds guards the positive path: a dependency
// tree that satisfies VerifyInstallIntegrity still passes the gate.
func TestInstall_DepWithBinarySucceeds(t *testing.T) {
	flutter := &depPlugin{name: "flutter", requires: []string{"java"}}
	java := &depPlugin{name: "java"}
	ins := newDepTestInstaller(t, flutter, java)

	// Build a real, integrity-satisfying dependency install tree.
	makeFakeInstalledVersion(t, ins.cfg.Core.InstallDir, "java", java.DefaultVersion())

	// Cache a valid archive so flutter's own install can proceed offline.
	const toolName = "flutter"
	cacheFile := filepath.Join(ins.cfg.Core.CacheDir, toolName+"-1.0.0.zip")
	makeFakeZip(t, cacheFile, toolName)
	realSHA := fileSHA(t, cacheFile)
	flutter.archive = &manifest.Archive{
		OS: runtime.GOOS, Arch: runtime.GOARCH, Format: "zip",
		URL: "https://example.test/flutter-1.0.0.zip", SHA256: realSHA, Strip: 1,
	}

	_, err := ins.InstallWithProgress(context.Background(), flutter, "1.0.0", nil, InstallOptions{NoActivate: true})
	if err != nil {
		t.Fatalf("install failed although the dependency is intact: %v", err)
	}
}

// TestInstall_DepMissingDirStillFails guards the original behaviour.
func TestInstall_DepMissingDirStillFails(t *testing.T) {
	flutter := &depPlugin{name: "flutter", requires: []string{"java"}}
	java := &depPlugin{name: "java"}
	ins := newDepTestInstaller(t, flutter, java)

	_, err := ins.InstallWithProgress(context.Background(), flutter, "1.0.0", nil, InstallOptions{NoActivate: true})
	if err == nil {
		t.Fatal("expected an error for a missing dependency")
	}
	if !strings.Contains(err.Error(), "not installed") {
		t.Errorf("error = %v, want 'required plugin \"java\" is not installed'", err)
	}
}
