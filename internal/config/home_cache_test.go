// home_cache_test.go — 票 43：home 缓存「写入端一行 / 读取端要求两行」的格式错配。
//
// 背景（宿主安全复盘连带发现）：writeHomeCache 的内容是
//
//	dir + "\n" + os.Getenv("XMDEV_HOME")
//
// 而生产环境的正常情形是 XMDEV_HOME **未设置**（home 是「被发现」的，不是配置
// 的），于是文件只有一行。readCacheFile 原先要求 `len(lines) >= 2`，永远拒收，
// 结果是跨进程缓存从未生效：每个 xdev/shim/CLI/GUI 进程都重新全盘扫描
// （saferWindowsHome 对 8 个盘符求剩余空间 / scanForExistingInstall 逐盘 stat）
// 并重写该文件——宿主安全快照因此把 <home>/.home.cache 的 mtime 变化误报成
// 「有测试写真实 home」，而真实原因是每次调用 xdev 二进制都在重写。
//
// 本文件全部用例都是**只读**或只写 t.TempDir()：即便在红状态下（缓存被拒收），
// 也不会走到写真实 home 的分支。
package config

import (
	"os"
	"path/filepath"
	"testing"
)

// writeCacheFile 在临时目录里造一个 home 缓存文件，返回其路径。
func writeCacheFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "home.cache")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestReadCacheFile_AcceptsSingleLine（票 43 必红）：单行缓存（= 生产环境写出的
// 形态）必须被接受。修复前 readCacheFile 返回 ""，跨进程缓存形同虚设。
func TestReadCacheFile_AcceptsSingleLine(t *testing.T) {
	home := t.TempDir() // 记录在缓存里的 home 必须真实存在（校验条件之一）
	t.Setenv("XMDEV_HOME", "")
	p := writeCacheFile(t, home+"\n")

	if got := readCacheFile(p); got != home {
		t.Errorf("readCacheFile(单行) = %q, want %q —— 生产写出的单行缓存被判无效，"+
			"每次都重新全盘扫描并重写（票 43）", got, home)
	}
}

// TestReadCacheFile_TwoLineFormatStillWorks：两行格式（XMDEV_HOME 有值时写出）
// 语义不变：环境变量一致才认，不一致即失效（防换 home 后读到旧值）。
func TestReadCacheFile_TwoLineFormatStillWorks(t *testing.T) {
	home := t.TempDir()

	t.Run("env matches", func(t *testing.T) {
		t.Setenv("XMDEV_HOME", home)
		p := writeCacheFile(t, home+"\n"+home)
		if got := readCacheFile(p); got != home {
			t.Errorf("readCacheFile = %q, want %q", got, home)
		}
	})

	t.Run("env mismatch invalidates", func(t *testing.T) {
		t.Setenv("XMDEV_HOME", t.TempDir())
		p := writeCacheFile(t, home+"\n"+home)
		if got := readCacheFile(p); got != "" {
			t.Errorf("readCacheFile = %q, want \"\" —— 记录的 XMDEV_HOME 与当前不符时必须作废", got)
		}
	})

	t.Run("single line but env set invalidates", func(t *testing.T) {
		// 单行 = 记录时环境变量为空；当前有值 → 语义上不匹配。
		t.Setenv("XMDEV_HOME", t.TempDir())
		p := writeCacheFile(t, home+"\n")
		if got := readCacheFile(p); got != "" {
			t.Errorf("readCacheFile = %q, want \"\"", got)
		}
	})
}

// TestReadCacheFile_RejectsBadRecords：空文件、空首行、目录已不存在都必须作废。
func TestReadCacheFile_RejectsBadRecords(t *testing.T) {
	t.Setenv("XMDEV_HOME", "")

	if got := readCacheFile(writeCacheFile(t, "\n")); got != "" {
		t.Errorf("空首行 = %q, want \"\"", got)
	}
	missing := filepath.Join(t.TempDir(), "no-such-dir")
	if got := readCacheFile(writeCacheFile(t, missing+"\n")); got != "" {
		t.Errorf("已消失目录 = %q, want \"\"", got)
	}
	if got := readCacheFile(filepath.Join(t.TempDir(), "absent.cache")); got != "" {
		t.Errorf("文件不存在 = %q, want \"\"", got)
	}
}

// TestReadHomeCache_UsesSingleLineCache：跨进程入口 readHomeCache 必须能用上
// 单行缓存。之所以断言 readHomeCache 而不是 ResolveHomeDir：写缓存发生在
// ResolveHomeDir 的 miss 分支里，命中即不写——而 miss 分支会把缓存写到**真实
// home**（homeVal 已解析），这样的用例在红状态下会污染宿主。断言「读命中」
// 与「不会重写」等价，且全程零写入。
//
// LOCALAPPDATA/APPDATA 被指向临时目录，hostCachePath 的 fallback 分支因此完全
// 落在临时目录内，不去碰本机可能存在的 <drive>\XDev\.home.cache。
func TestReadHomeCache_UsesSingleLineCache(t *testing.T) {
	base := t.TempDir()
	home := t.TempDir()
	t.Setenv("LOCALAPPDATA", base)
	t.Setenv("APPDATA", base)
	t.Setenv("XMDEV_HOME", "")

	cacheDir := filepath.Join(base, "xdev")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "home.cache"), []byte(home+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ResetHomeDirForTest()
	t.Cleanup(ResetHomeDirForTest) // 不把 fake home 泄漏给同包其它用例

	if got := readHomeCache(); got != home {
		t.Errorf("readHomeCache() = %q, want %q —— 缓存未被采用（票 43：跨进程缓存失效）", got, home)
	}
}
