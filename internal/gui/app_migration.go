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

	// Security (F-10): legacyDir 将被子目录迁移后 RemoveAll，
	// 必须校验为绝对路径、存在、非符号链接、且不在 xdev 主目录内。
	if legacyDir == "" || !filepath.IsAbs(legacyDir) {
		return fmt.Errorf("legacy dir must be absolute: %q", legacyDir)
	}
	li, err := os.Lstat(legacyDir)
	if err != nil {
		return fmt.Errorf("legacy dir does not exist: %s", legacyDir)
	}
	// Go 的 Lstat 对 Windows junction/reparse point 也报告 ModeSymlink。
	if li.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("legacy dir must not be a symlink/junction: %s", legacyDir)
	}
	if !li.IsDir() {
		return fmt.Errorf("legacy dir is not a directory: %s", legacyDir)
	}
	cfg := a.getCfg()
	if util.ValidateAbsPathUnder(cfg.Core.XMDevDir, legacyDir) == nil {
		return fmt.Errorf("legacy dir is inside xdev home, refusing to migrate: %s", legacyDir)
	}

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
	cfg = a.cfg
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
	// Verify HMAC if present (constant-time via util.VerifyHMAC).
	if sel.HMAC != "" {
		data, _ := json.Marshal(struct {
			InstallDir string `json:"installDir"`
			Drive      string `json:"drive"`
		}{sel.InstallDir, sel.Drive})
		if !util.VerifyHMAC(data, sel.HMAC) {
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
	if err := validateInitBaseDir(baseDir); err != nil {
		return err
	}
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
	a.cleanupUserPath(a.cfg, baseDir)

	// 将 xdev 自身安装到 install/xdev/<version>/ 并创建 junction
	// 注意：此处持有 cfgMu 写锁，必须直接使用 a.cfg 而非 a.getCfg()
	// （RWMutex 不可重入，getCfg 的 RLock 会造成死锁，F-18）。
	if _, _, err := installer.InstallSelf(a.cfg); err != nil {
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
// cfg 由调用方传入（InitConfig 持写锁时传 a.cfg，避免 getCfg 死锁，F-18）。
func (a *App) cleanupUserPath(cfg *config.Config, baseDir string) {
	mode := cfg.GetPathMode()
	shimDir := filepath.Join(baseDir, "shims")
	// 票 11（P0-13）：home 锚定——仅清理 baseDir 下的 xdev 自有条目，
	// 用户无关目录（如 D:\xdev）不再被误删。
	changed, err := env.CleanupUserPath(baseDir, shimDir, mode)
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
// 单个迁移完成后会刷新本地缓存；批量迁移（MigrateAllLocalTools）在
// 全部完成后统一刷新一次。
func (a *App) MigrateLocalToolByJunction(toolJson string) error {
	logInfo("[MigrateLocalToolByJunction] 接收到迁移请求")

	var tool env.LocalTool
	if err := json.Unmarshal([]byte(toolJson), &tool); err != nil {
		return fmt.Errorf("解析工具信息失败: %v", err)
	}

	// Security (F-10): 校验工具名与源路径（绝对路径、目录、非符号链接、
	// 不在 xdev 主目录内）。
	if err := a.validateMigrationSource(tool); err != nil {
		return err
	}

	if err := a.migrateLocalToolByJunction(tool); err != nil {
		return err
	}
	// 票 22（P1-16 / D-08）：刷新上提到调用方——单工具迁移恰刷新一次。
	// 批量迁移走 migrateLocalToolOne（不经此函数），由批量尾部刷新一次。
	a.refreshAndSaveLocalCache()
	return nil
}

// validateMigrationSource 校验迁移源（F-10）：
//   - 工具名必须通过 ValidateShimName（防止目录穿越）
//   - 源路径必须为绝对路径、存在且为目录
//   - 源路径不得是符号链接/junction（避免跟随到系统任意目录）
//   - 源路径不得位于 xdev 主目录内（避免迁移自管目录形成循环）
func (a *App) validateMigrationSource(tool env.LocalTool) error {
	if err := util.ValidateShimName(tool.Name); err != nil {
		return fmt.Errorf("invalid tool name %q: %w", tool.Name, err)
	}
	if tool.Path == "" || !filepath.IsAbs(tool.Path) {
		return fmt.Errorf("source path must be absolute: %q", tool.Path)
	}
	li, err := os.Lstat(tool.Path)
	if err != nil {
		return fmt.Errorf("source path does not exist: %s", tool.Path)
	}
	// Go 的 Lstat 对 Windows junction/reparse point 也报告 ModeSymlink。
	if li.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("source path must not be a symlink/junction: %s", tool.Path)
	}
	if !li.IsDir() {
		return fmt.Errorf("source path is not a directory: %s", tool.Path)
	}
	cfg := a.getCfg()
	if util.ValidateAbsPathUnder(cfg.Core.XMDevDir, tool.Path) == nil {
		return fmt.Errorf("source path is inside xdev home, refusing to migrate: %s", tool.Path)
	}
	return nil
}

// migrateLocalToolByJunction 通过 junction 方式迁移单个本地工具。
func (a *App) migrateLocalToolByJunction(tool env.LocalTool) error {
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

	// 票 22（P1-16 / D-08）：worker 内不再刷新本地缓存。本函数被批量迁移
	// （MigrateAllLocalTools → migrateLocalToolOne）复用，N 个工具会并发进入，
	// 若在此刷新会退化成 N 次全盘 hostscan + N 次落盘。刷新统一由调用方负责：
	// 批量迁移在尾部刷一次，单工具迁移在入口（MigrateLocalToolByJunction）刷一次。

	logInfo("[MigrateLocalToolByJunction] %s 迁移成功", tool.Name)
	return nil
}

// MigrateLocalToolByMove 将本地工具目录移动到 xdev install 目录下管理。
// 与 Junction 方式不同，Move 方式会将原始目录移走，原始路径将不再存在。
// 单个迁移完成后会刷新本地缓存；批量迁移（MigrateAllLocalTools）在
// 全部完成后统一刷新一次。
func (a *App) MigrateLocalToolByMove(toolJson string) error {
	logInfo("[MigrateLocalToolByMove] 接收到迁移请求")

	var tool env.LocalTool
	if err := json.Unmarshal([]byte(toolJson), &tool); err != nil {
		return fmt.Errorf("解析工具信息失败: %v", err)
	}

	// Security (F-10): 校验工具名与源路径（绝对路径、目录、非符号链接、
	// 不在 xdev 主目录内）。move 模式随后会删除源目录，校验尤为重要。
	if err := a.validateMigrationSource(tool); err != nil {
		return err
	}

	if err := a.migrateLocalToolByMove(tool); err != nil {
		return err
	}
	// 票 22（P1-16 / D-08）：刷新上提到调用方——单工具迁移恰刷新一次。
	a.refreshAndSaveLocalCache()
	return nil
}

// migrateLocalToolByMove 以 move 方式迁移单个本地工具。
func (a *App) migrateLocalToolByMove(tool env.LocalTool) error {
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
		// 回退：复制目录（带进度反馈 + 断链可见化——票 09/N-01：
		// 复制报告携带被跳过的链接条目数，在源目录即将被 RemoveAll
		// 之前把潜在数据损失显式暴露出来）
		_, skippedLinks, copyErr := util.CopyDirReport(localPath, targetPath, func(copied, total int) {
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
		if copyErr != nil {
			return fmt.Errorf("复制目录失败: %v", copyErr)
		}
		if skippedLinks > 0 {
			logWarn("[MigrateLocalToolByMove] 复制完成但跳过了 %d 个无法解析/断链的条目（详见 [copydir] 日志）——请确认这些内容不需要", skippedLinks)
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

	// 票 22（P1-16 / D-08）：worker 内不再刷新本地缓存（同 junction 版说明）。
	// 批量迁移由 MigrateAllLocalTools 尾部统一刷新一次；单工具迁移由
	// MigrateLocalToolByMove 入口刷新一次。

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

// scanLocalToolsForMigration 是 MigrateAllLocalTools 的扫描入口 seam
// （票 22 守卫测试用）：生产恒为 env.ScanLocalTools（全盘扫描真实主机），
// 仅测试替换为临时目录中的假工具集，以便在无副作用的前提下驱动批量迁移
// 路径并断言「每批恰好刷新一次缓存」。非测试代码从不赋值 → 生产行为不变。
var scanLocalToolsForMigration = env.ScanLocalTools

// MigrateAllLocalTools 一键迁移所有扫描到的本地工具（并行执行）。
// mode 指定迁移方式："junction" | "move"。
func (a *App) MigrateAllLocalTools(mode string) []MigrationResult {
	logInfo("[MigrateAllLocalTools] 开始一键迁移 (mode=%s)", mode)
	if mode != "move" {
		mode = "junction"
	}
	cfg := a.getCfg()
	tools := scanLocalToolsForMigration(cfg.Core.XMDevDir)

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

			if err := a.migrateLocalToolOne(t, mode); err != nil {
				results[idx] = MigrationResult{
					Name:   t.Name,
					Status: "failed",
					Error:  err.Error(),
				}
				logWarn("[MigrateAllLocalTools] 迁移 %s 失败: %v", t.Name, err)
				return
			}

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

	// 所有迁移完成后统一刷新一次缓存（票 22：worker 内已移除刷新，
	// 这里是批量路径唯一的刷新点）
	a.refreshAndSaveLocalCache()

	logInfo("[MigrateAllLocalTools] 一键迁移完成，共 %d 个工具", len(results))
	return results
}

// migrateLocalToolOne 按指定迁移方式迁移单个本地工具。
// mode: "junction" | "move"（其他值回退到 junction）。
func (a *App) migrateLocalToolOne(tool env.LocalTool, mode string) error {
	if mode == "move" {
		return a.migrateLocalToolByMove(tool)
	}
	return a.migrateLocalToolByJunction(tool)
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
	//
	// P1-1（第六轮审计）：这里此前直调 env.SetEnvVar——manifest env_vars 是
	// **不可信数据**（第三方/手工放置的 yaml 可声明任意键值），而 SetEnvVar 的
	// PATH 分支在所有校验之前路由到整串覆写（WriteUserPath）——"env_vars:
	// {Path: C:\evil}" 经 GUI 迁移就能整体替换用户 PATH；其它键（如
	// GOROOT=%TEMP%\evil、目录外 JAVA_HOME）也全无 X-08 校验。与安装器
	// setUserEnvVars 同语义对齐：
	//   - PATH 一律跳过（大小写不敏感）——由 xdev 的 shim/目录注入机制管理；
	//   - 其余键走 SetUserEnvVarTrusted（受保护键拒绝 + 路径型值 X-08 校验）。
	vars := p.EnvVars(installPath)
	if len(vars) > 0 {
		// Windows registry writes must be serialized to prevent lost updates
		regMu.Lock()
		for k, v := range vars {
			if strings.EqualFold(k, "PATH") {
				logWarn("[postMigrateSetup] 跳过 manifest 声明的 %s（PATH 由 xdev 目录注入机制管理，迁移不覆写用户 PATH）", k)
				continue
			}
			if err := env.SetUserEnvVarTrusted(k, v); err != nil {
				logWarn("[postMigrateSetup] 设置环境变量 %s 失败: %v", k, err)
			} else {
				logInfo("[postMigrateSetup] 设置环境变量: %s", k)
			}
		}
		regMu.Unlock()
	}

	shim.InvalidateShimIndex()
}

// validateInitBaseDir rejects empty, relative, device, or UNC paths so
// first-run setup cannot be pointed at an attacker-controlled location
// that later becomes the xdev home (and therefore the install/cache root).
func validateInitBaseDir(baseDir string) error {
	baseDir = strings.TrimSpace(baseDir)
	if baseDir == "" {
		return fmt.Errorf("installation directory must not be empty")
	}
	if strings.ContainsRune(baseDir, '\x00') {
		return fmt.Errorf("installation directory must not contain null bytes")
	}
	if !filepath.IsAbs(baseDir) {
		return fmt.Errorf("installation directory must be an absolute path: %q", baseDir)
	}
	cleaned := filepath.Clean(baseDir)
	if strings.HasPrefix(cleaned, `\\`) || strings.HasPrefix(cleaned, `//`) {
		return fmt.Errorf("installation directory must not be a UNC/network path: %q", baseDir)
	}
	vol := filepath.VolumeName(cleaned)
	if vol != "" && strings.HasPrefix(vol, `\\`) {
		return fmt.Errorf("installation directory must not be a UNC/network path: %q", baseDir)
	}
	return nil
}
