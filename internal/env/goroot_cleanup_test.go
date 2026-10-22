//go:build windows

// Guard tests for CleanupStaleGOROOT (audit-fixes ticket 01 / X-07).
//
// 第四轮复核重写：这些用例**不再写开发者真实的 HKCU\Environment**。
//
// 为什么要改：原实现用 SetUserEnvVar/DeleteUserEnvVar 直接改真实注册表（再 defer 恢复），
// 带来两个真实后果：
//  1. 并发跑全量测试时，另一个包的宿主快照窗口会撞上"正在写真实注册表"的瞬间 → 假红
//     （实测 3 轮全量里出现 2 次，明细 `[env] GOROOT` 由临时路径变为空）；
//  2. 若测试进程被强杀（CI 超时、管道截断），defer 恢复不会执行 → 用户真实 GOROOT 被
//     永久留在临时路径上，正是 P0-17「package fmt is not in std」的病因。
//
// 现在通过 gorootGetFn/gorootDeleteFn/gorootBroadcastFn 接缝注入内存假注册表：
// 判定逻辑（含"只删 V 与 V\go 的组合、其它一律保留"）被测对象一字未改，而宿主零接触。
package env

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// fakeGOROOTRegistry 是内存版 HKCU GOROOT 槽位 + 广播计数。
type fakeGOROOTRegistry struct {
	value     string
	hasValue  bool
	deletes   int
	broadcast int
	deleteErr error
}

// install 把三个接缝指向假实现，并返回它；t.Cleanup 自动还原（用例失败/提前退出也生效）。
func (f *fakeGOROOTRegistry) install(t *testing.T) {
	t.Helper()
	origGet, origDel, origCast := gorootGetFn, gorootDeleteFn, gorootBroadcastFn
	gorootGetFn = func(string) (string, error) {
		if !f.hasValue {
			return "", errors.New("not found")
		}
		return f.value, nil
	}
	gorootDeleteFn = func(string) error {
		if f.deleteErr != nil {
			return f.deleteErr
		}
		f.deletes++
		f.value, f.hasValue = "", false
		return nil
	}
	gorootBroadcastFn = func() { f.broadcast++ }
	t.Cleanup(func() { gorootGetFn, gorootDeleteFn, gorootBroadcastFn = origGet, origDel, origCast })
}

func (f *fakeGOROOTRegistry) set(v string) {
	f.value, f.hasValue = v, true
}

// G1: the exact historical bug layout — GOROOT points at the parent of the
// real toolchain (strip:0 keeps the archive's top-level go/ directory).
func TestCleanupStaleGOROOT_RemovesWrongLayer(t *testing.T) {
	reg := &fakeGOROOTRegistry{}
	reg.install(t)

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "go", "pkg", "tool"), 0o755); err != nil {
		t.Fatal(err)
	}
	reg.set(root)

	if err := CleanupStaleGOROOT(); err != nil {
		t.Fatalf("CleanupStaleGOROOT: %v", err)
	}
	if reg.hasValue {
		t.Errorf("stale GOROOT=%q should have been deleted", root)
	}
	if reg.deletes != 1 {
		t.Errorf("deletes = %d, want 1", reg.deletes)
	}
	if reg.broadcast != 1 {
		t.Errorf("broadcast = %d, want 1（删除后必须广播 WM_SETTINGCHANGE 让新终端立刻生效）", reg.broadcast)
	}
}

// G2: a value that is itself a valid GOROOT (pkg/tool exists) must be kept —
// manual installs and multi-GOROOT setups must never be clobbered.
func TestCleanupStaleGOROOT_KeepsValidValue(t *testing.T) {
	reg := &fakeGOROOTRegistry{}
	reg.install(t)

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "pkg", "tool"), 0o755); err != nil {
		t.Fatal(err)
	}
	reg.set(root)

	if err := CleanupStaleGOROOT(); err != nil {
		t.Fatalf("CleanupStaleGOROOT: %v", err)
	}
	if !reg.hasValue || reg.value != root {
		t.Errorf("valid GOROOT = %q (has=%v), want preserved %q", reg.value, reg.hasValue, root)
	}
	if reg.deletes != 0 || reg.broadcast != 0 {
		t.Errorf("deletes=%d broadcast=%d, want 0/0（值有效时不得有任何副作用）", reg.deletes, reg.broadcast)
	}
}

// G3: unknown provenance (neither V nor V\go looks like a GOROOT) —
// conservatively keep the value.
func TestCleanupStaleGOROOT_KeepsUnknown(t *testing.T) {
	reg := &fakeGOROOTRegistry{}
	reg.install(t)

	root := t.TempDir() // exists but contains no pkg/tool anywhere
	reg.set(root)

	if err := CleanupStaleGOROOT(); err != nil {
		t.Fatalf("CleanupStaleGOROOT: %v", err)
	}
	if !reg.hasValue || reg.value != root {
		t.Errorf("GOROOT of unknown origin was modified: %q (has=%v)", reg.value, reg.hasValue)
	}
	if reg.deletes != 0 {
		t.Errorf("deletes = %d, want 0", reg.deletes)
	}
}

// G0: no GOROOT at all — cleanup must be a clean no-op.
func TestCleanupStaleGOROOT_NoopWhenAbsent(t *testing.T) {
	reg := &fakeGOROOTRegistry{}
	reg.install(t)

	if err := CleanupStaleGOROOT(); err != nil {
		t.Fatalf("expected no error when GOROOT is absent, got %v", err)
	}
	if reg.deletes != 0 || reg.broadcast != 0 {
		t.Errorf("deletes=%d broadcast=%d, want 0/0（缺失时无副作用）", reg.deletes, reg.broadcast)
	}
}

// G4（新增）：删除失败必须**向上报错**，且不广播（否则调用方会以为修好了）。
func TestCleanupStaleGOROOT_DeleteFailurePropagates(t *testing.T) {
	reg := &fakeGOROOTRegistry{deleteErr: errors.New("registry locked")}
	reg.install(t)

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "go", "pkg", "tool"), 0o755); err != nil {
		t.Fatal(err)
	}
	reg.set(root)

	err := CleanupStaleGOROOT()
	if err == nil {
		t.Fatal("删除失败必须返回错误（不能静默）")
	}
	if reg.broadcast != 0 {
		t.Errorf("broadcast = %d, want 0（删除失败时不该广播）", reg.broadcast)
	}
	if !reg.hasValue {
		t.Error("删除失败时值应保持不变")
	}
}

// 宿主安全守卫（本文件的核心承诺）：整套用例跑完，真实注册表必须零接触。
// 通过比较"接缝被替换前后"的真实 GetUserEnvVar 结果实现——若哪个用例漏用了接缝，
// 它的写会体现在真实值上，本用例即红。
func TestGOROOTCleanupTests_NeverTouchRealRegistry(t *testing.T) {
	before, beforeErr := GetUserEnvVar("GOROOT")
	sumBefore := userEnvSnapshot(t)

	if err := CleanupStaleGOROOT(); err != nil {
		t.Fatalf("CleanupStaleGOROOT: %v", err)
	}

	after, afterErr := GetUserEnvVar("GOROOT")
	sumAfter := userEnvSnapshot(t)
	if before != after || (beforeErr == nil) != (afterErr == nil) {
		t.Errorf("真实 GOROOT 被改动: before=%q(err=%v) after=%q(err=%v)", before, beforeErr, after, afterErr)
	}
	if sumBefore != sumAfter {
		t.Error("真实 HKCU\\Environment 被改动（本包测试必须零接触宿主注册表）")
	}
}

// userEnvSnapshot 取一份 HKCU\Environment 的轻量指纹（全部键值对）。
func userEnvSnapshot(t *testing.T) string {
	t.Helper()
	all := GetAllUserEnvVars()
	keys := make([]string, 0, len(all))
	for k := range all {
		keys = append(keys, k)
	}
	sortStrings(keys)
	out := ""
	for _, k := range keys {
		out += k + "=" + all[k] + "\n"
	}
	return out
}

// sortStrings 避免为一个迷你需求引入 sort 之外的依赖变化（保持本文件自洽）。
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// Layout probe sanity: isGOROOTDir must mirror the go command's pkg/tool
// heuristic without touching the registry.
func TestIsGOROOTDir_Probe(t *testing.T) {
	root := t.TempDir()
	if isGOROOTDir(root) {
		t.Errorf("isGOROOTDir(empty dir) = true, want false")
	}
	if err := os.MkdirAll(filepath.Join(root, "pkg", "tool"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !isGOROOTDir(root) {
		t.Errorf("isGOROOTDir(dir with pkg/tool) = false, want true")
	}
	if isGOROOTDir("") {
		t.Error(`isGOROOTDir("") = true, want false`)
	}
}
