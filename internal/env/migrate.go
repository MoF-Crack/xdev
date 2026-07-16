package env

import (
	"fmt"
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
	// 确保新目录存在
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		return fmt.Errorf("failed to create new directory: %w", err)
	}

	// 需要迁移的子目录（合并模式）
	mergeDirs := []string{"install", "cache", "downloads", "env"}

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

			// 检查目标是否已存在同名条目，避免覆盖
			if _, err := os.Stat(dstPath); err == nil {
				// 目标已存在，跳过（不覆盖）
				continue
			}

			if entry.IsDir() {
				if err := util.CopyDir(srcPath, dstPath); err != nil {
					return fmt.Errorf("failed to copy directory %s to %s: %w", srcPath, dstPath, err)
				}
			} else {
				if err := util.CopyFile(srcPath, dstPath); err != nil {
					return fmt.Errorf("failed to copy file %s to %s: %w", srcPath, dstPath, err)
				}
			}
		}
	}

	// 删除旧目录
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
func cleanupLegacyPath(legacyDir string) {
	if runtime.GOOS != "windows" {
		return
	}

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

// getCommonInstallPaths 返回工具在系统中的常见安装路径
func getCommonInstallPaths(name string) []string {
	var paths []string
	home, _ := os.UserHomeDir()

	if runtime.GOOS == "windows" {
		programFiles := os.Getenv("ProgramFiles")
		programFilesX86 := os.Getenv("ProgramFiles(x86)")
		localAppData := os.Getenv("LOCALAPPDATA")

		switch name {
		case "python", "python3":
			paths = append(paths,
				filepath.Join(localAppData, "Programs", "Python"),
			)
		case "node":
			paths = append(paths,
				filepath.Join(programFiles, "nodejs"),
				filepath.Join(localAppData, "nvm"),
			)
		case "go":
			paths = append(paths,
				filepath.Join(programFiles, "Go"),
				filepath.Join(home, "go"),
				filepath.Join(home, "sdk", "go"),
			)
		case "java":
			paths = append(paths,
				filepath.Join(programFiles, "Java"),
				filepath.Join(programFilesX86, "Java"),
			)
		case "dotnet":
			paths = append(paths,
				filepath.Join(programFiles, "dotnet"),
			)
		case "ruby":
			paths = append(paths,
				filepath.Join(programFiles, "Ruby"),
			)
		case "git":
			paths = append(paths,
				filepath.Join(programFiles, "Git"),
			)
		case "flutter":
			paths = append(paths,
				filepath.Join(localAppData, "flutter"),
			)
		case "android-platform-tools":
			paths = append(paths,
				filepath.Join(home, "AppData", "Local", "Android", "Sdk", "platform-tools"),
			)
		}
	}

	return paths
}

// isVersionLike 检查字符串是否看起来像版本号
func isVersionLike(s string) bool {
	s = strings.TrimPrefix(s, "v")
	s = strings.TrimPrefix(strings.ToLower(s), "go")
	if idx := strings.Index(s, ".windows."); idx > 0 {
		s = s[:idx]
	}
	if len(s) == 0 || s[0] < '0' || s[0] > '9' {
		return false
	}
	dots := strings.Count(s, ".")
	return dots >= 1 && dots <= 3
}
