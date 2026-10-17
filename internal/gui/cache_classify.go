// cache_classify.go 缓存文件归档后缀与分类的单一来源（Spec 021 D2）。
//
// 归档后缀表、isArchiveName、cacheEntryCategory 在此处唯一定义，供
// app_config.go（GetCacheBreakdown/CleanCache）、app_size_breakdown.go
// （computeCacheSizeBreakdown/parseCacheName）、app_cache_retention.go
// （清理策略）、app_cache_delete.go（CleanCacheCategory）共同引用，
// 消除各处后缀表与分类规则的重复与分歧。
package gui

import "strings"

// archiveSuffixes 是下载归档/安装包扩展名（含多段扩展名如 .tar.xz）。
//
// 注意：xdev 自身的元数据/辅助文件（.json/.dat/.lock/.sha256/.part/.txt/.log）
// 不属于归档 —— 归档表刻意不含这些尾缀，避免误删或误分类（Spec 021 D2
// 统一了此前 parseCacheName 的旧表曾包含 .dat 的分歧）。
var archiveSuffixes = []string{
	".tar.gz", ".tar.xz", ".tar.bz2", ".tar.zst", ".tar.lz4",
	".zip", ".tgz", ".txz", ".tbz2", ".tar", ".gz", ".xz", ".bz2", ".zst",
	".7z", ".rar", ".exe", ".msi", ".dmg", ".pkg", ".jar", ".sh", ".bin",
}

// isArchiveName 判断缓存文件名是否为下载归档/安装包。
// 与 GetCacheBreakdown / CleanCache / 保留策略 / 清理 API 共用。
func isArchiveName(name string) bool {
	lower := strings.ToLower(name)
	for _, suffix := range archiveSuffixes {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return false
}

// cacheEntryCategory 返回缓存文件所属分类，与设置页 GetCacheBreakdown 的
// 分类规则完全一致：isArchiveName → "archives"；.sha256 → "shaFiles"；
// .part → "partFiles"；tool_scan_cache.json → "scanCache"；其余 → "otherFiles"。
func cacheEntryCategory(name string) string {
	switch {
	case isArchiveName(name):
		return "archives"
	case strings.HasSuffix(name, ".sha256"):
		return "shaFiles"
	case strings.HasSuffix(name, ".part"):
		return "partFiles"
	case name == "tool_scan_cache.json":
		return "scanCache"
	default:
		return "otherFiles"
	}
}

// cacheMetadataFiles 是**住在 CacheDir 里但并非缓存**的用户状态文件与
// xdev 派生索引（第七轮审计 F-06）。
//
// 背景：CacheDir 同时承载两类内容——
//   - 真缓存：下载归档、`.sha256` 旁证、`.part` 中间态 —— 可安全重下；
//   - 用户状态：收藏、更新忽略列表、自定义工具注册、TOFU 密钥库；
//   - 派生索引：shim 索引、嗅探缓存、本地扫描缓存 —— 丢了会退化但可重建。
//
// 旧实现里 `CleanCache("all")` 与 `CleanCacheCategory("all")` 都会把 CacheDir
// 下的**每一个**文件删掉，于是用户点一次「清理所有缓存」就静默清空了收藏、
// 忽略列表、自定义工具与密钥库，而确认文案只说「需要重新下载」。
//
// 现在 `"all"` 只删真缓存（archives / shaFiles / partFiles）；本清单里的文件
// 必须显式点名其分类才会被删除（例如 "scanCache" 删 tool_scan_cache.json）。
var cacheMetadataFiles = map[string]bool{
	"favorites.json":       true, // 用户收藏
	"ignored_tools.json":   true, // 更新忽略列表
	"custom_tools.json":    true, // 自定义工具注册
	"keystore.dat":         true, // TOFU 密钥库
	"shim-index.v1":        true, // shim 分发索引（派生，可重建）
	"sniff-cache.json":     true, // 版本嗅探缓存（派生，可重建）
	"tool_scan_cache.json": true, // 本地扫描缓存（scanCache 分类）
}

// isCacheMetadata 报告该文件名是否为「不可被 all 删除」的用户状态/派生索引。
func isCacheMetadata(name string) bool {
	if cacheMetadataFiles[name] {
		return true
	}
	// 锁文件是运行态旁证（如 sniff-cache.json.lock），同属元数据。
	return strings.HasSuffix(name, ".lock")
}
