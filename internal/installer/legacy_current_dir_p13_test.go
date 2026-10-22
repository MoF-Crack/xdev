package installer

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// 第八轮审计 P1-3 守卫（F-22 的自引入回归）：遗留的**真实目录**
// `current/<tool>`（旧版 xdev 的 CopyDir 兜底产物）不得让 Activate 永久失败。
//
// 缺陷现场：F-22 移除 CopyDir 兜底后，`removeLinkEntry` 仍用裸 `os.Remove`，
// 对**非空**目录必定失败（Windows: "The directory is not empty"）；随后的
// `os.Rename(tmpLink, currentLink)` 也无法覆盖已存在的目录（"Access is denied"），
// `rollbackActivation` 走同一函数同样失败 → `xdev install`/`xdev use` **每次重试
// 都是同一条错误链，永远修不好**（实测 H10）。
//
// 修复后：真实目录（非 symlink、非 junction）会被 rename 到旁路名再 RemoveAll，
// 安装流程得以继续；同时**绝不**对链接调用 RemoveAll（那会删掉安装树）。
func TestRemoveLinkEntry_ClearsLegacyRealDirectory(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "current", "legacy")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	// 非空 —— 这是关键（空目录 os.Remove 本来就能删）。
	if err := os.WriteFile(filepath.Join(legacy, "marker.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	removeLinkEntry(legacy, dir)

	if _, err := os.Lstat(legacy); !os.IsNotExist(err) {
		t.Errorf("P1-3 复现：遗留真实目录 %s 未被清除（err=%v）—— Activate 会永久失败", legacy, err)
	}
	// 旁路残留不得留在 current/ 下（否则仍会被 ActiveVersion 看到）。
	entries, err := os.ReadDir(filepath.Join(dir, "current"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		t.Errorf("current/ 下仍有残留条目 %q —— 会干扰 ActiveVersion 解析", e.Name())
	}
}

// 守卫：空真实目录同样必须被清除（旧代码能删，不能因修复而回归）。
func TestRemoveLinkEntry_ClearsEmptyRealDirectory(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "current", "empty")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	removeLinkEntry(empty, dir)
	if _, err := os.Lstat(empty); !os.IsNotExist(err) {
		t.Errorf("空目录未被清除（err=%v）", err)
	}
}

// 守卫（**安全关键**）：对**符号链接**绝不能走 RemoveAll 路径 —— 那会顺着
// reparse point 删掉安装树。这里建一个「链接 → 真实目录（含文件）」，
// 断言 removeLinkEntry 只删链接、目标内容完好。
func TestRemoveLinkEntry_NeverDeletesSymlinkTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows; covered on Unix CI")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "install", "legacy", "1.0.0")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	payload := filepath.Join(target, "binary")
	if err := os.WriteFile(payload, []byte("important"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "current", "legacy")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	removeLinkEntry(link, dir)

	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Errorf("链接未被移除（err=%v）", err)
	}
	// 目标必须完好 —— 这是本用例的核心断言。
	if _, err := os.Stat(payload); err != nil {
		t.Errorf("安全违规：removeLinkEntry 顺着链接删掉了安装树内容: %v", err)
	}
}

// 守卫：路径不存在时静默返回（幂等）。
func TestRemoveLinkEntry_MissingPathIsNoOp(t *testing.T) {
	tmp := t.TempDir()
	removeLinkEntry(filepath.Join(tmp, "current", "nope"), tmp)
}

// 单元：isLegacyRealDir 的判定矩阵。
func TestIsLegacyRealDir_Classification(t *testing.T) {
	dir := t.TempDir()
	realDir := filepath.Join(dir, "realdir")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(realDir)
	if err != nil {
		t.Fatal(err)
	}
	if !isLegacyRealDir(fi) {
		t.Error("普通目录应被识别为遗留真实目录")
	}

	f := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	ffi, _ := os.Lstat(f)
	if isLegacyRealDir(ffi) {
		t.Error("普通文件不得被识别为遗留真实目录（否则会走 RemoveAll）")
	}
	if isLegacyRealDir(nil) {
		t.Error("nil FileInfo 必须返回 false")
	}
}
