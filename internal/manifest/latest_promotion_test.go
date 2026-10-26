package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 回归守卫（2026-10-26 架构评估）：latest 晋升必须检查候选**可安装**。
//
// # 修复前的缺陷
//
// discovery/scheduler.go 的晋升判据只有 `util.CompareSemver(dv, latest) > 0`，
// 完全不看候选是否带 archives。嗅探发现的版本常常只有版本号（占位条目），
// 一旦被推成 latest，`xdev install <tool>`（默认装 latest）就因「无 archive」
// 直接失败。
//
// 实测命中 android-ndk：latest 从 r29（有 archives）被推到 "30"（无 archives）。
func TestCanPromoteLatest_RejectsVersionWithoutArchives(t *testing.T) {
	versions := []Version{
		{Version: "r29", Archives: []Archive{{OS: "windows", Arch: "amd64", URL: "https://example.test/r29.zip", Format: "zip"}}},
		{Version: "30"}, // 占位：无 archives
	}
	if CanPromoteLatest(versions, "30", "r29", true) {
		t.Error("无 archives 的占位版本不得被推成 latest —— " +
			"否则 `xdev install <tool>` 默认装 latest 会直接失败（android-ndk 实测）")
	}
}

// TestCanPromoteLatest_AcceptsInstallableVersion 确认正常晋升仍工作。
func TestCanPromoteLatest_AcceptsInstallableVersion(t *testing.T) {
	versions := []Version{
		{Version: "1.0.0", Archives: []Archive{{OS: "windows", Arch: "amd64", URL: "https://example.test/1.zip", Format: "zip"}}},
		{Version: "1.1.0", Archives: []Archive{{OS: "windows", Arch: "amd64", URL: "https://example.test/2.zip", Format: "zip"}}},
	}
	if !CanPromoteLatest(versions, "1.1.0", "1.0.0", true) {
		t.Error("更高且可安装的版本应被晋升")
	}
}

// TestCanPromoteLatest_RejectsDowngrade 确认不降级。
func TestCanPromoteLatest_RejectsDowngrade(t *testing.T) {
	versions := []Version{
		{Version: "1.0.0", Archives: []Archive{{OS: "windows", Arch: "amd64", URL: "https://e.test/1.zip", Format: "zip"}}},
		{Version: "2.0.0", Archives: []Archive{{OS: "windows", Arch: "amd64", URL: "https://e.test/2.zip", Format: "zip"}}},
	}
	if CanPromoteLatest(versions, "1.0.0", "2.0.0", true) {
		t.Error("低版本不得覆盖更高的 latest")
	}
}

// TestCanPromoteLatest_RejectsSameVersion 确认等值不写回（保住注释）。
func TestCanPromoteLatest_RejectsSameVersion(t *testing.T) {
	versions := []Version{
		{Version: "1.0.0", Archives: []Archive{{OS: "windows", Arch: "amd64", URL: "https://e.test/1.zip", Format: "zip"}}},
	}
	if CanPromoteLatest(versions, "1.0.0", "1.0.0", true) {
		t.Error("候选与现值相同时不应写回")
	}
}

// TestCanPromoteLatest_StableNotHijackedByPrerelease 确认稳定 latest
// 不被预发布劫持。
func TestCanPromoteLatest_StableNotHijackedByPrerelease(t *testing.T) {
	versions := []Version{
		{Version: "1.0.0", Archives: []Archive{{OS: "windows", Arch: "amd64", URL: "https://e.test/1.zip", Format: "zip"}}},
		{Version: "1.1.0-alpha.1", Archives: []Archive{{OS: "windows", Arch: "amd64", URL: "https://e.test/a.zip", Format: "zip"}}},
	}
	if CanPromoteLatest(versions, "1.1.0-alpha.1", "1.0.0", false) {
		t.Error("稳定版 latest 不得被 alpha 劫持")
	}
}

// TestCanPromoteLatest_PrereleaseSameChannelPromotes 确认同通道内可推进。
func TestCanPromoteLatest_PrereleaseSameChannelPromotes(t *testing.T) {
	versions := []Version{
		{Version: "1.1.0-rc.1", Archives: []Archive{{OS: "windows", Arch: "amd64", URL: "https://e.test/r1.zip", Format: "zip"}}},
		{Version: "1.1.0-rc.2", Archives: []Archive{{OS: "windows", Arch: "amd64", URL: "https://e.test/r2.zip", Format: "zip"}}},
	}
	if !CanPromoteLatest(versions, "1.1.0-rc.2", "1.1.0-rc.1", true) {
		t.Error("同一 rc 通道内更高版本应被晋升")
	}
}

// TestCanPromoteLatest_NPMKindCountsAsInstallable 确认 npm kind 的
// archive 也算可安装（它没有 url，走 npm 安装）。
func TestCanPromoteLatest_NPMKindCountsAsInstallable(t *testing.T) {
	versions := []Version{
		{Version: "1.0.0", Archives: []Archive{{OS: "any", Arch: "any", Format: "zip", URL: "https://e.test/1.zip"}}},
		{Version: "1.1.0", Archives: []Archive{{OS: "any", Arch: "any", InstallKind: InstallKindNPM}}},
	}
	if !CanPromoteLatest(versions, "1.1.0", "1.0.0", true) {
		t.Error("npm kind 的 archive 应视为可安装")
	}
}

// TestCanPromoteLatest_EmptyCurrent 确认首次确定 latest 时只看可安装性。
func TestCanPromoteLatest_EmptyCurrent(t *testing.T) {
	versions := []Version{
		{Version: "1.0.0", Archives: []Archive{{OS: "windows", Arch: "amd64", URL: "https://e.test/1.zip", Format: "zip"}}},
	}
	if !CanPromoteLatest(versions, "1.0.0", "", true) {
		t.Error("latest 为空时应允许确定")
	}
}

// 源码级守卫：两处晋升调用点必须用同一个判据。
//
// 修复前 scheduler 用 util.CompareSemver、refresh 用 VersionReleaseNewer ——
// 两套排序在「数字段相同、发布日期不同」时结论相反。本守卫确保
// 两边都走 manifest.CanPromoteLatest，不会再分叉。
func TestLatestPromotion_UsesSingleJudge(t *testing.T) {
	// scheduler 侧
	src, err := readRepoFile(t, "internal/discovery/scheduler.go")
	if err != nil {
		t.Fatal(err)
	}
	if !contains(src, "manifest.CanPromoteLatest") {
		t.Error("discovery/scheduler.go 必须用 manifest.CanPromoteLatest（或其 WithOptions 形式）做晋升判据")
	}
	if contains(src, "util.CompareSemver(dv.Version") {
		t.Error("discovery/scheduler.go 不得再用 CompareSemver 决定 latest 晋升（会与 refresh 的判据分裂）")
	}

	// refresh 侧
	rsrc, err := readRepoFile(t, "internal/cli/refresh.go")
	if err != nil {
		t.Fatal(err)
	}
	if !contains(rsrc, "manifest.CanPromoteLatest(") {
		t.Error("cli/refresh.go 必须用 manifest.CanPromoteLatest 做晋升判据")
	}
}

// readRepoFile 读仓库内的源文件（从测试目录回溯到仓库根）。
func readRepoFile(t *testing.T, rel string) (string, error) {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := wd
	for i := 0; i < 5; i++ {
		p := filepath.Join(dir, rel)
		if b, rerr := os.ReadFile(p); rerr == nil {
			return string(b), nil
		}
		dir = filepath.Dir(dir)
	}
	return "", fmt.Errorf("找不到 %s（从 %s 向上查找）", rel, wd)
}

func contains(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}
