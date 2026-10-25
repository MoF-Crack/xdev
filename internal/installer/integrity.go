// integrity.go implements installation integrity verification.
//
// An installation is considered complete when its install directory is
// non-empty and, for plugins that declare shims, at least one primary
// shim binary is present. This catches strip-layout mistakes, interrupted
// extractions, and other partial states that a mere directory-existence
// check would miss.
//
// VerifyInstallIntegrity is used both for:
//   - post-extract hard gate during install (fail the install if broken)
//   - existing-install soft checks in the GUI (prompt repair)
package installer

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/xmdemt/xdev/internal/plugin"
	"github.com/xmdemt/xdev/internal/util"
)

// IntegrityResult is the outcome of VerifyInstallIntegrity.
type IntegrityResult struct {
	// OK is true when the installation is complete.
	OK bool
	// Reason is a human-readable explanation when OK is false.
	Reason string
	// Missing lists the shim binary names that could not be found
	// (empty when the failure is not shim-related).
	Missing []string
}

// VerifyInstallIntegrity checks installPath for a non-empty directory and
// the presence of the plugin's shim binaries.
//
// Binary lookup candidates (Windows + Unix):
//
//	<install>/<shim>, <install>/<shim>.exe
//	<install>/bin/<shim>, <install>/bin/<shim>.exe, <install>/bin/<shim>.cmd
//	<install>/*/bin/<shim>, <install>/*/<shim> (one-level nested layout)
//
// Rules:
//   - missing or empty install dir → !OK
//   - plugin declares no shims: non-empty dir is OK（含 gui 但 gui_binary
//     缺失也不失败——gui_binary 只是"至少一个入口二进制"的额外候选）
//   - plugin declares shims: NONE found (shim 或 gui_binary) → !OK with Missing list
//   - plugin declares shims: at least one found → OK
func VerifyInstallIntegrity(p plugin.Plugin, installPath string) IntegrityResult {
	fi, err := os.Stat(installPath)
	if err != nil || !fi.IsDir() {
		return IntegrityResult{OK: false, Reason: "installation directory is missing"}
	}

	entries, err := os.ReadDir(installPath)
	if err != nil {
		return IntegrityResult{OK: false, Reason: "cannot read installation directory: " + err.Error()}
	}
	if len(entries) == 0 {
		return IntegrityResult{OK: false, Reason: "installation directory is empty"}
	}

	shims := p.Shims()
	if len(shims) == 0 {
		// 无 shims：非空目录即完整（Spec 023 契约，见函数注释）。
		return IntegrityResult{OK: true}
	}

	// gui_binary 作为"至少一个入口二进制"规则的额外候选：仅当有效形态含
	// "gui" 时参与（B2-A6-02：CLI/TUI-only manifest 不得用 gui_binary 补缺
	// shim）。仅检查存在性，非硬性要求——manifest 为 nil（如 Lua 插件/
	// 测试桩）时忽略。
	guiBinary := ""
	if m := p.Manifest(); m != nil {
		for _, f := range m.EffectiveFormFactor() {
			if f == "gui" {
				guiBinary = safeGUIBinary(m.GUIBinary)
				break
			}
		}
	}

	// A tool is considered intact when at least one of its declared shim
	// binaries is present. Many shims are aliases (bunx = bun with a
	// different argv0), platform-specific scripts (python3 on Windows),
	// or wrappers (.cmd files) that may not exist as separate binaries.
	// Requiring ALL shims to be present causes false positives, so we only
	// report incomplete when NONE of the declared shims can be found.
	var missing []string
	foundAny := false
	for _, shim := range shims {
		if findBinary(installPath, shim) {
			foundAny = true
		} else {
			missing = append(missing, shim)
		}
	}
	if guiBinary != "" && guiBinaryExists(installPath, guiBinary) {
		foundAny = true
	}
	if !foundAny {
		return IntegrityResult{
			OK:      false,
			Reason:  "missing binary " + strings.Join(missing, ", "),
			Missing: missing,
		}
	}
	return IntegrityResult{OK: true}
}

// safeGUIBinary 规范化 manifest 的 gui_binary 相对路径（允许正斜杠）并做
// 词法防御（B2-A6-01）：
//   - 绝对路径 → 拒绝；
//   - filepath.Clean 归一化（"a/../b/app.exe" → "b/app.exe"，无逃逸则接受）；
//   - Clean 后仍以 ".." 开头（如 "bin/../../outside.exe" → "../outside.exe"）
//     → 拒绝（Clean 已消解内部 ".."，残留的 ".." 只能位于开头）。
//
// 返回空串表示忽略。最终是否越界由 guiBinaryExists 做解析后校验（纵深防御）。
func safeGUIBinary(guiBinary string) string {
	g := strings.TrimSpace(guiBinary)
	if g == "" {
		return ""
	}
	g = filepath.FromSlash(g)
	// 拒绝绝对路径与根化相对路径（"/etc/passwd" 或 "\x"——manifest 应使用
	// 纯相对路径；Windows 上 IsAbs 不认 "/x" 这种根化路径，需显式拒绝）。
	if filepath.IsAbs(g) || strings.HasPrefix(g, string(filepath.Separator)) {
		return ""
	}
	g = filepath.Clean(g)
	if g == ".." || strings.HasPrefix(g, ".."+string(filepath.Separator)) {
		return ""
	}
	return g
}

// guiBinaryExists 精确检查 <install>/<guiBinary> 存在且为普通文件。
// 不使用 findBinary（shim 定位器会搜索 bin/、嵌套布局等候选，gui_binary
// 是精确相对路径，混用会让 stray 布局误通过——B2-A6-03）。
// 解析后经 util.ValidateSubPath 校验仍在 installPath 内（纵深防御）。
func guiBinaryExists(installPath, guiBinary string) bool {
	full := filepath.Join(installPath, guiBinary)
	if err := util.ValidateSubPath(installPath, full); err != nil {
		return false
	}
	fi, err := os.Stat(full)
	return err == nil && !fi.IsDir()
}

// ensureInstallIntegrity runs VerifyInstallIntegrity after extract/install
// and returns a hard error when the tree is incomplete. Callers should
// treat this as install failure (not soft activation failure).
//
// stripHint is optional archive strip value for a more actionable message
// when the directory is empty (common when strip>=1 but archive is flat).
func ensureInstallIntegrity(p plugin.Plugin, installPath string, stripHint int) error {
	res := VerifyInstallIntegrity(p, installPath)
	if res.OK {
		return nil
	}
	msg := fmt.Sprintf("post-extract integrity check failed for %s at %s: %s", p.Name(), installPath, res.Reason)
	if len(res.Missing) > 0 {
		msg += " (missing: " + strings.Join(res.Missing, ", ") + ")"
	}
	if stripHint > 0 && (res.Reason == "installation directory is empty" || strings.Contains(res.Reason, "missing binary")) {
		msg += fmt.Sprintf("; archive strip=%d may be wrong for this package layout (flat root archives need strip: 0)", stripHint)
	}
	return fmt.Errorf("%s", msg)
}

// findBinary reports whether the named shim binary exists in any of the
// supported candidate locations under installPath.
func findBinary(installPath, shim string) bool {
	for _, cand := range binaryCandidates(installPath, shim) {
		if fi, err := os.Stat(cand); err == nil && !fi.IsDir() {
			return true
		}
	}
	return false
}

// binaryCandidates returns the ordered list of locations where a shim
// binary may live. Order is not significant — any match counts.
func binaryCandidates(installPath, shim string) []string {
	exeSuffix := ""
	if runtime.GOOS == "windows" {
		exeSuffix = ".exe"
	}
	// Round 15：根部的 `.cmd` / `.bat` 包装脚本也是合法入口。
	//
	// 实测（android-ndk r29 windows 包）：zip 根部只有 ndk-build.cmd /
	// ndk-gdb.cmd / ndk-stack.cmd，**没有**无扩展名的 ndk-build；而 manifest
	// 是 strip: 0，解包后它们位于 <install>/android-ndk-r29/*.cmd。旧候选表
	// 只给 bin/ 与 sub/bin/ 配了 .cmd，于是完整的安装树被判 "missing binary"
	// 并回滚——`xdev install android-ndk` 在 Windows 上 100% 失败。
	cands := []string{
		filepath.Join(installPath, shim),
		filepath.Join(installPath, shim+".exe"),
		filepath.Join(installPath, shim+".cmd"),
		filepath.Join(installPath, shim+".bat"),
		filepath.Join(installPath, shim+exeSuffix),
		filepath.Join(installPath, "bin", shim),
		filepath.Join(installPath, "bin", shim+".exe"),
		filepath.Join(installPath, "bin", shim+".cmd"),
		filepath.Join(installPath, "bin", shim+".bat"),
	}

	// One-level nested layout, e.g. <install>/<tool>/bin/<shim> and
	// <install>/<tool>/<shim> (covers archives extracted without strip).
	if entries, err := os.ReadDir(installPath); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			sub := filepath.Join(installPath, e.Name())
			cands = append(cands,
				filepath.Join(sub, "bin", shim),
				filepath.Join(sub, "bin", shim+".exe"),
				filepath.Join(sub, "bin", shim+".cmd"),
				filepath.Join(sub, "bin", shim+".bat"),
				filepath.Join(sub, shim),
				filepath.Join(sub, shim+".exe"),
				// Round 15：一层子目录根部的 Windows 包装脚本
				// （NDK 的 <install>/android-ndk-r29/ndk-build.cmd）。
				filepath.Join(sub, shim+".cmd"),
				filepath.Join(sub, shim+".bat"),
			)
		}
	}

	// rustup 布局：安装器把二进制放到 install/.cargo/bin/（cargo、rustup、
	// rustc、rustdoc），工具链在 install/.rustup/toolchains/<ver>-<triple>/bin/。
	// 这两个目录仅对 rust 等工具存在，对其他工具无害。
	cands = append(cands,
		filepath.Join(installPath, ".cargo", "bin", shim),
		filepath.Join(installPath, ".cargo", "bin", shim+".exe"),
		filepath.Join(installPath, ".cargo", "bin", shim+".cmd"),
	)
	// .rustup/toolchains/*/bin/<shim>(.exe) — 用 glob 匹配一层版本目录。
	if matches, err := filepath.Glob(filepath.Join(installPath, ".rustup", "toolchains", "*", "bin", shim+".exe")); err == nil {
		cands = append(cands, matches...)
	}
	if matches, err := filepath.Glob(filepath.Join(installPath, ".rustup", "toolchains", "*", "bin", shim)); err == nil {
		cands = append(cands, matches...)
	}
	return cands
}
