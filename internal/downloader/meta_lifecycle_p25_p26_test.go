package downloader

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 第十轮审计 P2-5 / P2-6 守卫：`.meta`（内容身份校验器）必须与 `.part` 同生共死，
// 且**每一个**成功出口都要清理残留。
//
// P2-5 缺陷现场：`.meta` 只在 `finalizeDownload` 与 `removePartResidueIfUnheld` 两处
// 被删，其余「删 `.part`」的地方都是裸 `os.Remove(partPath)`。审计实测「GUI 清缓存」
// （`CleanupAllPartialsInDir`）之后目录仍残留 `archive.zip.<gen>.part.meta`。
//
// P2-6 缺陷现场：清理只挂在 `tryURLList` 成功的**第一个**出口；「代理失败 → 直连重试
// 成功」是第二个出口，它直接 return —— 审计实测该组合下残留 `.part` + 2 个 `.part.lock`。

// listPartialArtifacts 返回目录里所有 `.part` / `.part.lock` / `.part.meta` 残留。
func listPartialArtifacts(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if strings.Contains(name, ".part") {
			out = append(out, name)
		}
	}
	return out
}

// P2-5：`CleanupPartialFile` 必须清掉 `.meta`。
func TestP2_5_CleanupPartialFileRemovesMeta(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "archive.zip")
	rawURL := "https://example.test/a.zip"
	part := partPathFor(dest, rawURL)
	meta := partValidatorFor(part)

	if err := os.WriteFile(part, []byte("partial"), 0o644); err != nil {
		t.Fatal(err)
	}
	writePartValidator(meta, partValidator{ETag: `"v1"`})
	if _, err := os.Stat(meta); err != nil {
		t.Fatalf("前置条件：.meta 应已写入: %v", err)
	}

	if err := CleanupPartialFile(dest); err != nil {
		t.Fatalf("CleanupPartialFile: %v", err)
	}

	if got := listPartialArtifacts(t, dir); len(got) != 0 {
		t.Errorf("P2-5 复现：CleanupPartialFile 之后仍残留 %v（.meta 未被清理）", got)
	}
}

// P2-5：`CleanupAllPartialsInDir`（GUI「清缓存」走的就是它）必须清掉 `.meta`。
func TestP2_5_CleanupAllPartialsInDirRemovesMeta(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "archive.zip")
	rawURL := "https://example.test/a.zip"
	part := partPathFor(dest, rawURL)

	if err := os.WriteFile(part, []byte("partial"), 0o644); err != nil {
		t.Fatal(err)
	}
	writePartValidator(partValidatorFor(part), partValidator{ETag: `"v1"`})

	if err := CleanupAllPartialsInDir(dir); err != nil {
		t.Fatalf("CleanupAllPartialsInDir: %v", err)
	}

	if got := listPartialArtifacts(t, dir); len(got) != 0 {
		t.Errorf("P2-5 复现：CleanupAllPartialsInDir（GUI 清缓存）之后仍残留 %v", got)
	}
}

// P2-5：**只有 `.meta` 存在**（`.part` 已被删）时也必须被清掉 —— 这是最常见的孤儿形态。
func TestP2_5_OrphanMetaAloneIsCleaned(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "archive.zip")
	part := partPathFor(dest, "https://example.test/a.zip")

	// 只写 .meta，不写 .part。
	writePartValidator(partValidatorFor(part), partValidator{ETag: `"orphan"`})
	if got := listPartialArtifacts(t, dir); len(got) == 0 {
		t.Fatal("前置条件：应已写入孤儿 .meta")
	}

	if err := CleanupAllPartialsInDir(dir); err != nil {
		t.Fatalf("CleanupAllPartialsInDir: %v", err)
	}
	if got := listPartialArtifacts(t, dir); len(got) != 0 {
		t.Errorf("P2-5 复现：孤儿 .meta 未被清理: %v", got)
	}

	// CleanupPartialFile 同样要覆盖该形态。
	writePartValidator(partValidatorFor(part), partValidator{ETag: `"orphan2"`})
	if err := CleanupPartialFile(dest); err != nil {
		t.Fatalf("CleanupPartialFile: %v", err)
	}
	if got := listPartialArtifacts(t, dir); len(got) != 0 {
		t.Errorf("P2-5 复现：CleanupPartialFile 未清理孤儿 .meta: %v", got)
	}
}

// P2-5：失败路径（新鲜下载收到 206 → 拒绝）也必须清掉 `.meta`，不得留下陈旧校验器。
//
// 孤儿 `.meta` 的实际危害：下一次同代下载若响应**不带**校验器，旧 `.meta` 不会被覆盖；
// 若那次下载中断，续传会带着**上一代内容的 ETag** 发 If-Range → 难以解释的
// "content changed" 报错。
//
// **判定口径（如实标注）**：本用例只断言 `.meta` 被清 —— **不**断言 `.part.lock` 也消失。
// 原因：失败路径返回时本次下载仍持有该锁（`defer partLock.Release()` 尚未执行），
// 此时删锁正是 Round 9 用 `removePartLockIfUnheld` 刻意避免的 TOCTOU。锁文件本身是
// 一个空的、未被持有的普通文件，`AcquireFileLock` 会复用它 —— 属**良性残留**，
// 由 Pause/Cancel/退出清理与两个清理 API 覆盖，不是本项要修的东西。
func TestP2_5_FailurePathRemovesMeta(t *testing.T) {
	body := bytes.Repeat([]byte("X"), 1024)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 新鲜请求（无 Range）却回 206 → 触发 P1-1(a) 拒绝路径。
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", len(body)-1, len(body)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "archive.zip")

	err := Download(context.Background(), Options{
		URL: srv.URL + "/archive.zip", Dest: dest, Client: srv.Client(),
		IsThirdParty: true, MaxRetries: 1,
	})
	if err == nil {
		t.Fatal("新鲜请求收到 206 应被拒绝")
	}

	// 只判定 .meta 与 .part。
	entries, rerr := os.ReadDir(dir)
	if rerr != nil {
		t.Fatal(rerr)
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".part.meta") {
			t.Errorf("P2-5 复现：失败路径留下陈旧校验器 %s（.meta 未随 .part 一起清）", name)
		}
		if strings.HasSuffix(name, ".part") {
			t.Errorf("P2-5 复现：失败路径留下 %s", name)
		}
	}
}

// P2-5：成功下载后不得残留任何 `.part*`（含 `.meta`）。
func TestP2_5_SuccessLeavesNoPartialArtifacts(t *testing.T) {
	body := bytes.Repeat([]byte("Y"), 2048)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "archive.zip")
	if err := Download(context.Background(), Options{
		URL: srv.URL + "/archive.zip", Dest: dest, Client: srv.Client(),
		IsThirdParty: true, MaxRetries: 1,
	}); err != nil {
		t.Fatalf("download: %v", err)
	}
	if got := listPartialArtifacts(t, dir); len(got) != 0 {
		t.Errorf("成功下载后仍有残留: %v", got)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Errorf("目标文件应存在: %v", err)
	}
}

// P2-6：**主 URL 失败、回退 URL 成功**这条出口必须清理失败候选的残留
// （含 `.meta`）。这是 `cleanupAfterSuccess` 抽取前就存在的行为，抽 helper 后不得回归。
func TestP2_6_FallbackSuccessCleansFailedCandidateResidue(t *testing.T) {
	body := bytes.Repeat([]byte("Z"), 2048)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "primary") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "archive.zip")
	primary := srv.URL + "/primary.zip"
	fallback := srv.URL + "/fallback.zip"

	// 预置主 URL 代的残留 + 校验器（模拟主 URL 中途断流留下的东西）。
	primaryPart := partPathFor(dest, primary)
	if err := os.WriteFile(primaryPart, []byte("stale-primary-prefix"), 0o644); err != nil {
		t.Fatal(err)
	}
	writePartValidator(partValidatorFor(primaryPart), partValidator{ETag: `"stale"`})

	if err := Download(context.Background(), Options{
		URL: primary, FallbackURLs: []string{fallback}, Dest: dest, Client: srv.Client(),
		IsThirdParty: true, MaxRetries: 1,
	}); err != nil {
		t.Fatalf("download via fallback: %v", err)
	}

	if got := listPartialArtifacts(t, dir); len(got) != 0 {
		t.Errorf("P2-6 复现：回退成功后仍残留失败候选的 %v", got)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Errorf("目标文件应存在: %v", err)
	}
}
