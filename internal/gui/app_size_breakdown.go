// app_size_breakdown.go implements per-tool install/cache disk usage
// breakdowns (Spec 019). The dashboard shows totals (InstallSize /
// CacheSize); these methods break them down per install version and per
// cache archive so the user can see what is consuming space.
package gui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DiskUsageEntry 单个安装版本/缓存文件的占用。
type DiskUsageEntry struct {
	Tool    string `json:"tool"`    // xdev 工具名（如 "go"）
	Version string `json:"version"` // 版本目录名（如 "1.26.5"）；安装占用必填
	Path    string `json:"path"`    // 绝对路径
	Size    int64  `json:"size"`    // 字节数
	// IsStale 缓存明细专用：true 表示无对应已安装版本（孤儿缓存）
	IsStale bool `json:"isStale,omitempty"`
	// Category 缓存文件所属分类，与设置页 CacheBreakdown 一致：
	// "archives" | "shaFiles" | "partFiles" | "scanCache" | "otherFiles"。
	// 安装占用明细不设置该字段（omitempty）。
	Category string `json:"category,omitempty"`
}

// sizeBreakdownTTL 是安装/缓存明细的缓存有效期。扫描 install/ 可能涉及数 GB
// 数据，抽屉每次打开都全量扫描会很慢；TTL 内直接复用上次结果。
const sizeBreakdownTTL = time.Minute

// GetInstallSizeBreakdown 返回 install/<tool>/<version> 各级占用，按 Size 降序。
// 结果缓存 60 秒；安装/卸载/切换后调用 InvalidateSizeBreakdown 立即刷新。
// 版本目录名直接作为 Version 展示（包括 local 等非语义目录，用户可见）。
// 返回空切片而非 nil。
func (a *App) GetInstallSizeBreakdown() []DiskUsageEntry {
	a.sizeBreakdownMu.Lock()
	defer a.sizeBreakdownMu.Unlock()
	if time.Now().Before(a.installSizeExp) {
		return cloneEntries(a.installSizeCache)
	}
	entries := a.computeInstallSizeBreakdown()
	a.installSizeCache = entries
	a.installSizeExp = time.Now().Add(sizeBreakdownTTL)
	return cloneEntries(entries)
}

// computeInstallSizeBreakdown 实际扫描 install/<tool>/<version> 目录。
func (a *App) computeInstallSizeBreakdown() []DiskUsageEntry {
	cfg := a.getCfg()
	root := cfg.Core.InstallDir
	entries := make([]DiskUsageEntry, 0)

	tools, err := os.ReadDir(root)
	if err != nil {
		return entries
	}
	for _, tool := range tools {
		if !tool.IsDir() {
			continue
		}
		toolDir := filepath.Join(root, tool.Name())
		versions, err := os.ReadDir(toolDir)
		if err != nil {
			continue
		}
		for _, ver := range versions {
			if !ver.IsDir() {
				continue
			}
			versionDir := filepath.Join(toolDir, ver.Name())
			var size int64
			dirSizeFast(versionDir, &size)
			entries = append(entries, DiskUsageEntry{
				Tool:    tool.Name(),
				Version: ver.Name(),
				Path:    versionDir,
				Size:    size,
			})
		}
	}

	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Size != entries[j].Size {
			return entries[i].Size > entries[j].Size
		}
		if entries[i].Tool != entries[j].Tool {
			return entries[i].Tool < entries[j].Tool
		}
		return entries[i].Version < entries[j].Version
	})
	return entries
}

// GetCacheSizeBreakdown 返回 cache/ 下各工具归档文件占用，按 Size 降序。
// 结果缓存 60 秒；清理缓存/安装完成后调用 InvalidateSizeBreakdown 立即刷新。
// 归档文件名形如 <tool>-<version>.<ext>，解析为 (tool, version) 并标记
// IsStale（无对应已安装版本时为孤儿缓存）；xdev 自身的元数据文件
// （.json/.dat/.lock/.sha256/.txt/.log 等）与无法解析的文件原样展示
// （Tool=完整文件名，Version=""，IsStale=false），避免误标「未安装」。
// 返回空切片而非 nil。
func (a *App) GetCacheSizeBreakdown() []DiskUsageEntry {
	a.sizeBreakdownMu.Lock()
	defer a.sizeBreakdownMu.Unlock()
	if time.Now().Before(a.cacheSizeExp) {
		return cloneEntries(a.cacheSizeCache)
	}
	entries := a.computeCacheSizeBreakdown()
	a.cacheSizeCache = entries
	a.cacheSizeExp = time.Now().Add(sizeBreakdownTTL)
	return cloneEntries(entries)
}

// computeCacheSizeBreakdown 实际扫描 cache/ 目录。
// 仅把真正的归档/安装包文件（以已知归档尾缀结尾）解析为 (tool, version)
// 并做 IsStale 标记；xdev 自身的元数据/校验文件（.json/.dat/.lock/.sha256 等）
// 原样展示（Tool=完整文件名，Version=""，IsStale=false），避免误标「未安装」。
func (a *App) computeCacheSizeBreakdown() []DiskUsageEntry {
	cfg := a.getCfg()
	root := cfg.Core.CacheDir
	installRoot := cfg.Core.InstallDir
	entries := make([]DiskUsageEntry, 0)

	files, err := os.ReadDir(root)
	if err != nil {
		return entries
	}
	for _, f := range files {
		if f.IsDir() {
			continue
		}
		info, err := f.Info()
		if err != nil {
			continue
		}
		name := f.Name()
		path := filepath.Join(root, name)
		e := DiskUsageEntry{
			Tool:     name, // 默认原样展示；归档文件下面会覆盖为解析出的工具名
			Path:     path,
			Size:     info.Size(),
			Category: cacheEntryCategory(name),
		}
		if !isMetadataFile(name) {
			tool, version := parseCacheName(name)
			e.Tool = tool
			e.Version = version
			if tool != "-" && version != "" {
				if _, err := os.Stat(filepath.Join(installRoot, tool, version)); err != nil {
					e.IsStale = true
				}
			}
		}
		entries = append(entries, e)
	}

	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Size != entries[j].Size {
			return entries[i].Size > entries[j].Size
		}
		if entries[i].Tool != entries[j].Tool {
			return entries[i].Tool < entries[j].Tool
		}
		return entries[i].Path < entries[j].Path
	})
	return entries
}

// InvalidateSizeBreakdown 使安装/缓存明细缓存立即过期，下次调用会重新扫描磁盘。
// 在安装完成、卸载、切换版本、清理缓存后调用，保证抽屉展示最新数据。
func (a *App) InvalidateSizeBreakdown() {
	a.sizeBreakdownMu.Lock()
	defer a.sizeBreakdownMu.Unlock()
	a.installSizeExp = time.Time{}
	a.cacheSizeExp = time.Time{}
}

// cloneEntries 返回 entries 的深拷贝（元素均为值类型，复制切片即复制元素），
// 避免调用方修改内部缓存。
func cloneEntries(entries []DiskUsageEntry) []DiskUsageEntry {
	if entries == nil {
		return make([]DiskUsageEntry, 0)
	}
	out := make([]DiskUsageEntry, len(entries))
	copy(out, entries)
	return out
}

// cacheTrailingSuffixes 是需要从缓存文件名尾部剥离的下载中间态/校验尾缀，
// 在解析工具名之前处理。
var cacheTrailingSuffixes = []string{".part", ".downloading", ".sha256"}

// cacheMetadataSuffixes 是 xdev 自身的元数据/校验文件尾缀（非工具归档）。
// 命中这些尾缀的文件不参与 tool-version 解析与 IsStale 标记，直接原样展示。
var cacheMetadataSuffixes = []string{
	".json.lock", ".json", ".dat", ".lock", ".sha256", ".txt", ".log",
}

// isMetadataFile 报告文件名是否为 xdev 元数据/校验文件。
// 命中即视为元数据：Tool 使用完整文件名，不做 tool-version 解析，
// 也不会被标记为「未安装」（IsStale 恒为 false）。
func isMetadataFile(name string) bool {
	lower := strings.ToLower(name)
	for _, suffix := range cacheMetadataSuffixes {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return false
}

// parseCacheName 从缓存文件名解析 (tool, version)。
// 规则：先剥掉 .part/.sha256/.downloading 尾缀；取第一个 "-" 前的部分为 Tool，
// 第一个 "-" 之后反复剥离归档扩展名（cache_classify.go 的 archiveSuffixes）
// 所得为 Version。无 "-" 时 Tool="-"、Version=""。
func parseCacheName(name string) (tool, version string) {
	base := name
	for _, suffix := range cacheTrailingSuffixes {
		if len(base) > len(suffix) && strings.HasSuffix(strings.ToLower(base), suffix) {
			base = base[:len(base)-len(suffix)]
			break
		}
	}

	idx := strings.Index(base, "-")
	if idx < 0 {
		return "-", ""
	}
	tool = base[:idx]

	rest := base[idx+1:]
	for {
		stripped := false
		for _, suffix := range archiveSuffixes {
			if len(rest) > len(suffix) && strings.HasSuffix(strings.ToLower(rest), suffix) {
				rest = rest[:len(rest)-len(suffix)]
				stripped = true
				break
			}
		}
		if !stripped {
			break
		}
	}
	return tool, rest
}
