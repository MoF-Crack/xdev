//go:build !bindings

package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TC-34-2 单命令不再有固定 100ms 等待：main.go 的 time.Sleep 必须包在
// `if startupSniffPending`（由 cli.HasStartupSniff() 求出）条件内，
// 且全文件只保留这一处 sleep（票 34 / P1-30 / E-11）。
func TestMainStartupSleep_IsConditional(t *testing.T) {
	data, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)

	if n := strings.Count(src, "time.Sleep("); n != 1 {
		t.Errorf("main.go time.Sleep call sites = %d, want exactly 1 (条件化后的启动嗅探等待)", n)
	}
	// 条件来源必须是 cli.HasStartupSniff()。
	if !strings.Contains(src, "cli.HasStartupSniff()") {
		t.Error("main.go 必须用 cli.HasStartupSniff() 求等待条件（含 interval TTL 到期语义）")
	}
	guarded := regexp.MustCompile(`(?s)if\s+startupSniffPending\s*\{\s*\n\s*time\.Sleep\(`)
	if !guarded.MatchString(src) {
		t.Error("main.go 的 time.Sleep 必须位于 if startupSniffPending 分支内（不得无条件等待）")
	}
	// 判定必须在 ShutdownSniffScheduler 之前取值：Shutdown 会把
	// sniff-cache 的 last_sniff_at 刷成 now，令 interval TTL 判定失真。
	iAssign := strings.Index(src, "startupSniffPending := cli.HasStartupSniff()")
	iShutdown := strings.LastIndex(src, "cli.ShutdownSniffScheduler()")
	if iAssign < 0 || iShutdown < 0 || iAssign > iShutdown {
		t.Error("main.go 必须在 ShutdownSniffScheduler() 之前求 HasStartupSniff()")
	}
}

// TC-34-2b 零参 GUI 双击路径（main.go 的 len(os.Args)<=1 → runGUI）不得新增
// 启动嗅探：加了会与 `xdev gui` 路径双嗅探（票 34 第 3 条）。
func TestZeroArgGUI_NoStartupSniff(t *testing.T) {
	mainSrc, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(mainSrc)
	// HasStartupSniff 只允许出现在 root 命令构建之后（零参 GUI 分支在
	// cli.NewRootCommand 之前 return，用位置判定而非跨行正则）。
	iRoot := strings.Index(src, "cli.NewRootCommand(")
	iSniff := strings.Index(src, "HasStartupSniff(")
	if iRoot < 0 || iSniff < 0 {
		t.Fatal("main.go: cli.NewRootCommand / cli.HasStartupSniff 未找到")
	}
	if iSniff < iRoot {
		t.Error("零参 GUI 分支（cli.NewRootCommand 之前）不得触发启动嗅探")
	}
	for _, f := range []string{"main_gui.go", "main.go"} {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), ".OnStartup(") {
			t.Errorf("%s 不得调用 Scheduler.OnStartup（票 34：三入口由 cli 侧挂载）", f)
		}
	}
}
