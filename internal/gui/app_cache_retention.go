// app_cache_retention.go implements the download archive retention policy
// (Spec 020). After a successful install the archive can be deleted
// immediately (default), kept for 7/30 days, or kept forever. Expired
// archives are swept at startup as a safety net.
package gui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/downloader"
	"github.com/xmdemt/xdev/internal/util"
)

// applyArchiveRetention removes the selected archive and its owned sidecars.
// Tool names and versions may themselves contain hyphens; prefix scans cannot
// identify ownership (e.g. android-platform and android-platform-tools).
func (a *App) applyArchiveRetention(tool, version, format string) error {
	cfg := a.getCfg()
	if cfg == nil || cfg.GetArchiveRetention() != "immediate" {
		return nil
	}
	for _, component := range []string{tool, version, format} {
		if _, err := util.SafeName(component); err != nil {
			return a.finishCacheCleanup(downloader.CleanupResult{Failed: 1}, fmt.Errorf("invalid cache filename component: %w", err))
		}
	}
	filename := tool + "-" + version + "." + format
	result, err := removeCacheArchive(filepath.Join(cfg.Core.CacheDir, filename))
	return a.finishCacheCleanup(result, err)
}

// applyArchivePathRetention uses the installer's exact selected cache path,
// including form-specific names and already-cached downloads.
func (a *App) applyArchivePathRetention(path string) error {
	cfg := a.getCfg()
	if cfg == nil || cfg.GetArchiveRetention() != "immediate" {
		return nil
	}
	if err := util.ValidateAbsPathUnder(cfg.Core.CacheDir, path); err != nil {
		return a.finishCacheCleanup(downloader.CleanupResult{Failed: 1}, err)
	}
	result, err := removeCacheArchive(path)
	return a.finishCacheCleanup(result, err)
}

// CleanupExpiredArchives preserves the Wails return type. Partial failures are
// logged along with accurate counts by finishCacheCleanup.
func (a *App) CleanupExpiredArchives() int {
	result, err := a.cleanupExpiredArchives()
	_ = a.finishCacheCleanup(result, err)
	return result.Deleted
}

func (a *App) cleanupExpiredArchives() (downloader.CleanupResult, error) {
	var result downloader.CleanupResult
	cfg := a.getCfg()
	if cfg == nil {
		return result, nil
	}
	var retention time.Duration
	switch cfg.GetArchiveRetention() {
	case "7d":
		retention = 7 * 24 * time.Hour
	case "30d":
		retention = 30 * 24 * time.Hour
	default:
		return result, nil
	}
	entries, err := downloader.ReadCacheDir(cfg.Core.CacheDir)
	if os.IsNotExist(err) {
		return result, nil
	}
	if err != nil {
		return downloader.CleanupResult{Failed: 1}, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !isArchiveName(entry.Name()) {
			continue
		}
		info, e := entry.Info()
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			result.Failed++
			err = errors.Join(err, e)
			continue
		}
		if time.Since(info.ModTime()) <= retention {
			continue
		}
		r, e := removeCacheArchive(filepath.Join(cfg.Core.CacheDir, entry.Name()))
		result.Add(r)
		err = errors.Join(err, e)
	}
	return result, err
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
