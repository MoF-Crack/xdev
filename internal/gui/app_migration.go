// app_migration.go implements initialization, migration, and self-install
// methods for the GUI.
//
// Provides:
//   - IsInitialized: check if xdev has been initialized (config.yaml exists)
//   - InitConfig: run first-time setup with drive selection and tool installation
//   - SaveLastSelection: persist the user's home directory choice
//   - MigrateLocalTool: import an existing local tool installation into xdev
//   - MigrateAllLocalTools: batch-migrate all detected local tools
//
// Migration works by finding the tool's installation path, copying or
// moving it into the xdev install directory, and activating it.
// The original installation is left intact (not deleted) to avoid
// breaking other tools that may depend on it.
package gui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/env"
	"github.com/xmdemt/xdev/internal/installer"
	"github.com/xmdemt/xdev/internal/shim"
	"github.com/xmdemt/xdev/internal/util"
)

// ScanLegacyDir 扫描所有驱动器，查找旧版 X:\XimuLocalDev\xmdevenvmt 目录。
func (a *App) ScanLegacyDir() []env.LegacyDirInfo {
	logInfo("[ScanLegacyDir] 扫描旧版目录...")
	found := env.ScanLegacyDir()
	logInfo("[ScanLegacyDir] 发现 %d 个旧版目录", len(found))
	return found
}

// MigrateLegacyDir 执行旧版目录迁移，将旧目录内容复制到新目录并删除旧目录。
func (a *App) MigrateLegacyDir(legacyDir string) error {
	logInfo("[MigrateLegacyDir] 开始迁移 %s", legacyDir)

	// 根据旧目录路径推算新目录路径
	vol := filepath.VolumeName(legacyDir)
	newDir := filepath.Join(vol, "XDev")

	if err := env.MigrateLegacyDir(legacyDir, newDir); err != nil {
		logError("[MigrateLegacyDir] 迁移失败: %v", err)
		return err
	}

	logInfo("[MigrateLegacyDir] 迁移完成: %s -> %s", legacyDir, newDir)

	// 更新配置中的路径（如果当前配置指向旧目录）
	a.cfgMu.Lock()
	cfg := a.cfg
	normalized := strings.ReplaceAll(strings.ToLower(cfg.Core.XMDevDir), "/", "\\")
	legacyLower := strings.ReplaceAll(strings.ToLower(legacyDir), "/", "\\")
	if strings.Contains(normalized, legacyLower) || normalized == legacyLower {
		cfg.Core.XMDevDir = newDir
		cfg.ResolvePaths(newDir)
		cfg.Save()
		config.UpdateConfigPaths(newDir)
		config.InvalidateHomeCache()
	}
	a.cfgMu.Unlock()

	// 刷新本地缓存
	a.refreshAndSaveLocalCache()

	return nil
}

// IsInitialized checks whether the config file exists on disk.
func (a *App) IsInitialized() bool {
	cfg := a.getCfg()
	path := filepath.Join(cfg.Core.XMDevDir, "config.yaml")
	if _, err := os.Stat(path); err != nil {
		return false
	}
	return true
}

// ---- Last Selection Memory ----

// lastSelectionFile returns the path to the file that stores the
// user's last installation directory selection.
func lastSelectionFile() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".xdev", "last_selection.json")
}

// LastSelection is the JSON structure stored in last_selection.json.
//
// Security (L-3): The HMAC field provides integrity verification.
// It is computed over the JSON-serialized {installDir, drive} pair
// (excluding the HMAC field itself) using the machine-specific key
// from util.ComputeHMAC. On load, if the HMAC is present but does not
// verify, the file is treated as tampered and the selection is ignored.
type LastSelection struct {
	InstallDir string `json:"installDir"`
	Drive      string `json:"drive"`
	HMAC       string `json:"hmac,omitempty"`
}

// computeLastSelectionHMAC computes the machine-specific HMAC over the
// installDir and drive fields (excluding the HMAC field itself).
// This is used for L-3 integrity verification of last_selection.json.
func computeLastSelectionHMAC(installDir, drive string) string {
	// Use a deterministic JSON encoding of the data fields only (no HMAC)
	data, _ := json.Marshal(struct {
		InstallDir string `json:"installDir"`
		Drive      string `json:"drive"`
	}{installDir, drive})
	return util.ComputeHMAC(data)
}

// GetLastSelection returns the user's last installation directory
// selection, or an empty object if no record exists.
//
// Security (L-3): If the HMAC field is present but verification fails,
// the selection is treated as tampered and an empty map is returned.
func (a *App) GetLastSelection() map[string]string {
	path := lastSelectionFile()
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]string{}
	}
	var sel LastSelection
	if err := json.Unmarshal(data, &sel); err != nil {
		return map[string]string{}
	}
	// Verify HMAC if present
	if sel.HMAC != "" {
		expected := computeLastSelectionHMAC(sel.InstallDir, sel.Drive)
		if expected != sel.HMAC {
			// Tampered file — ignore the selection
			return map[string]string{}
		}
	}
	return map[string]string{
		"installDir": sel.InstallDir,
		"drive":      sel.Drive,
	}
}

// SaveLastSelection persists the user's installation directory
// choice so it can be pre-selected on the next launch.
//
// Security (L-3): An HMAC is computed over the selection data and
// stored alongside it, so that tampering can be detected on load.
func (a *App) SaveLastSelection(installDir, drive string) error {
	sel := LastSelection{
		InstallDir: installDir,
		Drive:      drive,
		HMAC:       computeLastSelectionHMAC(installDir, drive),
	}
	data, err := json.MarshalIndent(sel, "", "  ")
	if err != nil {
		return err
	}
	path := lastSelectionFile()
	os.MkdirAll(filepath.Dir(path), 0o755)
	return os.WriteFile(path, data, 0o644)
}

// DriveInfoJSON is the JSON-serializable drive info for the frontend.
type DriveInfoJSON struct {
	Letter string `json:"letter"`
	Path   string `json:"path"`
	Free   int64  `json:"free"`
	Total  int64  `json:"total"`
}

// GetDrives returns available drives with free space info.
func (a *App) GetDrives() []DriveInfoJSON {
	logInfo("[GetDrives] scanning drives")
	drives := config.ScanDrives()
	var result []DriveInfoJSON
	for _, d := range drives {
		if d.Mounted {
			result = append(result, DriveInfoJSON{
				Letter: d.Letter,
				Path:   d.Path,
				Free:   d.Free,
				Total:  d.Total,
			})
			logDebug("[GetDrives] %s free=%d total=%d", d.Letter, d.Free, d.Total)
		}
	}
	logInfo("[GetDrives] found %d mounted drives", len(result))
	return result
}

// InitConfig sets up the config with the chosen base directory and saves it.
func (a *App) InitConfig(baseDir string) error {
	logInfo("[InitConfig] baseDir=%s", baseDir)
	a.cfgMu.Lock()
	defer a.cfgMu.Unlock()

	// If the user selected a legacy directory, rename it to the new XDev path automatically.
	baseDir = a.migrateLegacyDir(baseDir)

	// Auto-migrate from legacy xmdevenvmt directory if the new dir is empty.
	if _, err := os.Stat(filepath.Join(baseDir, "install")); err != nil {
		vol := filepath.VolumeName(baseDir)
		if vol != "" {
			legacies := []string{
				filepath.Join(vol, "XimuLocalDev", "xmdevenvmt"),
			}
			for _, legacy := range legacies {
				if fi, err := os.Stat(filepath.Join(legacy, "install")); err == nil && fi.IsDir() {
					// Migrate: copy install/, cache/, shims/, env/ to new dir
					for _, sub := range []string{"install", "cache", "shims", "env"} {
						src := filepath.Join(legacy, sub)
						dst := filepath.Join(baseDir, sub)
						if fi, err := os.Stat(src); err == nil && fi.IsDir() {
							os.Rename(src, dst)
						}
					}
					// Copy config if it exists
					if data, err := os.ReadFile(filepath.Join(legacy, "config.yaml")); err == nil {
						os.MkdirAll(baseDir, 0o755)
						os.WriteFile(filepath.Join(baseDir, "config.yaml"), data, 0o644)
					}
					break
				}
			}
		}
	}

	a.cfg.Core.XMDevDir = baseDir
	a.cfg.ResolvePaths(baseDir)
	if err := a.cfg.EnsureDirs(); err != nil {
		logError("[InitConfig] EnsureDirs failed: %v", err)
		return err
	}
	if err := a.cfg.Save(); err != nil {
		logError("[InitConfig] Save failed: %v", err)
		return err
	}

	// Now that the directory is set up, initialise the log file.
	logDir := filepath.Join(baseDir, "logs")
	InitLogger(logDir)
	logInfo("[InitConfig] config saved, logger initialised at %s", logDir)

	// Save the selection so it can be pre-selected on next launch.
	drive := filepath.VolumeName(baseDir)
	if drive == "" {
		drive = "C:"
	}
	a.SaveLastSelection(baseDir, drive)

	// 清理用户 PATH 中旧的 xdev install 目录条目和双反斜杠路径
	a.cleanupUserPath(baseDir)

	// 将 xdev 自身安装到 install/xdev/<version>/ 并创建 junction
	if _, _, err := installer.InstallSelf(a.getCfg()); err != nil {
		logWarn("[InitConfig] failed to install xdev self: %v", err)
	}
	xdevBinDir := filepath.Join(baseDir, "current", "xdev")
	if err := env.AppendToUserPath(xdevBinDir); err != nil {
		logWarn("[InitConfig] failed to add xdev dir to user PATH: %v", err)
	} else {
		logInfo("[InitConfig] added xdev dir to user PATH: %s", xdevBinDir)
	}

	return nil
}

// cleanupUserPath 清理用户 PATH 中旧的 xdev install 目录条目、
// 双反斜杠路径和旧版 xmdevenvmt shim 目录。
func (a *App) cleanupUserPath(baseDir string) {
	cfg := a.getCfg()
	mode := cfg.GetPathMode()
	shimDir := filepath.Join(baseDir, "shims")
	changed, err := env.CleanupUserPath(shimDir, mode)
	if err != nil {
		logWarn("[cleanupUserPath] 清理用户 PATH 失败: %v", err)
	} else if changed {
		logInfo("[cleanupUserPath] 已清理用户 PATH (%s 模式)", mode)
	}
}

// migrateLegacyDir checks if the given directory is a legacy path
// and renames it to the new XDev path. Returns the (possibly updated) path.
func (a *App) migrateLegacyDir(dir string) string {
	// Normalize to forward slashes for comparison
	normalized := strings.ReplaceAll(strings.ToLower(dir), "/", "\\")

	legacyPatterns := []struct {
		pattern     string
		replacement func(string) string
	}{
		{`\ximulocaldev\xmdevenvmt`, func(vol string) string { return vol + "\\XDev" }},
		{`\ximulocaldev\xdev`, func(vol string) string { return vol + "\\XDev" }},
	}

	for _, lp := range legacyPatterns {
		if strings.Contains(normalized, lp.pattern) {
			vol := filepath.VolumeName(dir)
			newPath := lp.replacement(vol)
			logInfo("[migrateLegacyDir] detected legacy dir %s, renaming to %s", dir, newPath)

			// If the legacy dir exists, try to rename it
			if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
				// Check if newPath already exists
				if _, err := os.Stat(newPath); err == nil {
					// newPath exists, move contents instead
					logInfo("[migrateLegacyDir] %s already exists, moving contents", newPath)
					for _, sub := range []string{"install", "cache", "shims", "env", "downloads", "manifests", "config.yaml"} {
						src := filepath.Join(dir, sub)
						dst := filepath.Join(newPath, sub)
						if _, err := os.Stat(src); err == nil {
							os.Rename(src, dst)
						}
					}
					// Remove the now-empty legacy dir
					os.Remove(dir)
				} else if err := os.Rename(dir, newPath); err != nil {
					logError("[migrateLegacyDir] rename failed: %v", err)
					return dir // fallback to original
				}
				// Update config.yaml with new paths
				config.UpdateConfigPaths(newPath)
				config.InvalidateHomeCache()
				return newPath
			}
			// Legacy dir doesn't exist, just use the new path
			return newPath
		}
	}
	return dir
}

// SelectDirectory opens a native directory picker dialog.
func (a *App) SelectDirectory(title string) string {
	dir, err := wailsRuntime.OpenDirectoryDialog(a.ctx, wailsRuntime.OpenDialogOptions{
		Title: title,
	})
	if err != nil {
		return ""
	}
	return dir
}

// BrowseDirectory 打开系统目录选择对话框，返回用户选择的目录路径
func (a *App) BrowseDirectory(title string) string {
	if a.ctx == nil {
		return ""
	}
	dir, err := wailsRuntime.OpenDirectoryDialog(a.ctx, wailsRuntime.OpenDialogOptions{
		Title: title,
	})
	if err != nil {
		logWarn("[BrowseDirectory] 目录选择对话框失败: %v", err)
		return ""
	}
	return dir
}

// ScanLocalEnvironmentsForMigration 扫描系统中已安装的开发工具，
// 返回可用于迁移的本地工具列表。
func (a *App) ScanLocalEnvironmentsForMigration() []env.LocalTool {
	logInfo("[ScanLocalEnvironmentsForMigration] 开始扫描本地环境")
	cfg := a.getCfg()
	tools := env.ScanLocalTools(cfg.Core.XMDevDir)

	// 过滤掉已由 xdev 管理的工具
	var result []env.LocalTool
	for _, t := range tools {
		// 检查是否已有 xdev 管理的版本
		if _, installed := a.currentVersion(t.Name); installed {
			logDebug("[ScanLocalEnvironmentsForMigration] 跳过已管理的工具: %s", t.Name)
			continue
		}
		result = append(result, t)
	}

	logInfo("[ScanLocalEnvironmentsForMigration] 找到 %d 个可迁移工具", len(result))
	return result
}

// MigrateLocalToolByJunction 通过 junction 方式将本地工具迁移到 xdev 管理。
// 注意：此方法不刷新缓存，调用方应在所有迁移完成后统一刷新。
func (a *App) MigrateLocalToolByJunction(toolJson string) error {
	logInfo("[MigrateLocalToolByJunction] 接收到迁移请求")

	var tool env.LocalTool
	if err := json.Unmarshal([]byte(toolJson), &tool); err != nil {
		return fmt.Errorf("解析工具信息失败: %v", err)
	}

	logInfo("[MigrateLocalToolByJunction] 迁移 %s (版本: %s, 路径: %s)", tool.Name, tool.Version, tool.Path)

	cfg := a.getCfg()

	// 检查工具是否已在 xdev 管理中
	if _, installed := a.currentVersion(tool.Name); installed {
		return fmt.Errorf("工具 %s 已由 xdev 管理", tool.Name)
	}

	// 在迁移前获取真实版本号
	if tool.Version == "" {
		tool.Version = a.detectToolVersion(tool.Name, tool.Path)
	}

	// 执行 junction 迁移
	if err := env.MigrateLocalTool(tool, cfg.Core.XMDevDir); err != nil {
		return err
	}

	// 迁移成功后，创建 shim 文件和设置环境变量
	a.postMigrateSetup(tool, cfg)

	// 广播环境变量变更
	env.BroadcastEnvironmentChange()

	// 刷新本地缓存（单次迁移后刷新一次）
	a.refreshAndSaveLocalCache()

	logInfo("[MigrateLocalToolByJunction] %s 迁移成功", tool.Name)
	return nil
}

// MigrateLocalToolByMove 将本地工具目录移动到 xdev install 目录下管理。
// 与 Junction 方式不同，Move 方式会将原始目录移走，原始路径将不再存在。
// 注意：此方法不刷新缓存，调用方应在所有迁移完成后统一刷新。
func (a *App) MigrateLocalToolByMove(toolJson string) error {
	logInfo("[MigrateLocalToolByMove] 接收到迁移请求")

	var tool env.LocalTool
	if err := json.Unmarshal([]byte(toolJson), &tool); err != nil {
		return fmt.Errorf("解析工具信息失败: %v", err)
	}

	logInfo("[MigrateLocalToolByMove] 迁移 %s (版本: %s, 路径: %s)", tool.Name, tool.Version, tool.Path)

	cfg := a.getCfg()

	// 1. 检查工具是否已在 xdev 管理中
	if _, installed := a.currentVersion(tool.Name); installed {
		return fmt.Errorf("工具 %s 已由 xdev 管理", tool.Name)
	}

	// 2. 获取工具真实版本号
	ver := tool.Version
	if ver == "" {
		ver = a.detectToolVersion(tool.Name, tool.Path)
	}

	// 3. 确定目标路径
	p, err := a.mustGetPlugin(tool.Name)
	if err != nil {
		return err
	}
	targetPath := p.InstallDir(cfg.Core.InstallDir, ver)

	// 4. 如果目标路径已存在，返回错误
	if _, err := os.Stat(targetPath); err == nil {
		return fmt.Errorf("目标路径已存在: %s", targetPath)
	}

	// 5. 检查源路径
	localPath := tool.Path
	srcInfo, err := os.Stat(localPath)
	if err != nil {
		return fmt.Errorf("源路径不存在: %s", localPath)
	}
	if !srcInfo.IsDir() {
		return fmt.Errorf("源路径不是目录: %s", localPath)
	}

	// 6. 移动目录
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return fmt.Errorf("创建目标父目录失败: %v", err)
	}

	movedByRename := true
	if err := os.Rename(localPath, targetPath); err != nil {
		logInfo("[MigrateLocalToolByMove] Rename 失败（可能跨盘符），回退到复制+删除: %v", err)
		movedByRename = false
		// 回退：复制目录（带进度反馈）
		err := util.CopyDirProgress(localPath, targetPath, func(copied, total int) {
			if a.ctx != nil {
				wailsRuntime.EventsEmit(a.ctx, "migrationProgress", map[string]interface{}{
					"tool":    tool.Name,
					"phase":   "copying",
					"copied":  copied,
					"total":   total,
					"percent": float64(copied) / float64(total) * 100,
				})
			}
		})
		if err != nil {
			return fmt.Errorf("复制目录失败: %v", err)
		}
		// 复制成功后删除原始目录
		if err := os.RemoveAll(localPath); err != nil {
			logWarn("[MigrateLocalToolByMove] 删除原始目录失败: %v", err)
		}
	}

	logInfo("[MigrateLocalToolByMove] 目录已移动到 %s", targetPath)

	// 7. 创建 junction 和激活
	ins := a.newInstaller(cfg)
	if err := ins.Activate(p, targetPath, ver); err != nil {
		logError("[MigrateLocalToolByMove] 激活失败，尝试回滚: %v", err)
		a.rollbackMove(targetPath, localPath, movedByRename)
		return fmt.Errorf("激活失败: %v", err)
	}

	// 8. 创建 shim 文件和环境变量
	a.postMigrateSetup(tool, cfg)

	// 9. 广播环境变量变更
	env.BroadcastEnvironmentChange()

	// 刷新本地缓存（单次迁移后刷新一次）
	a.refreshAndSaveLocalCache()

	logInfo("[MigrateLocalToolByMove] %s 迁移成功", tool.Name)
	return nil
}

// rollbackMove 尝试将已移动的目录回滚到原始位置。
// movedByRename 为 true 表示通过 os.Rename 移动，可以尝试 Rename 回去；
// 为 false 表示通过复制+删除移动，需要再复制回去。
func (a *App) rollbackMove(targetPath, originalPath string, movedByRename bool) {
	if movedByRename {
		if err := os.Rename(targetPath, originalPath); err != nil {
			logError("[rollbackMove] Rename 回滚失败: %v", err)
		} else {
			logInfo("[rollbackMove] 已回滚到原始位置: %s", originalPath)
		}
	} else {
		if err := util.CopyDir(targetPath, originalPath); err != nil {
			logError("[rollbackMove] 复制回滚失败: %v", err)
		} else {
			logInfo("[rollbackMove] 已复制回滚到原始位置: %s", originalPath)
			if err := os.RemoveAll(targetPath); err != nil {
				logWarn("[rollbackMove] 清理目标路径失败: %v", err)
			}
		}
	}
}

// MigrateAllLocalTools 一键迁移所有扫描到的本地工具（并行执行）
func (a *App) MigrateAllLocalTools() []MigrationResult {
	logInfo("[MigrateAllLocalTools] 开始一键迁移")
	cfg := a.getCfg()
	tools := env.ScanLocalTools(cfg.Core.XMDevDir)

	results := make([]MigrationResult, len(tools))
	var wg sync.WaitGroup
	limiter := make(chan struct{}, 4) // 最多4个并发迁移

	for i, tool := range tools {
		// 跳过已管理的工具
		if _, installed := a.currentVersion(tool.Name); installed {
			results[i] = MigrationResult{
				Name:   tool.Name,
				Status: "skipped",
				Reason: "已由 xdev 管理",
			}
			continue
		}

		// 在迁移前获取真实版本号
		if tool.Version == "" {
			tool.Version = a.detectToolVersion(tool.Name, tool.Path)
		}

		wg.Add(1)
		limiter <- struct{}{}
		go func(idx int, t env.LocalTool) {
			defer wg.Done()
			defer func() { <-limiter }()

			if err := env.MigrateLocalTool(t, cfg.Core.XMDevDir); err != nil {
				results[idx] = MigrationResult{
					Name:   t.Name,
					Status: "failed",
					Error:  err.Error(),
				}
				logWarn("[MigrateAllLocalTools] 迁移 %s 失败: %v", t.Name, err)
				return
			}

			// 迁移成功后创建 shim 和设置环境变量
			a.postMigrateSetup(t, cfg)

			results[idx] = MigrationResult{
				Name:    t.Name,
				Status:  "success",
				Version: t.Version,
				Path:    t.Path,
			}
			logInfo("[MigrateAllLocalTools] 迁移 %s 成功", t.Name)
		}(i, tool)
	}
	wg.Wait()

	env.BroadcastEnvironmentChange()

	// 所有迁移完成后统一刷新一次缓存
	a.refreshAndSaveLocalCache()

	logInfo("[MigrateAllLocalTools] 一键迁移完成，共 %d 个工具", len(results))
	return results
}

// postMigrateSetup 迁移成功后的公共设置逻辑：创建 shim、设置环境变量
func (a *App) postMigrateSetup(tool env.LocalTool, cfg *config.Config) {
	p, err := a.mustGetPlugin(tool.Name)
	if err != nil {
		return
	}

	ver := tool.Version
	if ver == "" {
		ver = a.detectToolVersion(tool.Name, tool.Path)
	}
	installPath := p.InstallDir(cfg.Core.InstallDir, ver)

	// 创建 shim 文件
	if cfg.GetPathMode() == "shim" {
		ins := a.newInstaller(cfg)
		shimExe := ins.ResolveShimExe()
		for _, shimName := range p.Shims() {
			if err := shim.InstallShim(cfg.Core.ShimDir, shimName, shimExe); err != nil {
				logWarn("[postMigrateSetup] 创建 shim %s 失败: %v", shimName, err)
			} else {
				logInfo("[postMigrateSetup] 创建 shim: %s", shimName)
			}
		}
	}

	// 设置环境变量
	vars := p.EnvVars(installPath)
	if len(vars) > 0 {
		// Windows registry writes must be serialized to prevent lost updates
		regMu.Lock()
		for k, v := range vars {
			if err := env.SetEnvVar(k, v, "user"); err != nil {
				logWarn("[postMigrateSetup] 设置环境变量 %s 失败: %v", k, err)
			} else {
				logInfo("[postMigrateSetup] 设置环境变量: %s=%s", k, v)
			}
		}
		regMu.Unlock()
	}

	shim.InvalidateShimIndex()
}
