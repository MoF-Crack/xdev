package config

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// 第十二轮审计（Round 11 报告 §七-②，**并更正 Round 11 的误判**）：
// `captureStderr` 存在 **46% 概率的输出丢失竞态**，Round 11 把它归因为
// 「负载抖动」是错的。
//
// 缺陷：旧 restore() 在 `os.Stderr = old` 之后**立刻 `r.Close()`**（关闭读端）。
// 读端 goroutine 可能还没被调度去读，于是带着「已读到的部分」退出，
// **管道里剩余的数据永久丢失**。
//
// 为什么 Round 11 会误判：单跑、整包跑、`-count=3` 都通过 —— 但这三种方式只是
// **恰好没命中那 46%**。用「多跑几次都过」来判断「不是竞态」是**方法论错误**：
// 一个 46% 的竞态，三次全过的概率仍有 (1-0.46)^3 ≈ 16%（若假定独立）。
// 正确做法是**直接构造高压循环**去逼它现形，而不是靠重跑碰运气。
//
// 本文件提供两条守卫：
//  1. TestCaptureStderr_NoOutputLossUnderPressure（行为，高压循环，确定性红）
//  2. TestCaptureStderr_RestoreDoesNotCloseReadEnd（源码级，钉住具体错误写法）

// TestCaptureStderr_NoOutputLossUnderPressure 是核心守卫。
//
// 每轮写 3 行（与真实 integrity 告警同量级）并断言**第一行必须被读到**。
// 旧实现下实测 300 轮丢 139 轮；新实现下必须 0 丢失。
//
// 循环次数刻意取 300：旧实现下「全 300 轮都不丢」的概率约为 (0.54)^300 ≈ 10^-77，
// 因此这条断言在旧实现下**几乎必然变红** —— 这是一个**确定性**守卫，不是碰运气。
func TestCaptureStderr_NoOutputLossUnderPressure(t *testing.T) {
	const rounds = 300
	var lost int
	var firstLost string
	for i := 0; i < rounds; i++ {
		captured, restore := captureStderr(t)

		// 三行，模拟真实用例（integrity 告警是多行 Fprintln）。
		fmt.Fprintln(os.Stderr, "line-1-marker")
		fmt.Fprintln(os.Stderr, "line-2")
		fmt.Fprintln(os.Stderr, "line-3")

		restore()
		out := captured()

		// 每一行都必须完整读到。
		for _, want := range []string{"line-1-marker", "line-2", "line-3"} {
			if !strings.Contains(out, want) {
				lost++
				if firstLost == "" {
					firstLost = want
				}
				break
			}
		}
	}
	if lost > 0 {
		t.Errorf("captureStderr 丢失输出：%d/%d 轮（首例丢的是 %q）—— "+
			"restore() 不得关闭**读端**，只应关闭写端（发 EOF）", lost, rounds, firstLost)
	}
}

// TestCaptureStderr_RestoreDoesNotCloseReadEnd 是**源码级**守卫（确定性）。
//
// 行为守卫依赖调度时机；源码守卫不受时序影响，直接钉住那个错误写法：
// restore 闭包里不得出现 `r.Close()`。
//
// 注意：判据必须**剥掉注释**再匹配 —— restore 闭包的文档注释里为了解释这条
// 竞态恰恰**写着** `r.Close()` 这个名字（本守卫初版就因此对自己误报）。
func TestCaptureStderr_RestoreDoesNotCloseReadEnd(t *testing.T) {
	data, err := os.ReadFile("integrity_hmac_root_a15_test.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)

	start := strings.Index(src, "func captureStderr(")
	if start < 0 {
		t.Fatal("captureStderr 未找到")
	}
	body := src[start:]

	// 定位 restore 闭包：从 `restore := func() {` 到配平的 `}`。
	rs := strings.Index(body, "restore := func() {")
	if rs < 0 {
		t.Fatal("restore 闭包未找到")
	}
	closure := extractBracedBlock(body[rs:])
	if closure == "" {
		t.Fatal("restore 闭包结尾未找到（花括号未配平）")
	}

	code := stripSlashCommentsR12(closure)
	if strings.Contains(code, "r.Close()") {
		t.Errorf("restore() 内不得 r.Close() —— 会丢弃管道中尚未读走的数据"+
			"（本仓库实测 300 轮丢 139 轮）。只应 w.Close() 来发 EOF。\n可执行代码:\n%s", code)
	}
	if !strings.Contains(code, "w.Close()") {
		t.Error("restore() 必须 w.Close()（关闭写端 = 给读端发 EOF）")
	}
	// 读端必须由读端 goroutine 自己关闭。
	if !strings.Contains(stripSlashCommentsR12(body), "defer r.Close()") {
		t.Error("读端 goroutine 应以 defer r.Close() 自行关闭读端")
	}
}

// extractBracedBlock 从 s 里第一个 `{` 开始，返回配平后的整段（含花括号）。
// 用于准确截出闭包体，避免被闭包内部出现的 `}` 或注释里的花括号干扰。
func extractBracedBlock(s string) string {
	open := strings.Index(s, "{")
	if open < 0 {
		return ""
	}
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[open : i+1]
			}
		}
	}
	return ""
}

// stripSlashCommentsR12 去掉整行 `//` 注释与行尾 `// ...`（本仓库该文件无块注释）。
func stripSlashCommentsR12(s string) string {
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

// 对照：内容读取器在 restore 之后仍能拿到**完整**内容（含大量数据，超过单次
// Read 的 4096 字节缓冲），确保修复没有引入「只能读一屏」的回归。
func TestCaptureStderr_HandlesMultiReadPayload(t *testing.T) {
	captured, restore := captureStderr(t)

	// 写 ~64 KiB（超过 4096 缓冲 16 倍），迫使读端多次 Read。
	const lines = 2000
	for i := 0; i < lines; i++ {
		fmt.Fprintf(os.Stderr, "payload-line-%04d-xxxxxxxxxxxxxxxxxxxxxxxxxxxx\n", i)
	}
	restore()
	out := captured()

	if !strings.Contains(out, "payload-line-0000-") {
		t.Error("首行丢失")
	}
	if !strings.Contains(out, fmt.Sprintf("payload-line-%04d-", lines-1)) {
		t.Errorf("末行丢失（是否只读了一屏？len=%d）", len(out))
	}
	if n := strings.Count(out, "payload-line-"); n != lines {
		t.Errorf("读到 %d 行, want %d 行（数据被截断）", n, lines)
	}
}
