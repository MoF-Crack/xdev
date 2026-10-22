package shim

// 缺陷④（shim-alias-case-bug plan §3）守卫：shims 分发器版本一致性刷新。
// Windows shim 是 Activate 时刻的硬链接（回退 copy）；自更新/stale 镜像替换
// 走 rename 落新 inode → 旧链接永远持有旧分发器。本测试证明 RefreshDispatchers
// 能把现存副本重指到新源，且不碰非分发器文件、不新建文件、幂等。

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// makeStaleShimDir 构造含新旧分发器、非分发器文件与目录的 shims 场景。
func makeStaleShimDir(t *testing.T) (shimDir, newSrc string) {
	t.Helper()
	shimDir = t.TempDir()
	newSrc = t.TempDir()
	newSrc = filepath.Join(newSrc, "xdev.exe")

	// 新源（模拟刚完成自更新的新二进制）
	if err := os.WriteFile(newSrc, []byte("NEW-DISPATCHER-v2"), 0o755); err != nil {
		t.Fatal(err)
	}

	// 旧分发器（模拟 Activate 时期的旧硬链接/copy——现在内容是旧的）
	for _, name := range []string{"npm", "npm.exe", "git.exe"} {
		if err := os.WriteFile(filepath.Join(shimDir, name), []byte("OLD-DISPATCHER-v1"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// 非分发器文件：不能被碰
	if err := os.WriteFile(filepath.Join(shimDir, "readme.txt"), []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	// .old 运行锁残留：不能被碰
	if err := os.WriteFile(filepath.Join(shimDir, "npm.exe.old"), []byte("lock"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 子目录：不能被碰
	if err := os.MkdirAll(filepath.Join(shimDir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	return shimDir, newSrc
}

func TestRefreshDispatchers_ReroutesStaleShims(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("regular-file shim copies are Windows-only; Unix shims are symlinks")
	}
	shimDir, newSrc := makeStaleShimDir(t)

	refreshed, failed := RefreshDispatchers(shimDir, newSrc)
	if failed != 0 {
		t.Fatalf("failed = %d, want 0", failed)
	}
	if refreshed == 0 {
		t.Fatal("expected nonzero refreshed count (stale copies present)")
	}

	// 分发器条目必须已指向新源
	newData, _ := os.ReadFile(newSrc)
	for _, name := range []string{"npm", "npm.exe", "git.exe"} {
		got, err := os.ReadFile(filepath.Join(shimDir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if !bytes.Equal(got, newData) {
			t.Errorf("%s: content not refreshed (still stale)", name)
		}
	}

	// 非分发器文件不动
	if got, _ := os.ReadFile(filepath.Join(shimDir, "readme.txt")); string(got) != "keep me" {
		t.Error("readme.txt was modified")
	}
	// .old 残留：不被 RefreshDispatchers 直接处理（不生成 .old.exe 孪生），
	// 但 installShimWindows 刷新对应 .exe 时会合法清理它（rename 舞步的一部分）。
	if _, err := os.Stat(filepath.Join(shimDir, "npm.exe.old.exe")); err == nil {
		t.Error("npm.exe.old.exe was created (must not process .old entries)")
	}
	// 子目录不动
	if fi, err := os.Stat(filepath.Join(shimDir, "subdir")); err != nil || !fi.IsDir() {
		t.Error("subdir was removed or converted to file")
	}
	// 不能新建文件
	if _, err := os.Stat(filepath.Join(shimDir, "readme.txt.exe")); err == nil {
		t.Error("readme.txt.exe was created (twin creation must NOT happen)")
	}
}

func TestRefreshDispatchers_IdempotentSecondCall(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("regular-file shim copies are Windows-only; Unix shims are symlinks")
	}
	shimDir, newSrc := makeStaleShimDir(t)

	RefreshDispatchers(shimDir, newSrc)
	refreshed2, failed2 := RefreshDispatchers(shimDir, newSrc)
	if failed2 != 0 {
		t.Fatalf("second call failed = %d, want 0", failed2)
	}
	// 幂等：第二轮全 SameFile no-op（硬链接）——refreshed 必须为 0
	// （copy 型可能首轮重链成功 → 第二轮 SameFile no-op → 也是 0）
	if refreshed2 != 0 {
		t.Errorf("second call refreshed = %d, want 0 (idempotent)", refreshed2)
	}
	// 内容仍正确
	newData, _ := os.ReadFile(newSrc)
	got, _ := os.ReadFile(filepath.Join(shimDir, "npm.exe"))
	if !bytes.Equal(got, newData) {
		t.Error("content diverged after second call")
	}
}

func TestRefreshDispatchers_EmptyOrMissingDir(t *testing.T) {
	// shims 目录不存在：无操作、无报错
	r, f := RefreshDispatchers(filepath.Join(t.TempDir(), "nonexistent"), "whatever")
	if r != 0 || f != 0 {
		t.Errorf("missing dir: refreshed=%d failed=%d, want 0,0", r, f)
	}
	// shims 目录存在但为空：同上
	empty := t.TempDir()
	r, f = RefreshDispatchers(empty, "whatever")
	if r != 0 || f != 0 {
		t.Errorf("empty dir: refreshed=%d failed=%d, want 0,0", r, f)
	}
}

func TestRefreshDispatchers_UnixSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix symlink semantics")
	}
	shimDir := t.TempDir()
	oldSrc := filepath.Join(t.TempDir(), "xdev-old")
	if err := os.WriteFile(oldSrc, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}
	// 现存符号链接 → 旧源
	link := filepath.Join(shimDir, "npm")
	if err := os.Symlink(oldSrc, link); err != nil {
		t.Fatal(err)
	}
	// 普通文件（非符号链接）→ 不碰
	plain := filepath.Join(shimDir, "plain")
	if err := os.WriteFile(plain, []byte("not-mine"), 0o755); err != nil {
		t.Fatal(err)
	}

	newSrc := filepath.Join(t.TempDir(), "xdev-new")
	if err := os.WriteFile(newSrc, []byte("NEW"), 0o755); err != nil {
		t.Fatal(err)
	}

	refreshed, failed := RefreshDispatchers(shimDir, newSrc)
	if failed != 0 {
		t.Fatalf("failed = %d, want 0", failed)
	}
	if refreshed != 1 {
		t.Errorf("refreshed = %d, want 1 (only the symlink)", refreshed)
	}
	// 符号链接已指向新源
	if cur, err := os.Readlink(link); err != nil || cur != newSrc {
		t.Errorf("link target = %q, err=%v; want %q", cur, err, newSrc)
	}
	// 普通文件不动
	if got, _ := os.ReadFile(plain); string(got) != "not-mine" {
		t.Error("plain file was modified")
	}
}
