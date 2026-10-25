package plugin

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/manifest"
)

// Round 15 守卫：**字面版本**也必须能被 ResolveVersion 解析。
//
// 实测来源：`xdev install android-ndk`（不带 @version）在纯净 home 上直接失败：
//
//	[install] Starting install: tool=android-ndk spec=r29
//	[install] ERROR: resolve version failed for android-ndk@r29:
//	          unrecognised version spec: "r29"
//
// 原因是 install 把 `p.DefaultVersion()`（= manifest 的 latest 字面值）当成
// **版本 spec** 交给 resolver.Parse，而 Parse 只认 semver/约束/latest/lts。
// android-ndk 的版本名是 "r29"（字母开头，见随包 manifests/android-ndk.yaml），
// 于是 53 个随包工具里有 1 个**根本装不上**。
//
// 语义边界：只有清单里**确实存在**的同名字面版本才放行；垃圾输入继续报错
// （resolver_test.go 的 TestParseInvalid 契约不变）。
func TestR15_GenericResolveVersionAcceptsLiteralNonSemverVersion(t *testing.T) {
	g := NewGeneric(&manifest.Tool{
		Name:   "android-ndk",
		Latest: "r29",
		VersionList: []manifest.Version{
			{Version: "r29"},
			{Version: "26.3.11579264"},
			{Version: "25.2.9519653"},
		},
	})

	got, err := g.ResolveVersion("r29")
	if err != nil {
		t.Fatalf(`ResolveVersion("r29") 报错 %v —— 字面版本必须可解析`, err)
	}
	if got != "r29" {
		t.Fatalf(`ResolveVersion("r29") = %q, want "r29"`, got)
	}

	// 默认版本（install 不传 @version 时的入口）也必须自洽。
	if def := g.DefaultVersion(); def != "r29" {
		t.Fatalf("DefaultVersion() = %q, want r29", def)
	}
	if got, err := g.ResolveVersion(g.DefaultVersion()); err != nil || got != "r29" {
		t.Fatalf("默认版本无法自解析：got=%q err=%v", got, err)
	}
}

// 反向守卫：字面匹配只对「清单里存在」成立，垃圾输入仍必须报错。
func TestR15_GenericResolveVersionStillRejectsUnknownLiteral(t *testing.T) {
	g := NewGeneric(&manifest.Tool{
		Name:        "android-ndk",
		Latest:      "r29",
		VersionList: []manifest.Version{{Version: "r29"}},
	})
	if _, err := g.ResolveVersion("not-a-version!!!"); err == nil {
		t.Fatal("未知字面版本未被拒绝 —— 放行过宽")
	}
}

// 语料驱动（Round 14 风格）：把判据钉在**全部随包 manifest** 上——
// 每个工具的 latest / lts 别名都必须能被自己的 ResolveVersion 解析。
// 防的是「换个工具又踩同一个坑」，而不是只修 android-ndk。
func TestR15_ShippedLatestResolvesForEveryCatalogTool(t *testing.T) {
	dir := filepath.Join("..", "..", "manifests")
	cat, err := manifest.LoadCatalog(dir)
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	tools := cat.Tools()
	if len(tools) < 40 {
		t.Fatalf("catalog 只有 %d 个工具，语料读取异常，拒绝在空集上假绿", len(tools))
	}
	checked := 0
	for _, tool := range tools {
		g := NewGeneric(tool)
		for _, alias := range []string{tool.Latest, tool.LTS} {
			if strings.TrimSpace(alias) == "" {
				continue
			}
			if _, err := g.ResolveVersion(alias); err != nil {
				t.Errorf("%s: latest/lts 字面值 %q 无法解析: %v", tool.Name, alias, err)
			}
			checked++
		}
	}
	if checked < 40 {
		t.Fatalf("只检查了 %d 个 latest/lts 别名，语料读取异常", checked)
	}
}
