// teardown_missing_container_r10_test.go 复现第十轮 P3-2（P3）：
// **容器已被手工删除后 Teardown 永远失败、state 永不清除**。
//
// 缺陷现场（container.go:174-178）：
//
//	cmd := exec.CommandContext(ctx, "docker", "rm", "-f", c.containerID)
//	if err := cmd.Run(); err != nil {
//	    return fmt.Errorf("docker rm: %w", err)
//	}
//	clearState(...)   // ← 永远到不了
//
// `docker rm -f` 对已经不存在的容器返回非零（"Error response from daemon:
// No such container: <id>"）。用户的容器早就手工删掉了，语义上「环境已销毁」，
// 但 Teardown 把任何非零都当失败 → 报错、state 永久保留 → 每次 teardown 都失败，
// 且下一次 setup 会拿着这个死句柄继续错下去。
//
// 审计实测：`err1=docker rm: exit status 1 err2=... state_kept=true`。
//
// 修复要求：把 "No such container" 视为成功并 clearState；其它 rm 失败仍必须
// 报错并保留 state（不能一律放过）。
package isolator

import (
	"context"
	"strings"
	"testing"
)

// TestR10_TeardownSucceedsWhenContainerAlreadyGone 是 P3-2 的行为级复现。
func TestR10_TeardownSucceedsWhenContainerAlreadyGone(t *testing.T) {
	home := t.TempDir()
	fake := newFakeDocker(t, fakeDockerRMNoSuchContainer)
	p := writeStateT(t, home, string(ModeContainer), persistedState{ContainerID: "already-gone-container"})

	c := &containerIsolator{xmdeDir: home}
	c.loadSaved()
	err := c.Teardown(context.Background())
	if err != nil {
		t.Fatalf("P3-2 复现：容器已被手工删除（docker rm 报 \"No such container\"）时 Teardown 必须成功，"+
			"got: %v（state 未清除 → 之后每次 teardown 都失败）", err)
	}
	if rm := fake.rmArgs(t); len(rm) != 1 {
		t.Errorf("必须真的尝试过 docker rm，got %v", rm)
	}
	if fileExists(p) {
		t.Error("P3-2：容器确认已不存在时必须 clearState，否则死句柄永久残留")
	}
	// 恢复路径：第二次 teardown 静默成功。
	c2 := &containerIsolator{xmdeDir: home}
	c2.loadSaved()
	if err := c2.Teardown(context.Background()); err != nil {
		t.Errorf("第二次 Teardown 必须幂等成功，got: %v", err)
	}
}

// TestR10_TeardownKeepsStateOnRealDockerFailure：反向守卫——非 "No such container"
// 的 rm 失败必须照旧报错并保留 state（否则真故障被伪装成成功，容器继续泄漏）。
func TestR10_TeardownKeepsStateOnRealDockerFailure(t *testing.T) {
	home := t.TempDir()
	newFakeDocker(t, fakeDockerRMFails)
	p := writeStateT(t, home, string(ModeContainer), persistedState{ContainerID: "busy-container"})

	c := &containerIsolator{xmdeDir: home}
	c.loadSaved()
	err := c.Teardown(context.Background())
	if err == nil {
		t.Fatal("docker rm 因真实故障失败时必须报错，不得当作成功")
	}
	if !strings.Contains(err.Error(), "docker rm") {
		t.Errorf("错误信息必须点明 docker rm，got: %v", err)
	}
	if !fileExists(p) {
		t.Error("真实故障时必须保留 state，否则容器句柄丢失、容器永久泄漏")
	}
}

// TestR10_TeardownTreatsMissingContainerMessageCaseInsensitively：
// docker 在不同平台/版本上的措辞大小写不一（"No such container" / "no such
// container"），识别必须大小写不敏感。
func TestR10_TeardownTreatsMissingContainerMessageCaseInsensitively(t *testing.T) {
	home := t.TempDir()
	fake := newFakeDocker(t, fakeDockerRMNoSuchContainer)
	p := writeStateT(t, home, string(ModeContainer), persistedState{ContainerID: "gone"})

	c := &containerIsolator{xmdeDir: home}
	c.loadSaved()
	if err := c.Teardown(context.Background()); err != nil {
		t.Fatalf("Teardown: %v", err)
	}
	if len(fake.rmArgs(t)) == 0 {
		t.Fatal("前置条件：必须真的调用过 docker rm")
	}
	if fileExists(p) {
		t.Error("容器已不存在时 state 必须清除")
	}
}
