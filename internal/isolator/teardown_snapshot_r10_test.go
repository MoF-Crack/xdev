// teardown_snapshot_r10_test.go 复现第十轮 P2-3（P2）：
// **container Teardown 用构造时快照并无条件 clearState：并发 Setup 的新会话被删**。
//
// 缺陷链：
//
//	container.go:56-67 loadSaved 只读一次快照并缓存 stateErr；Teardown
//	（:164-182）从不回读 state，直接删**快照里的旧 ID**，然后无条件 clearState
//	（:180）把文件删掉 —— 而该文件此时可能已经属于另一个进程刚写入的**新会话**。
//	对照 nix.go:100-110，nix Teardown 每次都重新 loadState（两侧不对称）。
//
// 审计现场（假 docker）：
//
//	docker rm args="rm -f old-container"  state_file_exists=false
//
// 即：本进程构造 isolator（读到 old-container）→ 另一进程写入
// `{"containerId":"new-container"}` → 本进程 Teardown 删掉 old 后把 new 的 state
// 一并删除 → new 容器失管（再也没人知道它存在）。
//
// 修复要求：Teardown 内**重新** loadState（不跨操作缓存 stateErr），且只在
// 「持久化 ID == 本次删除的 ID」时才 clearState。
package isolator

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeStateT 直接落一份合法 state（不经 saveState，避免依赖 P2-2 引入的
// `saveState(...) error` 新签名——本文件的行为级用例必须能在修复前的源码上跑红），
// 返回其路径。
func writeStateT(t *testing.T, home, mode string, s persistedState) string {
	t.Helper()
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	p := statePath(home, mode)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestR10_ContainerTeardownKeepsConcurrentSession 是 P2-3 的行为级复现：
// 构造后另一个进程把 state 换成新会话，Teardown 只允许删自己**快照里**的旧容器，
// 并且**不得**删除新会话的 state 文件。
func TestR10_ContainerTeardownKeepsConcurrentSession(t *testing.T) {
	home := t.TempDir()
	fake := newFakeDocker(t, fakeDockerOK)

	// 进程 A：读到旧会话。
	writeStateT(t, home, string(ModeContainer), persistedState{ContainerID: "old-container", Image: "ubuntu:22.04"})
	a := &containerIsolator{xmdeDir: home}
	a.loadSaved()

	// 进程 B：并发跑了一次 setup，state 现在是新会话。
	p := writeStateT(t, home, string(ModeContainer), persistedState{ContainerID: "new-container", Image: "ubuntu:22.04"})

	if err := a.Teardown(context.Background()); err != nil {
		t.Fatalf("Teardown: %v", err)
	}

	// 删除动作必须针对快照里的旧 ID（本次会话的所有权），不能误删新容器。
	for _, rm := range fake.rmArgs(t) {
		if strings.Contains(rm, "new-container") {
			t.Errorf("Teardown 删除了并发会话的容器：%q", rm)
		}
	}
	// 关键断言：新会话的 state 必须还在，否则 new-container 失管。
	st, err := loadState(home, string(ModeContainer))
	if err != nil {
		t.Fatalf("并发会话的 state 被破坏：%v", err)
	}
	if st.ContainerID != "new-container" {
		t.Errorf("P2-3 复现：Teardown 无条件 clearState 把并发 setup 的新会话删掉了，"+
			"state 现在 %+v（文件存在=%v）—— new-container 从此无人回收", st, fileExists(p))
	}
}

// TestR10_ContainerTeardownClearsStateWhenItOwnsIt：防「修复把正常路径弄红」——
// 持久化 ID 就是本次删掉的 ID 时，state 必须被清除（幂等、不残留）。
func TestR10_ContainerTeardownClearsStateWhenItOwnsIt(t *testing.T) {
	home := t.TempDir()
	newFakeDocker(t, fakeDockerOK)
	p := writeStateT(t, home, string(ModeContainer), persistedState{ContainerID: "only-container"})

	c := &containerIsolator{xmdeDir: home}
	c.loadSaved()
	if err := c.Teardown(context.Background()); err != nil {
		t.Fatalf("Teardown: %v", err)
	}
	if fileExists(p) {
		t.Error("Teardown 删除了自己拥有的容器后必须清掉 state（否则重复 teardown 会拿着死句柄报错）")
	}
	// 幂等：第二次 Teardown 静默成功。
	c2 := &containerIsolator{xmdeDir: home}
	c2.loadSaved()
	if err := c2.Teardown(context.Background()); err != nil {
		t.Errorf("第二次 Teardown 必须幂等成功，got: %v", err)
	}
}

// TestR10_ContainerTeardownPicksUpRepairedState 对应审计附带后果
// （TestR9B_RepairedStateAsymmetry）：state 在**构造之后**被修复为合法内容时，
// container Teardown 不得再按构造时缓存的 stateErr 把有效文件 quarantine 并报错。
// nix 侧（每次重新 loadState）一直是正常的，container 侧必须与之对称。
func TestR10_ContainerTeardownPicksUpRepairedState(t *testing.T) {
	home := t.TempDir()
	fake := newFakeDocker(t, fakeDockerOK)

	// 构造时 state 损坏（loadSaved 不缓存该判断，只填充句柄——见 loadSaved 注释）。
	p := writeCorruptState(t, home, string(ModeContainer), corruptContainerState)
	c := &containerIsolator{xmdeDir: home}
	c.loadSaved()
	if c.containerID != "" {
		t.Fatal("前置条件：构造时 state 损坏，loadSaved 不得填出任何句柄")
	}

	// 用户/工具在构造之后修复了 state。
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	writeStateT(t, home, string(ModeContainer), persistedState{ContainerID: "repaired-container"})

	err := c.Teardown(context.Background())
	if err != nil {
		t.Fatalf("P2-3 复现：state 已被修复为有效内容，container Teardown 仍按构造时的 "+
			"stateErr 报错（nix 侧每次重新 loadState，行为不对称）：%v", err)
	}
	if matches, _ := filepath.Glob(p + ".corrupt-*"); len(matches) != 0 {
		t.Errorf("P2-3 复现：有效 state 被当成损坏文件 quarantine：%v", matches)
	}
	rm := fake.rmArgs(t)
	if len(rm) != 1 || !strings.Contains(rm[0], "repaired-container") {
		t.Errorf("Teardown 必须重新读取 state 并删除其中记录的容器，got rm 调用 %v", rm)
	}
}

// TestR10_ContainerTeardownDetectsStateCorruptedAfterConstruction：反向守卫——
// 构造时**正常**、Teardown 前 state 被损坏时，必须报错（A-02 语义）而不是
// 因为快照里的旧 ID 还在就报成功。
func TestR10_ContainerTeardownDetectsStateCorruptedAfterConstruction(t *testing.T) {
	home := t.TempDir()
	newFakeDocker(t, fakeDockerOK)
	p := writeStateT(t, home, string(ModeContainer), persistedState{ContainerID: "snapshot-container"})

	c := &containerIsolator{xmdeDir: home}
	c.loadSaved()
	if c.containerID != "snapshot-container" {
		t.Fatalf("前置条件：state 合法，loadSaved 必须填充句柄，got %q", c.containerID)
	}

	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	writeCorruptState(t, home, string(ModeContainer), corruptContainerState)

	if err := c.Teardown(context.Background()); err == nil {
		t.Error("Teardown 前 state 被损坏时必须报错（A-02：不得拿旧快照谎报成功）")
	}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
