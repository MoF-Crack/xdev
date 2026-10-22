// instance_store_test.go 守卫票 35（audit-fixes B5 / C-08 / P1-35）：
// 远程实例持久化统一到 <home>/instances/<id>.json（GUI 读同一目录/结构），
// 旧 <home>/remote/<ID> marker 兼容读 + 迁移提示，且落盘内容不含任何密码。
//
// 这些用例不连远端、不建容器：持久化层是纯文件操作，`docker ps` 不可用时
// ListInstances 也必须照常给出持久化实例。
package orchestrator

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func captureLog(t *testing.T) *strings.Builder {
	t.Helper()
	var buf strings.Builder
	old := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(old) })
	return &buf
}

func writeFileT(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// sshLikeInstance mirrors what SSHAdapter.Create returns: the adapter/user
// metadata only — the password lives in the in-memory SSHTarget and never in
// Instance.Metadata.
func sshLikeInstance(id string) *Instance {
	return &Instance{
		ID:        id,
		Status:    "running",
		Addresses: []string{"dev.example.com:22"},
		Metadata:  map[string]string{"adapter": "ssh", "user": "dev"},
	}
}

// TestSaveInstance_WritesInstanceJSONInInstancesDir：位置与格式（GUI 的
// ListRemoteInstances 读 <XMDevDir>/instances/*.json 并解析成 Instance）。
func TestSaveInstance_WritesInstanceJSONInInstancesDir(t *testing.T) {
	home := t.TempDir()
	inst := sshLikeInstance("ssh-dev-dev.example.com-22")
	if err := SaveInstance(home, inst); err != nil {
		t.Fatalf("SaveInstance: %v", err)
	}

	path := filepath.Join(home, "instances", inst.ID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected %s: %v", path, err)
	}
	var round Instance
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatalf("persisted JSON must unmarshal into Instance: %v", err)
	}
	if round.ID != inst.ID || round.Status != "running" || round.Metadata["adapter"] != "ssh" {
		t.Errorf("round-tripped instance = %+v", round)
	}
	if _, err := os.Stat(filepath.Join(home, "remote", inst.ID)); err == nil {
		t.Error("the pre-C-08 <home>/remote/<ID> marker must no longer be written")
	}

	listed := ListSavedInstances(home)
	if len(listed) != 1 || listed[0].ID != inst.ID {
		t.Errorf("ListSavedInstances = %+v, want the single saved instance", listed)
	}
}

// TestSaveInstance_NeverPersistsCredentials（C-08 安全硬约束）：SSHTarget 的
// Password 绝不能落盘——即使调用方把敏感键塞进 Metadata，SaveInstance 也必须
// 丢弃并记日志。
func TestSaveInstance_NeverPersistsCredentials(t *testing.T) {
	home := t.TempDir()
	inst := sshLikeInstance("ssh-dev-dev.example.com-22")
	const secret = "s3cr3t-do-not-persist"
	inst.Metadata["password"] = secret
	inst.Metadata["sshPassword"] = secret
	inst.Metadata["db_secret"] = secret
	inst.Metadata["api_token"] = secret

	logs := captureLog(t)
	if err := SaveInstance(home, inst); err != nil {
		t.Fatalf("SaveInstance: %v", err)
	}
	if !strings.Contains(logs.String(), "dropping metadata key") {
		t.Errorf("dropping credentials must be logged, got: %q", logs.String())
	}

	data, err := os.ReadFile(filepath.Join(home, "instances", inst.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(string(data))
	if strings.Contains(string(data), secret) || strings.Contains(lower, "password") || strings.Contains(lower, "secret") || strings.Contains(lower, "token") {
		t.Errorf("persisted instance leaked a credential field:\n%s", data)
	}
	// 非敏感键必须保留（不能把 Metadata 整个清空导致 GUI 掉信息）。
	var round Instance
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatal(err)
	}
	if round.Metadata["adapter"] != "ssh" || round.Metadata["user"] != "dev" {
		t.Errorf("non-sensitive metadata was dropped: %+v", round.Metadata)
	}
}

// TestSaveInstance_RejectsUnsafeID：实例 id 会变成文件名，不得逃出目录。
func TestSaveInstance_RejectsUnsafeID(t *testing.T) {
	home := t.TempDir()
	for _, id := range []string{"", ".", "..", "../evil", `..\evil`, "a/b", `a\b`, "C:evil", "ssh dev"} {
		if err := SaveInstance(home, &Instance{ID: id, Status: "running"}); err == nil {
			t.Errorf("SaveInstance accepted unsafe id %q", id)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(home), "evil.json")); err == nil {
		t.Error("unsafe id escaped the instance directory")
	}
}

// TestListSavedInstances_ReadsLegacyMarkerAndLogs（守卫 35-3 兼容读）：
// 旧 `remote/<ID>` marker（内容为状态字符串）仍被列出，并给出一条迁移提示；
// 兼容读不修改也不删除旧文件。
func TestListSavedInstances_ReadsLegacyMarkerAndLogs(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, "remote", "ssh-root-legacy.example-22")
	writeFileT(t, legacy, "running")

	logs := captureLog(t)
	listed := ListSavedInstances(home)
	if len(listed) != 1 {
		t.Fatalf("legacy marker not listed: %+v", listed)
	}
	got := listed[0]
	if got.ID != "ssh-root-legacy.example-22" || got.Status != "running" {
		t.Errorf("legacy instance = %+v, want id + running status from the marker content", got)
	}
	if got.Metadata["legacyMarker"] != "true" {
		t.Errorf("legacy instance must be marked, got metadata %v", got.Metadata)
	}
	logText := logs.String()
	if !strings.Contains(logText, "legacy instance marker") || !strings.Contains(logText, "instances/") {
		t.Errorf("legacy read must log a migration hint, got: %q", logText)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Errorf("compatibility read must not touch the legacy marker: %v", err)
	}
}

// TestListSavedInstances_JSONWinsOverLegacy：同一个 id 既有新 JSON 又有旧
// marker（迁移中途的典型状态）→ 只出一条（新格式胜），且不再提示迁移。
func TestListSavedInstances_JSONWinsOverLegacy(t *testing.T) {
	home := t.TempDir()
	const id = "ssh-dev-dev.example.com-22"
	if err := SaveInstance(home, sshLikeInstance(id)); err != nil {
		t.Fatal(err)
	}
	writeFileT(t, filepath.Join(home, "remote", id), "stopped")

	logs := captureLog(t)
	listed := ListSavedInstances(home)
	if len(listed) != 1 {
		t.Fatalf("expected exactly one deduped entry, got %+v", listed)
	}
	if listed[0].Status != "running" || listed[0].Metadata["legacyMarker"] == "true" {
		t.Errorf("the JSON store must win over the legacy marker, got %+v", listed[0])
	}
	if strings.Contains(logs.String(), "legacy instance marker") {
		t.Errorf("no migration hint for an already-migrated instance, got: %q", logs.String())
	}
}

// TestListSavedInstances_IgnoresUnreadableJSON：损坏的 JSON 不得让整个列表
// 失败（用户仍应看到其余实例），且被记录。
func TestListSavedInstances_IgnoresUnreadableJSON(t *testing.T) {
	home := t.TempDir()
	if err := SaveInstance(home, sshLikeInstance("ssh-dev-good-22")); err != nil {
		t.Fatal(err)
	}
	writeFileT(t, filepath.Join(home, "instances", "broken.json"), "{not json")

	logs := captureLog(t)
	listed := ListSavedInstances(home)
	if len(listed) != 1 || listed[0].ID != "ssh-dev-good-22" {
		t.Errorf("unreadable file must be skipped, got %+v", listed)
	}
	if !strings.Contains(logs.String(), "broken.json") {
		t.Errorf("skipping an unreadable instance must be logged, got: %q", logs.String())
	}
}

// TestListInstances_IncludesPersistedInstances：docker 不可用（本机/CI 上
// `docker ps` 失败或有守护进程但无容器）也必须列出持久化实例——这正是
// CLI 与 GUI 互见的基础。
func TestListInstances_IncludesPersistedInstances(t *testing.T) {
	home := t.TempDir()
	inst := sshLikeInstance("ssh-dev-dev.example.com-22")
	if err := SaveInstance(home, inst); err != nil {
		t.Fatal(err)
	}

	listed := ListInstances(context.Background(), home)
	found := false
	for _, got := range listed {
		if got.ID == inst.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("ListInstances dropped the persisted instance: %+v", listed)
	}
	// 空 home：不得 panic、不得报错。
	if got := ListInstances(context.Background(), ""); len(got) != 0 && got != nil {
		t.Logf("ListInstances(\"\") = %+v (docker containers, if any)", got)
	}
}

// TestContainsInstance_MatchesDockerShortID：`docker run -d` 返回 64 位 ID
// 而 `docker ps` 报 12 位短 ID —— 同一容器不得被列两次；不同的长 ID 不得
// 误判为同一个。
func TestContainsInstance_MatchesDockerShortID(t *testing.T) {
	full := strings.Repeat("a", 64)
	short := full[:12]
	if !containsInstance([]Instance{{ID: short}}, full) {
		t.Error("short docker id must match the full id persisted by remote create")
	}
	if !containsInstance([]Instance{{ID: full}}, short) {
		t.Error("full docker id must match the short id from docker ps")
	}
	if containsInstance([]Instance{{ID: strings.Repeat("b", 64)}}, full) {
		t.Error("different container ids must not match")
	}
	// 两个 SSH 实例 id（非 hex、长度不同）不得因前缀规则互相吞掉。
	if containsInstance([]Instance{{ID: "ssh-dev-host1-22"}}, "ssh-dev-host2-22") {
		t.Error("distinct SSH instance ids must not match")
	}
}
