package downloader

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 第十轮审计 P2-2 守卫：轮换 ETag 的服务端不得让续传**永远无法完成**。
//
// 缺陷现场（Round 9 P2-6 修复引入的可用性回归）：部分 CDN / 对象存储 / 网关的 ETag
// **不是内容哈希**，同一份内容每次响应都换一个值。此时 P2-6 的本地校验把「续传」变成
// 「永远从零开始」：每次响应中断后，下一次续传拿到新 ETag → 判定「内容已变」→ 删 `.part`
// → 从头下 → 再次中断 ……
// 审计实测：轮换组 MaxRetries=8 **全部失败、dest 长度 0**；对照组（固定 ETag）
// 7 次请求成功、dest 长度 8192。
//
// 修复（取舍）：**首次**不匹配仍按 P2-6 处理（删前缀 —— 那是真正危险的场景：旧内容
// 前缀拼到新内容上会被 TOFU 永久固化），并在新一代 `.meta` 里打上 `Unreliable`；
// 此后该 URL 的续传不再因校验器不一致而丢弃前缀，累积恢复正常。
//
// **测试构造说明（为什么不模拟「中断连接」）**：本用例最初用
// `panic(http.ErrAbortHandler)` 在写完部分字节后中断响应来复刻审计的网络形态，但
// 实测**服务端中断会截断 TLS 流**，客户端拿到的是请求级 `EOF` 并**丢弃**已收到的
// 部分（`Get "...": EOF`，dest 从未生成）—— 连对照组（固定 ETag）都无法完成，
// 说明该构造测不出目标行为。因此改为**直接驱动那一个决策点**：预置 `.part` +
// 带/不带 `Unreliable` 的 `.meta`，让服务端在**一次** 206 响应里换 ETag。
// 这样「是否保留前缀」只由 `Unreliable` 决定，与连接中断的时序无关 —— 确定性强，
// 且正是修复所针对的分支。

// body206Server 对任何带 Range 的请求回 206（完整剩余区间），ETag 固定为 etag。
func body206Server(t *testing.T, body []byte, etag string) *httptest.Server {
	t.Helper()
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", etag)
		if r.Header.Get("Range") != "" {
			start := parseRangeStart(r.Header.Get("Range"))
			if start < 0 || start > int64(len(body)) {
				w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
				return
			}
			part := body[start:]
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(body)-1, len(body)))
			w.Header().Set("Content-Length", fmt.Sprint(len(part)))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(part)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
}

// parseRangeStart 从 "bytes=N-" 里取 N。
func parseRangeStart(v string) int64 {
	var n int64
	if _, err := fmt.Sscanf(v, "bytes=%d-", &n); err != nil {
		return 0
	}
	return n
}

// 核心：`.meta` 标记 Unreliable 时，即使响应换了 ETag，也必须**保留前缀**并完成下载。
func TestP2_2_UnreliableValidatorKeepsPrefixAcrossETagChange(t *testing.T) {
	body := bytes.Repeat([]byte("R"), 4096)
	srv := body206Server(t, body, `"rotated-new"`)
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "archive.zip")
	rawURL := srv.URL + "/archive.zip"
	part := partPathFor(dest, rawURL)

	// 预置前 2048 字节 + 「校验器不可靠」的标记（模拟上一轮已发现该 URL 轮换 ETag）。
	if err := os.WriteFile(part, body[:2048], 0o644); err != nil {
		t.Fatal(err)
	}
	writePartValidator(partValidatorFor(part), partValidator{ETag: `"rotated-old"`, Unreliable: true})

	err := Download(t.Context(), Options{
		URL: rawURL, Dest: dest, Client: srv.Client(),
		IsThirdParty: true, MaxRetries: 1,
	})
	if err != nil {
		t.Fatalf("P2-2 复现：标记 Unreliable 后仍因 ETag 变化而失败（应保留前缀完成下载）: %v", err)
	}
	got, rerr := os.ReadFile(dest)
	if rerr != nil {
		t.Fatalf("目标未生成: %v", rerr)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("P2-2 复现：内容不完整 —— len=%d want %d（前缀被丢弃或拼接错误）", len(got), len(body))
	}
}

// 对照/取舍：**未**标记 Unreliable 时，首次 ETag 不一致仍必须丢弃前缀（P2-6 的核心保护）。
func TestP2_2_FirstMismatchStillDiscardsPrefix(t *testing.T) {
	oldBody := bytes.Repeat([]byte("O"), 4096)
	newBody := bytes.Repeat([]byte("N"), 4096)
	srv := body206Server(t, newBody, `"v2"`)
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "archive.zip")
	rawURL := srv.URL + "/archive.zip"
	part := partPathFor(dest, rawURL)

	// 残留 = 旧内容前 2048 字节；`.meta` 记旧 ETag 且**未**标记 Unreliable。
	if err := os.WriteFile(part, oldBody[:2048], 0o644); err != nil {
		t.Fatal(err)
	}
	writePartValidator(partValidatorFor(part), partValidator{ETag: `"v1"`})

	dlErr := Download(t.Context(), Options{
		URL: rawURL, Dest: dest, Client: srv.Client(),
		IsThirdParty: true, MaxRetries: 1,
	})

	got, readErr := os.ReadFile(dest)
	if dlErr == nil && readErr == nil {
		if bytes.Equal(got, newBody) {
			return // 重试自愈，落盘完整新内容 —— 可接受
		}
		if bytes.Contains(got, []byte("O")) {
			t.Errorf("P2-2 复现：旧内容前缀被拼到新内容上（len=%d）", len(got))
			return
		}
		t.Errorf("落盘内容既非完整新内容也含旧前缀: len=%d", len(got))
		return
	}
	// fail-closed 路径：Download 必须报出「内容已变」，且 `.part` 已被删除（下次能重来）。
	if dlErr == nil {
		t.Fatalf("既未成功落盘完整内容，也没报错 —— 状态不一致（readErr=%v）", readErr)
	}
	if !strings.Contains(dlErr.Error(), "changed") && !strings.Contains(dlErr.Error(), "ETag") {
		t.Errorf("错误信息应说明内容已变，got: %v", dlErr)
	}
	if _, statErr := os.Stat(part); statErr == nil {
		t.Errorf("fail closed 后 .part 应被删除（否则下次仍会用旧前缀）")
	}
	// 且标记必须被写下，供后续代次使用（这是 P2-2 修复的关键副作用）。
	if v, ok := readPartValidator(partValidatorFor(part)); ok {
		if !v.Unreliable {
			t.Errorf("首次不匹配后应在 .meta 里标记 Unreliable，got unreliable=false")
		}
	} else {
		t.Errorf("首次不匹配后应写下新一代 .meta（含 Unreliable 标记）")
	}
}

// 对照：固定 ETag 且 `.meta` 一致时，续传必须正常（修复不得弄坏正常续传）。
func TestP2_2_StableValidatorResumeStillWorks(t *testing.T) {
	body := bytes.Repeat([]byte("S"), 4096)
	srv := body206Server(t, body, `"stable"`)
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "archive.zip")
	rawURL := srv.URL + "/archive.zip"
	part := partPathFor(dest, rawURL)

	if err := os.WriteFile(part, body[:2048], 0o644); err != nil {
		t.Fatal(err)
	}
	writePartValidator(partValidatorFor(part), partValidator{ETag: `"stable"`})

	if err := Download(t.Context(), Options{
		URL: rawURL, Dest: dest, Client: srv.Client(),
		IsThirdParty: true, MaxRetries: 1,
	}); err != nil {
		t.Fatalf("校验器一致时续传应成功: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("续传结果错误: len=%d want %d", len(got), len(body))
	}
}

// Unreliable 标记必须**跨代保留**：`.part` 被丢弃后它仍然有效（是 URL 的性质，
// 不是某一代 `.part` 的性质）。否则下一次从零下载会把标记覆盖掉，问题复发。
func TestP2_2_UnreliableFlagSurvivesNewDownload(t *testing.T) {
	body := bytes.Repeat([]byte("T"), 4096)
	srv := body206Server(t, body, `"yet-another"`)
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "archive.zip")
	rawURL := srv.URL + "/archive.zip"
	part := partPathFor(dest, rawURL)

	// 已标记不可靠，但**没有** .part（模拟上一代被丢弃）。
	writePartValidator(partValidatorFor(part), partValidator{ETag: `"old"`, Unreliable: true})

	if err := Download(t.Context(), Options{
		URL: rawURL, Dest: dest, Client: srv.Client(),
		IsThirdParty: true, MaxRetries: 1,
	}); err != nil {
		t.Fatalf("download: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("内容不完整: len=%d want %d", len(got), len(body))
	}
}
