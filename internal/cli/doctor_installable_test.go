// doctor_installable_test.go — 票 18 / P2-29：doctor 的「不可安装」提示。
package cli

import (
	"testing"

	"github.com/xmdemt/xdev/internal/manifest"
)

// TestNotInstallableTools 锁定 doctor 提示的取值口径：只列 LoadCatalog 标记为
// 不可安装的工具，按名字排序（输出稳定、便于断言与人工核对）。
func TestNotInstallableTools(t *testing.T) {
	tools := []*manifest.Tool{
		{Name: "zeta", Installable: false, VersionList: []manifest.Version{{Version: "1.0.0"}}},
		{Name: "alpha", Installable: false, VersionList: []manifest.Version{{Version: "1.0.0"}}},
		{Name: "ok", Installable: true, VersionList: []manifest.Version{{Version: "1.0.0"}}},
		// 非工具 YAML 的产物（实测 index.yaml → 名为 index 的零版本条目）不得入列。
		{Name: "index", Installable: false},
		nil,
	}
	got := notInstallableTools(tools)
	if len(got) != 2 || got[0] != "alpha" || got[1] != "zeta" {
		t.Fatalf("notInstallableTools = %v, want [alpha zeta]（排序、跳过可安装项与零版本伪条目）", got)
	}
	if len(notInstallableTools([]*manifest.Tool{{Name: "a", Installable: true}})) != 0 {
		t.Error("全部可安装时必须返回空列表")
	}
}
