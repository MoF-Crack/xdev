package update

import (
	"os"
	"path/filepath"
	"testing"
)

// 回归守卫（2026-10-26 架构评估）：崩溃态自愈必须**先于**符号链接解析。
//
// # 修复前的缺陷
//
// ApplyTo 的执行顺序是：
//
//	EvalSymlinks(exePath)        ← :116，失败即 return
//	...下载...
//	replaceBinary → replaceBinaryWindows
//	                └─ restoreFromBackup  ← 自愈埋在这里
//
// 崩溃态的特征恰恰是「currentExe 已被 rename 成 .old、新二进制未就位」，
// 此时 EvalSymlinks 必然失败，函数直接返回 "resolve symlinks: ..." ——
// 自愈代码永远到不了。用户看到的是指向错误方向的错误信息，
// 而工具「坏了修不回来」。
//
// 修复：在 EvalSymlinks 之前先调 healInterruptedUpdate。
func TestHealInterruptedUpdate_RestoresMissingBinary(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "xdev.exe")
	backup := exe + ".old"

	// 构造崩溃态：活二进制缺失，.old 完好。
	if err := os.WriteFile(backup, []byte("old-binary-content"), 0o755); err != nil {
		t.Fatal(err)
	}

	healed, err := healInterruptedUpdate(exe)
	if err != nil {
		t.Fatalf("healInterruptedUpdate: %v", err)
	}
	if !healed {
		t.Fatal("崩溃态下应报告已自愈")
	}

	// 活二进制必须回来了，且内容正确。
	got, err := os.ReadFile(exe)
	if err != nil {
		t.Fatalf("自愈后活二进制仍不可读: %v", err)
	}
	if string(got) != "old-binary-content" {
		t.Errorf("恢复的内容不对: %q", got)
	}
	// .old 应已被 rename 消耗掉。
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Errorf("自愈后 .old 应已不存在，实际 err=%v", err)
	}
}

// TestHealInterruptedUpdate_NoopWhenHealthy 确认健康状态下不做任何事（幂等）。
func TestHealInterruptedUpdate_NoopWhenHealthy(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "xdev.exe")
	if err := os.WriteFile(exe, []byte("live"), 0o755); err != nil {
		t.Fatal(err)
	}
	// 同时存在一个陈旧的 .old —— 健康状态下不应被它影响。
	if err := os.WriteFile(exe+".old", []byte("stale"), 0o755); err != nil {
		t.Fatal(err)
	}

	healed, err := healInterruptedUpdate(exe)
	if err != nil {
		t.Fatalf("healInterruptedUpdate: %v", err)
	}
	if healed {
		t.Error("活二进制在位时不应报告自愈")
	}
	// 活二进制内容不应被改动。
	got, _ := os.ReadFile(exe)
	if string(got) != "live" {
		t.Errorf("健康状态下的活二进制被改动了: %q", got)
	}
}

// TestHealInterruptedUpdate_NoBackup 确认无备份时安静返回。
func TestHealInterruptedUpdate_NoBackup(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "xdev.exe")

	healed, err := healInterruptedUpdate(exe)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if healed {
		t.Error("没有备份时不应报告自愈")
	}
}

// TestHealInterruptedUpdate_IgnoresDirBackup 确认 .old 是目录时不误操作。
func TestHealInterruptedUpdate_IgnoresDirBackup(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "xdev.exe")
	if err := os.MkdirAll(exe+".old", 0o755); err != nil {
		t.Fatal(err)
	}

	healed, err := healInterruptedUpdate(exe)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if healed {
		t.Error("备份是目录时不应报告自愈")
	}
}

// TestHealInterruptedUpdate_EmptyPath 确认空路径不 panic。
func TestHealInterruptedUpdate_EmptyPath(t *testing.T) {
	healed, err := healInterruptedUpdate("")
	if err != nil || healed {
		t.Errorf("空路径应安静返回 (false,nil)，得到 (%v,%v)", healed, err)
	}
}

// 源码级守卫：ApplyTo 里 healInterruptedUpdate 的调用必须出现在
// EvalSymlinks 之前 —— 这正是本次修复的实质。
//
// 为什么用源码断言：ApplyTo 需要真实 release 资产与网络才能跑到自愈分支，
// 行为测试要拉起整套 mock；而这条缺陷的本质是**语句顺序**，
// 顺序错误本身就是可静态判定的事实（仓库对同类约束已有先例）。
func TestApplyTo_HealsBeforeEvalSymlinks(t *testing.T) {
	data, err := os.ReadFile("update.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)

	idx := indexOfFunc(src, "func ApplyTo(")
	if idx < 0 {
		t.Fatal("ApplyTo 未找到")
	}
	body := src[idx:]
	if end := indexOfFunc(body[1:], "\nfunc "); end >= 0 {
		body = body[:end+1]
	}

	healPos := indexOf(body, "healInterruptedUpdate(exePath)")
	evalPos := indexOf(body, "filepath.EvalSymlinks(exePath)")
	if healPos < 0 {
		t.Fatal("ApplyTo 必须调用 healInterruptedUpdate(exePath) —— 崩溃态自愈不能只藏在 replaceBinaryWindows 里")
	}
	if evalPos < 0 {
		t.Fatal("ApplyTo 里未找到 EvalSymlinks")
	}
	if healPos > evalPos {
		t.Error("healInterruptedUpdate 必须出现在 EvalSymlinks 之前：" +
			"崩溃态下 EvalSymlinks 必然失败，排在它后面就永远到不了")
	}
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func indexOfFunc(s, sig string) int { return indexOf(s, sig) }
