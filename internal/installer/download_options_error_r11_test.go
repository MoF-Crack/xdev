package installer

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
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
// # 第十二轮的重要修正：诊断信息**不能**走实例字段
//
// 第十一轮的修法是加一个 `lastBuildOptionsErr` 字段 + `LastDownloadOptionsError()`
// 访问器。它能工作，但**有一个隐含的单线程假设**：两个 goroutine 同时调
// `BuildDownloadOptions` 时会互相覆盖，让 A 调用方读到 B 的错误 —— 这既是真实的
// 数据竞争，也比「没有诊断」更糟（**错误的诊断信息会把排查引向无关的工具/版本**）。
//
// 第十二轮改为**无共享状态**：`BuildDownloadOptionsE` 把诊断错误作为第五个返回值。
// 本文件相应重写为断言新契约（**保证不变、机制更好**），并新增一条并发守卫钉住
// 「两个并发调用不能互相串味」—— 那条用例在旧的字段实现下**必然失败**。
//
// 本守卫钉住：① 失败时原因**可读且含具体组件**；② 成功时**没有**错误；
// ③ **并发调用互不串味**；④ fail-closed 契约本身不变；⑤ GUI 走无状态路径。

// TestBuildDownloadOptionsE_PopulatedOnUnsafeVersion 是核心守卫。
func TestBuildDownloadOptionsE_PopulatedOnUnsafeVersion(t *testing.T) {
	ins, _, _ := newTestInstaller(t)

	arch := &manifest.Archive{
		OS: runtime.GOOS, Arch: runtime.GOARCH, Format: "zip",
		URL: "https://example.test/x.zip",
	}

	// 不安全版本号 → dest 必须为空（fail closed），且原因必须可读。
	_, _, dest, _, err := ins.BuildDownloadOptionsE("safetool", "../../evil", arch)
	if dest != "" {
		t.Fatalf("前置条件失败：不安全版本号仍产出了 dest=%q（fail-closed 契约被破坏）", dest)
	}
	if err == nil {
		t.Fatal("待办 7 复现：dest 为空但错误为 nil —— " +
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
	if strings.TrimSpace(msg) == "empty destination" {
		t.Errorf("错误仍是降级后的 'empty destination': %v", msg)
	}
}

// 不安全工具名同源覆盖。
func TestBuildDownloadOptionsE_PopulatedOnUnsafeTool(t *testing.T) {
	ins, _, _ := newTestInstaller(t)
	arch := &manifest.Archive{
		OS: runtime.GOOS, Arch: runtime.GOARCH, Format: "zip",
		URL: "https://example.test/x.zip",
	}
	_, _, dest, _, err := ins.BuildDownloadOptionsE("../../evil", "1.0.0", arch)
	if dest != "" {
		t.Fatalf("前置条件失败：不安全工具名仍产出了 dest=%q", dest)
	}
	if err == nil {
		t.Fatal("待办 7 复现：不安全工具名时原因丢失")
	}
	if !strings.Contains(err.Error(), "../../evil") {
		t.Errorf("错误未含被拒绝的工具名: %v", err)
	}
}

// **成功时不得有错误**（返回值天然无残留 —— 这正是相对旧字段实现的优势）。
func TestBuildDownloadOptionsE_NoErrorOnSuccess(t *testing.T) {
	ins, _, _ := newTestInstaller(t)
	arch := &manifest.Archive{
		OS: runtime.GOOS, Arch: runtime.GOARCH, Format: "zip",
		URL: "https://example.test/x.zip",
	}

	// 先制造一次失败。
	if _, _, _, _, err := ins.BuildDownloadOptionsE("safetool", "../../evil", arch); err == nil {
		t.Fatal("前置条件：不安全版本号应报错")
	}
	// 再做一次**合法**调用：不得带出上一次的错误。
	_, _, dest, _, err := ins.BuildDownloadOptionsE("safetool", "1.0.0", arch)
	if dest == "" {
		t.Fatal("合法调用不应 fail closed")
	}
	if err != nil {
		t.Errorf("待办 7 复现：成功调用仍带出错误（旧字段实现会读到陈旧值）: %v", err)
	}
}

// **并发守卫**：两个 goroutine 用不同工具名交替调用，各自的诊断不得互相串味。
//
// 这是第十一轮「实例字段」实现**必然失败**的用例（A 读到 B 的错误），
// 也是它必须在第十二轮被替换掉的根本原因。
func TestBuildDownloadOptionsE_ConcurrentCallsDoNotCrossContaminate(t *testing.T) {
	ins, _, _ := newTestInstaller(t)
	arch := &manifest.Archive{
		OS: runtime.GOOS, Arch: runtime.GOARCH, Format: "zip",
		URL: "https://example.test/x.zip",
	}

	const perGoroutine = 200
	var wg sync.WaitGroup
	errCh := make(chan string, 2*perGoroutine)

	// 两个 goroutine 用**不同**的工具名做同样的非法版本号调用。
	for _, tool := range []string{"alpha", "bravo"} {
		tool := tool
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				_, _, _, _, err := ins.BuildDownloadOptionsE(tool, "../../evil", arch)
				if err == nil {
					errCh <- tool + ": 期望错误但为 nil"
					continue
				}
				// 本次调用的错误里必须含**自己的**工具名。
				if !strings.Contains(err.Error(), tool) {
					errCh <- tool + ": 错误串味了 -> " + err.Error()
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)

	n := 0
	for msg := range errCh {
		if n < 3 {
			t.Errorf("%s", msg)
		}
		n++
	}
	if n > 0 {
		t.Errorf("并发调用出现 %d 次错误串味/缺失（实例字段实现必然失败）", n)
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
		_, _, dest, _, _ := ins.BuildDownloadOptionsE("safetool", ver, arch)
		if dest == "" {
			continue // 显式拒绝是可接受的
		}
		rel, err := filepath.Rel(cfg.Core.CacheDir, dest)
		if err != nil || strings.HasPrefix(rel, "..") {
			t.Errorf("cache destination %q escapes CacheDir (rel=%q err=%v)", dest, rel, err)
		}
	}
}

// 端到端（源码级）：下载选项失败时的诊断不得丢失。
//
// 2026-10-25（架构收口）：GUI 不再自己构造下载选项 —— 整段安装已委托
// installer.InstallWithProgress，而它内部就用 BuildDownloadOptionsE 并
// 把诊断包进返回错误。
//
// 守卫意图不变（“dest 不安全时必须带得上真实原因”），但断言位置从
// 「GUI 源码文本」改到「installer 源码文本 + GUI 不再重复实现」：
//  1. installer 侧：必须用 BuildDownloadOptionsE 且用 %w 包装；
//  2. GUI 侧：不得再出现自己那套 BuildDownloadOptionsE 调用 ——
//     两份实现正是本仓库 Round 8-11 反复出现的失败模式。
func TestGUIInstall_SurfacesDownloadOptionsError(t *testing.T) {
	// 1. installer 侧必须保留诊断链路。
	insSrc, err := os.ReadFile("installer.go")
	if err != nil {
		t.Fatal(err)
	}
	ins := string(insSrc)
	if !strings.Contains(ins, "BuildDownloadOptionsE(") {
		t.Error("installer 应使用 BuildDownloadOptionsE 取诊断错误")
	}
	// 诊断错误必须被**返回**而不是吞掉。installer 的写法是
	// `if optErr != nil { ...; return optErr }` —— 直接透传已带上下文的
	// 错误，比再包一层前缀更清楚。
	if !strings.Contains(ins, "return optErr") {
		t.Error("installer 必须把 BuildDownloadOptionsE 的诊断错误返回给调用方")
	}

	// 2. GUI 侧不得自己再构造一遍下载选项。
	guiSrc, err := os.ReadFile("../gui/app_install.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(guiSrc), "ins.BuildDownloadOptionsE(") {
		t.Error("GUI 不得自己构造下载选项：安装已委托 InstallWithProgress，" +
			"两份实现会导致诊断/校验语义分叉")
	}
}

// 第十三轮内联后的**唯一性守卫**：全仓不得再有「四返回值 + 旁路字段/访问器」
// 那一套残留。留两个版本就有「行为分叉」的长期风险 —— 这正是本仓库
// Round 8-11 反复出现的失败模式。
func TestBuildDownloadOptions_OnlyOneVersionRemains(t *testing.T) {
	// 1. 全仓（含测试）不得再调用四返回值版 `BuildDownloadOptions(`。
	//    注意区分 `BuildDownloadOptionsE(` —— 用负向断言匹配调用形态。
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		// 跳过本守卫自身：它必须**写出**那个被禁的调用形态才能做断言。
		if f == "download_options_error_r11_test.go" {
			continue
		}
		data, rerr := os.ReadFile(f)
		if rerr != nil {
			continue
		}
		for i, line := range strings.Split(string(data), "\n") {
			// 跳过注释行。
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			if strings.Contains(line, ".BuildDownloadOptions(") {
				t.Errorf("%s:%d 仍在调用四返回值版 BuildDownloadOptions（应改用 E 版）: %s",
					f, i+1, strings.TrimSpace(line))
			}
		}
	}

	// 2. 生产代码不得再出现共享字段/弃用访问器/四返回值版定义。
	//    判据必须**剥掉注释** —— 迁移说明的注释里恰恰写着这些名字
	//    （本守卫初版就因此对自己误报）。
	srcRaw, err := os.ReadFile("installer.go")
	if err != nil {
		t.Fatal(err)
	}
	var codeOnly strings.Builder
	for _, line := range strings.Split(string(srcRaw), "\n") {
		if idx := strings.Index(line, "//"); idx >= 0 {
			line = line[:idx]
		}
		codeOnly.WriteString(line)
		codeOnly.WriteString("\n")
	}
	code := codeOnly.String()

	for _, gone := range []string{
		"lastBuildOptionsErr",
		"func (i *Installer) LastDownloadOptionsError(",
		"func (i *Installer) BuildDownloadOptions(",
	} {
		if strings.Contains(code, gone) {
			t.Errorf("installer.go 仍残留 %q —— 四返回值/共享字段那一套应已完全内联掉", gone)
		}
	}

	// 3. 唯一入口必须存在且带 error。
	if !strings.Contains(code, "func (i *Installer) BuildDownloadOptionsE(") {
		t.Error("installer.go 必须有 BuildDownloadOptionsE")
	}
}
