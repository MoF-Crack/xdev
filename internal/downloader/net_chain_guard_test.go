// net_chain_guard_test.go — bugx round 7 守卫测试（downloader 下载/网络链）：
//
//	F-12  : 跨 URL 续传产生混合归档（P0）
//	F-12b : resume 不校验响应 Content-Range 起始偏移
//	F-13  : 完整 .part 残留触发 HTTP 416 且永不清理
//	F-13b : 服务端谎报/提前收尾的短 body 被当完整下载接受
//	F-14  : Pause 在无 Content-Length 的下载上退化为 Cancel
//	F-17  : 代理凭据泄漏到用户可见错误与日志
//	F-18  : sanitizedReason 漏掉引号包裹的 URL（查询串凭据泄漏）
//
// 测试约定：downloader 强制 HTTPS，故一律 httptest.NewTLSServer +
// srv.Client() 注入 Options.Client。
package downloader

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// urlScopedPartPath 是 F-12 修复后生产代码使用的 .part 命名：
// <dest>.<sha256(url)[:16]>.part。测试内联同一公式，既钉住命名契约，
// 又让用例在修复前后都能编译（红 → 绿）。
func urlScopedPartPath(dest, rawURL string) string {
	sum := sha256.Sum256([]byte(rawURL))
	return dest + "." + hex.EncodeToString(sum[:])[:16] + ".part"
}

// serveRange 按 "bytes=N-" 简单切片响应，无 Range 时整份返回。
func serveRange(w http.ResponseWriter, r *http.Request, payload []byte) {
	rangeHdr := r.Header.Get("Range")
	if rangeHdr == "" {
		w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
		return
	}
	var start int
	if _, err := fmt.Sscanf(rangeHdr, "bytes=%d-", &start); err != nil || start >= len(payload) {
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	}
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(payload)-1, len(payload)))
	w.Header().Set("Content-Length", fmt.Sprint(len(payload)-start))
	w.WriteHeader(http.StatusPartialContent)
	_, _ = w.Write(payload[start:])
}

// ---------------------------------------------------------------------------
// F-12 — 跨 URL 续传
// ---------------------------------------------------------------------------

// TestF12_CrossURLFallback_NoMixedArchive 是 F-12 的端到端复现：主 URL 传
// 到一半连接被硬中断（残留 4096 字节 'A'），回退 URL 提供另一份 8192 字节
// 'B'。修复前 .part 只由 dest 决定 → 回退 URL 收到 Range: bytes=4096- →
// 落盘 4096 'A' + 4096 'B' 的混合归档，且 Download 返回 nil。
func TestF12_CrossURLFallback_NoMixedArchive(t *testing.T) {
	primaryPayload := bytes.Repeat([]byte("A"), 8192)
	fallbackPayload := bytes.Repeat([]byte("B"), 8192)

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/primary.zip" {
			w.Header().Set("Content-Length", fmt.Sprint(len(primaryPayload)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(primaryPayload[:4096])
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			// 硬中断：客户端收到 unexpected EOF，已写入的 4096 字节留在 .part。
			panic(http.ErrAbortHandler)
		}
		serveRange(w, r, fallbackPayload)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "archive.zip")
	err := Download(context.Background(), Options{
		URL:          srv.URL + "/primary.zip",
		FallbackURLs: []string{srv.URL + "/fallback.zip"},
		Dest:         dest,
		Client:       srv.Client(),
		MaxRetries:   1,
		IsThirdParty: true, // 无哈希通路（TOFU）——正是被钉死混合文件的场景
	})
	if err != nil {
		t.Fatalf("fallback download failed: %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	if !bytes.Equal(got, fallbackPayload) {
		t.Errorf("mixed archive: got %d bytes (A=%d, B=%d, head=%q), want exactly the fallback URL's %d bytes",
			len(got), bytes.Count(got, []byte("A")), bytes.Count(got, []byte("B")),
			string(got[:min(8, len(got))]), len(fallbackPayload))
	}
}

// TestF12_LegacyPartResidue_NotReusedForAnotherURL：任何「不由本 URL 生成」
// 的 .part 残留都不得被当成前缀（这里是修复前命名的 <dest>.part）。
func TestF12_LegacyPartResidue_NotReusedForAnotherURL(t *testing.T) {
	payload := bytes.Repeat([]byte("B"), 8192)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveRange(w, r, payload)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "archive.zip")
	// 另一个 URL（或修复前的版本）留下的残留。
	if err := os.WriteFile(dest+".part", bytes.Repeat([]byte("A"), 4096), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Download(context.Background(), Options{
		URL: srv.URL + "/archive.zip", Dest: dest, Client: srv.Client(),
		MaxRetries: 1, IsThirdParty: true,
	}); err != nil {
		t.Fatalf("download failed: %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("foreign .part residue became a prefix: got %d bytes (head=%q), want %d bytes of 'B'",
			len(got), string(got[:min(8, len(got))]), len(payload))
	}
}

// TestF12_Control_SameURLResumeStillResumes 是对照组：同一个 URL 的残留必须
// 仍然走 Range 续传（修复不能把断点续传一起砍掉）。
func TestF12_Control_SameURLResumeStillResumes(t *testing.T) {
	payload := bytes.Repeat([]byte("C"), 8192)
	var sawRange string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rng := r.Header.Get("Range"); rng != "" {
			sawRange = rng
		}
		serveRange(w, r, payload)
	}))
	defer srv.Close()

	url := srv.URL + "/archive.zip"
	dest := filepath.Join(t.TempDir(), "archive.zip")
	if err := os.WriteFile(urlScopedPartPath(dest, url), payload[:4096], 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Download(context.Background(), Options{
		URL: url, Dest: dest, Client: srv.Client(),
		ExpectedSHA: sha256Of(payload), MaxRetries: 1,
	}); err != nil {
		t.Fatalf("resume download failed: %v", err)
	}

	if sawRange != "bytes=4096-" {
		t.Errorf("same-URL residue must still resume: server saw Range=%q, want %q", sawRange, "bytes=4096-")
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("resumed archive corrupted: got %d bytes, want %d", len(got), len(payload))
	}
}

// TestF12_PartLookupSurvivesGlobMetacharacters：dest 里含 glob 元字符（`[`）
// 时，.part 的查找/清理仍必须命中——否则会留下孤儿 .part 或误报「没有残留」。
// 这是 partialPathsInDir 刻意不用 filepath.Glob 的原因（glob 把 `[` 当字符类
// 起始，匹配会静默失配）。
//
// 只用 `[`：`*`/`?` 在 Windows 上不是合法文件名字符，无法构造。
func TestF12_PartLookupSurvivesGlobMetacharacters(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "weird[1]name.zip")
	part := partPathFor(dest, "https://example.test/archive.zip")
	if err := os.WriteFile(part, []byte("partial bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 同名但分代非法的邻居：绝不能被当成残留。
	if err := os.WriteFile(dest+".notageneration.part", []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if !HasPartialFile(dest) {
		t.Error("HasPartialFile missed the generation-scoped .part (glob metacharacters in dest)")
	}
	if got := GetPartialFileSize(dest); got != int64(len("partial bytes")) {
		t.Errorf("GetPartialFileSize = %d, want %d", got, len("partial bytes"))
	}
	if got := GetPartialFileSizeForURL(dest, "https://example.test/archive.zip"); got != int64(len("partial bytes")) {
		t.Errorf("GetPartialFileSizeForURL = %d, want %d", got, len("partial bytes"))
	}

	if err := CleanupPartialFile(dest); err != nil {
		t.Fatalf("CleanupPartialFile: %v", err)
	}
	if _, err := os.Stat(part); !os.IsNotExist(err) {
		t.Error("CleanupPartialFile left the generation-scoped .part behind")
	}
	if _, err := os.Stat(dest + ".notageneration.part"); err != nil {
		t.Error("CleanupPartialFile removed a file whose suffix is not a valid generation")
	}
}

// ---------------------------------------------------------------------------
// F-12b — Content-Range 起始偏移校验
// ---------------------------------------------------------------------------

// TestF12b_ContentRangeOffsetMismatch_Discarded：服务端回 206 但 Content-Range
// 起点为 0（请求的是 bytes=5000-）→ 修复前 body 被写在偏移 5000 处，产出
// 15000 字节的错位文件并被无哈希通路接受。
func TestF12b_ContentRangeOffsetMismatch_Discarded(t *testing.T) {
	payload := bytes.Repeat([]byte("R"), 10000)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 无视 Range 起点，从 0 开始回整份，并如实声明 Content-Range 起点 0。
		w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", len(payload)-1, len(payload)))
		w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	url := srv.URL + "/archive.zip"
	dest := filepath.Join(t.TempDir(), "archive.zip")
	partPath := urlScopedPartPath(dest, url)
	if err := os.WriteFile(partPath, payload[:5000], 0o644); err != nil {
		t.Fatal(err)
	}

	err := Download(context.Background(), Options{
		URL: url, Dest: dest, Client: srv.Client(), MaxRetries: 1, IsThirdParty: true,
	})
	if err == nil {
		got, _ := os.ReadFile(dest)
		t.Fatalf("mismatched Content-Range must be rejected, got nil error (final size=%d, want an error)", len(got))
	}
	if !strings.Contains(err.Error(), "Content-Range") {
		t.Errorf("error = %v, want it to name the Content-Range mismatch", err)
	}
	if _, statErr := os.Stat(dest); statErr == nil {
		t.Error("a mis-offset archive must not be renamed into place")
	}
	if _, statErr := os.Stat(partPath); statErr == nil {
		t.Error("the unusable .part must be discarded")
	}
}

// ---------------------------------------------------------------------------
// F-13 — HTTP 416 + 完整 .part 残留
// ---------------------------------------------------------------------------

// TestF13_CompletePartResidue_416VerifiesAndRenames：本地 .part 已等于完整文件
// 时服务器只能回 416（PoC 里服务端就是裸 416）。修复前该分支直接返回错误且
// 不删 .part → 该 dest 永久不可恢复。修复后应把手上的 .part 当完整文件走
// 「校验 + rename」。
func TestF13_CompletePartResidue_416VerifiesAndRenames(t *testing.T) {
	payload := bytes.Repeat([]byte("P"), 8192)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "" {
			// 起点越界（本地已完整）——服务器的唯一合法回答。
			// 与 PoC 一致：裸 416，不带 Content-Range。
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	url := srv.URL + "/archive.zip"
	dest := filepath.Join(t.TempDir(), "archive.zip")
	partPath := urlScopedPartPath(dest, url)
	if err := os.WriteFile(partPath, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	// 同时放一份修复前命名的残留：修复前代码读的是 <dest>.part，于是发出
	// 越界 Range 并吃到 416（红）；修复后只看本 URL 分代的 .part（绿）。
	if err := os.WriteFile(dest+".part", payload, 0o644); err != nil {
		t.Fatal(err)
	}

	err := Download(context.Background(), Options{
		URL: url, Dest: dest, Client: srv.Client(),
		ExpectedSHA: sha256Of(payload), MaxRetries: 1,
	})
	if err != nil {
		t.Fatalf("complete .part residue must be verified and renamed, got: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("archive content mismatch after 416 recovery: got %d bytes, want %d", len(got), len(payload))
	}
	if _, statErr := os.Stat(partPath); statErr == nil {
		t.Error("the .part file must be consumed (renamed) by the 416 recovery")
	}
}

// TestF13_416WithContentRangeTotal_Promotes：服务端按 RFC 回 `bytes */total`
// 且 total 与本地长度一致时，同样应恢复（这是「.part 完整」的正面证据）。
func TestF13_416WithContentRangeTotal_Promotes(t *testing.T) {
	payload := bytes.Repeat([]byte("Q"), 4096)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", len(payload)))
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
	}))
	defer srv.Close()

	url := srv.URL + "/archive.zip"
	dest := filepath.Join(t.TempDir(), "archive.zip")
	if err := os.WriteFile(urlScopedPartPath(dest, url), payload, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Download(context.Background(), Options{
		URL: url, Dest: dest, Client: srv.Client(),
		ExpectedSHA: sha256Of(payload), MaxRetries: 1,
	}); err != nil {
		t.Fatalf("416 with a matching `bytes */total` must recover: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("recovered archive mismatch: got %d bytes, want %d", len(got), len(payload))
	}
}

// TestF13_416WithMismatchingTotal_DiscardsPart：服务端声明的 total 与本地
// .part 不符（本地是截断的残缺品）→ 必须删掉 .part，不能把残缺品 rename 成
// 正式归档。
func TestF13_416WithMismatchingTotal_DiscardsPart(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Range", "bytes */10000")
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
	}))
	defer srv.Close()

	url := srv.URL + "/archive.zip"
	dest := filepath.Join(t.TempDir(), "archive.zip")
	partPath := urlScopedPartPath(dest, url)
	if err := os.WriteFile(partPath, bytes.Repeat([]byte("T"), 4096), 0o644); err != nil {
		t.Fatal(err)
	}

	err := Download(context.Background(), Options{
		URL: url, Dest: dest, Client: srv.Client(),
		ExpectedSHA: sha256Of(bytes.Repeat([]byte("T"), 10000)), MaxRetries: 1,
	})
	if err == nil {
		t.Fatal("a truncated .part must not be promoted by a 416")
	}
	if _, statErr := os.Stat(dest); statErr == nil {
		t.Error("the truncated residue must not be renamed into place")
	}
	if _, statErr := os.Stat(partPath); statErr == nil {
		t.Error("the truncated .part must be discarded so the next attempt can start over")
	}
}

// ---------------------------------------------------------------------------
// F-13b — 短 body 不得被当完整下载接受
// ---------------------------------------------------------------------------

// lyingLengthTransport 声明 ContentLength 为 declared，但 body 只有 len(body)
// 字节且以干净的 EOF 结束（服务端提前收尾/谎报长度）。
type lyingLengthTransport struct {
	body     []byte
	declared int64
}

func (t *lyingLengthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode:    http.StatusOK,
		Header:        http.Header{},
		Body:          io.NopCloser(bytes.NewReader(t.body)),
		ContentLength: t.declared,
		Request:       req,
	}, nil
}

// TestF13b_ShortBodyNotAcceptedAsComplete：声明 10000 字节、实际只给 4000 字节
// 的响应不得被当成完整归档（修复前：err=nil，且 TOFU 通路还会写 sidecar pin）。
func TestF13b_ShortBodyNotAcceptedAsComplete(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "archive.zip")
	err := Download(context.Background(), Options{
		URL:  "https://example.invalid/archive.zip",
		Dest: dest,
		Client: &http.Client{Transport: &lyingLengthTransport{
			body:     bytes.Repeat([]byte("S"), 4000),
			declared: 10000,
		}},
		MaxRetries: 1, IsThirdParty: true,
	})
	if err == nil {
		t.Fatalf("short body accepted as complete (final size=%d)", fileSizeOrZero(dest))
	}
	if _, statErr := os.Stat(dest); statErr == nil {
		t.Error("an incomplete archive must not be renamed into place")
	}
	if _, statErr := os.Stat(dest + ".sha256"); statErr == nil {
		t.Error("no TOFU pin may be written for an incomplete archive")
	}
}

func fileSizeOrZero(path string) int64 {
	if fi, err := os.Stat(path); err == nil {
		return fi.Size()
	}
	return 0
}

// ---------------------------------------------------------------------------
// F-14 — chunked（无 Content-Length）下载上的 Pause
// ---------------------------------------------------------------------------

// chunkedSlowServer 不设 Content-Length（Flush 强制 chunked），并按 100ms
// 一块持续输出，给 Pause 留出窗口。
func chunkedSlowServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fl, _ := w.(http.Flusher)
		chunk := bytes.Repeat([]byte("c"), 1024)
		for i := 0; i < 60; i++ {
			if _, err := w.Write(chunk); err != nil {
				return
			}
			if fl != nil {
				fl.Flush()
			}
			select {
			case <-r.Context().Done():
				return
			case <-time.After(100 * time.Millisecond):
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// waitForPart 等到盘上出现 .part 字节（不依赖进度回调）。
func waitForPart(t *testing.T, dest string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if GetPartialFileSize(dest) > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no .part bytes appeared on disk within 3s")
}

// TestF14_PauseOnChunkedDownload_StaysPaused：无 Content-Length 时进度回调被
// total>0 门控，pausedAt 恒 0 → 修复前 Pause 退化为 Cancel（删 .part + 状态
// 错报 cancelled）。
func TestF14_PauseOnChunkedDownload_StaysPaused(t *testing.T) {
	srv := chunkedSlowServer(t)
	dest := filepath.Join(t.TempDir(), "archive.zip")
	m, id := managerFor(t, srv, dest)

	waitForPart(t, dest)
	if err := m.Pause(id); err != nil {
		t.Fatalf("Pause: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if st := mustStatus(t, m, id); st == StatusPaused || st == StatusCancelled || st == StatusFailed {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if st := mustStatus(t, m, id); st != StatusPaused {
		t.Errorf("status = %s, want paused (chunked download must not degrade Pause to Cancel)", st)
	}
	if GetPartialFileSize(dest) <= 0 {
		t.Error("a paused chunked download lost its .part bytes (pre-fix: treated as Cancel and deleted)")
	}
}

// TestF14_Control_CancelOnChunkedDownload_DeletesPart：对照组——Cancel 仍然
// 必须删除临时文件（修复不得把取消语义改坏）。
func TestF14_Control_CancelOnChunkedDownload_DeletesPart(t *testing.T) {
	srv := chunkedSlowServer(t)
	dest := filepath.Join(t.TempDir(), "archive.zip")
	m, id := managerFor(t, srv, dest)

	waitForPart(t, dest)
	if err := m.Cancel(id); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if mustStatus(t, m, id) == StatusCancelled && GetPartialFileSize(dest) == 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if st := mustStatus(t, m, id); st != StatusCancelled {
		t.Errorf("status = %s, want cancelled", st)
	}
	if GetPartialFileSize(dest) != 0 {
		t.Errorf("cancelled download left %d bytes of .part residue", GetPartialFileSize(dest))
	}
}

// ---------------------------------------------------------------------------
// F-17 — 代理凭据脱敏
// ---------------------------------------------------------------------------

// TestF17_ProxyCredentialsRedactedInError：代理不可达时用户可见的错误消息
// 不得回显 userinfo 里的凭据。
func TestF17_ProxyCredentialsRedactedInError(t *testing.T) {
	const secret = "s3cr3tPoc"
	err := Download(context.Background(), Options{
		URL:        "https://127.0.0.1:1/archive.zip",
		Dest:       filepath.Join(t.TempDir(), "archive.zip"),
		ProxyURL:   "http://alice:" + secret + "@127.0.0.1:1",
		MaxRetries: 1,
	})
	if err == nil {
		t.Fatal("expected the download to fail")
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("error leaks the proxy password: %s", err.Error())
	}
	if !strings.Contains(err.Error(), "<redacted>@") {
		t.Errorf("expected the proxy userinfo to be redacted to <redacted>@; got: %s", err.Error())
	}
}

// TestF17_InvalidProxyURLRedactedInLog：非法代理 URL 的 WARNING 日志同样不得
// 回显凭据（url.Parse 的错误文本会原样带上 URL）。
func TestF17_InvalidProxyURLRedactedInLog(t *testing.T) {
	const secret = "s3cr3tPoc"
	var buf bytes.Buffer
	origWriter := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(origWriter)

	_ = Download(context.Background(), Options{
		URL:        "https://127.0.0.1:1/archive.zip",
		Dest:       filepath.Join(t.TempDir(), "archive.zip"),
		ProxyURL:   "http://alice:" + secret + "@[::1", // 未闭合 IPv6 字面量 → 解析失败
		MaxRetries: 1,
	})

	out := buf.String()
	if !strings.Contains(out, "invalid proxy URL") {
		t.Errorf("expected an invalid-proxy WARNING; log=%q", out)
	}
	if strings.Contains(out, secret) {
		t.Errorf("WARNING log leaks the proxy password: %q", out)
	}
}

// ---------------------------------------------------------------------------
// F-18 — sanitizedReason 对引号包裹的 URL 也要脱敏
// ---------------------------------------------------------------------------

// TestF18_SanitizedReasonRedactsQuotedURL：net/http 的错误文本是
// `Get "https://host/path?token=X": ...`，字段以引号开头，修复前的
// strings.HasPrefix(field, "https://") 判假 → 查询串凭据原样泄漏。
func TestF18_SanitizedReasonRedactsQuotedURL(t *testing.T) {
	const secret = "topsecretPoc"
	reason := `Get "https://host.example/a.zip?token=` + secret + `": dial tcp 1.2.3.4:443: connect: connection refused`

	got := sanitizedReason(errors.New(reason))
	if strings.Contains(got, secret) {
		t.Errorf("sanitizedReason leaks the query credential: %s", got)
	}
	if !strings.Contains(got, "https://host.example/a.zip") {
		t.Errorf("sanitizedReason dropped the URL path: %s", got)
	}
}

// TestF18_TransportErrorHidesQueryCredential：端到端——网络层失败时聚合错误
// 不得带上 URL 查询串里的凭据。
func TestF18_TransportErrorHidesQueryCredential(t *testing.T) {
	const secret = "topsecretPoc"
	err := Download(context.Background(), Options{
		URL:        "https://127.0.0.1:1/archive.zip?token=" + secret,
		Dest:       filepath.Join(t.TempDir(), "archive.zip"),
		MaxRetries: 1,
	})
	if err == nil {
		t.Fatal("expected the download to fail")
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("aggregated error leaks the query credential: %s", err.Error())
	}
}
