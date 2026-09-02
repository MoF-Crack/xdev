package installer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/xmdemt/xdev/internal/manifest"
)

// mockExecRunner records the command the installer would run without
// actually executing anything. It satisfies the execRunner interface.
type mockExecRunner struct {
	called    bool
	name      string
	args      []string
	env       []string
	dir       string
	returnErr error
}

func (m *mockExecRunner) Run(ctx context.Context, name string, args []string, env []string, dir string) error {
	m.called = true
	m.name = name
	m.args = args
	m.env = env
	m.dir = dir
	return m.returnErr
}

// seedCacheFile writes content to the standard cache path
// <cacheDir>/<tool>-<ver>.<format> and returns that path.
func seedCacheFile(t *testing.T, cacheDir, tool, ver, format, content string) string {
	t.Helper()
	p := filepath.Join(cacheDir, tool+"-"+ver+"."+format)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir cache dir: %v", err)
	}
	if err := os.WriteFile(p, []byte(content), 0o755); err != nil {
		t.Fatalf("write cache file %s: %v", p, err)
	}
	return p
}

func sha256Hex(t *testing.T, content string) string {
	t.Helper()
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// TestKindRouting_ArchiveExtractsNotRuns verifies that the default kind
// (archive) routes through extraction and never invokes the exec runner.
func TestKindRouting_ArchiveExtractsNotRuns(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)
	mr := &mockExecRunner{}
	ins.SetExecRunner(mr)

	cacheFile := filepath.Join(cfg.Core.CacheDir, "fakego-1.0.0.zip")
	makeFakeZip(t, cacheFile, "fakego")
	realSHA := fileSHA(t, cacheFile)

	p := &fakePlugin{
		name:  "fakego",
		shims: []string{},
		archive: &manifest.Archive{
			OS: runtime.GOOS, Arch: runtime.GOARCH, Format: "zip",
			URL: "https://example.test/fakego-1.0.0.zip", SHA256: realSHA, Strip: 1,
		},
	}
	res, err := ins.InstallWithProgress(context.Background(), p, "1.0.0", nil, InstallOptions{NoActivate: true})
	if err != nil {
		t.Fatalf("Install failed: %v", err)
	}
	if mr.called {
		t.Error("execRunner must NOT be called for archive kind")
	}
	if _, err := os.Stat(filepath.Join(res.InstallPath, "bin")); err != nil {
		t.Errorf("archive was not extracted (bin/ missing): %v", err)
	}
}

// TestKindRouting_PhaseInstallingForExecutable 验证 executable/script 安装
// 在 install-by-kind 阶段发送 PhaseInstalling（而非 Extracting）——滚动
// 安装器（rustup 等）运行期无确定进度，前端据此显示"正在安装"文案。
func TestKindRouting_PhaseInstallingForExecutable(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)
	mr := &mockExecRunner{}
	ins.SetExecRunner(mr)

	content := "fake-exe-payload"
	seedCacheFile(t, cfg.Core.CacheDir, "fakeexe3", "1.0.0", "exe", content)
	realSHA := sha256Hex(t, content)

	p := &fakePlugin{
		name:  "fakeexe3",
		shims: []string{},
		archive: &manifest.Archive{
			OS: runtime.GOOS, Arch: runtime.GOARCH, Format: "exe",
			URL: "https://example.test/fakeexe3-1.0.0.exe", SHA256: realSHA,
			InstallKind: manifest.InstallKindExecutable,
			InstallArgs: []string{"--silent", "-y"},
		},
	}

	var phases []ProgressPhase
	_, err := ins.InstallWithProgress(context.Background(), p, "1.0.0",
		func(phase ProgressPhase, percent float64, speed, eta string) {
			phases = append(phases, phase)
		}, InstallOptions{NoActivate: true})
	if err != nil {
		t.Fatalf("Install failed: %v", err)
	}
	found := false
	for _, ph := range phases {
		if ph == PhaseInstalling {
			found = true
		}
	}
	if !found {
		t.Errorf("executable install phases = %v, want PhaseInstalling present (no Extracting for rolling installers)", phases)
	}
}

// TestKindRouting_ArchiveEmitsExtracting 回归：archive 仍走 PhaseExtracting。
func TestKindRouting_ArchiveEmitsExtracting(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)
	ins.SetExecRunner(&mockExecRunner{})

	cacheFile := filepath.Join(cfg.Core.CacheDir, "fakego2-1.0.0.zip")
	makeFakeZip(t, cacheFile, "fakego2")
	realSHA := fileSHA(t, cacheFile)

	p := &fakePlugin{
		name:  "fakego2",
		shims: []string{},
		archive: &manifest.Archive{
			OS: runtime.GOOS, Arch: runtime.GOARCH, Format: "zip",
			URL: "https://example.test/fakego2-1.0.0.zip", SHA256: realSHA, Strip: 1,
		},
	}

	var phases []ProgressPhase
	_, err := ins.InstallWithProgress(context.Background(), p, "1.0.0",
		func(phase ProgressPhase, percent float64, speed, eta string) {
			phases = append(phases, phase)
		}, InstallOptions{NoActivate: true})
	if err != nil {
		t.Fatalf("Install failed: %v", err)
	}
	found := false
	for _, ph := range phases {
		if ph == PhaseExtracting {
			found = true
		}
	}
	if !found {
		t.Errorf("archive install phases = %v, want PhaseExtracting present", phases)
	}
}
func TestKindRouting_ExecutableRunsAfterSHA(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)
	mr := &mockExecRunner{}
	ins.SetExecRunner(mr)

	content := "fake-exe-payload"
	seedCacheFile(t, cfg.Core.CacheDir, "fakeexe", "1.0.0", "exe", content)
	realSHA := sha256Hex(t, content)

	p := &fakePlugin{
		name:  "fakeexe",
		shims: []string{},
		archive: &manifest.Archive{
			OS: runtime.GOOS, Arch: runtime.GOARCH, Format: "exe",
			URL: "https://example.test/fakeexe-1.0.0.exe", SHA256: realSHA,
			InstallKind: manifest.InstallKindExecutable,
			InstallArgs: []string{"--silent", "-y"},
		},
	}
	_, err := ins.InstallWithProgress(context.Background(), p, "1.0.0", nil, InstallOptions{NoActivate: true})
	if err != nil {
		t.Fatalf("Install failed: %v", err)
	}
	if !mr.called {
		t.Fatal("execRunner was not called for executable kind")
	}
	wantName := filepath.Join(cfg.Core.CacheDir, "fakeexe-1.0.0.exe")
	if mr.name != wantName {
		t.Errorf("runner name = %q, want %q", mr.name, wantName)
	}
	wantArgs := []string{"--silent", "-y"}
	if len(mr.args) != len(wantArgs) {
		t.Fatalf("runner args len = %d, want %d", len(mr.args), len(wantArgs))
	}
	for i, w := range wantArgs {
		if mr.args[i] != w {
			t.Errorf("runner args[%d] = %q, want %q", i, mr.args[i], w)
		}
	}
}

// TestKindRouting_ScriptRunsAfterSHA verifies that kind=script executes
// the cached script via "sh" (mocked) after SHA verification. The
// script path is the first argument, followed by InstallArgs.
func TestKindRouting_ScriptRunsAfterSHA(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)
	mr := &mockExecRunner{}
	ins.SetExecRunner(mr)

	content := "#!/bin/sh\necho fake"
	seedCacheFile(t, cfg.Core.CacheDir, "fakescript", "1.0.0", "sh", content)
	realSHA := sha256Hex(t, content)

	p := &fakePlugin{
		name:  "fakescript",
		shims: []string{},
		archive: &manifest.Archive{
			OS: runtime.GOOS, Arch: runtime.GOARCH, Format: "sh",
			URL: "https://example.test/fakescript-1.0.0.sh", SHA256: realSHA,
			InstallKind: manifest.InstallKindScript,
			InstallArgs: []string{"-y", "--no-modify-path"},
		},
	}
	_, err := ins.InstallWithProgress(context.Background(), p, "1.0.0", nil, InstallOptions{NoActivate: true})
	if err != nil {
		t.Fatalf("Install failed: %v", err)
	}
	if !mr.called {
		t.Fatal("execRunner was not called for script kind")
	}
	if mr.name != "sh" {
		t.Errorf("runner name = %q, want %q", mr.name, "sh")
	}
	wantScript := filepath.Join(cfg.Core.CacheDir, "fakescript-1.0.0.sh")
	if len(mr.args) < 1 || mr.args[0] != wantScript {
		t.Errorf("first arg = %v, want %q", mr.args, wantScript)
	}
}

// TestRunInstaller_SHAMismatchRejects verifies that runInstaller refuses
// to run a binary whose cached SHA-256 does not match the manifest's
// declared hash. This is the defense-in-depth check inside runInstaller
// itself (the download stage also checks, but this catches tampering
// after download or a corrupted cache).
func TestRunInstaller_SHAMismatchRejects(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)
	mr := &mockExecRunner{}
	ins.SetExecRunner(mr)

	content := "real-content"
	seedCacheFile(t, cfg.Core.CacheDir, "fakeexe", "1.0.0", "exe", content)
	realSHA := sha256Hex(t, content)
	// Declare a hash that differs from the real one.
	wrongSHA := "1" + realSHA[1:]
	if wrongSHA == realSHA {
		wrongSHA = "0" + realSHA[1:]
	}

	arch := &manifest.Archive{
		Format:      "exe",
		SHA256:      wrongSHA,
		InstallKind: manifest.InstallKindExecutable,
	}
	p := &fakePlugin{name: "fakeexe", archive: arch}
	installPath := filepath.Join(cfg.Core.InstallDir, "fakeexe", "1.0.0")
	err := ins.runInstaller(context.Background(), p, arch, "1.0.0", installPath)
	if err == nil {
		t.Fatal("expected error for SHA mismatch, got nil")
	}
	if mr.called {
		t.Error("execRunner must NOT be called when SHA mismatches")
	}
}

// TestRunInstaller_PlaceholderSHARejects verifies that an executable or
// script with an all-zero (placeholder) SHA-256 is refused — running an
// unverified binary is a security risk.
func TestRunInstaller_PlaceholderSHARejects(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)
	mr := &mockExecRunner{}
	ins.SetExecRunner(mr)

	content := "exe-with-placeholder-sha"
	seedCacheFile(t, cfg.Core.CacheDir, "fakeexe", "1.0.0", "exe", content)

	arch := &manifest.Archive{
		Format:      "exe",
		SHA256:      "0000000000000000000000000000000000000000000000000000000000000000",
		InstallKind: manifest.InstallKindExecutable,
	}
	p := &fakePlugin{name: "fakeexe", archive: arch}
	installPath := filepath.Join(cfg.Core.InstallDir, "fakeexe", "1.0.0")
	err := ins.runInstaller(context.Background(), p, arch, "1.0.0", installPath)
	if err == nil {
		t.Fatal("expected error for placeholder SHA, got nil")
	}
	if mr.called {
		t.Error("execRunner must NOT be called when SHA is a placeholder")
	}
}

// TestResolveKind_NilArchive (Spec 035 F10): the exported entry point
// must not panic on nil — GUI preview paths call it defensively.
func TestResolveKind_NilArchive(t *testing.T) {
	if got := ResolveKind(nil); got != manifest.InstallKindArchive {
		t.Errorf("ResolveKind(nil) = %q, want %q", got, manifest.InstallKindArchive)
	}
}

// TestResolveKind_RustupURLHeuristic (Spec 035 F10): the rustup URL
// heuristic must survive the export — unsigned/older manifests without
// an explicit kind still route to script/executable.
func TestResolveKind_RustupURLHeuristic(t *testing.T) {
	script := &manifest.Archive{URL: "https://sh.rustup.rs"}
	if got := ResolveKind(script); got != manifest.InstallKindScript {
		t.Errorf("ResolveKind(rustup URL) = %q, want %q", got, manifest.InstallKindScript)
	}
	exe := &manifest.Archive{URL: "https://example.test/rustup-init.exe"}
	if got := ResolveKind(exe); got != manifest.InstallKindExecutable {
		t.Errorf("ResolveKind(rustup-init URL) = %q, want %q", got, manifest.InstallKindExecutable)
	}
	// Explicit kind wins over the heuristic.
	declared := &manifest.Archive{URL: "https://sh.rustup.rs", InstallKind: manifest.InstallKindArchive}
	if got := ResolveKind(declared); got != manifest.InstallKindArchive {
		t.Errorf("ResolveKind(declared archive) = %q, want %q (explicit kind wins)", got, manifest.InstallKindArchive)
	}
}
