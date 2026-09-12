// remote_instances_test.go 守卫票 35（audit-fixes B5 / C-08 / P1-35）的 CLI 与
// GUI 可见性契约：
//
//   - `xdev remote list` 必须列出持久化的实例（旧行为只看 `docker ps`，
//     于是 CLI 自己创建的 SSH 实例在 list 里根本不出现）；
//   - 持久化位置统一为 <home>/instances/<id>.json，且写入的是
//     orchestrator.Instance 的 JSON（GUI 的 ListRemoteInstances 读的就是
//     这个目录/格式 → 两侧互见）；
//   - 落盘 JSON 不含任何密码字段（SSHTarget.Password 明文落盘禁令）；
//   - GUI 侧读取位置不漂移（源码级守卫；internal/gui 不在本票改动范围）。
package cli

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/orchestrator"
)

// sshInstance 是 SSH 适配器 Create 产出的实例形态（Metadata 只有
// user/adapter —— 密码留在内存 target 里，永不进 Metadata）。
func sshInstance() *orchestrator.Instance {
	return &orchestrator.Instance{
		ID:        "ssh-dev-dev.example.com-22",
		Status:    "running",
		Addresses: []string{"dev.example.com:22"},
		Metadata:  map[string]string{"adapter": "ssh", "user": "dev"},
	}
}

// runRemoteList 执行 `xdev remote list` 并返回 stdout。
func runRemoteList(t *testing.T) string {
	t.Helper()
	oldStdout := os.Stdout
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = pw
	cmd := newRemoteListCmd()
	cmd.SilenceUsage = true
	execErr := cmd.Execute()
	pw.Close()
	os.Stdout = oldStdout
	out, _ := io.ReadAll(pr)
	pr.Close()
	if execErr != nil {
		t.Fatalf("xdev remote list: %v\noutput:\n%s", execErr, out)
	}
	return string(out)
}

// TestRemoteList_ShowsPersistedSSHInstance（C-08 / 守卫 35-3）：CLI 自己
// 持久化的 SSH 实例必须出现在 `remote list` 里——且 ID 不被截断（Docker 的
// 12 位短 ID 规则套到 ssh-<user>-<host>-<port> 上会把主机名吃掉，两台主机
// 变得无法区分）。
func TestRemoteList_ShowsPersistedSSHInstance(t *testing.T) {
	home := t.TempDir()
	injectHome(t, home)
	// 本用例断言的是「列表读哪个 home 的实例库」。同包其它用例（或正在并行
	// 修改的 root/init 流程）可能把全局上下文重新解析到默认 home（注册表 /
	// last_selection 推断），因此这里把生效的 home 明确钉在临时目录上——
	// 否则断言会读到宿主真实的 XMDevDir。injectHome 的 cleanup 会在用例结束
	// 后重置全局上下文，改动不外泄。
	Cfg().Core.XMDevDir = home
	// 同时钉住环境变量：整包运行时若有其它用例（或其后残留的 goroutine）重新
	// 解析 home，env 是 config.Load 的第二优先级来源，仍会落回本用例的临时目录。
	t.Setenv("XMDEV_HOME", home)

	inst := sshInstance()
	if err := orchestrator.SaveInstance(home, inst); err != nil {
		t.Fatalf("SaveInstance: %v", err)
	}

	out := runRemoteList(t)
	if !strings.Contains(out, inst.ID) {
		t.Errorf("`remote list` does not show the persisted SSH instance %q:\n%s", inst.ID, out)
	}
	if !strings.Contains(out, "ssh") {
		t.Errorf("`remote list` output should mark the adapter for persisted instances:\n%s", out)
	}
}

// TestRemoteInstanceStore_LayoutAndNoPassword（C-08 / 守卫 35-3 安全硬约束）：
// 实例落在 <home>/instances/<id>.json（GUI 读同一路径/同一结构），并且即使
// 调用方把密码塞进 Metadata，落盘内容里也不得出现。
func TestRemoteInstanceStore_LayoutAndNoPassword(t *testing.T) {
	home := t.TempDir()

	inst := sshInstance()
	// 防御性场景：有人（未来的适配器/GUI 改动）试图把密码塞进 Metadata。
	inst.Metadata["password"] = "s3cr3t-do-not-persist"
	if err := orchestrator.SaveInstance(home, inst); err != nil {
		t.Fatalf("SaveInstance: %v", err)
	}

	path := filepath.Join(home, "instances", inst.ID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("instance must be persisted at <home>/instances/<id>.json: %v", err)
	}
	if strings.Contains(string(data), "s3cr3t-do-not-persist") || strings.Contains(strings.ToLower(string(data)), "password") {
		t.Errorf("persisted instance must never carry a password:\n%s", data)
	}

	// 落盘格式必须是 GUI 直接反序列化的 orchestrator.Instance。
	var round orchestrator.Instance
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatalf("persisted JSON must unmarshal into orchestrator.Instance: %v", err)
	}
	if round.ID != inst.ID || round.Status != inst.Status || round.Metadata["adapter"] != "ssh" {
		t.Errorf("round-tripped instance = %+v", round)
	}
	if _, ok := round.Metadata["password"]; ok {
		t.Error("password key survived into Metadata")
	}
}

// TestGUIRemoteListSharesInstanceStore：GUI 的 ListRemoteInstances 必须继续
// 读同一个实例目录与同一种 JSON。internal/gui 不在本票白名单内，因此这里
// 用源码级守卫锁定契约（读取位置/格式一旦漂移，两侧就不再互见）。
func TestGUIRemoteListSharesInstanceStore(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "gui", "app_project.go"))
	if err != nil {
		t.Fatalf("read gui/app_project.go: %v", err)
	}
	text := string(src)
	for _, want := range []string{`"instances"`, `".json"`, "orchestrator.Instance"} {
		if !strings.Contains(text, want) {
			t.Errorf("internal/gui/app_project.go no longer contains %s — the GUI and CLI instance stores have drifted apart (C-08)", want)
		}
	}
}
