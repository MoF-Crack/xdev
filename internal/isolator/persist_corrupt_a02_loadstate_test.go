package isolator

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// 第九轮 A-02：loadState 的**签名契约**守卫。
//
// 与 persist_corrupt_a02_test.go（行为级：Teardown 不得静默报成功）分文件存放，
// 因为本文件断言的是 `loadState(...) (persistedState, error)` 这个新签名——它在
// 修复前的源码上根本无法编译，而行为级守卫可以在未修复的源码上直接跑出红。
//
// 契约本身是 A-02 的根因：F-51 让 loadState 只打一条 WARNING 就返回零值，于是
// 「文件损坏」与「从未 setup 过」对调用方完全不可区分，零值被解读为「没有容器」。

// TestLoadState_CorruptReturnsError：损坏文件 → (零值, 错误)，错误可被
// errors.Is(err, ErrStateCorrupt) 识别，且信息含 state 路径。
func TestLoadState_CorruptReturnsError(t *testing.T) {
	home := t.TempDir()
	p := writeCorruptState(t, home, string(ModeContainer), corruptContainerState)

	got, err := loadState(home, string(ModeContainer))
	if err == nil {
		t.Fatal("损坏的 state 必须返回错误（否则调用方会把零值当成「没有会话」）")
	}
	if !errors.Is(err, ErrStateCorrupt) {
		t.Errorf("error = %v, want it to wrap ErrStateCorrupt", err)
	}
	if !strings.Contains(err.Error(), p) {
		t.Errorf("错误信息必须指出损坏文件路径 %s，got: %v", p, err)
	}
	if got.ContainerID != "" {
		t.Errorf("损坏时仍须返回零值（绝不拿半截状态去操作），got ContainerID=%q", got.ContainerID)
	}
}

// TestLoadState_MissingReturnsZeroNoError：文件不存在 → (零值, nil)。
// 首次运行不是错误（防「修复把正常路径弄红」）。
func TestLoadState_MissingReturnsZeroNoError(t *testing.T) {
	home := t.TempDir()
	got, err := loadState(home, string(ModeContainer))
	if err != nil {
		t.Fatalf("文件不存在必须返回 nil error（首次运行），got: %v", err)
	}
	if got.ContainerID != "" || got.Image != "" || got.ProjectDir != "" || got.WrittenFiles != nil {
		t.Errorf("文件不存在必须返回零值，got %+v", got)
	}
}

// TestSetup_QuarantinesCorruptStateBeforeOverwriting：Setup 会用新会话覆盖旧
// state，若不先留证，损坏文件里泄漏容器的句柄就永久消失。断言旧的损坏内容被
// 改名保留，且新 state 可正常读回（saveState 语义未被破坏）。
func TestSetup_QuarantinesCorruptStateBeforeOverwriting(t *testing.T) {
	home, proj := t.TempDir(), t.TempDir()
	p := writeCorruptState(t, home, string(ModeNix), `{"flakeDir": "/tmp/old", "projectDir": "/tmp/ol`)

	// nix Setup 不触碰 nix 二进制（generateFlake 是纯文件写入）。
	n := &nixIsolator{xmdeDir: home}
	n.loadSaved()
	if err := n.Setup(context.Background(), &EnvironmentSpec{WorkingDir: proj}); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	matches, err := filepath.Glob(p + ".corrupt-*")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("Setup 必须把损坏的旧 state 改名留证，got %v", matches)
	}
	if got := readFileT(t, matches[0]); !strings.Contains(got, "/tmp/old") {
		t.Errorf("quarantine 文件必须保留原损坏内容，got %q", got)
	}

	// 新会话正常落盘（saveState 语义未破坏）。
	st, err := loadState(home, string(ModeNix))
	if err != nil {
		t.Fatalf("Setup 之后的新 state 必须可读: %v", err)
	}
	if st.FlakeDir != proj || len(st.WrittenFiles) == 0 {
		t.Errorf("新 state 不完整: %+v", st)
	}
}
