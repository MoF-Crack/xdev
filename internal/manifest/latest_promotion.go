package manifest

import "github.com/xmdemt/xdev/internal/util"

// 本文件统一「latest 该不该推进」这一判据。
//
// # 为什么需要统一（2026-10-26 架构评估修复）
//
// 修复前有两个地方各自决定 latest 是否推进，且判据不同：
//
//	internal/discovery/scheduler.go  → util.CompareSemver(dv, latest) > 0
//	internal/cli/refresh.go          → manifest.VersionReleaseNewer(...)
//
// 两套排序在「数字段相同、发布日期不同」（真实语料 clion/goland/webstorm
// 的 2026.2 vs 2026.2.0.1）时结论相反，R8-2 已在 refresh 侧统一为全序，
// 但 scheduler 侧仍是 CompareSemver。于是「修了 refresh 会被 scheduler
// 覆盖回去」。
//
// 更严重的是两边**都没检查候选版本是否可安装**：嗅探发现的版本常常只有
// 版本号、没有 archives（占位条目），一旦被推成 latest，
// `xdev install <tool>`（默认版本 = latest）就会因为「无 archive」而失败。
// 实测命中 android-ndk：latest 从 r29（有 archives）被推到 "30"（无 archives）。
//
// 现在两处都调 CanPromoteLatest，判据只有一个。

// PromoteOptions 是 CanPromoteLatestWithOptions 的开关集。
type PromoteOptions struct {
	// AllowPrerelease 为真时允许预发布候选推进预发布 latest（同通道）。
	AllowPrerelease bool

	// RequireArchives 为真时，候选必须至少有一个结构完整的 archive
	// 才能被晋升（防止占位版本成为默认安装目标）。
	//
	// 默认应为 true。拒绝它只在「用户显式关掉 auto_archive_fill」时
	// 才有意义 —— 那种配置下所有发现版本都没 archives，仍然强制
	// 要求会让 latest 永远推不动，整个嗅探功能失效。
	RequireArchives bool
}

// CanPromoteLatest 是 CanPromoteLatestWithOptions 的默认形式：
// 允许预发布同通道推进，**要求**候选可安装。
func CanPromoteLatest(versions []Version, candidate, current string, allowPrerelease bool) bool {
	return CanPromoteLatestWithOptions(versions, candidate, current, PromoteOptions{
		AllowPrerelease: allowPrerelease,
		RequireArchives: true,
	})
}

// CanPromoteLatestWithOptions 是完整形式。
func CanPromoteLatestWithOptions(versions []Version, candidate, current string, opts PromoteOptions) bool {
	if candidate == "" || candidate == current {
		return false
	}

	// 条件 1：候选必须存在；RequireArchives 时必须可安装。
	cand, ok := findVersion(versions, candidate)
	if !ok {
		return false
	}
	if opts.RequireArchives && !hasCompleteArchive(cand) {
		return false
	}

	// 条件 3：预发布通道。
	if IsPrerelease(candidate) && !opts.AllowPrerelease {
		return false
	}

	// 条件 2：严格更新（全序）。
	if current == "" {
		return true
	}
	cur, ok := findVersion(versions, current)
	if !ok {
		// 当前 latest 不在列表里（可能是手写的旧值）：只要候选可安装就推进。
		return true
	}
	return VersionReleaseNewer(cand, cur)
}

// IsPrerelease 报告版本串是否为预发布（含 '-' 的 SemVer）。
//
// 委托给 util.IsPrereleaseVersion —— 全仓只有一个预发布判定实现，
// 避免 manifest / discovery / util 三处各写一份正则。
func IsPrerelease(v string) bool {
	return util.IsPrereleaseVersion(v)
}

// findVersion 在列表里按版本串查找。
func findVersion(versions []Version, v string) (Version, bool) {
	for _, item := range versions {
		if item.Version == v {
			return item, true
		}
	}
	return Version{}, false
}

// hasCompleteArchive 报告该版本是否至少有一个结构完整的 archive。
//
// 与 Archive.Installable() 同判据（url+format，或 npm kind），
// 但不限定平台 —— 晋升决策与宿主平台无关：一个只有 windows archive
// 的工具在 Linux 上依然应该能作为 latest（用户可能就是想装它）。
func hasCompleteArchive(v Version) bool {
	for _, a := range v.Archives {
		if a.Installable() {
			return true
		}
	}
	return false
}
