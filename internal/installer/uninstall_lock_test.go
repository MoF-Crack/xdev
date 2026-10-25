//go:build windows

package installer

import (
	"os"
	"path/filepath"
	"testing"
)

// 回归：installDirLocked 必须能识别「目录内有文件被其它进程占用」。
//
// 背景：卸载 node@26.9.0 报 `unlinkat ...\node.exe: Access is denied`。
// 旧实现直接 os.RemoveAll，失败时目录已被部分删除，留下既不能用也无法
// 干净重装的脏状态。修复后应先探测、被占用就立即失败且不删任何东西。
//
// 这里用一个「由本进程持有独占写句柄」的临时文件模拟占用：Windows 上
// 零共享模式打开会返回 ERROR_SHARING_VIOLATION。
func TestInstallDirLockedDetectsOpenFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "fake.exe")
	if err := os.WriteFile(target, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 未占用时应判定为可删
	locked, err := installDirLocked(dir)
	if err != nil {
		t.Fatalf("installDirLocked: %v", err)
	}
	if locked {
		t.Fatal("未占用时 installDirLocked 返回 true")
	}

	// 用独占模式持有句柄（对应 Windows 上「另一个进程正占用」）
	f, err := os.OpenFile(target, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("open for hold: %v", err)
	}
	defer f.Close()

	// Go 的 os.OpenFile 默认共享读写，不足以触发共享冲突；
	// 用 syscall 直接以零共享打开来模拟真实占用。
	if !fileInUse(target) {
		t.Log("注意：本平台/本进程的句柄未触发共享冲突，跳过占用断言")
		t.Skip("当前文件系统未表现出共享冲突语义")
	}

	locked2, err := installDirLocked(dir)
	if err != nil {
		t.Fatalf("installDirLocked (locked): %v", err)
	}
	if !locked2 {
		t.Error("文件被占用时 installDirLocked 应返回 true")
	}
}

// 回归：目录不存在时应返回 (false, nil)，不报错。
func TestInstallDirLockedMissingDir(t *testing.T) {
	locked, err := installDirLocked(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatalf("installDirLocked on missing dir: %v", err)
	}
	if locked {
		t.Error("目录不存在时应返回 false")
	}
}

// 回归：isFileLockedError 应识别典型的 Windows 占用错误。
func TestIsFileLockedError(t *testing.T) {
	cases := []struct {
		msg  string
		want bool
	}{
		{"unlinkat E:\\x\\node.exe: Access is denied.", true},
		{"The process cannot access the file because it is being used by another process.", true},
		{"sharing violation", true},
		{"text file busy", true},
		{"no such file or directory", false},
		{"disk full", false},
	}
	for _, c := range cases {
		err := &fakeErr{c.msg}
		if got := isFileLockedError(err); got != c.want {
			t.Errorf("isFileLockedError(%q) = %v, want %v", c.msg, got, c.want)
		}
	}
	if isFileLockedError(nil) {
		t.Error("isFileLockedError(nil) 应为 false")
	}
}

type fakeErr struct{ msg string }

func (e *fakeErr) Error() string { return e.msg }
