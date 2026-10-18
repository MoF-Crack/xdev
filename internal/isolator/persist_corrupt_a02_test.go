package isolator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 第九轮 A-02（P1）复现守卫：状态文件损坏时 Teardown 必须**报错**，而不是
// 「静默成功」。
//
// 缺陷链：saveState 写盘被打断留下截断 JSON → loadState 吞掉解析错误返回零值
// （containerID == ""）→ containerIsolator.Teardown 开头 `if c.containerID == ""
// { return nil }` 认为「没有容器」→ 返回 nil（CLI 打印
// `✔ isolated environment destroyed`）→ 记录在 state 里的容器句柄
// （审计现场为 `deadbeefcafe`）**永不回收**：容器/端口/磁盘持续泄漏，损坏的
// state 文件也不被清除、无恢复路径。
//
// 这些用例不触碰 docker：状态损坏时 Teardown 在任何 `docker rm` 之前就必须
// 失败返回，因此无 docker 守护进程的机器上同样可跑（本仓库 CI 即如此）。
const corruptContainerState = `{"containerId": "deadbeefcafe", "image": "ubuntu:22.04"` // 截断的 JSON

// writeCorruptState 在 <home>/isolate/<mode>.json 落一份损坏状态，返回其路径。
func writeCorruptState(t *testing.T, home, mode, content string) string {
	t.Helper()
	p := statePath(home, mode)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestTeardown_CorruptStateIsNotReportedAsSuccess 是 A-02 的行为级复现：
// 损坏的 state 里有容器句柄，Teardown 却报成功。
func TestTeardown_CorruptStateIsNotReportedAsSuccess(t *testing.T) {
	home := t.TempDir()
	writeCorruptState(t, home, string(ModeContainer), corruptContainerState)

	c := &containerIsolator{xmdeDir: home}
	c.loadSaved() // 生产路径：NewIsolator 就是这样恢复上次会话的

	if err := c.Teardown(context.Background()); err == nil {
		t.Fatal("A-02 复现：状态损坏时 Teardown 返回 nil（报成功）—— " +
			"state 里记录的容器句柄 'deadbeefcafe' 永远不会被回收，容器静默泄漏")
	}
}

// TestTeardown_MissingStateFileIsSilentSuccess 防「修复把正常路径弄红」：
// 从未 setup 过（state 文件不存在）是正常情况，Teardown 必须静默成功，
// 且可重复调用（幂等）。
func TestTeardown_MissingStateFileIsSilentSuccess(t *testing.T) {
	home := t.TempDir()
	c := &containerIsolator{xmdeDir: home}
	c.loadSaved()

	if err := c.Teardown(context.Background()); err != nil {
		t.Fatalf("首次运行（无 state 文件）的 Teardown 必须静默成功，got: %v", err)
	}
	if err := c.Teardown(context.Background()); err != nil {
		t.Fatalf("重复 Teardown 必须幂等成功，got: %v", err)
	}
}

// TestLoadState_CorruptReturnsError / TestLoadState_MissingReturnsZeroNoError
// 在 persist_corrupt_a02_loadstate_test.go（签名契约；修复前无法编译）。

// TestTeardown_CorruptStateErrorIsActionable：错误必须给出可操作指引
// （A-02 要求 2），而不是一句「corrupt」了事。
func TestTeardown_CorruptStateErrorIsActionable(t *testing.T) {
	home := t.TempDir()
	writeCorruptState(t, home, string(ModeContainer), corruptContainerState)

	c := &containerIsolator{xmdeDir: home}
	c.loadSaved()
	err := c.Teardown(context.Background())
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"docker ps -a", "docker rm -f", "state file"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("错误信息缺少可操作指引 %q，got: %v", want, err)
		}
	}
}

// TestTeardown_CorruptStateIsQuarantinedNotDeleted：损坏文件必须**改名保留**
// （<mode>.json.corrupt-<时间戳>），内容一字不丢——它是泄漏容器的唯一线索；
// 同时 statePath 被腾空，后续 setup/teardown 才能推进。
func TestTeardown_CorruptStateIsQuarantinedNotDeleted(t *testing.T) {
	home := t.TempDir()
	p := writeCorruptState(t, home, string(ModeContainer), corruptContainerState)

	c := &containerIsolator{xmdeDir: home}
	c.loadSaved()
	if err := c.Teardown(context.Background()); err == nil {
		t.Fatal("expected an error")
	}

	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("损坏的 state 文件应被改名腾空（否则后续调用永远卡在同一坏文件上），stat err = %v", err)
	}
	matches, err := filepath.Glob(p + ".corrupt-*")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected exactly one quarantine file %s.corrupt-*, got %v", p, matches)
	}
	if got := readFileT(t, matches[0]); got != corruptContainerState {
		t.Errorf("quarantine 文件内容必须原样保留（证据不可丢）:\n got %q\nwant %q", got, corruptContainerState)
	}

	// 恢复路径：改名后再 teardown 应回到「无会话」的静默成功。
	c2 := &containerIsolator{xmdeDir: home}
	c2.loadSaved()
	if err := c2.Teardown(context.Background()); err != nil {
		t.Errorf("quarantine 之后 teardown 应恢复为静默成功（恢复路径），got: %v", err)
	}
}

// TestNixTeardown_CorruptStateIsNotReportedAsSuccess：nix 侧同源缺陷——
// 损坏状态返回零值 WrittenFiles 会伪装成「legacy state 降级」，teardown 报
// 成功但 xdev 生成的 flake.nix 残留。这里断言必须报错（同样不触碰 nix）。
func TestNixTeardown_CorruptStateIsNotReportedAsSuccess(t *testing.T) {
	home := t.TempDir()
	writeCorruptState(t, home, string(ModeNix), `{"flakeDir": "/tmp/proj", "projectDir": "/tmp/pro`)

	n := &nixIsolator{xmdeDir: home}
	n.loadSaved()
	if err := n.Teardown(context.Background()); err == nil {
		t.Fatal("A-02（nix 侧）：状态损坏时 Teardown 返回 nil（报成功），flake.nix 归属记录丢失却无人告知")
	}
}

// TestExec_CorruptStateDoesNotClaimNotSetup：损坏状态下 Exec 不能笼统报
// 「container not set up」——用户确实 setup 过，误导性提示会把人引向重跑
// setup（覆盖掉泄漏容器的唯一线索）。
func TestExec_CorruptStateDoesNotClaimNotSetup(t *testing.T) {
	home := t.TempDir()
	writeCorruptState(t, home, string(ModeContainer), corruptContainerState)

	c := &containerIsolator{xmdeDir: home}
	c.loadSaved()
	_, err := c.Exec(context.Background(), []string{"echo", "hi"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "not set up") {
		t.Errorf("损坏状态不得伪装成「未 setup」，got: %v", err)
	}
	if !strings.Contains(err.Error(), "corrupt") {
		t.Errorf("错误必须点明状态损坏，got: %v", err)
	}
	// Exec 是只读路径：不得消费掉证据，否则紧随其后的 teardown 会「文件不存在 →
	// 静默成功」，容器再泄漏一次。
	if _, statErr := os.Stat(statePath(home, string(ModeContainer))); statErr != nil {
		t.Errorf("Exec 不得改动损坏的 state 文件（证据留给 Teardown 处理）: %v", statErr)
	}
	if matches, _ := filepath.Glob(statePath(home, string(ModeContainer)) + ".corrupt-*"); len(matches) != 0 {
		t.Errorf("Exec 不得 quarantine 损坏文件，got %v", matches)
	}
}

// TestSetup_QuarantinesCorruptStateBeforeOverwriting 见
// persist_corrupt_a02_loadstate_test.go（该用例需断言新签名读回新 state）。
