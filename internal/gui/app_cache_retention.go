// app_cache_retention.go implements the download archive retention policy
// (Spec 020). After a successful install the archive can be deleted
// immediately (default), kept for 7/30 days, or kept forever. Expired
// archives are swept at startup as a safety net.
package gui

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xmdemt/xdev/internal/config"
)

// applyArchiveRetention 按配置策略处理安装成功后的缓存归档。
// immediate → 删除该工具全部归档（含 .sha256/.part 伴生文件）；
// 7d/30d/forever → 保留（7d/30d 由 CleanupExpiredArchives 启动兜底清理）。
// 归档判定复用 cache_classify.go 的 isArchiveName（Spec 021 D2）。
func (a *App) applyArchiveRetention(toolName string) {
	cfg := a.getCfg()
	if cfg == nil {
		return
	}
	switch cfg.GetArchiveRetention() {
	case "immediate":
		removed := 0
		pattern := filepath.Join(cfg.Core.CacheDir, toolName+"-*")
		matches, _ := filepath.Glob(pattern)
		for _, f := range matches {
			base := filepath.Base(f)
			// 确保文件名以 toolName- 开头后跟版本号，避免误删其他工具
			if !strings.HasPrefix(base, toolName+"-") {
				continue
			}
			switch {
			case isArchiveName(base):
				// 归档本体 + 伴生文件（.sha256/.part）。
				if err := os.Remove(f); err == nil {
					removed++
				} else if !os.IsNotExist(err) {
					logWarn("[cache-retention] remove %s failed: %v", f, err)
				}
				removed += removeArchiveCompanions(f)
			case strings.HasSuffix(strings.ToLower(base), ".sha256"),
				strings.HasSuffix(strings.ToLower(base), ".part"):
				// 孤立伴生文件（无归档本体）也一并删除。
				if err := os.Remove(f); err == nil {
					removed++
				} else if !os.IsNotExist(err) {
					logWarn("[cache-retention] remove %s failed: %v", f, err)
				}
			}
		}
		if removed > 0 {
			logInfo("[cache-retention] immediate: removed %d archive file(s) for %s", removed, toolName)
		}
	default:
		logInfo("[cache-retention] policy %q: keeping archives for %s", cfg.GetArchiveRetention(), toolName)
	}
}

// CleanupExpiredArchives 删除超过保留期限的归档（7d/30d 策略）。
// 依据文件 ModTime；immediate/forever 策略跳过（不自动删）。返回删除数量。
// 同时删除归档的 .sha256/.part 伴生文件；删除后刷新明细缓存。
func (a *App) CleanupExpiredArchives() int {
	cfg := a.getCfg()
	if cfg == nil {
		return 0
	}
	var retention time.Duration
	switch cfg.GetArchiveRetention() {
	case "7d":
		retention = 7 * 24 * time.Hour
	case "30d":
		retention = 30 * 24 * time.Hour
	default:
		return 0 // immediate/forever：不自动清理
	}

	entries, err := os.ReadDir(cfg.Core.CacheDir)
	if err != nil {
		return 0
	}

	removed := 0
	var toRemove []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !isArchiveName(name) {
			// 元数据（.json/.dat/.lock/.sha256/.txt）不参与清理
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if time.Since(info.ModTime()) <= retention {
			continue
		}
		toRemove = append(toRemove, filepath.Join(cfg.Core.CacheDir, name))
	}
	for _, p := range toRemove {
		if err := os.Remove(p); err == nil {
			removed++
		} else if !os.IsNotExist(err) {
			logWarn("[cache-retention] remove %s failed: %v", p, err)
		}
		// 伴生文件：校验与下载中间态（best-effort）。
		removed += removeArchiveCompanions(p)
	}
	if removed > 0 {
		logInfo("[cache-retention] removed %d expired archive file(s) (policy %s)", removed, cfg.GetArchiveRetention())
		a.InvalidateSizeBreakdown()
	}
	return removed
}

// GetArchiveRetention 返回当前保留策略（"immediate"|"7d"|"30d"|"forever"）。
func (a *App) GetArchiveRetention() string {
	cfg := a.getCfg()
	if cfg == nil {
		return "immediate"
	}
	return cfg.GetArchiveRetention()
}

// SetArchiveRetention 更新保留策略并保存配置（F-19/D17：写锁内操作实时配置）。
func (a *App) SetArchiveRetention(policy string) error {
	if err := a.mutateCfg(func(cfg *config.Config) error {
		return cfg.SetArchiveRetention(policy)
	}); err != nil {
		return err
	}
	logInfo("[cache-retention] policy set to %q", policy)
	return nil
}
