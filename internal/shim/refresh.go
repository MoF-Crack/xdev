package shim

import (
	"log"
	"os"
	"path/filepath"
	"strings"
)

// RefreshDispatchers 把 shims/ 目录下现存分发器副本重新指向 selfPath。
//
// 缺陷④（shim-alias-case-bug plan §3 / shim-passthrough 部署面闭环）：
// Windows shim 是工具 Activate 时刻由 InstallShim 写入的硬链接（回退 copy）。
// 主二进制自更新与 stale 镜像替换走 rename 语义——新内容落在**新 inode**，
// 旧 inode 被现存硬链接继续持有，分发器版本因此永远滞后（本机实测：
// E:\XDev\xdev.exe 已替换而 shims 仍为 2026-08-29 的 0.3.3-rc.2 构建，
// 无 PATH 透传/别名修复，遮蔽系统真实工具）。EnsureSelfInstalled 等自愈
// 入口在替换镜像后调用本函数做"分发器版本一致性"刷新。
//
// 语义：
//   - 只刷新**已存在**的目录条目（绝不新建文件）；不调用 InstallShim 的
//     孪生写入，逐条目直接调用 installShimWindows / 重指符号链接；
//   - 名称过滤：跳过目录、".old" 运行锁残留、以及非 InstallShim 产物形态
//     的文件（裸名或 .exe 之外的扩展名——如 readme.txt 不碰）；
//   - 幂等：Windows 条目与源同文件（os.SameFile）时为 no-op；Unix 仅重指
//     本工具创建的符号链接，普通文件跳过；copy 型 shim 经一轮刷新后收敛
//     为硬链接（installShimWindows 的 SameFile 检查不再命中 → rename→
//     hard link/copy → 后续轮次 no-op）；
//   - 尽力而为：逐条目失败仅计数返回，不中断其余刷新。
//
// 返回 (refreshed, failed)：refreshed = 实际发生重指的条目数（SameFile
// no-op 不计）；failed = 因文件系统错误未能刷新的条目数。
func RefreshDispatchers(shimDir, selfPath string) (refreshed, failed int) {
	entries, err := os.ReadDir(shimDir)
	if err != nil {
		return 0, 0 // 无 shims 目录（全新 home）：无事可做
	}
	if _, err := os.Stat(selfPath); err != nil {
		return 0, 0 // 源不可用：交回调用方自愈
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(strings.ToLower(name), ".old") {
			continue // installShimWindows 的运行中 exe 残留（Windows 可 rename 不可删）
		}
		if ext := filepath.Ext(name); ext != "" && !strings.EqualFold(ext, ".exe") {
			continue // 非 InstallShim 产物形态（InstallShim 只产生裸名 + <name>.exe 孪生）
		}
		dst := filepath.Join(shimDir, name)
		if os.PathSeparator == '\\' {
			changed, err := refreshWindowsEntry(selfPath, dst)
			if err != nil {
				failed++
				log.Printf("[shim] refresh dispatcher %s: %v", name, err)
				continue
			}
			if changed {
				refreshed++
			}
			continue
		}
		changed, err := refreshUnixEntry(selfPath, dst)
		if err != nil {
			failed++
			log.Printf("[shim] refresh dispatcher %s: %v", name, err)
			continue
		}
		if changed {
			refreshed++
		}
	}
	return refreshed, failed
}

// refreshWindowsEntry 幂等重指一个现存 Windows 分发器条目。
// 返回是否实际发生重指（SameFile no-op 返回 false）。
func refreshWindowsEntry(selfPath, dst string) (bool, error) {
	srcInfo, err := os.Stat(selfPath)
	if err != nil {
		return false, err
	}
	if dstInfo, statErr := os.Stat(dst); statErr == nil && !dstInfo.IsDir() && os.SameFile(srcInfo, dstInfo) {
		return false, nil // 已一致（硬链接同 inode / copy 同轮升级后已收敛）
	}
	return true, installShimWindows(selfPath, dst)
}

// refreshUnixEntry 重指一个现存 Unix 符号链接分发器；普通文件不碰
// （不归属 xdev 的文件不能被覆盖）。返回是否发生替换。
func refreshUnixEntry(selfPath, dst string) (bool, error) {
	info, err := os.Lstat(dst)
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return false, nil // 非本工具符号链接：跳过
	}
	// 符号链接已指向 selfPath 时 no-op（Readlink 对比，避免无谓重建）。
	if cur, err := os.Readlink(dst); err == nil && cur == selfPath {
		return false, nil
	}
	if err := os.Remove(dst); err != nil {
		return false, err
	}
	if err := os.Symlink(selfPath, dst); err != nil {
		return false, err
	}
	return true, nil
}
