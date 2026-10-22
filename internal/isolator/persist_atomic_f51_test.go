package isolator

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// 第七轮审计 F-51 守卫：隔离状态必须**原子**写盘。
//
// 缺陷现场：saveState 用裸 os.WriteFile，写盘瞬间崩溃/断电/磁盘满会留下截断的
// JSON，而 loadState 又静默吞掉解析错误返回零值。后果：
//   - 容器模式：containerID 为空 → Teardown 直接 return → **容器永久泄漏**；
//   - nix 模式：WrittenFiles 丢失 → flake.nix 残留且重跑 setup 被拒。
//
// 这里断言写入路径是「临时文件 + rename」形态（不留固定 .tmp），且写出的内容
// 可被 loadState 完整读回。
func TestSaveState_WritesAtomicallyAndRoundTrips(t *testing.T) {
	dir := t.TempDir()
	want := persistedState{
		ContainerID: "abc123",
		Image:       "ubuntu:22.04",
		ProjectDir:  "/workspace",
		WrittenFiles: map[string]string{
			"/workspace/.devcontainer/devcontainer.json": "deadbeef",
		},
	}
	// 第十轮 P2-2：saveState 现在返回 error（写盘失败不再被吞），此处是正常路径。
	if err := saveState(dir, string(ModeContainer), want); err != nil {
		t.Fatalf("saveState: %v", err)
	}

	got, err := loadState(dir, string(ModeContainer))
	if err != nil {
		t.Fatalf("loadState after an atomic save: %v", err)
	}
	if got.ContainerID != want.ContainerID {
		t.Errorf("ContainerID = %q, want %q", got.ContainerID, want.ContainerID)
	}
	if got.Image != want.Image {
		t.Errorf("Image = %q, want %q", got.Image, want.Image)
	}
	if len(got.WrittenFiles) != 1 {
		t.Errorf("WrittenFiles = %v, want the recorded entry", got.WrittenFiles)
	}

	// 原子写不得留下临时文件残渣。
	entries, err := os.ReadDir(filepath.Join(dir, "isolate"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("atomic save left a temp file behind: %s", e.Name())
		}
	}
}

// 守卫：状态文件损坏时 loadState 必须返回零值**加明确错误**，而不是静默返回
// 零值（旧行为让调用方无法区分「从未 setup 过」与「凭证丢了」，第九轮 A-02 的
// 容器泄漏正是这个歧义造成的）。
func TestLoadState_CorruptFileIsReported(t *testing.T) {
	dir := t.TempDir()
	p := statePath(dir, string(ModeContainer))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(`{"containerId": "trunc`), 0o644); err != nil {
		t.Fatal(err)
	}

	// 必须返回零值（不能 panic、不能返回半截状态）——调用方据此跳过删除……
	got, err := loadState(dir, string(ModeContainer))
	if err == nil {
		t.Fatal("corrupt state must be reported as an error, not silently swallowed (A-02)")
	}
	if !errors.Is(err, ErrStateCorrupt) {
		t.Errorf("corrupt state error = %v, want it to wrap ErrStateCorrupt", err)
	}
	// ……并且零值必须伴随错误：零值本身绝不代表「没有会话」。
	if got.ContainerID != "" {
		t.Errorf("corrupt state must yield a zero value, got ContainerID=%q", got.ContainerID)
	}
}

// 守卫：loadState 对**不存在**的文件必须静默（首次 setup 前的正常情况），
// 不得因缺文件而报错路径与「损坏」混为一谈。
func TestLoadState_MissingFileIsSilentZero(t *testing.T) {
	dir := t.TempDir()
	got, err := loadState(dir, string(ModeContainer))
	if err != nil {
		t.Fatalf("missing state file must not be an error (first run), got: %v", err)
	}
	if got.ContainerID != "" || got.Image != "" || got.WrittenFiles != nil {
		t.Errorf("missing state file must yield a zero value, got %+v", got)
	}
}

// 守卫：saveState 的 JSON 必须能被标准解析（形状契约）。
func TestSaveState_ProducesValidJSON(t *testing.T) {
	dir := t.TempDir()
	// 第十轮 P2-2：同上，正常路径必须返回 nil。
	if err := saveState(dir, string(ModeNix), persistedState{FlakeDir: "/tmp/f", ProjectDir: "/p"}); err != nil {
		t.Fatalf("saveState: %v", err)
	}
	data, err := os.ReadFile(statePath(dir, string(ModeNix)))
	if err != nil {
		t.Fatal(err)
	}
	var round persistedState
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatalf("state is not valid JSON: %v\n%s", err, string(data))
	}
	if round.FlakeDir != "/tmp/f" {
		t.Errorf("FlakeDir = %q, want /tmp/f", round.FlakeDir)
	}
}
