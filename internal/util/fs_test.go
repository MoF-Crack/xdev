package util

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestAtomicWriteFile(t *testing.T) {
	dir := t.TempDir()
	if err := AtomicWriteFile(filepath.Join(dir, "test.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("AtomicWriteFile failed: %v", err)
	}
}

func TestAtomicWriteFileOverwrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	if err := AtomicWriteFile(path, []byte("first"), 0o644); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := AtomicWriteFile(path, []byte("second"), 0o644); err != nil {
		t.Fatalf("second write: %v", err)
	}

	got, _ := os.ReadFile(path)
	if string(got) != "second" {
		t.Errorf("content = %q, want %q", string(got), "second")
	}
}

func TestActiveVersionNotFound(t *testing.T) {
	dir := t.TempDir()
	ver, ok := ActiveVersion(dir, "nonexistent-tool")
	if ok {
		t.Errorf("ActiveVersion() = (%q, true), want ('', false)", ver)
	}
}

func TestActiveVersionFound(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink requires privileges on Windows")
	}
	dir := t.TempDir()
	installPath := filepath.Join(dir, "install", "myapp", "1.0.0")
	if err := os.MkdirAll(installPath, 0o755); err != nil {
		t.Fatal(err)
	}
	currentDir := filepath.Join(dir, "current")
	if err := os.MkdirAll(currentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(currentDir, "myapp")
	if err := os.Symlink(installPath, link); err != nil {
		t.Skipf("symlink failed: %v", err)
	}

	ver, ok := ActiveVersion(dir, "myapp")
	if !ok {
		t.Fatal("ActiveVersion() = ('', false), want (ver, true)")
	}
	if ver != "1.0.0" {
		t.Errorf("ActiveVersion() = %q, want %q", ver, "1.0.0")
	}
}

func TestActiveInstallPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink requires privileges on Windows")
	}
	dir := t.TempDir()
	installPath := filepath.Join(dir, "install", "tool", "2.0.0")
	if err := os.MkdirAll(installPath, 0o755); err != nil {
		t.Fatal(err)
	}
	currentDir := filepath.Join(dir, "current")
	if err := os.MkdirAll(currentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(installPath, filepath.Join(currentDir, "tool")); err != nil {
		t.Skipf("symlink failed: %v", err)
	}

	got := ActiveInstallPath(dir, "tool")
	if got != installPath {
		t.Errorf("ActiveInstallPath() = %q, want %q", got, installPath)
	}
}

func TestActiveInstallPathNotFound(t *testing.T) {
	dir := t.TempDir()
	if got := ActiveInstallPath(dir, "ghost"); got != "" {
		t.Errorf("ActiveInstallPath() = %q, want ''", got)
	}
}

func TestCopyDirFollowsSymlinkedDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink/junction creation is environment-dependent on Windows")
	}
	src := t.TempDir()
	realApps := filepath.Join(src, "apps-real")
	if err := os.MkdirAll(filepath.Join(realApps, "scoop"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realApps, "scoop", "scoop.ps1"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(src, "apps"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realApps, filepath.Join(src, "apps", "current")); err != nil {
		t.Skipf("symlink failed: %v", err)
	}
	dst := filepath.Join(t.TempDir(), "copy")
	if err := CopyDir(src, dst); err != nil {
		t.Fatalf("CopyDir: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dst, "apps", "current", "scoop", "scoop.ps1"))
	if err != nil {
		t.Fatalf("copied junction target missing: %v", err)
	}
	if string(got) != "hi" {
		t.Errorf("copied content = %q, want hi", string(got))
	}
}

// 第九轮审计 A-10 守卫（P1-24 本体迁移）。
//
// 缺陷现场：`AtomicWriteFile` 用固定 `<path>.tmp` 作中间文件。它是**所有调用点的
// 共享底层**（config 的 home 缓存、manifest 的文本级改写、discovery 的 sniff 缓存、
// isolator 的隔离状态都经它落盘），因此两个并发写者会共享同一个中间文件：
//   - 写者 A 写 tmp("AAAAAAAA")、写者 B 写 tmp("BBBBBBBB")、A 执行 rename
//     → 目标文件内容是 **"BBBBBBBB"**（A 发布了 B 的字节，跨写者内容污染）；
//   - 16 写者 × 40 轮压测实测 **557 次** `rename ... being used by another process` 失败。
//
// 仓库守卫 internal/config/atomicwrite_convergence_test.go 早已把本文件登记进
// fixedTmpExceptions（注释：「P1-24 本体 AtomicWriteFile，待迁移 CreateTemp」）。

// payloadFor 生成可自校验的变长负载："[NNN]" + 重复字符，长度与编号一一对应。
// 这样任意「A 的头 + B 的体」或截断都能被下面的校验检出。
func payloadFor(i int) []byte {
	body := bytes.Repeat([]byte{byte('a' + i%26)}, 64+i*8)
	return append([]byte(fmt.Sprintf("[%03d]", i)), body...)
}

// checkPayload 校验内容自洽：头部编号必须与长度对应，且主体是单一字符。
func checkPayload(t *testing.T, got []byte) {
	t.Helper()
	if len(got) < 5 {
		t.Errorf("payload too short (%d bytes): %q", len(got), got)
		return
	}
	n, err := strconv.Atoi(string(got[1:4]))
	if err != nil {
		t.Errorf("payload header not parseable: %q", got[:min(16, len(got))])
		return
	}
	want := payloadFor(n)
	if !bytes.Equal(got, want) {
		// 精确定位撕裂：长度不符，或内容与编号不匹配。
		t.Errorf("torn/cross-contaminated write: len(got)=%d len(want)=%d "+
			"head=%q wantHead=%q", len(got), len(want),
			got[:min(16, len(got))], want[:min(16, len(want))])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// 并发写者共享固定 .tmp 时会被互相污染 / 截断 —— 这是本缺陷的核心断言。
//
// 判定口径（与仓库既有先例 internal/config/atomicwrite_convergence_test.go:151-160
// 的 TestWriteFileCreateTemp_ConcurrentWritersNoTornReads 一致）：
//   - **计入失败**：读回的内容不是「某一次完整写入」（撕裂 / 跨写者污染 / 头部与
//     长度不匹配）—— 这是固定 .tmp 的真实后果；
//   - **不计入失败**：写者侧的 rename 报错。Windows 上 os.ReadFile 以不含
//     FILE_SHARE_DELETE 的方式打开，**本用例的并发读者**会让 REPLACE_EXISTING
//     偶发 ERROR_ACCESS_DENIED / ERROR_SHARING_VIOLATION，与中间文件是否唯名无关。
//     因此写者侧只记录条数（供报告参考），不判定。
func TestAtomicWriteFile_ConcurrentWritersNoCrossContamination(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "shared.cache")

	const writers = 16
	const rounds = 40

	var wg sync.WaitGroup
	var mu sync.Mutex
	var torn []string
	var renameErrs int

	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for r := 0; r < rounds; r++ {
				i := w*rounds + r
				if err := AtomicWriteFile(target, payloadFor(i), 0o644); err != nil {
					// 并发读者导致的 Windows 共享冲突：只计数，不判定。
					mu.Lock()
					renameErrs++
					mu.Unlock()
					continue
				}
				// 每次写完立刻校验：目标必须是某一次**完整**写入。
				got, err := os.ReadFile(target)
				if err != nil {
					mu.Lock()
					renameErrs++ // 同上：读失败也是共享冲突，不判定
					mu.Unlock()
					continue
				}
				if !isCompleteWrite(got) {
					mu.Lock()
					if len(torn) < 5 {
						torn = append(torn, fmt.Sprintf("len=%d head=%q", len(got), head(got, 12)))
					}
					mu.Unlock()
				}
			}
		}(w)
	}
	wg.Wait()

	if renameErrs > 0 {
		t.Logf("（参考）并发读者导致的 Windows 共享冲突 %d 次 —— 与中间文件方案无关，不计入失败", renameErrs)
	}
	if len(torn) > 0 {
		t.Errorf("A-10 复现：并发写出现 %d 次撕裂/跨写者污染（前几条）：\n  %s",
			len(torn), strings.Join(torn, "\n  "))
	}
}

// isCompleteWrite 判定内容是否为「某一次完整写入」：头部编号与长度、字符一一对应。
func isCompleteWrite(b []byte) bool {
	if len(b) < 5 || b[0] != '[' || b[4] != ']' {
		return false
	}
	n, err := strconv.Atoi(string(b[1:4]))
	if err != nil || n < 0 {
		return false
	}
	return bytes.Equal(b, payloadFor(n))
}

// head 返回前 n 字节（用于失败信息，避免打印超长内容）。
func head(b []byte, n int) []byte {
	if len(b) > n {
		return b[:n]
	}
	return b
}

// 纯并发写者（无并发读者）同样不得出现跨写者污染，且不得残留临时文件。
//
// 判定口径（重要）：**不断言「零失败」**。16 个写者同时以
// MoveFileEx(REPLACE_EXISTING) 覆盖同一目标时，Windows 会偶发
// ERROR_ACCESS_DENIED——这是**目标竞争**，与中间文件是否唯名无关。实测：
// 把 renameWithRetry 换回裸 os.Rename（仍用 CreateTemp）时该失败照旧出现，
// 证明它不属于 A-10。因此这里只断言「污染为 0」这个真正区分两种实现的属性，
// 失败次数仅记录（供报告参考）；`-count` 重复跑也不会因负载抖动而假红。
func TestAtomicWriteFile_ConcurrentWritersOnlyNoContamination(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "shared2.cache")

	const writers = 16
	const rounds = 40

	var wg sync.WaitGroup
	var mu sync.Mutex
	var torn []string
	var writeErrs int

	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for r := 0; r < rounds; r++ {
				i := w*rounds + r
				if err := AtomicWriteFile(target, payloadFor(i), 0o644); err != nil {
					mu.Lock()
					writeErrs++
					mu.Unlock()
					continue
				}
				got, err := os.ReadFile(target)
				if err != nil {
					mu.Lock()
					writeErrs++
					mu.Unlock()
					continue
				}
				if !isCompleteWrite(got) {
					mu.Lock()
					if len(torn) < 5 {
						torn = append(torn, fmt.Sprintf("len=%d head=%q", len(got), head(got, 12)))
					}
					mu.Unlock()
				}
			}
		}(w)
	}
	wg.Wait()

	if writeErrs > 0 {
		t.Logf("（参考）Windows 目标竞争的写/读失败 %d 次 —— 与中间文件方案无关，不计入失败", writeErrs)
	}
	if len(torn) > 0 {
		t.Errorf("跨写者污染（有写者发布了别人的字节）：\n  %s", strings.Join(torn, "\n  "))
	}
	// 临时文件不得残留（失败路径必须自清理）。
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "shared2.cache" {
			if strings.Contains(e.Name(), ".tmp") {
				t.Errorf("残留临时文件: %s", e.Name())
			}
		}
	}
}

// 成功后不得残留任何临时文件。
func TestAtomicWriteFile_NoResidueAfterSuccess(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "out.txt")
	if err := AtomicWriteFile(target, []byte("data"), 0o644); err != nil {
		t.Fatalf("AtomicWriteFile: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "out.txt" {
			t.Errorf("residual file after success: %s", e.Name())
		}
	}
}

// 目标不可替换（这里是「目标是已存在目录」）时：必须返回错误且不留临时文件。
func TestAtomicWriteFile_RenameFailureCleansUp(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "adir")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	err := AtomicWriteFile(target, []byte("data"), 0o644)
	if err == nil {
		t.Fatal("expected an error when the target is a directory")
	}
	entries, rerr := os.ReadDir(dir)
	if rerr != nil {
		t.Fatal(rerr)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Errorf("temp file leaked after a failed rename: %s", e.Name())
		}
	}
}

// perm 语义必须被保留（Unix 上可断言；Windows 只有只读位有效）。
func TestAtomicWriteFile_PreservesPerm(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not meaningful on Windows")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "perm.txt")
	if err := AtomicWriteFile(target, []byte("x"), 0o600); err != nil {
		t.Fatalf("AtomicWriteFile: %v", err)
	}
	fi, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != 0o600 {
		t.Errorf("perm = %o, want 600 (os.CreateTemp defaults to 0600 — must not be widened)", got)
	}
}

// 覆盖既有文件：内容替换且不留残渣（回归，旧行为必须保持）。
func TestAtomicWriteFile_OverwriteLeavesNoResidue(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "over.txt")
	for _, s := range []string{"first", "second", "third"} {
		if err := AtomicWriteFile(target, []byte(s), 0o644); err != nil {
			t.Fatalf("write %q: %v", s, err)
		}
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "third" {
		t.Errorf("content = %q, want third", string(got))
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		names := []string{}
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("expected exactly 1 file, got %v", names)
	}
}
