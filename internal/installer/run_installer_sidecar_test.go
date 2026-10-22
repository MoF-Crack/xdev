// run_installer_sidecar_test.go tests runInstaller's TOFU/sidecar pin
// support and ExtractAndActivate kind routing (rust install fix:
// executable installers must be run, not extracted; empty manifest SHA
// allowed when a matching .sha256 sidecar pin exists).
package installer

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/manifest"
)

// TestRunInstaller_SidecarPinAllowsEmptySHA verifies runInstaller runs a
// cached executable when the manifest SHA is empty but a matching sidecar
// pin exists (rust 1.97.1 rustup-init.exe scenario).
func TestRunInstaller_SidecarPinAllowsEmptySHA(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)
	mr := &mockExecRunner{}
	ins.SetExecRunner(mr)

	content := "fake-installer-binary"
	src := seedCacheFile(t, cfg.Core.CacheDir, "fakerust", "1.0.0", "exe", content)
	realSHA := sha256Hex(t, content)
	// 预写匹配的 sidecar pin。
	if err := os.WriteFile(src+".sha256", []byte(realSHA+"  "+filepath.Base(src)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := &fakePlugin{
		name:  "fakerust",
		shims: []string{},
		archive: &manifest.Archive{
			OS: runtime.GOOS, Arch: runtime.GOARCH, Format: "exe",
			URL: "https://example.test/fakerust-1.0.0.exe", SHA256: "", // TOFU
			InstallKind: manifest.InstallKindExecutable,
		},
	}

	installPath, dirErr := p.InstallDir(cfg.Core.InstallDir, "1.0.0")
	if dirErr != nil {
		t.Fatal(dirErr)
	}
	if err := ins.ExtractAndActivate(p, p.archive, "1.0.0", installPath); err != nil {
		t.Fatalf("ExtractAndActivate failed: %v", err)
	}
	if !mr.called {
		t.Fatal("execRunner was not called for executable kind with sidecar pin")
	}
}

// TestRunInstaller_SidecarPinMismatchRejects verifies an existing but
// mismatching sidecar pin still refuses to run.
func TestRunInstaller_SidecarPinMismatchRejects(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)
	mr := &mockExecRunner{}
	ins.SetExecRunner(mr)

	src := seedCacheFile(t, cfg.Core.CacheDir, "fakerust", "1.0.0", "exe", "real-content")
	// 预写不匹配的 pin。
	if err := os.WriteFile(src+".sha256", []byte(strings.Repeat("1", 64)+"  "+filepath.Base(src)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := &fakePlugin{
		name:  "fakerust",
		shims: []string{},
		archive: &manifest.Archive{
			OS: runtime.GOOS, Arch: runtime.GOARCH, Format: "exe",
			URL: "https://example.test/fakerust-1.0.0.exe", SHA256: "",
			InstallKind: manifest.InstallKindExecutable,
		},
	}

	installPath, dirErr := p.InstallDir(cfg.Core.InstallDir, "1.0.0")
	if dirErr != nil {
		t.Fatal(dirErr)
	}
	err := ins.ExtractAndActivate(p, p.archive, "1.0.0", installPath)
	if err == nil {
		t.Fatal("expected refusal on mismatching sidecar pin")
	}
	if !strings.Contains(err.Error(), "SHA-256 is missing or placeholder") {
		t.Errorf("error = %v, want sidecar-pin refusal", err)
	}
	if mr.called {
		t.Error("execRunner must not be called when pin mismatches")
	}
}

// TestRunInstaller_NoSidecarRejectsEmptySHA verifies an empty manifest SHA
// with no sidecar pin is still refused (security preserved).
func TestRunInstaller_NoSidecarRejectsEmptySHA(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)
	mr := &mockExecRunner{}
	ins.SetExecRunner(mr)

	seedCacheFile(t, cfg.Core.CacheDir, "fakerust", "1.0.0", "exe", "real-content")

	p := &fakePlugin{
		name:  "fakerust",
		shims: []string{},
		archive: &manifest.Archive{
			OS: runtime.GOOS, Arch: runtime.GOARCH, Format: "exe",
			URL: "https://example.test/fakerust-1.0.0.exe", SHA256: "",
			InstallKind: manifest.InstallKindExecutable,
		},
	}

	installPath, dirErr := p.InstallDir(cfg.Core.InstallDir, "1.0.0")
	if dirErr != nil {
		t.Fatal(dirErr)
	}
	err := ins.ExtractAndActivate(p, p.archive, "1.0.0", installPath)
	if err == nil {
		t.Fatal("expected refusal when no sidecar pin exists")
	}
	if mr.called {
		t.Error("execRunner must not be called without any SHA verification")
	}
}

// TestExtractAndActivate_ExecutableDoesNotExtract verifies the GUI install
// path routes executable installers to the runner instead of the extractor
// (regression for "extraction failed: 7z extraction requires 7z").
func TestExtractAndActivate_ExecutableDoesNotExtract(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)
	mr := &mockExecRunner{}
	ins.SetExecRunner(mr)

	// 缓存里只有 exe（非 zip）——若误走 extract 会因格式不符而失败。
	content := "fake-installer-binary"
	seedCacheFile(t, cfg.Core.CacheDir, "fakerust", "1.0.0", "exe", content)
	realSHA := sha256Hex(t, content)

	p := &fakePlugin{
		name:  "fakerust",
		shims: []string{},
		archive: &manifest.Archive{
			OS: runtime.GOOS, Arch: runtime.GOARCH, Format: "exe",
			URL: "https://example.test/fakerust-1.0.0.exe", SHA256: realSHA,
			InstallKind: manifest.InstallKindExecutable,
		},
	}

	installPath, dirErr := p.InstallDir(cfg.Core.InstallDir, "1.0.0")
	if dirErr != nil {
		t.Fatal(dirErr)
	}
	if err := ins.ExtractAndActivate(p, p.archive, "1.0.0", installPath); err != nil {
		t.Fatalf("ExtractAndActivate failed: %v", err)
	}
	if !mr.called {
		t.Fatal("execRunner was not called — executable must be run, not extracted")
	}
	// 运行成功即视为安装完成（无需解压产物）。
	if fi, err := os.Stat(installPath); err != nil || !fi.IsDir() {
		t.Errorf("install dir missing after run: %v", err)
	}
}

// TestRunInstaller_TemplateSubstitution verifies {{version}} / {{tool}} /
// {{install}} placeholders in InstallArgs are substituted before running
// (rolling installers like rustup-init need --default-toolchain <version>;
// silent installers need the target install dir).
func TestRunInstaller_TemplateSubstitution(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)
	mr := &mockExecRunner{}
	ins.SetExecRunner(mr)

	content := "fake-installer-binary"
	seedCacheFile(t, cfg.Core.CacheDir, "fakerust", "1.97.1", "exe", content)
	realSHA := sha256Hex(t, content)

	p := &fakePlugin{
		name:  "fakerust",
		shims: []string{},
		archive: &manifest.Archive{
			OS: runtime.GOOS, Arch: runtime.GOARCH, Format: "exe",
			URL: "https://example.test/fakerust-1.97.1.exe", SHA256: realSHA,
			InstallKind: manifest.InstallKindExecutable,
			InstallArgs: []string{
				"-y", "--no-modify-path", "--default-toolchain", "{{version}}", "--tool", "{{tool}}", "/D={{install}}",
				// 023-A4-01: 同一参数内三个占位符一次替换（顺序无关）。
				"x{{version}}y{{tool}}z{{install}}w",
			},
		},
	}

	installPath, dirErr := p.InstallDir(cfg.Core.InstallDir, "1.97.1")
	if dirErr != nil {
		t.Fatal(dirErr)
	}
	if err := ins.ExtractAndActivate(p, p.archive, "1.97.1", installPath); err != nil {
		t.Fatalf("ExtractAndActivate failed: %v", err)
	}
	if !mr.called {
		t.Fatal("execRunner was not called")
	}
	// {{install}} 必须被替换为真实 installPath（与 runner 的 dir 一致）。
	want := []string{
		"-y", "--no-modify-path", "--default-toolchain", "1.97.1", "--tool", "fakerust", "/D=" + installPath,
		"x1.97.1yfakerustz" + installPath + "w",
	}
	if len(mr.args) != len(want) {
		t.Fatalf("runner args = %v, want %v", mr.args, want)
	}
	for i, w := range want {
		if mr.args[i] != w {
			t.Errorf("args[%d] = %q, want %q (template not substituted?)", i, mr.args[i], w)
		}
	}
	if mr.dir != installPath {
		t.Errorf("runner dir = %q, want %q", mr.dir, installPath)
	}
}

// TestRunInstaller_TemplateSubstitution_SinglePass verifies the replacer is
// single-pass: a placeholder inside a replacement value is NOT expanded
// again (strings.NewReplacer semantics — order-independent, no cascade).
// Here the tool name itself contains the literal "{{version}}".
func TestRunInstaller_TemplateSubstitution_SinglePass(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)
	mr := &mockExecRunner{}
	ins.SetExecRunner(mr)

	name := "fakerust{{version}}"
	content := "fake-installer-binary"
	seedCacheFile(t, cfg.Core.CacheDir, name, "1.0.0", "exe", content)
	realSHA := sha256Hex(t, content)

	p := &fakePlugin{
		name:  name,
		shims: []string{},
		archive: &manifest.Archive{
			OS: runtime.GOOS, Arch: runtime.GOARCH, Format: "exe",
			// URL 基名与缓存名一致 → 不走 alt 复制路径。
			URL: "https://example.test/fakerust{{version}}-1.0.0.exe", SHA256: realSHA,
			InstallKind: manifest.InstallKindExecutable,
			InstallArgs: []string{"{{tool}}", "{{install}}", "{{version}}"},
		},
	}

	installPath, dirErr := p.InstallDir(cfg.Core.InstallDir, "1.0.0")
	if dirErr != nil {
		t.Fatal(dirErr)
	}
	if err := ins.ExtractAndActivate(p, p.archive, "1.0.0", installPath); err != nil {
		t.Fatalf("ExtractAndActivate failed: %v", err)
	}
	if !mr.called {
		t.Fatal("execRunner was not called")
	}
	// {{tool}} → name 本身含 "{{version}}" 字面量：单趟替换不再展开。
	if mr.args[0] != name {
		t.Errorf("args[0] = %q, want %q (replacement value must not be re-scanned)", mr.args[0], name)
	}
	// {{install}} → installPath 也含 "{{version}}" 字面量：原样保留。
	if mr.args[1] != installPath {
		t.Errorf("args[1] = %q, want %q", mr.args[1], installPath)
	}
	if mr.args[2] != "1.0.0" {
		t.Errorf("args[2] = %q, want 1.0.0", mr.args[2])
	}
}

// TestRunInstaller_ExecutesUnderURLBasename verifies rustup-style
// installers are executed under their URL basename (argv[0] role
// dispatch: rustup-init.exe → installer mode), not the xdev cache name
// <tool>-<ver>.<fmt> which would trigger "unknown proxy name".
func TestRunInstaller_ExecutesUnderURLBasename(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)
	mr := &mockExecRunner{}
	ins.SetExecRunner(mr)

	content := "fake-installer-binary"
	src := seedCacheFile(t, cfg.Core.CacheDir, "fakerust", "1.0.0", "exe", content)
	realSHA := sha256Hex(t, content)

	p := &fakePlugin{
		name:  "fakerust",
		shims: []string{},
		archive: &manifest.Archive{
			OS: runtime.GOOS, Arch: runtime.GOARCH, Format: "exe",
			URL:         "https://static.rust-lang.org/rustup/dist/x86_64-pc-windows-msvc/rustup-init.exe",
			SHA256:      realSHA,
			InstallKind: manifest.InstallKindExecutable,
		},
	}

	installPath, dirErr := p.InstallDir(cfg.Core.InstallDir, "1.0.0")
	if dirErr != nil {
		t.Fatal(dirErr)
	}
	if err := ins.ExtractAndActivate(p, p.archive, "1.0.0", installPath); err != nil {
		t.Fatalf("ExtractAndActivate failed: %v", err)
	}
	if !mr.called {
		t.Fatal("execRunner was not called")
	}

	// 必须按 URL 原始文件名（rustup-init.exe）运行，而非 fakerust-1.0.0.exe。
	wantName := filepath.Join(cfg.Core.CacheDir, "rustup-init.exe")
	if mr.name != wantName {
		t.Errorf("runner name = %q, want %q (must run under URL basename)", mr.name, wantName)
	}
	// alt 副本运行后已清理；原始缓存文件保留。
	if _, err := os.Stat(wantName); err == nil {
		t.Error("alt installer copy should be removed after run")
	}
	if _, err := os.Stat(src); err != nil {
		t.Errorf("original cache file should remain: %v", err)
	}
}

// TestRunInstaller_SameBasenameNoCopy verifies installers whose URL
// basename equals the cache name run directly without a copy.
func TestRunInstaller_SameBasenameNoCopy(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)
	mr := &mockExecRunner{}
	ins.SetExecRunner(mr)

	content := "fake-installer-binary"
	src := seedCacheFile(t, cfg.Core.CacheDir, "fakeexe", "1.0.0", "exe", content)
	realSHA := sha256Hex(t, content)

	p := &fakePlugin{
		name:  "fakeexe",
		shims: []string{},
		archive: &manifest.Archive{
			OS: runtime.GOOS, Arch: runtime.GOARCH, Format: "exe",
			URL:         "https://example.test/fakeexe-1.0.0.exe",
			SHA256:      realSHA,
			InstallKind: manifest.InstallKindExecutable,
		},
	}

	installPath, dirErr := p.InstallDir(cfg.Core.InstallDir, "1.0.0")
	if dirErr != nil {
		t.Fatal(dirErr)
	}
	if err := ins.ExtractAndActivate(p, p.archive, "1.0.0", installPath); err != nil {
		t.Fatalf("ExtractAndActivate failed: %v", err)
	}
	if mr.name != src {
		t.Errorf("runner name = %q, want %q (no copy when basenames match)", mr.name, src)
	}
}
