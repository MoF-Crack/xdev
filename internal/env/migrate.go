package env

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/xmdemt/xdev/internal/util"
)

// LegacyDirInfo 描述一个发现的旧版目录
type LegacyDirInfo struct {
	Path    string `json:"path"`    // 旧目录完整路径，如 E:\XimuLocalDev\xmdevenvmt
	NewPath string `json:"newPath"` // 对应的新目录路径，如 E:\XDev
}

// ScanLegacyDir 扫描所有驱动器，查找 X:\XimuLocalDev\xmdevenvmt 目录是否存在。
// 返回所有发现的旧版目录列表。
func ScanLegacyDir() []LegacyDirInfo {
	if runtime.GOOS != "windows" {
		return nil
	}

	var found []LegacyDirInfo

	// 扫描 A: 到 Z: 所有驱动器
	for c := 'A'; c <= 'Z'; c++ {
		drive := string(c) + ":"
		// 检查驱动器是否存在
		if _, err := os.Stat(drive + string(os.PathSeparator)); err != nil {
			continue
		}

		legacyPath := filepath.Join(drive, "XimuLocalDev", "xmdevenvmt")
		fi, err := os.Stat(legacyPath)
		if err != nil || !fi.IsDir() {
			continue
		}

		// 验证它看起来像 xdev 主目录（包含 install/ 或 config.yaml）
		looksValid := false
		if _, err := os.Stat(filepath.Join(legacyPath, "install")); err == nil {
			looksValid = true
		}
		if _, err := os.Stat(filepath.Join(legacyPath, "config.yaml")); err == nil {
			looksValid = true
		}
		if !looksValid {
			continue
		}

		newPath := filepath.Join(drive, "XDev")
		found = append(found, LegacyDirInfo{
			Path:    legacyPath,
			NewPath: newPath,
		})
	}

	return found
}

// MigrateLegacyDir 将旧目录下的子目录内容迁移到新版本目录。
// 迁移逻辑：
//   - install/ → 将旧 install/ 下的工具目录复制到新 install/
//   - cache/ → 合并到新 cache/
//   - downloads/ → 合并到新 downloads/
//   - shims/ → 跳过（新版本会重新生成）
//   - env/ → 合并到新 env/
//   - manifests/ → 跳过（新版本自带）
//   - config.yaml → 跳过（新版本有自己的配置）
//   - current/ → 跳过（通过 xdev use 重新激活）
//
// 迁移完成后删除旧目录，并清理 PATH 中的旧条目。
func MigrateLegacyDir(legacyDir, newDir string) error {
	// Security (F-10): legacyDir 迁移后会被 RemoveAll —— 校验绝对路径
	// 且不等于目标目录，避免误删。
	if legacyDir == "" || !filepath.IsAbs(legacyDir) {
		return fmt.Errorf("legacy dir must be absolute: %q", legacyDir)
	}
	if filepath.Clean(legacyDir) == filepath.Clean(newDir) {
		return fmt.Errorf("legacy dir must differ from target dir: %q", legacyDir)
	}

	// 确保新目录存在
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		return fmt.Errorf("failed to create new directory: %w", err)
	}

	// 需要迁移的子目录（合并模式）
	mergeDirs := []string{"install", "cache", "downloads", "env"}

	// 第六轮审计（M1）：CopyDir 对不可解析/断链条目的「跳过」必须先于源删除被
	// 汇总可见（票 09/N-01 同型——app_migration.go 的本地工具迁移已消费
	// CopyDirReport，此调用点此前仍在用丢弃计数的 CopyDir）。清理/跳过计数在
	// RemoveAll(legacyDir) 之前落日志：一旦旧目录被删，跳过的内容即不可恢复。
	totalSkipped := 0
	totalFiles := 0

	for _, sub := range mergeDirs {
		srcDir := filepath.Join(legacyDir, sub)
		dstDir := filepath.Join(newDir, sub)

		// 检查源目录是否存在
		srcInfo, err := os.Stat(srcDir)
		if err != nil || !srcInfo.IsDir() {
			continue
		}

		// 确保目标目录存在
		if err := os.MkdirAll(dstDir, 0o755); err != nil {
			return fmt.Errorf("failed to create target directory %s: %w", dstDir, err)
		}

		// 读取源目录下的条目
		entries, err := os.ReadDir(srcDir)
		if err != nil {
			return fmt.Errorf("failed to read source directory %s: %w", srcDir, err)
		}

		for _, entry := range entries {
			srcPath := filepath.Join(srcDir, entry.Name())
			dstPath := filepath.Join(dstDir, entry.Name())

			// 检查目标路径是否已存在同名条目，避免覆盖
			if _, err := os.Stat(dstPath); err == nil {
				// 目标已存在，跳过（不覆盖）
				continue
			}

			if entry.IsDir() {
				files, skipped, err := util.CopyDirReport(srcPath, dstPath, nil)
				if err != nil {
					return fmt.Errorf("failed to copy directory %s to %s: %w", srcPath, dstPath, err)
				}
				totalFiles += files
				if skipped > 0 {
					totalSkipped += skipped
					log.Printf("[migrate] WARNING: %s → %s 复制时跳过 %d 个无法解析/断链的条目（详见 [copydir] 日志）——旧目录删除后这些内容不可恢复", srcPath, dstPath, skipped)
				}
			} else {
				if err := util.CopyFile(srcPath, dstPath); err != nil {
					return fmt.Errorf("failed to copy file %s to %s: %w", srcPath, dstPath, err)
				}
				totalFiles++
			}
		}
	}

	// 删除旧目录
	//
	// M1：若存在被跳过的条目，此处不再无条件删除旧目录——断链条目本身虽是死
	// 指针，但自引用/循环型 junction 的内容可能仍可达；把决定权交给迁移调用方
	//（GUI 弹窗确认）或至少要求人工清理。返回带哨兵前缀的错误，调用方可以按
	// 前缀识别并在确认后重跑。
	if totalSkipped > 0 {
		return fmt.Errorf("MIGRATE_SKIPPED_ENTRIES: 旧目录 %s 暂不删除——复制阶段跳过了 %d 个无法解析/断链的条目（共迁移 %d 个文件）；请检查 [copydir] 日志确认这些条目无需保留，处理完后重跑迁移", legacyDir, totalSkipped, totalFiles)
	}
	if err := os.RemoveAll(legacyDir); err != nil {
		// 删除失败不影响迁移结果，仅记录
		// 可能有文件被锁定
		_ = err
	}

	// 尝试删除父目录 XimuLocalDev（如果为空）
	parentDir := filepath.Dir(legacyDir)
	os.Remove(parentDir)

	// 清理用户 PATH 中旧的 xmdevenvmt 相关条目
	cleanupLegacyPath(legacyDir)

	return nil
}

// cleanupLegacyPath 清理用户 PATH 中旧版 xmdevenvmt 目录相关的条目
//
// Ticket 10 (audit-fixes / P0-11)：读改写全程走 withUserPathLock。注意
// 本文件无 build tag（全平台编译）而 withUserPathLock 只在 Windows 实现
// （win_path_other.go 提供直通 stub）——锁调用放在既有 GOOS guard 内，
// stub 防未来跨平台漏网。
func cleanupLegacyPath(legacyDir string) {
	if runtime.GOOS != "windows" {
		return
	}
	_ = withUserPathLock(func() error {
		cleanupLegacyPathLocked(legacyDir)
		return nil
	})
}

func cleanupLegacyPathLocked(legacyDir string) {
	// 读取当前用户 PATH
	current, err := ReadUserPath()
	if err != nil {
		return
	}

	// 将旧目录路径标准化（小写，反斜杠）
	legacyLower := strings.ToLower(filepath.Clean(legacyDir))
	if !strings.HasSuffix(legacyLower, "\\") {
		legacyLower += "\\"
	}

	parts := strings.Split(current, ";")
	var kept []string
	changed := false

	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			changed = true
			continue
		}
		// 检查是否包含旧目录路径
		normalized := strings.ToLower(filepath.Clean(trimmed))
		if strings.HasPrefix(normalized, legacyLower) {
			changed = true
			continue
		}
		// 也检查 \ximulocaldev\xmdevenvmt 模式
		if strings.Contains(normalized, "\\ximulocaldev\\xmdevenvmt") {
			changed = true
			continue
		}
		kept = append(kept, trimmed)
	}

	if changed {
		newPath := strings.Join(kept, ";")
		if err := WriteUserPath(newPath); err == nil {
			BroadcastEnvironmentChange()
		}
	}
}

// getCommonInstallPaths / isVersionLike 已收口到 internal/toolmeta
// （Spec 033 R4/R3）：env 侧统一调用 toolmeta.CommonInstallPaths /
// toolmeta.IsVersionLike，见 scan_local.go 与版本解析路径。
