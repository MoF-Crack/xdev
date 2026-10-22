package util

import (
	"os"
	"path/filepath"
	"sort"
)

// LooksLikeVersionDirName reports whether a directory name under
// install/<tool>/ should be treated as an installed version.
//
// 20261021 审计 P1-4：`xdev list`、GUI、`findOtherVersion` 过去都只判
// `IsDir()`，于是 install/ 下的辅助目录被当成版本显示（本机实测：
// Godot/export_templates、go/gopath、gradle/unsafe-version-blocked、
// llvm/lld_is_a_generic_driver._）。已有的 GUI `isComparableVersion`
// 只服务更新路径，且要求“数字开头”，会误杀合法版本 `go1.22.10`。
//
// 判据（**枚举/展示用，不是安全校验**）：
//   - 非空、SafeName 合法（拒绝 `..`、分隔符等）；
//   - 不是 `.stale-*` / `.tmp-*` 残留；
//   - 至少包含一个 ASCII 数字。
//
// 第三条同时满足两类真实语料：
//   - 语义化版本：`1.2.2-alpha`、`2026.2.0.1`；
//   - 带工具前缀的版本：`go1.22.10`（数字开头判据会误杀）。
//
// 而辅助目录（`gopath`、`export_templates`、`unsafe-version-blocked`、
// `lld_is_a_generic_driver._`）都不含数字，被正确排除。
func LooksLikeVersionDirName(name string) bool {
	if name == "" || IsResidueName(name) {
		return false
	}
	if _, err := SafeName(name); err != nil {
		return false
	}
	for _, r := range name {
		if r >= '0' && r <= '9' {
			return true
		}
	}
	return false
}

// ListVersionDirNames 返回 install/<tool>/ 下“看起来是版本”的子目录名，
// 按字典序排序（输出稳定，便于 CLI/GUI 展示与测试断言）。
//
// 用 os.Stat 而不是 DirEntry.IsDir：版本目录本身通常是真实目录，但跟随
// 链接的成本很低，且能避免第十轮 P0（junction 的 IsDir()=false）那类判据
// 在不同调用点各写一遍的回归。
//
// 目录不存在/不可读时返回错误，由调用方决定“无版本”还是“读取失败”。
func ListVersionDirNames(toolDir string) ([]string, error) {
	entries, err := os.ReadDir(toolDir)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if !LooksLikeVersionDirName(e.Name()) {
			continue
		}
		fi, err := os.Stat(filepath.Join(toolDir, e.Name()))
		if err != nil || !fi.IsDir() {
			continue
		}
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out, nil
}
