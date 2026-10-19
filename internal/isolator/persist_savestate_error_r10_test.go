// persist_savestate_error_r10_test.go 复现第十轮 P2-2（P2）：
// **saveState 失败被吞：Setup 报成功但句柄/归属记录没落盘 → 环境永久泄漏**。
//
// 缺陷链（persist.go:65-105，修复前 saveState 无返回值，失败只 log；rename 失败
// 同样只 log）：
//
//	container 模式：`docker run` 已经真的把容器启动了，saveState 却失败 →
//	  Setup 返回 nil（CLI 打印 `✔ isolated environment created`）→ 状态文件里没有
//	  容器 ID → 下一次 Teardown 读不到句柄 → 容器/端口/磁盘永久泄漏。
//	nix 模式：flake.nix 已经写进项目，WrittenFiles 归属记录却没落盘 → Teardown
//	  只能按「无会话」静默成功 → 生成的 flake.nix 残留，用户重跑 setup 又被
//	  ensureOverwritable 拒绝。
//
// 审计现场（把 <home>/isolate 做成文件使 MkdirAll 必失败）：
//
//	nix setup err=<nil> state_exists=false flake_written=true
//	teardown err=<nil> flake_left=true
//
// 这是第九轮 A-02 的「写侧」对应缺陷（A-02 修的是读侧：损坏状态必须报错）。
// 修复要求：saveState 返回 error；Setup 在 state 落盘失败时返回错误并**尽力回滚**
// （container：`docker rm -f` 本次新 ID；nix：删除刚写的 flake.nix）。
//
// 这些用例用假 docker（fakedocker_r10_test.go）与纯文件系统构造，不依赖本机
// 是否有 docker/nix。
package isolator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// saveState 的签名契约（`saveState(...) error`）在
// persist_savestate_signature_r10_test.go 单独断言——它无法在修复前的源码上编译。

// TestR10_NixSetupFailsAndRollsBackWhenStateCannotBePersisted 是 nix 侧的行为级
// 复现（审计现场原始命令）：
//
//	nix setup err=<nil> state_exists=false flake_written=true
//
// 修复后：Setup 必须返回错误，并且**不得**把刚写的 flake.nix 留在项目里
// （否则用户重跑 setup 会被 ensureOverwritable 拒绝，等于环境卡死）。
func TestR10_NixSetupFailsAndRollsBackWhenStateCannotBePersisted(t *testing.T) {
	home, proj := t.TempDir(), t.TempDir()
	blockStateDir(t, home)
	flake := filepath.Join(proj, "flake.nix")

	n := &nixIsolator{xmdeDir: home}
	err := n.Setup(context.Background(), &EnvironmentSpec{WorkingDir: proj})

	if err == nil {
		t.Fatal("P2-2 复现：saveState 失败（<home>/isolate 是文件）时 nix Setup 仍返回 nil —— " +
			"用户以为环境已创建，归属记录却没落盘，teardown 只会静默成功，flake.nix 永久残留")
	}
	if _, statErr := os.Stat(flake); statErr == nil {
		t.Errorf("P2-2：state 落盘失败时 Setup 必须回滚刚写的 %s（否则重跑 setup 被 ensureOverwritable 拒绝）", flake)
	}
	if _, statErr := os.Stat(statePath(home, string(ModeNix))); statErr == nil {
		t.Error("落盘失败却存在 state 文件，语义矛盾")
	}
	if !strings.Contains(err.Error(), "state") {
		t.Errorf("错误必须点明是 state 落盘失败，got: %v", err)
	}
}

// TestR10_ContainerSetupRemovesContainerWhenStateCannotBePersisted 是 container
// 侧的对应复现：`docker run` 已启动容器，saveState 失败 → Setup 必须报错并
// `docker rm -f` 这个新容器（否则 Teardown 读不到句柄，容器永久泄漏）。
func TestR10_ContainerSetupRemovesContainerWhenStateCannotBePersisted(t *testing.T) {
	home, proj := t.TempDir(), t.TempDir()
	blockStateDir(t, home)
	fake := newFakeDocker(t, fakeDockerOK)

	c := &containerIsolator{xmdeDir: home, spec: &EnvironmentSpec{Env: map[string]string{}}}
	err := c.Setup(context.Background(), &EnvironmentSpec{WorkingDir: proj, Env: map[string]string{}})

	if err == nil {
		t.Fatal("P2-2 复现：saveState 失败时 container Setup 仍返回 nil —— " +
			"docker run 已经启动的容器没有任何持久化句柄，Teardown 会认为「没有容器」，容器永久泄漏")
	}
	rm := fake.rmArgs(t)
	if len(rm) != 1 || !strings.Contains(rm[0], fakeContainerID) {
		t.Errorf("P2-2：Setup 落盘失败必须 `docker rm -f <本次新 ID>` 回滚，got rm 调用 %v", rm)
	}
}

// TestR10_ContainerSetupRemovesContainerWhenRenameFails：同一缺陷的第二个失败点
// （saveState 内部的 MkdirAll/rename 失败），同样必须回滚，证明修复覆盖 saveState
// 的**全部**失败出口，而不是只覆盖「Setup 开头就已经写不下去」那一种。
//
// 注入时机刻意放在 `docker run` 成功之后（见 blockStateDirAfterRun 的注释）：
// 这是唯一「容器已经真的在运行、state 却写不下去」的窗口，即真实磁盘满的时序。
func TestR10_ContainerSetupRemovesContainerWhenRenameFails(t *testing.T) {
	home, proj := t.TempDir(), t.TempDir()
	hook := blockStateDirAfterRun(t, home)
	fake := newFakeDockerWithRunHook(t, fakeDockerOK, hook)

	c := &containerIsolator{xmdeDir: home, spec: &EnvironmentSpec{Env: map[string]string{}}}
	err := c.Setup(context.Background(), &EnvironmentSpec{WorkingDir: proj, Env: map[string]string{}})
	if err == nil {
		t.Fatal("P2-2 复现：容器已启动而 state 写不下去时，container Setup 仍返回 nil，容器无句柄可回收")
	}
	if rm := fake.rmArgs(t); len(rm) != 1 || !strings.Contains(rm[0], fakeContainerID) {
		t.Errorf("state 落盘失败同样必须回滚容器，got rm 调用 %v", rm)
	}
}

// TestR10_SetupRollbackFailureIsReported：回滚本身失败时（docker rm 报错）也必须
// 把两件事都说出来——否则用户既不知道 state 没落盘，也不知道容器还在。
func TestR10_SetupRollbackFailureIsReported(t *testing.T) {
	home, proj := t.TempDir(), t.TempDir()
	blockStateDir(t, home)
	fake := newFakeDocker(t, fakeDockerRMFails)

	c := &containerIsolator{xmdeDir: home, spec: &EnvironmentSpec{Env: map[string]string{}}}
	err := c.Setup(context.Background(), &EnvironmentSpec{WorkingDir: proj, Env: map[string]string{}})
	if err == nil {
		t.Fatal("expected an error")
	}
	if len(fake.rmArgs(t)) == 0 {
		t.Error("回滚必须真的尝试 docker rm")
	}
	if !strings.Contains(err.Error(), "roll back") && !strings.Contains(err.Error(), "remove") {
		t.Errorf("回滚失败必须写进错误信息（用户需要手工 docker rm -f），got: %v", err)
	}
}
