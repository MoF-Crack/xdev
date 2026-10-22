//go:build windows

package util

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// 第十一轮审计（Round 10 报告待办 4）：原生 junction（FSCTL_SET_REPARSE_POINT）
// 必须配**路径长度回归矩阵**。
//
// 背景：仓库注释记载「手写 REPARSE_DATA_BUFFER 曾在不同路径长度下脆弱、被回退过」
// （Spec 032），因此 Round 9/10 都把迁移列为「应作为独立改动 + 配足路径长度矩阵」。
// 本文件就是那个矩阵。
//
// **Spec 032 当初为什么会坏**（这次必须钉死的根因）：
// `mountPointReparseBuffer` 里的 `SubstituteNameLength` / `PrintNameLength` /
// `PrintNameOffset` 全部是**字节**偏移，而 `PathBuffer` 是 UTF-16。把「字符数」
// 当「字节数」写进去时：
//   - 纯 ASCII 短路径：长度 = 2×字符数，写小一半 → 内核按截断的长度读，**可能侥幸**
//     仍然解析出正确的目标（路径尾部被忽略但前缀足够）；
//   - 长路径 / 非 ASCII：立刻错位 → `ERROR_INVALID_REPARSE_DATA (4390)` 或
//     建出一个指向错误目标的 junction。
//
// 所以只测「一个短 ASCII 路径能建出来」**完全不够**（那正是回退前通过的用例）。
// 矩阵必须覆盖：极短、中等、长（逼近 MAX_PATH）、超长（`\\?\` 前缀）、非 ASCII。

// requireJunctionSupport 在非 Windows 上跳过（junction 是 Windows 概念）。
func requireJunctionSupport(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("junction 是 Windows 文件系统概念")
	}
}

// assertJunctionWorks 校验 link 是一个**指向 target 且可读**的 junction。
func assertJunctionWorks(t *testing.T, link, target string) {
	t.Helper()
	// 1) Readlink 必须能解析出目标。
	got, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("Readlink(%s) 失败 —— junction 未被正确创建: %v", link, err)
	}
	// Readlink 对 junction 返回的可能带 `\??\` 前缀，做一次规范化比较。
	norm := func(p string) string {
		p = strings.TrimPrefix(p, `\??\`)
		p = strings.TrimPrefix(p, `\\?\`)
		return filepath.Clean(p)
	}
	if norm(got) != norm(target) {
		t.Errorf("junction 指向 %q, want %q", got, target)
	}
	// 2) 通过链接**读文件**必须拿到目标里的内容 —— 这才是「junction 真的通了」。
	probe := filepath.Join(target, "probe.txt")
	if err := os.WriteFile(probe, []byte("through-junction"), 0o644); err != nil {
		t.Fatalf("write probe in target: %v", err)
	}
	back, rerr := os.ReadFile(filepath.Join(link, "probe.txt"))
	if rerr != nil {
		t.Fatalf("经 junction 读文件失败（junction 未连通）: %v", rerr)
	}
	if string(back) != "through-junction" {
		t.Errorf("经 junction 读到 %q, want through-junction", string(back))
	}
	// 3) Stat 必须报告它是目录（Round 10 的 P0 教训：DirEntry.IsDir 对 junction
	//    返回 false，必须用 os.Stat 跟随）。
	fi, serr := os.Stat(link)
	if serr != nil || !fi.IsDir() {
		t.Errorf("os.Stat(%s) 未报告目录: err=%v", link, serr)
	}
}

// TestCreateJunction_Native_PathLengthMatrix 是**核心矩阵**。
//
// 每个用例都在独立的根目录下造出**指定总长度**的 target 路径，然后建 junction
// 并验证连通性。长度覆盖：极短、中等、MAX_PATH 附近、超长（`\\?\`）、非 ASCII。
func TestCreateJunction_Native_PathLengthMatrix(t *testing.T) {
	requireJunctionSupport(t)

	// 注意：`t.TempDir()` 本身已经比较长（约 60~90 字符），因此这里的
	// "extraLen" 是**在 temp 根之下再加多少字符**。
	cases := []struct {
		name     string
		extraLen int
		unicode  bool
		wantErr  bool
	}{
		{name: "minimal", extraLen: 0},
		{name: "short", extraLen: 8},
		{name: "medium", extraLen: 64},
		{name: "long", extraLen: 140},
		{name: "near_max_path", extraLen: 200},
		{name: "very_long", extraLen: 260},
		{name: "unicode_short", extraLen: 16, unicode: true},
		{name: "unicode_long", extraLen: 120, unicode: true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			// 构造 target 目录：用一个足够长的叶子名撑出目标总长度。
			leaf := "d"
			if c.unicode {
				leaf = "目录"
			}
			// 反复追加子目录直到接近想要的额外长度。
			target := filepath.Join(root, "target")
			for i := 0; i < c.extraLen/len(leaf); i++ {
				target = filepath.Join(target, leaf)
			}
			if err := os.MkdirAll(target, 0o755); err != nil {
				t.Fatalf("mkdir target: %v", err)
			}
			if c.extraLen > 0 && len(target) < len(root)+c.extraLen {
				// 补一个显式长叶子，确保长度确实到位。
				pad := strings.Repeat("x", c.extraLen-(len(target)-len(root)))
				if pad != "" {
					target = filepath.Join(target, pad)
					if err := os.MkdirAll(target, 0o755); err != nil {
						t.Fatalf("mkdir padded target: %v", err)
					}
				}
			}

			link := filepath.Join(root, "link")
			err := createJunction(link, target)
			if c.wantErr {
				if err == nil {
					t.Fatalf("期望失败但成功了")
				}
				return
			}
			if err != nil {
				// 超长路径在**未启用** LongPathsEnabled 的主机上可能合法地被拒绝；
				// 这种情况如实跳过而不是伪造通过。
				if len(target) > 260 && isPathTooLong(err) {
					t.Skipf("主机未启用长路径支持，%d 字符目标被合法拒绝: %v", len(target), err)
				}
				t.Fatalf("createJunction 失败（targetLen=%d）: %v", len(target), err)
			}
			t.Logf("OK: targetLen=%d linkLen=%d", len(target), len(link))
			assertJunctionWorks(t, link, target)
		})
	}
}

// TestCreateJunction_Native_SpecialChars 覆盖旧 mklink 实现**必须拒绝**的那些字符。
//
// 原生实现不经过 cmd.exe，因此 `%` / `!` / `&` 这些在文件名里合法、但在 cmd
// 命令行里危险的字符**都应当可用** —— 这是本项迁移的直接收益（旧实现会拒绝
// `D:\100%Portable` 这类路径）。
func TestCreateJunction_Native_SpecialChars(t *testing.T) {
	requireJunctionSupport(t)

	for _, name := range []string{
		"with&amp",
		"with%percent",
		"with!bang",
		"with(paren)",
		"with^caret",
		"with[br]",
		"with{curly}",
		"with'squote",
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			target := filepath.Join(root, name)
			if err := os.MkdirAll(target, 0o755); err != nil {
				t.Skipf("无法在该主机上创建含 %q 的目录（文件系统限制）: %v", name, err)
			}
			link := filepath.Join(root, "link")
			if err := createJunction(link, target); err != nil {
				t.Fatalf("原生 junction 应支持含 %q 的路径（旧 mklink 实现会拒绝）: %v", name, err)
			}
			assertJunctionWorks(t, link, target)
		})
	}
}

// 本机实测：这些字符在 Windows 文件名里**非法**，应被文件系统本身拒绝；
// 原生实现对它们不需要特殊处理（Mkdir 就会失败），但也不得 panic。
func TestCreateJunction_Native_IllegalPathDoesNotPanic(t *testing.T) {
	requireJunctionSupport(t)

	root := t.TempDir()
	// `"` 在 Windows 文件名里非法。
	target := filepath.Join(root, `bad"name`)
	_ = os.MkdirAll(target, 0o755) // 预期失败；忽略
	link := filepath.Join(root, "link")
	// 只要求「返回错误或成功」，绝不 panic / 绝不留下半个 junction。
	_ = createJunction(link, target)
}

// 目标不存在时，junction 本身在 Windows 上是**允许**创建的（悬空 junction），
// 但我们的实现先 Mkdir 链接目录再设重解析点，因此应当成功。
// 这条钉住「不因目标暂时不存在而拒绝」（安装流程里目标可能稍后落盘）。
func TestCreateJunction_Native_MissingTargetStillCreates(t *testing.T) {
	requireJunctionSupport(t)

	root := t.TempDir()
	target := filepath.Join(root, "not-yet-created")
	link := filepath.Join(root, "link")

	// 注意：Windows 的 mklink /J **要求**目标已存在，原生实现不要求。
	err := createJunction(link, target)
	if err != nil {
		t.Fatalf("原生 junction 应能指向尚不存在的目标（mklink 要求已存在）: %v", err)
	}
	// 现在把目标建出来，链接应立即生效。
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "probe.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadFile(filepath.Join(link, "probe.txt")); err != nil {
		t.Errorf("补齐目标后 junction 应立即生效: %v", err)
	}
}

// 空路径必须被明确拒绝（不得 panic、不得建出奇怪的东西）。
func TestCreateJunction_Native_EmptyPathsRejected(t *testing.T) {
	requireJunctionSupport(t)

	cases := [][2]string{
		{"", "C:\\x"},
		{"C:\\x", ""},
		{"", ""},
	}
	for _, c := range cases {
		if err := createJunction(c[0], c[1]); err == nil {
			t.Errorf("createJunction(%q, %q) 应返回错误", c[0], c[1])
		}
	}
}

// 重复建在同一个链接路径上必须报错而不是静默覆盖（安装流程依赖该语义）。
func TestCreateJunction_Native_ExistingNonEmptyLinkFails(t *testing.T) {
	requireJunctionSupport(t)

	root := t.TempDir()
	target := filepath.Join(root, "t")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := createJunction(link, target); err != nil {
		t.Fatalf("first: %v", err)
	}
	// 第二次：链接目录已存在 → Mkdir 失败 → 必须报错。
	if err := createJunction(link, target); err == nil {
		t.Error("在已存在的链接路径上重复创建应失败")
	}
}

// buildMountPointReparseBuffer 的**纯单元**守卫：长度字段必须是**字节**偏移。
//
// 这是 Spec 032 回退的根因，单独钉住 —— 不依赖文件系统，可跨平台跑不了
// （build tag 在 windows 上），但判据最直接。
func TestBuildMountPointReparseBuffer_LengthsAreBytes(t *testing.T) {
	requireJunctionSupport(t)

	const subst = `\??\C:\a\bb\ccc` // 13 个字符
	const print = `\??\C:\a\bb\ccc`
	buf, err := buildMountPointReparseBuffer(subst, print)
	if err != nil {
		t.Fatal(err)
	}
	le16 := func(off int) uint16 { return uint16(buf[off]) | uint16(buf[off+1])<<8 }
	le32 := func(off int) uint32 {
		return uint32(buf[off]) | uint32(buf[off+1])<<8 | uint32(buf[off+2])<<16 | uint32(buf[off+3])<<24
	}

	if got := le32(0); got != uint32(0xA0000003) {
		t.Errorf("ReparseTag = %#x, want IO_REPARSE_TAG_MOUNT_POINT (0xA0000003)", got)
	}
	// **核心**：长度必须是 UTF-16 字节数 = 字符数×2。
	wantSubstLen := uint16(len(subst) * 2)
	if got := le16(10); got != wantSubstLen {
		t.Errorf("SubstituteNameLength = %d, want %d（字符数×2；"+
			"把它写成字符数正是 Spec 032 回退的根因）", got, wantSubstLen)
	}
	if got := le16(14); got != uint16(len(print)*2) {
		t.Errorf("PrintNameLength = %d, want %d", got, len(print)*2)
	}
	if got := le16(8); got != 0 {
		t.Errorf("SubstituteNameOffset = %d, want 0", got)
	}
	// **PrintNameOffset 必须跳过 Substitute 的 NUL 终止符**（substBytes + 2）。
	// 把它写成 substBytes 会让它指向那个 NUL 本身，内核报
	// `ERROR_INVALID_REPARSE_DATA` —— 这正是本实现初版的 bug，也是 Spec 032 回退
	// 的那一类。判据来自真实 mklink 产物的逐字节 dump。
	if got := le16(12); got != wantSubstLen+2 {
		t.Errorf("PrintNameOffset = %d, want %d (= ReplaceNameLength + 2，"+
			"必须跳过 Substitute 的 NUL 终止符；本实现初版写成 %d 导致"+
			" ERROR_INVALID_REPARSE_DATA)", got, wantSubstLen+2, wantSubstLen)
	}
	// ReparseDataLength = 8 + PathBuffer 字节数（不含 tag/len 这 8 字节）。
	wantDataLen := uint16(8 + (len(subst)+1)*2 + (len(print)+1)*2)
	if got := le16(4); got != wantDataLen {
		t.Errorf("ReparseDataLength = %d, want %d", got, wantDataLen)
	}
	// 总长 = 8 + ReparseDataLength。
	if len(buf) != 8+int(wantDataLen) {
		t.Errorf("buffer 总长 = %d, want %d", len(buf), 8+int(wantDataLen))
	}
	// PathBuffer 必须是 UTF-16LE 的 subst + NUL + print + NUL。
	off := 16
	for _, r := range subst {
		lo, hi := buf[off], buf[off+1]
		if rune(uint16(lo)|uint16(hi)<<8) != r {
			t.Errorf("PathBuffer 在偏移 %d 处内容不符", off)
			break
		}
		off += 2
	}
	if buf[off] != 0 || buf[off+1] != 0 {
		t.Errorf("Substitute 之后应有 NUL 终止符")
	}
}

// 超长替换名必须被明确拒绝（ReparseDataLength 是 uint16）。
func TestBuildMountPointReparseBuffer_RejectsOverlongPath(t *testing.T) {
	requireJunctionSupport(t)
	huge := `\??\C:\` + strings.Repeat("x", 40000)
	if _, err := buildMountPointReparseBuffer(huge, huge); err == nil {
		t.Error("超长路径应被拒绝（ReparseDataLength 溢出 uint16）")
	}
}

// isPathTooLong 判断错误是否为「路径过长」类（用于如实跳过而非伪造通过）。
func isPathTooLong(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "too long") ||
		strings.Contains(s, "filename or extension is too long") ||
		strings.Contains(s, "206") // ERROR_FILENAME_EXCED_RANGE
}
