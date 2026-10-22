package update

// 第六轮审计（shim 链）守卫测试：
//
//	P2-A  自更新 publish 后必须刷新 home 下 shims/ 的现存分发器副本——
//	      rename 舞步发布新 inode，旧硬链接分发器永不自愈；旧实现只挂在
//	      GUI domReady 上，`xdev self update` / TUI / GUI 手动更新全部不刷新。
//	P2-B  提取暂存文件必须唯名（票 31 / P1-24 教训：固定名被并发写者互相
//	      截断，混合损坏字节会被 rename 发布成主二进制，且损坏副本无法自愈）。

import (
	"archive/zip"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/xmdemt/xdev/internal/config"
)

// P2-B：两次提取的暂存名必须不同（并发更新器各写各的文件），
// 且不再使用固定名 .xdev-update-bin。
func TestExtractUpdateBinary_UniqueStagingNames(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "upd.zip")
	zf, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(zf)
	w, err := zw.Create(binaryNameInArchive())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("payload-v1")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zf.Close()

	p1, err := extractUpdateBinary(zipPath, "xdev-windows-amd64-v0.4.1.zip", dir)
	if err != nil {
		t.Fatalf("first extract: %v", err)
	}
	defer os.Remove(p1)
	if filepath.Base(p1) == ".xdev-update-bin" {
		t.Fatal("staging file must not use the fixed name .xdev-update-bin (concurrent updaters truncate each other)")
	}
	got, err := os.ReadFile(p1)
	if err != nil || string(got) != "payload-v1" {
		t.Fatalf("extracted %q (err=%v), want payload-v1", got, err)
	}

	p2, err := extractUpdateBinary(zipPath, "xdev-windows-amd64-v0.4.1.zip", dir)
	if err != nil {
		t.Fatalf("second extract: %v", err)
	}
	defer os.Remove(p2)
	if p1 == p2 {
		t.Fatal("two extractUpdateBinary calls must produce distinct staging names — a fixed name is the concurrent-truncation antipattern (ticket 31 / P1-24)")
	}
}

// P2-A：refreshShimDispatchers 必须把 home 下 shims/ 的现存分发器副本
// 重指到给定的新二进制（ApplyTo 在 replaceBinary 成功后调用它）。
func TestRefreshShimDispatchers_ReroutesStaleShims(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("regular-file shim copies are Windows-only; Unix shims are symlinks")
	}
	home := t.TempDir()
	t.Setenv("XMDEV_HOME", home)
	config.ResetHomeDirForTest()

	shimDir := filepath.Join(home, "shims")
	if err := os.MkdirAll(shimDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// 现存「旧分发器」（模拟 Activate 时刻的旧硬链接/copy——现在内容是旧的）。
	stale := filepath.Join(shimDir, "git.exe")
	if err := os.WriteFile(stale, []byte("OLD-DISPATCHER"), 0o755); err != nil {
		t.Fatal(err)
	}
	// 非分发器文件不能被碰。
	keep := filepath.Join(shimDir, "readme.txt")
	if err := os.WriteFile(keep, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 「新二进制」（模拟 replaceBinary 刚发布的新 inode）。
	newBin := filepath.Join(t.TempDir(), "xdev.exe")
	if err := os.WriteFile(newBin, []byte("NEW-DISPATCHER"), 0o755); err != nil {
		t.Fatal(err)
	}

	refreshShimDispatchers(newBin)

	got, err := os.ReadFile(stale)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "NEW-DISPATCHER" {
		t.Errorf("stale dispatcher content = %q, want NEW-DISPATCHER (self-update must refresh existing shims immediately)", got)
	}
	if k, _ := os.ReadFile(keep); string(k) != "keep me" {
		t.Error("non-dispatcher file was modified")
	}
}

// P2-A（Unix 语义）：非 Windows 上 shims 是符号链接，刷新器重指链接目标。
func TestRefreshShimDispatchers_UnixSymlinkRetarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix symlink semantics")
	}
	home := t.TempDir()
	t.Setenv("XMDEV_HOME", home)
	config.ResetHomeDirForTest()

	shimDir := filepath.Join(home, "shims")
	if err := os.MkdirAll(shimDir, 0o755); err != nil {
		t.Fatal(err)
	}
	oldSrc := filepath.Join(t.TempDir(), "xdev-old")
	if err := os.WriteFile(oldSrc, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(shimDir, "npm")
	if err := os.Symlink(oldSrc, link); err != nil {
		t.Fatal(err)
	}

	newBin := filepath.Join(t.TempDir(), "xdev-new")
	if err := os.WriteFile(newBin, []byte("NEW"), 0o755); err != nil {
		t.Fatal(err)
	}

	refreshShimDispatchers(newBin)

	cur, err := os.Readlink(link)
	if err != nil {
		t.Fatal(err)
	}
	if cur != newBin {
		t.Errorf("link target = %q, want %q", cur, newBin)
	}
}
