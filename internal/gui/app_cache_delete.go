// app_cache_delete.go implements per-file and per-category cache deletion
// for the size-breakdown drawer (Spec 019 F6). Both methods are security-
// conscious: paths are confined to the cache directory and only files
// (never directories) are removed.
package gui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DeleteCacheFile 删除缓存目录下的单个文件，并顺带删除同名伴生文件
// （<file>.sha256 与 <file>.part）。path 必须位于 CacheDir 内（防目录穿越），
// 且不能是目录。文件不存在时视为幂等成功。有实际删除时刷新明细缓存。
func (a *App) DeleteCacheFile(path string) error {
	cfg := a.getCfg()
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}
	cacheDir := filepath.Clean(cfg.Core.CacheDir)
	cleanPath := filepath.Clean(path)

	// Security (Spec 019 F6): 禁止目录穿越 — path 必须位于 CacheDir 内。
	rel, err := filepath.Rel(cacheDir, cleanPath)
	if err != nil {
		return fmt.Errorf("invalid cache file path %q: %v", path, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("invalid cache file path %q: outside cache directory", path)
	}

	// 只允许删除文件，不删除目录。
	if fi, err := os.Stat(cleanPath); err == nil && fi.IsDir() {
		return fmt.Errorf("invalid cache file path %q: is a directory", path)
	}

	// 删除伴生文件（best-effort），再删除文件本体。
	removed := removeArchiveCompanions(cleanPath)
	if err := os.Remove(cleanPath); err == nil {
		removed++
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("删除缓存文件失败 %s: %w", cleanPath, err)
	}

	if removed > 0 {
		logInfo("[DeleteCacheFile] removed %d file(s): %s", removed, cleanPath)
		a.InvalidateSizeBreakdown()
	}
	return nil
}

// removeArchiveCompanions 删除归档文件的伴生文件（<file>.sha256 校验与
// <file>.part 下载中间态），best-effort：不存在或删除失败仅记日志。
// 返回实际删除数量（Spec 021 D3 单一实现）。
func removeArchiveCompanions(path string) int {
	removed := 0
	for _, companion := range []string{path + ".sha256", path + ".part"} {
		if err := os.Remove(companion); err == nil {
			removed++
		} else if !os.IsNotExist(err) {
			logWarn("[cache] 删除伴生文件失败 %s: %v", companion, err)
		}
	}
	return removed
}

// CleanCacheCategory 按分类删除缓存文件，分类规则与 GetCacheBreakdown 完全一致
// （单一来源 cache_classify.go 的 cacheEntryCategory）：
// "archives" | "shaFiles" | "partFiles" | "scanCache" | "otherFiles" | "all"。
// 仅删除文件，不删除目录。有实际删除时刷新明细缓存。未知分类返回错误。
func (a *App) CleanCacheCategory(category string) error {
	switch category {
	case "archives", "shaFiles", "partFiles", "scanCache", "otherFiles", "all":
	default:
		return fmt.Errorf("未知的缓存分类: %s", category)
	}

	cfg := a.getCfg()
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}
	entries, err := os.ReadDir(cfg.Core.CacheDir)
	if err != nil {
		return fmt.Errorf("读取缓存目录失败: %w", err)
	}

	removed := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue // 不删除目录
		}
		name := entry.Name()
		if category != "all" && cacheEntryCategory(name) != category {
			continue
		}
		if err := os.Remove(filepath.Join(cfg.Core.CacheDir, name)); err == nil {
			removed++
		} else if !os.IsNotExist(err) {
			logWarn("[CleanCacheCategory] 删除缓存文件失败 %s: %v", name, err)
		}
	}
	if removed > 0 {
		logInfo("[CleanCacheCategory] removed %d file(s) (category %s)", removed, category)
		a.InvalidateSizeBreakdown()
	}
	return nil
}
