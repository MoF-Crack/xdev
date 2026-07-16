package manifest

import (
	"strings"
	"testing"
)

// TestMirrorURLsAfterTask16 verifies that manifest mirror_url fields have
// been updated to the verified-reachable mirror sources identified in
// Task 16 (follow-up to the Task 13-14 probe audit that found many
// manifests pointed mirror_url at 404 or root-directory paths).
//
// This is a string-pattern test (no network) so it runs in the normal
// test suite and guards against regressions to the stale mirrors.
func TestMirrorURLsAfterTask16(t *testing.T) {
	dir := "../manifests"
	cat, err := LoadCatalog(dir)
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}

	// Each entry maps a tool name to the prefix that EVERY archive's
	// mirror_url must start with after the Task 16 rewrite.
	prefixByTool := map[string]string{
		"bun":                    "https://npmmirror.com/mirrors/bun/bun-v",
		"cmake":                  "https://cmake.org/files/v",
		"deno":                   "https://npmmirror.com/mirrors/deno/v",
		"dotnet":                 "https://dotnetcli.azureedge.net/dotnet/Sdk/",
		"gradle":                 "https://repo.huaweicloud.com/gradle/gradle-",
		"julia":                  "https://mirrors.tuna.tsinghua.edu.cn/julia-releases/bin/",
		"android-platform-tools": "https://redirector.gvt1.com/edgedl/android/repository/",
		"android-build-tools":    "https://redirector.gvt1.com/edgedl/android/repository/",
		"android-cmdline-tools":  "https://redirector.gvt1.com/edgedl/android/repository/",
		"android-ndk":            "https://redirector.gvt1.com/edgedl/android/repository/",
		"android-platform":       "https://redirector.gvt1.com/edgedl/android/repository/",
		"android-emulator":       "https://redirector.gvt1.com/edgedl/android/repository/",
	}

	for _, tool := range cat.Tools() {
		wantPrefix, isTarget := prefixByTool[tool.Name]
		if !isTarget {
			continue // tool not in scope of Task 16
		}
		if len(tool.VersionList) == 0 {
			t.Errorf("%s: no versions", tool.Name)
			continue
		}
		for vi := range tool.VersionList {
			v := &tool.VersionList[vi]
			for ai := range v.Archives {
				a := &v.Archives[ai]
				m := a.MirrorURL
				if m == "" {
					t.Errorf("%s@%s %s/%s: mirror_url is empty, want prefix %q",
						tool.Name, v.Version, a.OS, a.Arch, wantPrefix)
					continue
				}
				if !strings.HasPrefix(m, wantPrefix) {
					t.Errorf("%s@%s %s/%s: mirror_url %q does not start with %q",
						tool.Name, v.Version, a.OS, a.Arch, m, wantPrefix)
				}
			}
		}
	}
}

// TestErlangMirrorURLPolicy verifies erlang's split mirror policy:
//   - Windows: erlang.org/download/otp_win64_{version}.exe (official 2nd CDN)
//   - Unix (linux/darwin): no mirror_url (field empty) so the runtime
//     falls back to the official GitHub source tarball, because no
//     verified working mirror exists for the Unix tarball.
func TestErlangMirrorURLPolicy(t *testing.T) {
	dir := "../manifests"
	cat, err := LoadCatalog(dir)
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	tool, ok := cat.Tool("erlang")
	if !ok {
		t.Fatalf("erlang tool not found in catalog")
	}
	for vi := range tool.VersionList {
		v := &tool.VersionList[vi]
		for ai := range v.Archives {
			a := &v.Archives[ai]
			switch a.OS {
			case "windows":
				want := "https://erlang.org/download/otp_win64_" + v.Version + ".exe"
				if a.MirrorURL != want {
					t.Errorf("erlang@%s windows/%s: mirror_url = %q, want %q",
						v.Version, a.Arch, a.MirrorURL, want)
				}
			default:
				if a.MirrorURL != "" {
					t.Errorf("erlang@%s %s/%s: mirror_url should be empty (no working Unix mirror), got %q",
						v.Version, a.OS, a.Arch, a.MirrorURL)
				}
			}
		}
	}
}

// TestNoStaleMirrorURLs verifies that none of the manifests still reference
// the mirror prefixes that the Task 13-14 probe audit identified as 404
// or root-directory (unreachable) paths.
func TestNoStaleMirrorURLs(t *testing.T) {
	dir := "../manifests"
	cat, err := LoadCatalog(dir)
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	stalePrefixes := []string{
		// Bun: TUNA github-release path (404)
		"https://mirrors.tuna.tsinghua.edu.cn/github-release/oven-sh/bun",
		// CMake: TUNA github-release path (404)
		"https://mirrors.tuna.tsinghua.edu.cn/github-release/Kitware/CMake",
		// Deno: TUNA github-release path (404)
		"https://mirrors.tuna.tsinghua.edu.cn/github-release/denoland/deno",
		// Dotnet: aliyun dotnet mirror (404)
		"https://mirrors.aliyun.com/dotnet/",
		// Erlang: aliyun erlang mirror (404)
		"https://mirrors.aliyun.com/erlang/",
		// Gradle: aliyun macports path (stale/unverified)
		"https://mirrors.aliyun.com/macports/distfiles/gradle/",
		// Julia: TUNA /julia/releases/ path (404, wrong path)
		"https://mirrors.tuna.tsinghua.edu.cn/julia/releases/",
		// Android family: aliyun android.googlesource.com (root/404)
		"https://mirrors.aliyun.com/android.googlesource.com",
	}
	for _, tool := range cat.Tools() {
		for vi := range tool.VersionList {
			v := &tool.VersionList[vi]
			for ai := range v.Archives {
				a := &v.Archives[ai]
				for _, stale := range stalePrefixes {
					if strings.HasPrefix(a.MirrorURL, stale) {
						t.Errorf("%s@%s %s/%s: mirror_url %q still references stale prefix %q",
							tool.Name, v.Version, a.OS, a.Arch, a.MirrorURL, stale)
					}
				}
			}
		}
	}
}
