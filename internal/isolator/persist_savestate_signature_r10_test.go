// persist_savestate_signature_r10_test.go 是第十轮 P2-2 的**签名契约**守卫。
//
// 与 persist_savestate_error_r10_test.go（行为级：Setup 必须报错并回滚）分文件
// 存放，沿用第九轮 A-02 的既有约定（见 persist_corrupt_a02_loadstate_test.go 的
// 文件头注释）：本文件断言 `saveState(...) error` 这个新签名，它在**修复前的
// 源码上根本无法编译**，而行为级守卫可以在未修复的源码上直接跑出红。
//
// 契约本身是 P2-2 的根因：修复前 saveState 无返回值，写盘失败（MkdirAll /
// Write / Close / rename 任一步）只 log 一行就返回，调用方（container.go:143、
// nix.go:81）无从得知，于是 CLI 打印 `✔ isolated environment created` 而磁盘上
// 没有任何句柄 / 归属记录。
package isolator

import (
	"strings"
	"testing"
)

// TestR10_SaveStateReportsFailure：<home>/isolate 被造成文件时 MkdirAll 必失败，
// saveState 必须把失败作为 error 返回（审计现场手法：
// `nix setup err=<nil> state_exists=false`）。
func TestR10_SaveStateReportsFailure(t *testing.T) {
	home := t.TempDir()
	blockStateDir(t, home) // <home>/isolate 是文件 → MkdirAll 必失败

	err := saveState(home, string(ModeNix), persistedState{FlakeDir: home})
	if err == nil {
		t.Fatal("P2-2 复现：saveState 写盘失败却返回 nil —— 调用方（Setup）会继续报成功，" +
			"容器句柄 / 文件归属记录永久丢失，环境永久泄漏")
	}
	if !strings.Contains(err.Error(), "isolate") {
		t.Errorf("错误信息必须指出失败的 state 路径，got: %v", err)
	}
}

// TestR10_SaveStateReportsRenameFailure：第二个被吞掉的失败点是 rename
// （persist.go:102-104）。把 state 文件路径本身造成目录，rename 必然失败。
func TestR10_SaveStateReportsRenameFailure(t *testing.T) {
	home := t.TempDir()
	blockStateFileAsDir(t, home, string(ModeContainer))

	err := saveState(home, string(ModeContainer), persistedState{ContainerID: "abc"})
	if err == nil {
		t.Fatal("P2-2 复现：rename 失败（state 路径被目录占位）时 saveState 返回 nil")
	}
	if !strings.Contains(err.Error(), "state") {
		t.Errorf("错误必须点明 state 发布失败，got: %v", err)
	}
}

// TestR10_SaveStateSuccessStillSilent：防「修复把正常路径弄红」——成功落盘
// 必须返回 nil，且内容可完整读回。
func TestR10_SaveStateSuccessStillSilent(t *testing.T) {
	home := t.TempDir()
	if err := saveState(home, string(ModeNix), persistedState{FlakeDir: "/proj"}); err != nil {
		t.Fatalf("正常落盘不得报错，got: %v", err)
	}
	st, err := loadState(home, string(ModeNix))
	if err != nil {
		t.Fatalf("loadState: %v", err)
	}
	if st.FlakeDir != "/proj" {
		t.Errorf("FlakeDir = %q, want /proj", st.FlakeDir)
	}
}

// TestR10_SaveStateNoOpArgumentsStillSucceed：空 xmdeDir/mode 是既有的
// 「不落盘」语义，必须继续返回 nil（不引入新错误）。
func TestR10_SaveStateNoOpArgumentsStillSucceed(t *testing.T) {
	if err := saveState("", string(ModeNix), persistedState{}); err != nil {
		t.Errorf("空 xmdeDir 必须是无操作的静默成功，got: %v", err)
	}
	if err := saveState(t.TempDir(), "", persistedState{}); err != nil {
		t.Errorf("空 mode 必须是无操作的静默成功，got: %v", err)
	}
}
