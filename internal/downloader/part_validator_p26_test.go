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

// 第九轮审计 P2-6 守卫：同一 URL 内容变更后，旧残留不得被当作续传前缀拼接。
//
// 缺陷现场：F-12 让 `.part` 按 URL 分代，「不同 URL 互不干扰」成立；但**同一个
// URL** 重新发布内容（nightly / 重打 tag / CDN 回源换了对象）时代际键不变，
// 旧内容的前缀会与**新内容**的后缀拼成一份文件。F-12b 只校验偏移（起点对得上），
// `fromBytes` 那段前缀本身没有任何一致性证据。
//
// 审计实测 H16：本地残留 = 旧内容前 2048 字节，服务端同一 URL 现返回新内容
// （同长度）→ err=nil、拼接结果被接受并写 pin。
//
// 修复后：下载开始时把响应校验器（ETag / Last-Modified）记进 `<part>.meta`，
// 续传时带上 If-Range，并在收到 206 后**本地再比对**一次校验器；不一致就删
// `.part` + `.meta` 并 fail closed。

// 场景 A：服务端合规（遵守 If-Range）→ 内容变了就回 200 全量，客户端重下，
// 最终得到**完整的新内容**（不是拼接）。
func TestP2_6_ContentChange_ServerHonorsIfRange(t *testing.T) {
	oldBody := bytes.Repeat([]byte("O"), 4096)
	newBody := bytes.Repeat([]byte("N"), 4096)

	var etag string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", etag)
		if r.Header.Get("Range") != "" {
			// 合规实现：If-Range 不匹配 → 忽略 Range，回 200 全量。
			if ir := r.Header.Get("If-Range"); ir != "" && ir != etag {
				w.Header().Set("Content-Length", fmt.Sprint(len(newBody)))
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(newBody)
				return
			}
			// If-Range 匹配（或无 If-Range）→ 正常 206。
			start := int64(2048)
			part := newBody[start:]
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(newBody)-1, len(newBody)))
			w.Header().Set("Content-Length", fmt.Sprint(len(part)))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(part)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(newBody)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(newBody)
	}))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "archive.zip")

	// 1) 先以「旧内容」建立一代残留 + 校验器。
	etag = `"v1"`
	if err := Download(context.Background(), Options{
		URL: srv.URL + "/archive.zip", Dest: dest, Client: srv.Client(),
		IsThirdParty: true, MaxRetries: 1,
	}); err != nil {
		t.Fatalf("seed download: %v", err)
	}
	_ = os.Remove(dest)
	// 直接构造该代的 .part 前 2048 字节（旧内容）+ 对应校验器。
	urls := []string{srv.URL + "/archive.zip"}
	part := partPathsFor(dest, urls...)[0]
	if err := os.WriteFile(part, oldBody[:2048], 0o644); err != nil {
		t.Fatal(err)
	}
	writePartValidator(partValidatorFor(part), partValidator{ETag: `"v1"`})

	// 2) 内容换成新内容（同长度），ETag 变化。
	etag = `"v2"`
	if err := Download(context.Background(), Options{
		URL: srv.URL + "/archive.zip", Dest: dest, Client: srv.Client(),
		IsThirdParty: true, MaxRetries: 1,
	}); err != nil {
		t.Fatalf("download after content change: %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, newBody) {
		if bytes.Contains(got[:2048], []byte("O")) {
			t.Errorf("P2-6 复现：旧内容前缀被拼到新内容上（spliced: 前 2048 字节仍是旧内容）")
		} else {
			t.Errorf("下载结果不是完整的新内容: len=%d", len(got))
		}
	}
}

// 场景 B：服务端**无视 If-Range**（非合规）→ 仍然回 206。
// 本地校验必须兜住：校验器不一致 → 删 .part 并报错（fail closed），
// 下一次从零重下拿到完整新内容。
func TestP2_6_ContentChange_ServerIgnoresIfRange_LocalCheckFailsClosed(t *testing.T) {
	oldBody := bytes.Repeat([]byte("O"), 4096)
	newBody := bytes.Repeat([]byte("N"), 4096)

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 始终声明新 ETag，且**永远**按 Range 回 206（忽略 If-Range）。
		w.Header().Set("ETag", `"v2"`)
		if r.Header.Get("Range") != "" {
			start := int64(2048)
			part := newBody[start:]
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(newBody)-1, len(newBody)))
			w.Header().Set("Content-Length", fmt.Sprint(len(part)))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(part)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(newBody)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(newBody)
	}))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "archive.zip")
	urls := []string{srv.URL + "/archive.zip"}
	part := partPathsFor(dest, urls...)[0]

	// 残留：旧内容前 2048 字节；校验器记录的是旧 ETag。
	if err := os.WriteFile(part, oldBody[:2048], 0o644); err != nil {
		t.Fatal(err)
	}
	writePartValidator(partValidatorFor(part), partValidator{ETag: `"v1"`})

	err := Download(context.Background(), Options{
		URL: urls[0], Dest: dest, Client: srv.Client(),
		IsThirdParty: true, MaxRetries: 1,
	})
	if err == nil {
		// 若重试策略让它自愈（删 part 后从零重下成功），结果必须是完整新内容。
		got, rerr := os.ReadFile(dest)
		if rerr != nil {
			t.Fatalf("err=nil 但目标不存在: %v", rerr)
		}
		if !bytes.Equal(got, newBody) {
			t.Errorf("P2-6 复现：err=nil 且落盘内容不是完整新内容（拼接被接受）: len=%d", len(got))
		}
		return
	}
	// fail closed 路径：错误信息必须可诊断，且 .part 已被删除（下次能重来）。
	if !strings.Contains(err.Error(), "changed") && !strings.Contains(err.Error(), "ETag") {
		t.Errorf("错误信息应说明内容已变，got: %v", err)
	}
	if _, statErr := os.Stat(part); statErr == nil {
		t.Errorf("fail closed 后 .part 应被删除（否则下次仍会用旧前缀），但它仍存在")
	}
}

// 场景 C：校验器一致时续传**仍必须工作**（修复不得把正常续传弄红）。
func TestP2_6_UnchangedContent_ResumeStillWorks(t *testing.T) {
	body := bytes.Repeat([]byte("A"), 4096)

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"stable"`)
		if r.Header.Get("Range") != "" {
			start := int64(2048)
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
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "archive.zip")
	urls := []string{srv.URL + "/archive.zip"}
	part := partPathsFor(dest, urls...)[0]
	if err := os.WriteFile(part, body[:2048], 0o644); err != nil {
		t.Fatal(err)
	}
	writePartValidator(partValidatorFor(part), partValidator{ETag: `"stable"`})

	if err := Download(context.Background(), Options{
		URL: urls[0], Dest: dest, Client: srv.Client(),
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

// 单元：校验器比较矩阵。
func TestPartValidatorMatching(t *testing.T) {
	mk := func(etag, lm string) *http.Response {
		h := http.Header{}
		if etag != "" {
			h.Set("ETag", etag)
		}
		if lm != "" {
			h.Set("Last-Modified", lm)
		}
		return &http.Response{Header: h}
	}

	cases := []struct {
		name string
		v    partValidator
		resp *http.Response
		want bool
	}{
		{"etag equal", partValidator{ETag: `"a"`}, mk(`"a"`, ""), true},
		{"etag differs", partValidator{ETag: `"a"`}, mk(`"b"`, ""), false},
		{"etag missing in response", partValidator{ETag: `"a"`}, mk("", ""), true},
		{"no stored etag", partValidator{LastModified: "x"}, mk(`"b"`, ""), true},
		{"last-modified equal", partValidator{LastModified: "d1"}, mk("", "d1"), true},
		{"last-modified differs", partValidator{LastModified: "d1"}, mk("", "d2"), false},
		{"both differ", partValidator{ETag: `"a"`, LastModified: "d1"}, mk(`"b"`, "d2"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.v.matchesResponse(tc.resp); got != tc.want {
				t.Errorf("matchesResponse() = %v, want %v", got, tc.want)
			}
		})
	}
}

// 单元：ifRange 优先强 ETag，弱 ETag 退回 Last-Modified。
func TestPartValidatorIfRange(t *testing.T) {
	if got := (partValidator{ETag: `"strong"`, LastModified: "d"}).ifRange(); got != `"strong"` {
		t.Errorf("强 ETag 应优先, got %q", got)
	}
	if got := (partValidator{ETag: `W/"weak"`, LastModified: "d"}).ifRange(); got != "d" {
		t.Errorf("弱 ETag 不能用于 If-Range，应退回 Last-Modified, got %q", got)
	}
	if got := (partValidator{}).ifRange(); got != "" {
		t.Errorf("空校验器应返回空串, got %q", got)
	}
}

// 校验器必须随 .part 一起被清理（不得留孤儿）。
func TestP2_6_ValidatorCleanedUpWithPart(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "archive.zip")
	part := partPathsFor(dest, "https://example.test/a.zip")[0]
	meta := partValidatorFor(part)

	if err := os.WriteFile(part, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	writePartValidator(meta, partValidator{ETag: `"v1"`})
	if _, err := os.Stat(meta); err != nil {
		t.Fatalf("校验器应已写入: %v", err)
	}

	if err := removePartialFiles(dest, "https://example.test/a.zip"); err != nil {
		t.Fatalf("removePartialFiles: %v", err)
	}
	if _, err := os.Stat(part); !os.IsNotExist(err) {
		t.Errorf(".part 应被删除")
	}
	if _, err := os.Stat(meta); !os.IsNotExist(err) {
		t.Errorf(".part.meta 应随 .part 一起删除（否则孤儿累积）")
	}
}
