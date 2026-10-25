//go:build windows

package shim

import (
	"os/exec"
	"syscall"
	"testing"
)

// 回归守卫（2026-10-25）：shim 在无可见控制台时，必须给子进程设置
// HideWindow + CREATE_NO_WINDOW，否则每启动一个长驻工具（语言服务器、
// MCP server）都会弹出一个关不掉的终端窗口。
//
// 缺陷背景：shim（E:\XDev\shims\*.exe）是 GUI 子系统二进制（PE Subsystem=2），
// 自身没有控制台。当 IDE 扩展 / MCP 服务把它当控制台工具调用时，Windows
// 会分配一个新的可见控制台窗口；shim 再 exec 真正的工具时若不带
// CREATE_NO_WINDOW，子进程就继承该窗口。jedi-language-server 与 MCP server
// 都是长驻进程 → 窗口一直不销毁，用户关掉后又被 IDE 重启。

// TestChildSysProcAttr_HidesWindowWhenNoConsole 验证决策函数：
// 无可见控制台时必须返回 HideWindow + CREATE_NO_WINDOW 的 SysProcAttr；
// 有可见控制台（交互式终端）时必须返回 nil（保持继承，不干扰 stdin/stdout）。
func TestChildSysProcAttr_HidesWindowWhenNoConsole(t *testing.T) {
	got := childSysProcAttrFor(!hasVisibleConsole())

	if hasVisibleConsole() {
		if got != nil {
			t.Errorf("有可见控制台时应返回 nil（保留窗口），实际得到 %+v", got)
		}
		return
	}

	if got == nil {
		t.Fatal("无可见控制台时必须设置 SysProcAttr，实际为 nil（会导致弹窗）")
	}
	if !got.HideWindow {
		t.Error("SysProcAttr.HideWindow 必须为 true")
	}
	if got.CreationFlags&0x08000000 == 0 {
		t.Errorf("CreationFlags 必须包含 CREATE_NO_WINDOW(0x08000000)，实际 0x%X", got.CreationFlags)
	}
}

// TestChildSysProcAttr_ExplicitBothWays 不依赖运行环境，直接覆盖两个分支。
func TestChildSysProcAttr_ExplicitBothWays(t *testing.T) {
	// hide=false：无论环境如何都必须返回 nil。
	if got := childSysProcAttrFor(false); got != nil {
		t.Errorf("hide=false 应返回 nil，实际 %+v", got)
	}

	// hide=true：必须带两个标志。
	got := childSysProcAttrFor(true)
	if got == nil {
		t.Fatal("hide=true 必须返回非 nil SysProcAttr")
	}
	if !got.HideWindow {
		t.Error("hide=true 时 HideWindow 必须为 true")
	}
	if got.CreationFlags != 0x08000000 {
		t.Errorf("hide=true 时 CreationFlags 应为 CREATE_NO_WINDOW(0x08000000)，实际 0x%X", got.CreationFlags)
	}
}

// TestHasVisibleConsole_Stable 确认判定函数可重复调用且不 panic。
func TestHasVisibleConsole_Stable(t *testing.T) {
	a := hasVisibleConsole()
	b := hasVisibleConsole()
	if a != b {
		t.Errorf("hasVisibleConsole 两次调用结果不一致: %v vs %v", a, b)
	}
}

// 编译期断言：确保我们用的类型与 exec.Cmd.SysProcAttr 兼容。
var _ = func() *syscall.SysProcAttr { return childSysProcAttrFor(true) }
var _ = exec.Command
