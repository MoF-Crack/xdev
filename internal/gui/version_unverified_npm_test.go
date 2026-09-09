// Spec 039: npm-kind versions skip the "unverified archive URL" gate —
// no archive is downloaded (npm resolves the tree and verifies registry
// integrity itself), so the confirmation dialog would be pure noise.
package gui

import (
	"testing"

	"github.com/xmdemt/xdev/internal/manifest"
)

func TestVersionUnverified_NPMKindExempt(t *testing.T) {
	tool := &manifest.Tool{
		Name: "dsh",
		VersionList: []manifest.Version{
			{
				Version:    "0.1.2-rc.1",
				Unverified: true, // sniff-discovered
				Archives: []manifest.Archive{
					{OS: "any", Arch: "any", Format: "tar.gz", URL: "https://registry.npmjs.org/x.tgz", InstallKind: manifest.InstallKindNPM},
				},
			},
		},
	}
	if versionUnverified(tool, "0.1.2-rc.1") {
		t.Error("npm-kind version must not trigger the unverified dialog")
	}
}

func TestVersionUnverified_ArchiveKindStillGated(t *testing.T) {
	tool := &manifest.Tool{
		Name: "opencode",
		VersionList: []manifest.Version{
			{
				Version:    "1.18.21",
				Unverified: true,
				Archives: []manifest.Archive{
					{OS: "windows", Arch: "amd64", Format: "tar.gz", URL: "https://example.test/opencode.tgz"},
				},
			},
		},
	}
	if !versionUnverified(tool, "1.18.21") {
		t.Error("archive-kind unverified version must keep the confirmation gate")
	}
}

// 无 archives 的版本（auto_archive_fill 关闭的嗅探产物）回退按工具其他
// 归档判定 npm-kind。
func TestVersionUnverified_NPMKindFallbackNoArchives(t *testing.T) {
	tool := &manifest.Tool{
		Name: "dsh",
		VersionList: []manifest.Version{
			{Version: "0.1.2-rc.1", Stable: false, Archives: []manifest.Archive{
				{OS: "any", Arch: "any", InstallKind: manifest.InstallKindNPM},
			}},
			{Version: "0.1.3-rc.1", Unverified: true}, // 嗅探合并产物：无 archives
		},
	}
	if versionUnverified(tool, "0.1.3-rc.1") {
		t.Error("no-archive version of an npm-kind tool must skip the unverified dialog")
	}
}

func TestVersionUnverified_UnknownVersion(t *testing.T) {
	tool := &manifest.Tool{Name: "dsh"}
	if versionUnverified(tool, "9.9.9") {
		t.Error("unknown version must not report unverified")
	}
	if versionUnverified(nil, "1.0.0") {
		t.Error("nil manifest must not report unverified")
	}
}
