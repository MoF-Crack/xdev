package isolator

import (
	"fmt"
	"os"
)

// ensureOverwritable is the C-07 / P1-34 existence gate for the files Setup
// generates (flake.nix, .devcontainer/devcontainer.json).
//
// Before this change Setup overwrote whatever was there and Teardown then
// deleted it, so a project with a hand-written flake.nix or devcontainer.json
// lost it twice over. A pre-existing file belongs to the user: refuse instead
// of replacing it, and let an explicit `--force` (= EnvironmentSpec.Force)
// opt into the overwrite.
//
// 第十轮 P3-3（安全）：符号链接**一律拒绝**，`--force` 也不例外。
//
// 旧实现用 os.Stat（**跟随**链接）判断「已存在」，于是：
//   - 链接目标存在 → 被当成「普通的已存在文件」，`--force` 一放行，
//     nix.go / container.go 的 os.WriteFile（O_TRUNC）就顺着链接写到**项目外**
//     的目标上（审计实测 `victim_overwritten=true flake_still_symlink=true`）；
//   - 链接目标不存在（悬空链接）→ os.Stat 返回 ENOENT，门禁**直接放行**，
//     连 --force 都不需要，os.WriteFile 便在项目外创建文件。
//
// 一个本意只是「覆盖项目内生成文件」的开关，因此变成了「任意文件写」原语。
// 这里改用 Lstat 检查链接自身：symlink（含 junction / reparse point，Go 在
// Windows 上把它们同样报为 ModeSymlink）是重定向，xdev 生成的是**普通文件**，
// 因此它既不是「可以覆盖的用户文件」，也绝不能顺着写。拒绝而不是「删掉链接再写」
// 是刻意的：删除链接会静默改变用户的项目结构，而链接常常是用户有意布置的
// （例如把 flake.nix 指向共享配置），报错让人来决定才安全。
//
// Unreadable/undetermined targets fail closed (the error propagates).
func ensureOverwritable(path string, force bool) error {
	// Lstat 而不是 Stat：必须看**路径自身**是什么，不能跟随链接。
	fi, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("check %s: %w", path, err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is a symbolic link; refusing to write through it (xdev generates a regular file, and following the link would modify a file outside the project) — remove the link and re-run setup", path)
	}
	if fi.IsDir() {
		return fmt.Errorf("%s exists and is a directory; remove it to let xdev generate the file", path)
	}
	if force {
		return nil
	}
	return fmt.Errorf("%s already exists; refusing to overwrite it (re-run with --force to replace the existing file)", path)
}
