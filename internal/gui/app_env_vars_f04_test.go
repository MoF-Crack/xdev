package gui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/env"
)

// 第七轮审计 F-04 守卫：PATH 清理的回报必须与**实际动作**一致。
//
// 缺陷现场：票 44 之后，PATH 在「全部条目失效」时是**保留原值**（不写不删，
// cleanEnvVarPath 返回 changed=false）。但 CleanAllInvalidEnvVars 用
// 「回读值是否为空」反推动作——PATH 既没被删也没被清，回读必然非空，
// 于是无条件落入 else 上报 Action="cleaned" / Detail="removed invalid paths"，
// 而实际**一个条目都没动**。用户看到「已清理」但无效条目仍在（虚假成功）。
func TestCleanAllInvalidEnvVars_AllInvalidPathIsNotReportedAsCleaned(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "gone-a") + ";" + filepath.Join(t.TempDir(), "gone-b")

	store := &b5EnvStore{vars: map[string]string{}}
	store.vars[b5EnvKey("user", "Path")] = missing
	// CleanAllInvalidEnvVars 经 enumerateEnvVarsFn 拿变量清单，必须显式提供。
	b5InstallEnvSeams(t, store, map[string]string{}, []env.EnvPair{{Name: "Path", Value: missing}})

	app := newTestApp(t)

	// 先确认真实动作：cleanEnvVarPath 不改动 PATH（保留原值）。
	before := store.get("user", "Path")
	if err := app.CleanEnvVarPath("Path", "user"); err != nil {
		t.Fatalf("CleanEnvVarPath: %v", err)
	}
	after := store.get("user", "Path")
	if after != before {
		t.Fatalf("前置假设失败：PATH 应保留原值，before=%q after=%q", before, after)
	}

	// 批量入口的回报必须诚实：实际零改动 ⇒ 不得声称 cleaned。
	results := app.CleanAllInvalidEnvVars("user")
	var pathResult *CleanResult
	for i := range results {
		if strings.EqualFold(results[i].Name, "Path") {
			pathResult = &results[i]
		}
	}
	if pathResult == nil {
		t.Fatalf("PATH 未出现在清理回报中：%+v", results)
	}
	if pathResult.Action == "cleaned" {
		t.Errorf("F-04 复现：PATH 实际未被修改，回报却为 Action=%q Detail=%q（虚假成功）",
			pathResult.Action, pathResult.Detail)
	}
	if pathResult.Action != "kept" {
		t.Errorf("Action = %q, want %q（零改动应如实回报 kept）", pathResult.Action, "kept")
	}
}

// 对照：真正被部分清理的非 PATH 变量仍须上报 cleaned（防「修成 no-op」）。
func TestCleanAllInvalidEnvVars_PartialCleanStillReportedAsCleaned(t *testing.T) {
	good := t.TempDir()
	missing := filepath.Join(t.TempDir(), "gone")

	store := &b5EnvStore{vars: map[string]string{}}
	store.vars[b5EnvKey("user", "MYPATH")] = good + ";" + missing
	b5InstallEnvSeams(t, store, map[string]string{},
		[]env.EnvPair{{Name: "MYPATH", Value: good + ";" + missing}})

	app := newTestApp(t)
	results := app.CleanAllInvalidEnvVars("user")

	var got *CleanResult
	for i := range results {
		if results[i].Name == "MYPATH" {
			got = &results[i]
		}
	}
	if got == nil {
		t.Fatalf("MYPATH 未出现在清理回报中：%+v", results)
	}
	if got.Action != "cleaned" {
		t.Errorf("Action = %q, want %q（部分清理应报 cleaned）", got.Action, "cleaned")
	}
	if v := store.get("user", "MYPATH"); v != good {
		t.Errorf("清理后 MYPATH = %q, want %q", v, good)
	}
}
