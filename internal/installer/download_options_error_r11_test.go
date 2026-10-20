package installer

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/manifest"
)

// 第十一轮审计（Round 10 报告待办 7）：GUI 的 `dest == ""` 把错误**降级**成
// `"empty destination"`，真实原因完全丢失。
//
// 缺陷链：
//  1. `BuildDownloadOptions` 刻意不返回 error（四处调用点直接消费四个返回值）。
//  2. 缓存路径不安全时它把 dest 置空 —— **fail closed 是对的**。
//  3. 但原因被丢弃，`downloader.Download` 于是以 `"empty destination"` 失败。
//  4. GUI 安装面板把这个字符串直接呈现给用户：**无法定位**是工具名、版本号还是
//     format 非法。
//
// 修复：`BuildDownloadOptions` 记录精确原因，调用方在 `dest == ""` 时用
// `LastDownloadOptionsError()` 读回，GUI 以 `cannot start download: <真实原因>` 返回。
//
// 本守卫钉住三件事：① 失败时原因**可读且含具体组件**；② **成功时不残留**上一次的
// 错误（跨调用陈旧值是这类「旁路字段」最经典的坑）；③ fail-closed 契约本身不变。

// TestLastDownloadOptionsError_PopulatedOnUnsafeVersion 是核心守卫。
func TestLastDownloadOptionsError_PopulatedOnUnsafeVersion(t *testing.T) {
	ins, _, _ := newTestInstaller(t)

	arch := &manifest.Archive{
		OS: runtime.GOOS, Arch: runtime.GOARCH, Format: "zip",
		URL: "https://example.test/x.zip",
	}

	// 不安全版本号 → dest 必须为空（fail closed），且原因必须可读。
	_, _, dest, _ := ins.BuildDownloadOptions("safetool", "../../evil", arch)
	if dest != "" {
		t.Fatalf("前置条件失败：不安全版本号仍产出了 dest=%q（fail-closed 契约被破坏）", dest)
	}

	err := ins.LastDownloadOptionsError()
	if err == nil {
		t.Fatal("待办 7 复现：dest 为空但 LastDownloadOptionsError() 返回 nil —— " +
			"调用方只能看到 'empty destination'，真实原因丢失")
	}
	// 诊断必须能定位到**具体组件**，而不是泛泛的「路径不安全」。
	msg := err.Error()
	if !strings.Contains(msg, "safetool") {
		t.Errorf("错误未含工具名，无法定位: %v", msg)
	}
	if !strings.Contains(msg, "../../evil") {
		t.Errorf("错误未含被拒绝的版本号，无法定位: %v", msg)
	}
	// 不得只说 "empty destination"。
	if strings.TrimSpace(msg) == "empty destination" {
		t.Errorf("错误仍是降级后的 'empty destination': %v", msg)
	}
}

// 不安全工具名同源覆盖。
func TestLastDownloadOptionsError_PopulatedOnUnsafeTool(t *testing.T) {
	ins, _, _ := newTestInstaller(t)
	arch := &manifest.Archive{
		OS: runtime.GOOS, Arch: runtime.GOARCH, Format: "zip",
		URL: "https://example.test/x.zip",
	}
	_, _, dest, _ := ins.BuildDownloadOptions("../../evil", "1.0.0", arch)
	if dest != "" {
		t.Fatalf("前置条件失败：不安全工具名仍产出了 dest=%q", dest)
	}
	err := ins.LastDownloadOptionsError()
	if err == nil {
		t.Fatal("待办 7 复现：不安全工具名时原因丢失")
	}
	if !strings.Contains(err.Error(), "../../evil") {
		t.Errorf("错误未含被拒绝的工具名: %v", err)
	}
}

// **跨调用不得残留**：成功调用之后，上一次的错误必须被清掉。
//
// 这是「旁路字段」最经典的坑 —— 如果只在失败时赋值、成功时不重置，调用方会在一次
// 正常下载里读到一个**陈旧的**错误，给出完全误导的提示。
func TestLastDownloadOptionsError_ClearedOnSuccess(t *testing.T) {
	ins, _, _ := newTestInstaller(t)
	arch := &manifest.Archive{
		OS: runtime.GOOS, Arch: runtime.GOARCH, Format: "zip",
		URL: "https://example.test/x.zip",
	}

	// 先制造一次失败。
	_, _, badDest, _ := ins.BuildDownloadOptions("safetool", "../../evil", arch)
	if badDest != "" {
		t.Fatal("前置条件：不安全版本号应 fail closed")
	}
	if ins.LastDownloadOptionsError() == nil {
		t.Fatal("前置条件：失败后应有错误")
	}

	// 再做一次**合法**调用。
	_, _, goodDest, _ := ins.BuildDownloadOptions("safetool", "1.0.0", arch)
	if goodDest == "" {
		t.Fatal("合法调用不应 fail closed")
	}
	if err := ins.LastDownloadOptionsError(); err != nil {
		t.Errorf("待办 7 复现：成功调用后仍残留上一次的错误（调用方会给出误导提示）: %v", err)
	}
}

// fail-closed 契约本身不得改变：不安全名字**永不**产出 CacheDir 之外的路径。
func TestBuildDownloadOptions_FailClosedContractUnchanged(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)
	arch := &manifest.Archive{
		OS: runtime.GOOS, Arch: runtime.GOARCH, Format: "zip",
		URL: "https://example.test/x.zip",
	}
	for _, ver := range []string{"../../evil", `..\..\evil`, "..", "a/b", `a\b`} {
		_, _, dest, _ := ins.BuildDownloadOptions("safetool", ver, arch)
		if dest == "" {
			continue // 显式拒绝是可接受的
		}
		rel, err := filepath.Rel(cfg.Core.CacheDir, dest)
		if err != nil || strings.HasPrefix(rel, "..") {
			t.Errorf("cache destination %q escapes CacheDir (rel=%q err=%v)", dest, rel, err)
		}
	}
}

// 端到端：GUI 的 dest=="" 分支必须把真实原因带进错误（而不是 "empty destination"）。
//
// 源码级断言：GUI 在 dest=="" 时必须调用 LastDownloadOptionsError()。
func TestGUIInstall_SurfacesDownloadOptionsError(t *testing.T) {
	data, err := os.ReadFile("../gui/app_install.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	if !strings.Contains(src, "ins.LastDownloadOptionsError()") {
		t.Error("待办 7 复现：GUI 的 dest==\"\" 分支未读回 LastDownloadOptionsError()，" +
			"用户仍只会看到 'empty destination'")
	}
	// 必须在返回错误里带上原因（%w 包装），而不是忽略。
	if !strings.Contains(src, "cannot start download: %w") {
		t.Error("GUI 应把 BuildDownloadOptions 的真实原因包装进返回错误")
	}
}
