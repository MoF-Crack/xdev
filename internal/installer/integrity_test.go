package installer

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/plugin"
)

// shimPlugin is a minimal Plugin that only carries a name and shim list,
// enough for VerifyInstallIntegrity. manifest is optional (nil → no
// gui_binary candidate).
type shimPlugin struct {
	name     string
	shims    []string
	manifest *manifest.Tool
}

func (s *shimPlugin) Name() string                          { return s.name }
func (s *shimPlugin) Shims() []string                       { return s.shims }
func (s *shimPlugin) DisplayName() string                   { return s.name }
func (s *shimPlugin) Description() string                   { return "" }
func (s *shimPlugin) Homepage() string                      { return "" }
func (s *shimPlugin) Category() string                      { return "language" }
func (s *shimPlugin) Manifest() *manifest.Tool              { return s.manifest }
func (s *shimPlugin) ResolveVersion(string) (string, error) { return s.name, nil }
func (s *shimPlugin) DefaultVersion() string                { return s.name }
func (s *shimPlugin) ArchiveFor(string) (*manifest.Archive, error) {
	return nil, nil
}
func (s *shimPlugin) InstallDir(root, v string) (string, error) {
	return filepath.Join(root, s.name, v), nil
}
func (s *shimPlugin) EnvVars(string) map[string]string { return nil }

// PathAppend 实现 plugin.Plugin（P1，2026-10-25）。
func (s *shimPlugin) PathAppend(string) []string { return nil }
func (s *shimPlugin) PostInstall(context.Context, string) error {
	return nil
}
func (s *shimPlugin) Deactivate(string) error  { return nil }
func (s *shimPlugin) RequiredPlugin() []string { return nil }

var _ plugin.Plugin = (*shimPlugin)(nil)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestVerifyInstallIntegrity_MissingDir(t *testing.T) {
	p := &shimPlugin{name: "tool", shims: []string{"tool"}}
	res := VerifyInstallIntegrity(p, filepath.Join(t.TempDir(), "nope"))
	if res.OK {
		t.Error("OK = true for missing dir, want false")
	}
	if res.Reason == "" {
		t.Error("Reason should be non-empty for missing dir")
	}
}

func TestVerifyInstallIntegrity_EmptyDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tool", "1.0.0")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := &shimPlugin{name: "tool", shims: []string{"tool"}}
	res := VerifyInstallIntegrity(p, dir)
	if res.OK {
		t.Error("OK = true for empty dir, want false")
	}
	if res.Reason == "" {
		t.Error("Reason should be non-empty for empty dir")
	}
}

func TestVerifyInstallIntegrity_MissingShim(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tool", "1.0.0")
	writeFile(t, filepath.Join(dir, "README.txt"), "hello")
	p := &shimPlugin{name: "tool", shims: []string{"tool", "toolctl"}}
	res := VerifyInstallIntegrity(p, dir)
	// Neither shim is present → incomplete.
	if res.OK {
		t.Error("OK = true when no shim binary present, want false")
	}
	if len(res.Missing) != 2 || res.Missing[0] != "tool" || res.Missing[1] != "toolctl" {
		t.Errorf("Missing = %v, want [tool toolctl]", res.Missing)
	}
	if res.Reason == "" {
		t.Error("Reason should be non-empty")
	}
}

func TestVerifyInstallIntegrity_PartialShimsOK(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tool", "1.0.0")
	// Only "tool" is present; "toolctl" is an alias/script not shipped
	// as a separate binary. At least one shim found → intact.
	writeFile(t, filepath.Join(dir, "bin", "tool.exe"), "MZ")
	p := &shimPlugin{name: "tool", shims: []string{"tool", "toolctl"}}
	res := VerifyInstallIntegrity(p, dir)
	if !res.OK {
		t.Errorf("OK = false, want true (at least one shim present): %s", res.Reason)
	}
}

func TestVerifyInstallIntegrity_ShimAtRoot(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tool", "1.0.0")
	writeFile(t, filepath.Join(dir, "tool"), "#!/bin/sh\n")
	p := &shimPlugin{name: "tool", shims: []string{"tool"}}
	res := VerifyInstallIntegrity(p, dir)
	if !res.OK {
		t.Errorf("OK = false, want true (shim at install root): %s", res.Reason)
	}
}

func TestVerifyInstallIntegrity_ShimInBin(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tool", "1.0.0")
	writeFile(t, filepath.Join(dir, "bin", "tool"), "#!/bin/sh\n")
	p := &shimPlugin{name: "tool", shims: []string{"tool"}}
	res := VerifyInstallIntegrity(p, dir)
	if !res.OK {
		t.Errorf("OK = false, want true (shim in bin/): %s", res.Reason)
	}
}

func TestVerifyInstallIntegrity_ShimExeOnWindows(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tool", "1.0.0")
	// The .exe variant must be found on any platform (the candidate list
	// always includes it) so installs extracted on Windows are recognized.
	writeFile(t, filepath.Join(dir, "bin", "tool.exe"), "MZ")
	p := &shimPlugin{name: "tool", shims: []string{"tool"}}
	res := VerifyInstallIntegrity(p, dir)
	if !res.OK {
		t.Errorf("OK = false, want true (shim.exe in bin/): %s", res.Reason)
	}
}

func TestVerifyInstallIntegrity_NestedLayout(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tool", "1.0.0")
	// Go-style nested layout: <install>/go/bin/go is found, but gofmt
	// is missing. Since at least one declared shim is present, the
	// install is considered intact (gofmt may be an optional alias).
	writeFile(t, filepath.Join(dir, "go", "bin", "go"), "binary")
	p := &shimPlugin{name: "go", shims: []string{"go", "gofmt"}}
	res := VerifyInstallIntegrity(p, dir)
	if !res.OK {
		t.Errorf("OK = false, want true (at least one shim present): %s", res.Reason)
	}
}

func TestVerifyInstallIntegrity_NestedLayoutComplete(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tool", "1.0.0")
	writeFile(t, filepath.Join(dir, "go", "bin", "go"), "binary")
	writeFile(t, filepath.Join(dir, "go", "bin", "gofmt"), "binary")
	p := &shimPlugin{name: "go", shims: []string{"go", "gofmt"}}
	res := VerifyInstallIntegrity(p, dir)
	if !res.OK {
		t.Errorf("OK = false, want true (nested layout complete): %s", res.Reason)
	}
}

func TestVerifyInstallIntegrity_NoShimsNonEmpty(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tool", "1.0.0")
	writeFile(t, filepath.Join(dir, "lib", "something.dll"), "data")
	p := &shimPlugin{name: "tool", shims: nil}
	res := VerifyInstallIntegrity(p, dir)
	if !res.OK {
		t.Errorf("OK = false, want true (no shims, non-empty dir): %s", res.Reason)
	}
}

// TestVerifyInstallIntegrity_RealLayout simulates the on-disk layout
// produced by the full install workflow test (strip=1 → bin at root).
func TestVerifyInstallIntegrity_RealLayout(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "fakego", "1.0.0")
	bin := "fakego"
	if runtime.GOOS == "windows" {
		bin = "fakego.exe"
	}
	writeFile(t, filepath.Join(dir, "bin", bin), "#!/bin/sh\n")
	p := &shimPlugin{name: "fakego", shims: []string{"fakego"}}
	res := VerifyInstallIntegrity(p, dir)
	if !res.OK {
		t.Errorf("OK = false, want true (real extracted layout): %s", res.Reason)
	}
}

// TestVerifyInstallIntegrity_RustupLayout verifies the rustup install
// layout is recognized: binaries live under .cargo/bin and
// .rustup/toolchains/<ver>-<triple>/bin, not in bin/ — with only one of
// the four rust shims present, the install is still intact.
func TestVerifyInstallIntegrity_RustupLayout(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "rust", "1.97.1")

	// rustup-init -y 之后的典型布局。
	writeFile(t, filepath.Join(dir, ".cargo", "bin", "rustc.exe"), "binary")
	writeFile(t, filepath.Join(dir, ".rustup", "toolchains", "1.97.1-x86_64-pc-windows-msvc", "bin", "rustc.exe"), "binary")

	p := &shimPlugin{name: "rust", shims: []string{"rustup", "rustc", "cargo", "rustdoc"}}
	res := VerifyInstallIntegrity(p, dir)
	if !res.OK {
		t.Errorf("OK = false, want true (rustup layout, rustc present): %s (missing=%v)", res.Reason, res.Missing)
	}
}

// TestVerifyInstallIntegrity_RustupLayoutNoneMissing verifies the rustup
// layout alone is NOT enough — at least one shim binary must exist.
func TestVerifyInstallIntegrity_RustupLayoutNoneMissing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "rust", "1.97.1")
	// 只有目录结构、没有二进制。
	writeFile(t, filepath.Join(dir, ".cargo", "bin", "README.txt"), "x")

	p := &shimPlugin{name: "rust", shims: []string{"rustup", "rustc", "cargo", "rustdoc"}}
	res := VerifyInstallIntegrity(p, dir)
	if res.OK {
		t.Error("OK = true, want false (no shim binary anywhere in rustup layout)")
	}
}

// ---------- gui_binary candidate (Spec 023 T-A6) ----------

// TestVerifyInstallIntegrity_GUIBinarySatisfiesShimRule verifies the
// gui_binary candidate satisfies the at-least-one-entry rule when shims
// are declared but missing (e.g. GUI tool whose shim list is incomplete).
func TestVerifyInstallIntegrity_GUIBinarySatisfiesShimRule(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "vscode", "1.0.0")
	// 只有 gui_binary（bin/code.exe），没有任何 shim 二进制。
	writeFile(t, filepath.Join(dir, "bin", "code.exe"), "binary")
	p := &shimPlugin{
		name:  "vscode",
		shims: []string{"code", "code-insiders"},
		manifest: &manifest.Tool{
			Name:       "vscode",
			Shims:      []string{"code", "code-insiders"},
			FormFactor: []string{"gui", "cli"},
			GUIBinary:  "bin/code.exe", // 正斜杠需规范化
		},
	}
	res := VerifyInstallIntegrity(p, dir)
	if !res.OK {
		t.Errorf("OK = false, want true (gui_binary present satisfies at-least-one rule): %s (missing=%v)", res.Reason, res.Missing)
	}
}

func TestVerifyInstallIntegrity_GUIBinaryPresentNoShims(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tool", "1.0.0")
	writeFile(t, filepath.Join(dir, "App.exe"), "binary")
	p := &shimPlugin{
		name: "tool",
		manifest: &manifest.Tool{
			Name:       "tool",
			FormFactor: []string{"gui"},
			GUIBinary:  "App.exe",
		},
	}
	res := VerifyInstallIntegrity(p, dir)
	if !res.OK {
		t.Errorf("OK = false, want true (no shims + non-empty dir): %s", res.Reason)
	}
}

// TestVerifyInstallIntegrity_GUIBinaryMissingNoShims verifies a missing
// gui_binary does NOT fail a no-shim install (non-empty dir rule intact).
func TestVerifyInstallIntegrity_GUIBinaryMissingNoShims(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tool", "1.0.0")
	writeFile(t, filepath.Join(dir, "lib", "data.bin"), "x")
	p := &shimPlugin{
		name: "tool",
		manifest: &manifest.Tool{
			Name:       "tool",
			FormFactor: []string{"gui"},
			GUIBinary:  "Missing.app/Contents/MacOS/App",
		},
	}
	res := VerifyInstallIntegrity(p, dir)
	if !res.OK {
		t.Errorf("OK = false, want true (no shims: non-empty dir is OK even when gui_binary missing): %s", res.Reason)
	}
}

func TestVerifyInstallIntegrity_GUIBinaryWithShimsPresent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tool", "1.0.0")
	writeFile(t, filepath.Join(dir, "bin", "tool.exe"), "binary")
	writeFile(t, filepath.Join(dir, "bin", "gui.exe"), "binary")
	p := &shimPlugin{
		name:  "tool",
		shims: []string{"tool"},
		manifest: &manifest.Tool{
			Name:       "tool",
			Shims:      []string{"tool"},
			FormFactor: []string{"gui", "cli"},
			GUIBinary:  "bin/gui.exe",
		},
	}
	res := VerifyInstallIntegrity(p, dir)
	if !res.OK {
		t.Errorf("OK = false, want true (shim + gui_binary both present): %s", res.Reason)
	}
}

// TestVerifyInstallIntegrity_GUIBinaryMissingShimsMissingStillFails
// verifies gui_binary is NOT a hard requirement: no shims found AND no
// gui_binary found still fails when shims are declared.
func TestVerifyInstallIntegrity_GUIBinaryMissingShimsMissingStillFails(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tool", "1.0.0")
	writeFile(t, filepath.Join(dir, "README.md"), "x")
	p := &shimPlugin{
		name:  "tool",
		shims: []string{"tool"},
		manifest: &manifest.Tool{
			Name:       "tool",
			Shims:      []string{"tool"},
			FormFactor: []string{"gui", "cli"},
			GUIBinary:  "bin/gui.exe",
		},
	}
	res := VerifyInstallIntegrity(p, dir)
	if res.OK {
		t.Error("OK = true, want false (no shim and no gui_binary found)")
	}
}

// TestVerifyInstallIntegrity_GUIBinaryUnsafePathIgnored verifies
// absolute / traversal gui_binary values are ignored (never checked).
func TestVerifyInstallIntegrity_GUIBinaryUnsafePathIgnored(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tool", "1.0.0")
	writeFile(t, filepath.Join(dir, "tool.exe"), "binary")
	p := &shimPlugin{
		name:  "tool",
		shims: []string{"tool"},
		manifest: &manifest.Tool{
			Name:       "tool",
			Shims:      []string{"tool"},
			FormFactor: []string{"gui"},
			GUIBinary:  "../../etc/passwd",
		},
	}
	res := VerifyInstallIntegrity(p, dir)
	if !res.OK {
		t.Errorf("OK = false, want true (unsafe gui_binary ignored, shim present): %s", res.Reason)
	}
}

// ---------- B2 L3 review fixes ----------

// TestVerifyInstallIntegrity_GUIBinaryNestedTraversalRejected verifies a
// traversal that does NOT start with ".." (bin/../../outside.exe) is
// rejected after Clean normalization — the external file must NOT satisfy
// integrity even when it exists (B2-A6-01).
func TestVerifyInstallIntegrity_GUIBinaryNestedTraversalRejected(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "tool", "1.0.0")
	writeFile(t, filepath.Join(dir, "README.md"), "x")
	// 外部文件：解析后位于 installPath 之外（tmp/tool/outside.exe 或 tmp/outside.exe）。
	outside := filepath.Join(tmp, "outside.exe")
	writeFile(t, outside, "binary")
	p := &shimPlugin{
		name:  "tool",
		shims: []string{"tool"},
		manifest: &manifest.Tool{
			Name:       "tool",
			Shims:      []string{"tool"},
			FormFactor: []string{"gui"},
			GUIBinary:  "bin/../../outside.exe",
		},
	}
	res := VerifyInstallIntegrity(p, dir)
	if res.OK {
		t.Error("OK = true, want false (nested traversal gui_binary must not escape installPath)")
	}
}

// TestVerifyInstallIntegrity_GUIBinaryCleanedNoEscapeAccepted verifies a
// path with internal ".." that does NOT escape (a/../b/app.exe → b/app.exe)
// is accepted after Clean and checked at the resolved location (B2-A6-01).
func TestVerifyInstallIntegrity_GUIBinaryCleanedNoEscapeAccepted(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tool", "1.0.0")
	// 解析位置 b/app.exe 存在；a/ 目录不存在（Clean 后无需存在）。
	writeFile(t, filepath.Join(dir, "b", "app.exe"), "binary")
	p := &shimPlugin{
		name:  "tool",
		shims: []string{"tool"},
		manifest: &manifest.Tool{
			Name:       "tool",
			Shims:      []string{"tool"},
			FormFactor: []string{"gui"},
			GUIBinary:  "a/../b/app.exe",
		},
	}
	res := VerifyInstallIntegrity(p, dir)
	if !res.OK {
		t.Errorf("OK = false, want true (clean-resolved b/app.exe present): %s", res.Reason)
	}
}

// TestVerifyInstallIntegrity_GUIBinaryNonGUIManifestIgnored verifies a
// CLI/TUI-only manifest's gui_binary does NOT satisfy the at-least-one
// rule — missing CLI shims must fail even if gui_binary exists (B2-A6-02).
func TestVerifyInstallIntegrity_GUIBinaryNonGUIManifestIgnored(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tool", "1.0.0")
	writeFile(t, filepath.Join(dir, "bin", "gui.exe"), "binary")
	p := &shimPlugin{
		name:  "tool",
		shims: []string{"tool"},
		manifest: &manifest.Tool{
			Name:       "tool",
			Shims:      []string{"tool"},
			FormFactor: []string{"cli", "tui"}, // 无 gui
			GUIBinary:  "bin/gui.exe",
		},
	}
	res := VerifyInstallIntegrity(p, dir)
	if res.OK {
		t.Error("OK = true, want false (gui_binary must not satisfy non-gui manifest shim rule)")
	}
}

// TestVerifyInstallIntegrity_GUIBinaryExactPathOnly verifies gui_binary is
// checked at its exact path, NOT via the shim locator's fallback layouts
// (B2-A6-03): a stray <install>/bin/bin/app.exe must not satisfy
// gui_binary "bin/app.exe".
func TestVerifyInstallIntegrity_GUIBinaryExactPathOnly(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tool", "1.0.0")
	// 精确路径 <install>/bin/app.exe 不存在；只有 stray bin/bin/app.exe。
	writeFile(t, filepath.Join(dir, "bin", "bin", "app.exe"), "binary")
	p := &shimPlugin{
		name:  "tool",
		shims: []string{"tool"},
		manifest: &manifest.Tool{
			Name:       "tool",
			Shims:      []string{"tool"},
			FormFactor: []string{"gui"},
			GUIBinary:  "bin/app.exe",
		},
	}
	res := VerifyInstallIntegrity(p, dir)
	if res.OK {
		t.Error("OK = true, want false (exact gui_binary path missing; shim-locator fallback must not apply)")
	}
}

// TestSafeGUIBinary 单测词法防御：绝对路径 / 越界 / Clean 归一化。
func TestSafeGUIBinary(t *testing.T) {
	tests := []struct {
		in   string
		want string // 期望输出；"" 表示拒绝
	}{
		{"bin/code.exe", "bin/code.exe"}, // 正斜杠归一化
		{"", ""},                         // 空
		{"   ", ""},                      // 空白
		{"/etc/passwd", ""},              // 根化路径（两平台均拒绝）
		{"../../outside.exe", ""},        // 开头越界
		{"bin/../../outside.exe", ""},    // 嵌套越界（Clean 后 ../outside.exe）
		{"a/../b/app.exe", "b/app.exe"},  // 内部 .. 不越界 → 归一化接受
		{"bin//app.exe", "bin/app.exe"},  // 重复分隔符
		{"bin/./app.exe", "bin/app.exe"}, // 点段
	}
	for _, tt := range tests {
		got := safeGUIBinary(tt.in)
		want := strings.ReplaceAll(tt.want, "/", string(filepath.Separator))
		if got != want {
			t.Errorf("safeGUIBinary(%q) = %q, want %q", tt.in, got, want)
		}
	}
	// Windows 专用：驱动器绝对路径必须拒绝（Unix 上反斜杠是普通字符，跳过）。
	if runtime.GOOS == "windows" {
		if got := safeGUIBinary(`C:\evil.exe`); got != "" {
			t.Errorf("safeGUIBinary(C:\\evil.exe) = %q, want rejected (absolute)", got)
		}
	}
}
