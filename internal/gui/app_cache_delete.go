// Cache cleanup uses the same per-generation exclusion as the downloader.
package gui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xmdemt/xdev/internal/downloader"
)

// CacheCleanupReport keeps counts available over Wails even on partial failure.
// Busy counts skipped generations, while Removed counts individual files.
type CacheCleanupReport struct {
	Removed int    `json:"removed"`
	Busy    int    `json:"busy"`
	Failed  int    `json:"failed"`
	Detail  string `json:"detail,omitempty"`
}

func cacheCleanupReport(result downloader.CleanupResult, err error) CacheCleanupReport {
	report := CacheCleanupReport{Removed: result.Deleted, Busy: result.SkippedBusy, Failed: result.Failed}
	if err != nil {
		report.Detail = err.Error()
	}
	return report
}

func (a *App) DeleteCacheFile(path string) error {
	_, err := a.deleteCacheFile(path)
	return err
}

func (a *App) DeleteCacheFileResult(path string) CacheCleanupReport {
	return cacheCleanupReport(a.deleteCacheFile(path))
}

func (a *App) deleteCacheFile(path string) (downloader.CleanupResult, error) {
	cfg := a.getCfg()
	if cfg == nil {
		return downloader.CleanupResult{Failed: 1}, fmt.Errorf("config not loaded")
	}
	rel, err := filepath.Rel(cfg.Core.CacheDir, filepath.Clean(path))
	// The drawer lists direct children. Never traverse a directory symlink or accept
	// an alternate Windows spelling/stream that could alias a stable lock file.
	if err != nil || !filepath.IsLocal(rel) || filepath.Base(rel) != rel || rel == "." || strings.Contains(rel, ":") || strings.TrimRight(rel, " .") != rel {
		return downloader.CleanupResult{Failed: 1}, fmt.Errorf("invalid cache file path %q", path)
	}
	if protectedPartialName(rel) {
		return downloader.CleanupResult{Failed: 1}, fmt.Errorf("保留下载锁或未识别的分代文件 %q", rel)
	}
	path = filepath.Join(cfg.Core.CacheDir, rel)
	var result downloader.CleanupResult
	switch {
	case isPartialData(rel):
		result, err = downloader.CleanupPartialPath(path)
	case isArchiveName(rel):
		result, err = removeCacheArchive(path)
	default:
		result, err = removeCacheFile(path)
	}
	return result, a.finishCacheCleanup(result, err)
}

// Check lock suffixes case-insensitively as Windows can resolve different case
// to the same inode. Unknown generation spellings are never sent to os.Remove.
func protectedPartialName(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".part.lock") || ((strings.HasSuffix(lower, ".part") || strings.HasSuffix(lower, ".part.meta")) && !isPartialData(name))
}

func isPartialData(name string) bool {
	_, _, ok := downloader.PartialCachePath(name)
	return ok && !strings.HasSuffix(name, ".lock")
}

func removeCacheFile(path string) (downloader.CleanupResult, error) {
	info, err := os.Lstat(path)
	if err == nil {
		if info.IsDir() {
			err = fmt.Errorf("refusing to delete cache directory %q", path)
		} else {
			err = os.Remove(path)
		}
		if err == nil {
			return downloader.CleanupResult{Deleted: 1}, nil
		}
	}
	if os.IsNotExist(err) {
		return downloader.CleanupResult{}, nil
	}
	return downloader.CleanupResult{Failed: 1}, err
}

func removeCacheArchive(path string) (downloader.CleanupResult, error) {
	result, err := downloader.CleanupPartialFileResult(path)
	if err != nil {
		return result, err
	}
	for _, f := range []string{path, path + ".sha256"} {
		r, e := removeCacheFile(f)
		result.Add(r)
		err = errors.Join(err, e)
	}
	return result, err
}

func (a *App) finishCacheCleanup(result downloader.CleanupResult, err error) error {
	if result.Deleted > 0 {
		a.InvalidateSizeBreakdown()
	}
	if err != nil {
		err = result.WithError(err)
		logWarn("[cache] %v", err)
		return err
	}
	logInfo("[cache] 已删除 %d 个文件", result.Deleted)
	return nil
}

// CleanCacheCategory keeps the error-only Wails contract. Partial failures
// include the actual deletion count and the number of busy generations.
func (a *App) CleanCacheCategory(category string) error {
	_, err := a.cleanCacheCategory(category)
	return err
}

func (a *App) CleanCacheCategoryResult(category string) CacheCleanupReport {
	return cacheCleanupReport(a.cleanCacheCategory(category))
}

func (a *App) cleanCacheCategory(category string) (downloader.CleanupResult, error) {
	switch category {
	case "archives", "shaFiles", "partFiles", "scanCache", "otherFiles", "all":
	default:
		return downloader.CleanupResult{Failed: 1}, fmt.Errorf("未知的缓存分类: %s", category)
	}
	cfg := a.getCfg()
	if cfg == nil {
		return downloader.CleanupResult{Failed: 1}, fmt.Errorf("config not loaded")
	}
	entries, err := downloader.ReadCacheDir(cfg.Core.CacheDir)
	if os.IsNotExist(err) {
		return downloader.CleanupResult{}, nil
	}
	if err != nil {
		return downloader.CleanupResult{Failed: 1}, fmt.Errorf("读取缓存目录失败: %w", err)
	}
	var result downloader.CleanupResult
	seen := make(map[string]bool)
	for _, entry := range entries {
		name := entry.Name()
		c := cacheEntryCategory(name)
		if protectedPartialName(name) || (entry.IsDir() && c != "partFiles") {
			continue
		}
		if category == "all" {
			if isCacheMetadata(name) || (c != "archives" && c != "shaFiles" && c != "partFiles") {
				continue
			}
		} else if c != category {
			continue
		}
		path := filepath.Join(cfg.Core.CacheDir, name)
		var r downloader.CleanupResult
		var e error
		if c == "partFiles" {
			part, _, _ := downloader.PartialCachePath(path)
			if seen[part] {
				continue
			}
			seen[part] = true
			r, e = downloader.CleanupPartialPath(part)
		} else {
			r, e = removeCacheFile(path)
		}
		result.Add(r)
		err = errors.Join(err, e)
	}
	return result, a.finishCacheCleanup(result, err)
}
