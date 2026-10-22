// Spec 039: npm-kind versions skip the CLI's unverified-version prompt —
// no archive is downloaded (npm resolves the tree and verifies registry
// integrity itself), so the confirmation gate would be noise.
package cli

import (
	"testing"

	"github.com/xmdemt/xdev/internal/manifest"
)

func TestVersionNPMManaged(t *testing.T) {
	npmArchive := manifest.Archive{OS: "any", Arch: "any", Format: "tar.gz", InstallKind: manifest.InstallKindNPM}

	// 版本自身归档声明 npm → 命中。
	tool := &manifest.Tool{
		Name: "dsh",
		VersionList: []manifest.Version{
			{Version: "0.1.2-rc.1", Unverified: true, Archives: []manifest.Archive{npmArchive}},
		},
	}
	if !versionNPMManaged(tool, &tool.VersionList[0]) {
		t.Error("version with install_kind:npm archives must be npm-managed")
	}

	// 无 archives 版本回退按工具其他归档判定。
	tool2 := &manifest.Tool{
		Name: "dsh",
		VersionList: []manifest.Version{
			{Version: "0.1.2-rc.1", Archives: []manifest.Archive{npmArchive}},
			{Version: "0.1.3-rc.1", Unverified: true},
		},
	}
	if !versionNPMManaged(tool2, &tool2.VersionList[1]) {
		t.Error("no-archive version of an npm-kind tool must fall back to tool-level detection")
	}

	// 归档类工具不命中。
	tool3 := &manifest.Tool{
		Name: "opencode",
		VersionList: []manifest.Version{
			{Version: "1.18.21", Unverified: true, Archives: []manifest.Archive{
				{OS: "windows", Arch: "amd64", Format: "tar.gz"},
			}},
		},
	}
	if versionNPMManaged(tool3, &tool3.VersionList[0]) {
		t.Error("archive-kind version must not be npm-managed")
	}
}
