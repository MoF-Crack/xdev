// round8_guard_test.go — bugx round 8 守卫测试（downloader）：
//
//	P1-1 : 部分 2xx 响应被当成完整归档接受，并在 TOFU 通路上永久固化
//	       (a) 新鲜下载（未发 Range）收到 206
//	       (b) 组装后总长度 != Content-Range 声明的 total
//	       (c) 空 body 的 200（Content-Length: 0）
//	P1-2 : 裸 416（无 Content-Range）把任意本地 .part（含截断残片）提升为正式归档
//	P2-5 : 失败候选 URL 的 .part / .part.lock 变成永久孤儿
//	P2-7 : GetPartialFileSize 取跨代最大值，进度/暂停字节数指向陌生 URL
//
// 测试约定同 net_chain_guard_test.go：downloader 强制 HTTPS，故一律
// httptest.NewTLSServer + srv.Client() 注入 Options.Client。
package downloader

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/util"
)

// tofuOptions 构造「无哈希（TOFU）通路」的 Options —— 正是被钉死残缺归档的场景。
func tofuOptions(srv *httptest.Server, url, dest string) Options {
	return Options{
		URL:          url,
		Dest:         dest,
		Client:       srv.Client(),
		MaxRetries:   1,
		IsThirdParty: true, // 无 ExpectedSHA → TOFU sidecar pin
	}
}

// assertNoArchiveAndNoPin 断言失败路径「删 .part + fail closed」的收尾效果：
// 正式归档不存在、TOFU pin 不存在、.part 残留不存在。
func assertNoArchiveAndNoPin(t *testing.T, dest string, partPath string) {
	t.Helper()
	if _, err := os.Stat(dest); err == nil {
		t.Error("a rejected response must not be renamed into the final archive")
	}
	if _, err := os.Stat(dest + ".sha256"); err == nil {
		t.Error("a TOFU pin must never be written for a rejected response (it would be permanent)")
	}
	if _, err := os.Stat(partPath); err == nil {
		t.Error("the unusable .part must be discarded so the next attempt can start over")
	}
}

// ---------------------------------------------------------------------------
// P1-1 (a) — 新鲜下载收到 206
// ---------------------------------------------------------------------------

// TestP1_1_FreshDownloadRejecting206：未发 Range 的新鲜请求收到 206（部分区间，
// 如 `bytes 0-499/1000`）。修复前：F-12b 被 `fromBytes > 0 && 206` 门控而跳过，
// F-13b 只比 ContentLength（500 == 500）→ 500 字节残缺文件被 rename 并写 TOFU pin。
func TestP1_1_FreshDownloadRejecting206(t *testing.T) {
	payload := bytes.Repeat([]byte("F"), 1000)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 无视请求（未带 Range），只回前 500 字节并如实声明该区间的 Content-Length。
		w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-499/%d", len(payload)))
		w.Header().Set("Content-Length", "500")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload[:500])
	}))
	defer srv.Close()

	url := srv.URL + "/archive.zip"
	dest := filepath.Join(t.TempDir(), "archive.zip")
	err := Download(context.Background(), tofuOptions(srv, url, dest))
	if err == nil {
		t.Fatalf("a 206 answer to a fresh (no-Range) request must be rejected, got nil error (final size=%d)",
			fileSizeOrZero(dest))
	}
	assertNoArchiveAndNoPin(t, dest, partPathFor(dest, url))
}

// ---------------------------------------------------------------------------
// P1-1 (b) — 组装后总长度 != Content-Range 的 total
// ---------------------------------------------------------------------------

// TestP1_1_AssembledLengthMismatchRejected：本地已有 500 字节，服务端回
// `bytes 500-599/1000`（total=1000）且 Content-Length 如实为 100。修复前：
// F-12b 起点校验通过（500 == 500），F-13b 的 100+500 == 100+500 通过 →
// 组装出的 600 字节残缺文件被接受并写 pin（永不自我修复）。
func TestP1_1_AssembledLengthMismatchRejected(t *testing.T) {
	payload := bytes.Repeat([]byte("R"), 1000)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") == "" {
			t.Error("precondition: the client was expected to resume with a Range header")
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes 500-599/%d", len(payload)))
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload[500:600])
	}))
	defer srv.Close()

	url := srv.URL + "/archive.zip"
	dest := filepath.Join(t.TempDir(), "archive.zip")
	partPath := partPathFor(dest, url)
	if err := os.WriteFile(partPath, payload[:500], 0o644); err != nil {
		t.Fatal(err)
	}

	err := Download(context.Background(), tofuOptions(srv, url, dest))
	if err == nil {
		t.Fatalf("an archive shorter than the declared Content-Range total must be rejected, got nil error (final size=%d)",
			fileSizeOrZero(dest))
	}
	assertNoArchiveAndNoPin(t, dest, partPath)
}

// TestP1_1_AssembledLengthMismatch_Control 是对照组：服务端如实把整份文件
// 补齐（`bytes 500-999/1000`，total 与组装长度一致）时必须成功——修复不能把
// 正常的断点续传一起砍掉。
func TestP1_1_AssembledLengthMismatch_Control(t *testing.T) {
	payload := bytes.Repeat([]byte("S"), 1000)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes 500-%d/%d", len(payload)-1, len(payload)))
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(payload)-500))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload[500:])
	}))
	defer srv.Close()

	url := srv.URL + "/archive.zip"
	dest := filepath.Join(t.TempDir(), "archive.zip")
	if err := os.WriteFile(partPathFor(dest, url), payload[:500], 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Download(context.Background(), Options{
		URL: url, Dest: dest, Client: srv.Client(),
		ExpectedSHA: sha256Of(payload), MaxRetries: 1,
	}); err != nil {
		t.Fatalf("a correct resume must still succeed: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("resumed archive mismatch: got %d bytes, want %d", len(got), len(payload))
	}
}

// ---------------------------------------------------------------------------
// P1-1 (c) — 空 body 的 200
// ---------------------------------------------------------------------------

// TestP1_1_EmptyBody200Rejected：`200 + Content-Length: 0`。修复前 F-13b 的
// `0 == 0` 通过 → 0 字节归档被 rename，并写入空文件哈希 e3b0c442… 的 TOFU pin。
func TestP1_1_EmptyBody200Rejected(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "0")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	url := srv.URL + "/archive.zip"
	dest := filepath.Join(t.TempDir(), "archive.zip")
	err := Download(context.Background(), tofuOptions(srv, url, dest))
	if err == nil {
		t.Fatalf("an empty 200 response must be rejected, got nil error (final size=%d)", fileSizeOrZero(dest))
	}
	assertNoArchiveAndNoPin(t, dest, partPathFor(dest, url))
}

// ---------------------------------------------------------------------------
// P1-1 (H19) — 残缺文件不得被「固化」：连续重试都必须失败且不留 pin
// ---------------------------------------------------------------------------

// TestP1_1_PartialResponseNeverPinned：服务端每次都只回 500/1000 字节的 206。
// 修复前：3 次重试全部接受同一份残缺文件，pin 固定为残缺文件的哈希，第 2/3 次
// 还会打印 "SHA-256 verification passed (cached sidecar)" —— 永不自我修复。
func TestP1_1_PartialResponseNeverPinned(t *testing.T) {
	payload := bytes.Repeat([]byte("H"), 1000)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-499/%d", len(payload)))
		w.Header().Set("Content-Length", "500")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload[:500])
	}))
	defer srv.Close()

	url := srv.URL + "/archive.zip"
	dest := filepath.Join(t.TempDir(), "archive.zip")
	for attempt := 1; attempt <= 3; attempt++ {
		err := Download(context.Background(), tofuOptions(srv, url, dest))
		if err == nil {
			t.Fatalf("attempt %d accepted a partial 206 as a complete archive (final size=%d)",
				attempt, fileSizeOrZero(dest))
		}
		if _, statErr := os.Stat(dest + ".sha256"); statErr == nil {
			t.Fatalf("attempt %d pinned a partial archive — every later install would reuse the broken pin", attempt)
		}
	}
}

// ---------------------------------------------------------------------------
// P1-2 — 裸 416 不得作为「本地 .part 已完整」的证据
// ---------------------------------------------------------------------------

// TestP1_2_Bare416MustNotPromoteTruncatedPart：真实文件 8192 字节，本地 .part
// 只有 4096 字节（截断），服务端回**裸 416**（RFC 9110 允许不带 Content-Range，
// Go 写 WriteHeader(416) 且无 body 时 ContentLength 就是 0）。修复前
// completePartResidue 的「ContentLength == 0 即完整」退路 → 4096 字节残片被
// rename 成正式归档并写 pin。
func TestP1_2_Bare416MustNotPromoteTruncatedPart(t *testing.T) {
	fullPayload := bytes.Repeat([]byte("T"), 8192)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "" {
			// 与 PoC 一致：裸 416，不带 Content-Range、不带 body。
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(fullPayload)))
		_, _ = w.Write(fullPayload)
	}))
	defer srv.Close()

	url := srv.URL + "/archive.zip"
	dest := filepath.Join(t.TempDir(), "archive.zip")
	partPath := partPathFor(dest, url)
	if err := os.WriteFile(partPath, fullPayload[:4096], 0o644); err != nil {
		t.Fatal(err)
	}

	err := Download(context.Background(), tofuOptions(srv, url, dest))
	if err == nil {
		t.Fatalf("a bare 416 must not promote a truncated .part, got nil error (final size=%d)", fileSizeOrZero(dest))
	}
	assertNoArchiveAndNoPin(t, dest, partPath)
}

// TestP1_2_Bare416CompletePart_StillRejected（无哈希通路）：即使本地 .part
// 恰好等于真实文件长度，裸 416 也不是完整性的**证据**——按审计要求宁可让用户
// 从零重下。只有服务端给出 `bytes */total` 或 `bytes A-B/total` 且 total 与本地
// 长度一致时才允许提升（该正向路径由 TestF13_* 覆盖）。
func TestP1_2_Bare416CompletePart_StillRejected(t *testing.T) {
	payload := bytes.Repeat([]byte("U"), 8192)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
	}))
	defer srv.Close()

	url := srv.URL + "/archive.zip"
	dest := filepath.Join(t.TempDir(), "archive.zip")
	partPath := partPathFor(dest, url)
	if err := os.WriteFile(partPath, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Download(context.Background(), tofuOptions(srv, url, dest)); err == nil {
		t.Fatal("a bare 416 carries no evidence of completeness — it must fail closed")
	}
	assertNoArchiveAndNoPin(t, dest, partPath)
}

// ---------------------------------------------------------------------------
// P2-5 — 失败候选 URL 的 .part / .part.lock 孤儿
// ---------------------------------------------------------------------------

// partArtifactsInDir 列出目录下全部 .part / .part.lock 残留（判据用）。
func partArtifactsInDir(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".part") || strings.HasSuffix(name, ".part.lock") {
			out = append(out, name)
		}
	}
	return out
}

// TestP2_5_FailedCandidateResidueCleaned：主 URL 中途断流（残留 4096 字节 +
// 一个 .part.lock），回退 URL 成功。修复前成功路径从不清理失败候选的代 →
// dest 已生成但目录里永久残留 .part 与 .part.lock。
func TestP2_5_FailedCandidateResidueCleaned(t *testing.T) {
	primaryPayload := bytes.Repeat([]byte("A"), 8192)
	fallbackPayload := bytes.Repeat([]byte("B"), 8192)

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/primary.zip" {
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(primaryPayload)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(primaryPayload[:4096])
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			panic(http.ErrAbortHandler) // 硬中断：4096 字节留在主 URL 的 .part
		}
		serveRange(w, r, fallbackPayload)
	}))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "archive.zip")
	if err := Download(context.Background(), Options{
		URL:          srv.URL + "/primary.zip",
		FallbackURLs: []string{srv.URL + "/fallback.zip"},
		Dest:         dest,
		Client:       srv.Client(),
		MaxRetries:   1,
		IsThirdParty: true,
	}); err != nil {
		t.Fatalf("fallback download failed: %v", err)
	}
	if got, err := os.ReadFile(dest); err != nil || !bytes.Equal(got, fallbackPayload) {
		t.Fatalf("archive not produced by the fallback URL: len=%d err=%v", len(got), err)
	}
	if left := partArtifactsInDir(t, dir); len(left) != 0 {
		t.Errorf("successful download left orphan partial artifacts: %v (want none)", left)
	}
}

// TestP2_5_CleanupHelpersRemovePartLocks：CleanupPartialFile /
// CleanupAllPartialsInDir 必须一并删除 <part>.lock —— 修复前
// util.AcquireFileLock 创建的锁文件没有任何代码删除。
func TestP2_5_CleanupHelpersRemovePartLocks(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "archive.zip")
	partA := partPathFor(dest, "https://a.test/archive.zip")
	partB := partPathFor(dest, "https://b.test/archive.zip")

	for _, p := range []string{partA, partB, dest + ".part"} {
		if err := os.WriteFile(p, []byte("residue"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// 锁文件：分代命名 + 历史未分代命名，以及「.part 已删但锁还在」的形态。
	for _, l := range []string{partA + ".lock", partB + ".lock", dest + ".part.lock"} {
		if err := os.WriteFile(l, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// 只留锁、不留 .part 的分代（孤儿锁的最坏形态）。
	orphanLock := partPathFor(dest, "https://c.test/archive.zip") + ".lock"
	if err := os.WriteFile(orphanLock, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := CleanupPartialFile(dest); err != nil {
		t.Fatalf("CleanupPartialFile: %v", err)
	}
	if left := partArtifactsInDir(t, dir); len(left) != 0 {
		t.Errorf("CleanupPartialFile left artifacts behind: %v", left)
	}

	// 目录级清理同样必须覆盖锁文件。
	if err := os.WriteFile(partA+".lock", nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest+".part", []byte("residue"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CleanupAllPartialsInDir(dir); err != nil {
		t.Fatalf("CleanupAllPartialsInDir: %v", err)
	}
	if left := partArtifactsInDir(t, dir); len(left) != 0 {
		t.Errorf("CleanupAllPartialsInDir left artifacts behind: %v", left)
	}
}

// TestP2_5_InFlightGenerationIsNotDeleted：清理必须**只删无主的代**。
// 同一 dest 下不同候选 URL 各有独立的分代与锁，两个下载可以同时在跑；成功
// 收尾的清理不得把别人在途的 .part 从目录里抽走（否则那个下载会写进一个已
// 解除链接的文件，或直接失败）。
func TestP2_5_InFlightGenerationIsNotDeleted(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "archive.zip")
	finishedURL := "https://finished.test/archive.zip"
	inflightURL := "https://inflight.test/archive.zip"

	finishedPart := partPathFor(dest, finishedURL)
	inflightPart := partPathFor(dest, inflightURL)
	if err := os.WriteFile(finishedPart, []byte("dead residue"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inflightPart, []byte("live bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 模拟另一个下载进程正持有 inflight 分代的锁。
	held, err := util.AcquireFileLock(inflightPart+".lock", 0)
	if err != nil {
		t.Fatalf("acquire the in-flight lock: %v", err)
	}
	defer held.Release()

	if err := removePartialFiles(dest, finishedURL, inflightURL); err != nil {
		t.Fatalf("removePartialFiles: %v", err)
	}

	if _, err := os.Stat(finishedPart); err == nil {
		t.Error("the finished candidate's dead .part residue was not cleaned up")
	}
	if got, err := os.ReadFile(inflightPart); err != nil || string(got) != "live bytes" {
		t.Errorf("an in-flight download's .part was deleted or damaged: content=%q err=%v", got, err)
	}
	if _, err := os.Stat(inflightPart + ".lock"); err != nil {
		t.Error("an in-flight download's lock file was deleted (would break the P1-01 mutual exclusion)")
	}
}

// ---------------------------------------------------------------------------
// P2-7 — 进度/暂停字节数必须按本任务的候选 URL 分代查询
// ---------------------------------------------------------------------------

// TestP2_7_PartialBytesIgnoreForeignGeneration：A 代残留 10 000 000 字节、
// 当前任务（URL B）残留 1024 字节。修复前 partialBytesForTask 取「所有代中最大
// 的那个」→ 把 A 的 10 000 000 当作 B 的进度上报。
func TestP2_7_PartialBytesIgnoreForeignGeneration(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "archive.zip")
	foreignURL := "https://a.test/archive.zip"
	ownURL := "https://b.test/archive.zip"

	if err := os.WriteFile(partPathFor(dest, foreignURL), make([]byte, 10000000), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(partPathFor(dest, ownURL), make([]byte, 1024), 0o644); err != nil {
		t.Fatal(err)
	}

	task := &Task{opts: Options{URL: ownURL, Dest: dest}}

	if got := partialBytesForTask(task); got != 1024 {
		t.Errorf("partialBytesForTask = %d, want 1024 (the foreign URL's 10 000 000-byte generation must not be reported as this task's progress)", got)
	}
	if got := GetPartialFileSizeForURL(dest, ownURL); got != 1024 {
		t.Errorf("GetPartialFileSizeForURL(own) = %d, want 1024", got)
	}
	// 纯展示型调用方保留「最大值」语义。
	if got := GetPartialFileSize(dest); got != 10000000 {
		t.Errorf("GetPartialFileSize = %d, want 10000000 (display-only max semantics)", got)
	}
}

// TestP2_7_PartialBytesCoverOwnFallbacks：本任务自己的回退 URL 的代仍必须计入
// （回退 URL 正在下载时 Pause 要能报出真实进度），但陌生 URL 的代不计入。
func TestP2_7_PartialBytesCoverOwnFallbacks(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "archive.zip")
	primaryURL := "https://primary.test/archive.zip"
	fallbackURL := "https://fallback.test/archive.zip"
	foreignURL := "https://foreign.test/archive.zip"

	if err := os.WriteFile(partPathFor(dest, primaryURL), make([]byte, 128), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(partPathFor(dest, fallbackURL), make([]byte, 4096), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(partPathFor(dest, foreignURL), make([]byte, 9999999), 0o644); err != nil {
		t.Fatal(err)
	}

	task := &Task{opts: Options{URL: primaryURL, FallbackURLs: []string{fallbackURL}, Dest: dest}}
	if got := partialBytesForTask(task); got != 4096 {
		t.Errorf("partialBytesForTask = %d, want 4096 (own fallback generation must be counted, foreign must not)", got)
	}
}

// ---------------------------------------------------------------------------
// 回归护栏：正常下载路径不受 P1-1/P1-2 收口影响
// ---------------------------------------------------------------------------

// TestP1_1_Control_Plain200StillWorks：普通 200 + 完整 body（带正确
// Content-Length）必须照常成功——三道新校验不得误伤正常下载。
func TestP1_1_Control_Plain200StillWorks(t *testing.T) {
	payload := bytes.Repeat([]byte("C"), 4096)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(payload)))
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "archive.zip")
	if err := Download(context.Background(), Options{
		URL: srv.URL + "/archive.zip", Dest: dest, Client: srv.Client(),
		ExpectedSHA: sha256Of(payload), MaxRetries: 1,
	}); err != nil {
		t.Fatalf("plain 200 download failed: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("archive mismatch: got %d bytes, want %d", len(got), len(payload))
	}
}

// TestP1_1_Control_ChunkedNoContentLength：chunked（无 Content-Length）响应
// 仍必须被接受——ContentLength == -1 不得被当成「空下载」。
func TestP1_1_Control_ChunkedNoContentLength(t *testing.T) {
	payload := bytes.Repeat([]byte("K"), 2048)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fl, _ := w.(http.Flusher)
		for i := 0; i < len(payload); i += 512 {
			if _, err := w.Write(payload[i : i+512]); err != nil {
				return
			}
			if fl != nil {
				fl.Flush()
			}
		}
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "archive.zip")
	if err := Download(context.Background(), Options{
		URL: srv.URL + "/archive.zip", Dest: dest, Client: srv.Client(),
		ExpectedSHA: sha256Of(payload), MaxRetries: 1,
	}); err != nil {
		t.Fatalf("chunked download failed: %v", err)
	}
	got, err := io.ReadAll(mustOpen(t, dest))
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("chunked archive mismatch: got %d bytes, want %d", len(got), len(payload))
	}
}

func mustOpen(t *testing.T, path string) *os.File {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}
