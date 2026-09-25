package cli

// 第六轮审计（fill/sign 数据线）守卫测试：
//
//	F3  fill 初始 URL 必须过产品统一 SSRF/scheme 校验（http 远程、私网保留地址拒绝）；
//	F6  缓存命中必须由 <dest>.url sidecar 证明「字节来自这个 URL」，URL 变更视为 miss；
//	F7  manifest 供给的 name/version/os/arch/format 不得逃逸缓存目录；
//	F10 写回失败时，本工具已登记的 filled 条目必须降级为 failed（报告不撒谎）。

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// F3：非 localhost 的 http URL 必须在发请求前被拒绝（H-1 降级防御的前置缺口）。
func TestFillDownloadAndHashOnce_RejectsPlaintextHTTPRemote(t *testing.T) {
	// httptest 默认服务器监听 127.0.0.1——本测试要求「远程 http」被拒，因此不真的
	// 起服务器：校验发生在任何网络 IO 之前，unreachable 端口即可。
	_, err := downloadAndHashOnce("http://example.invalid/x.zip", filepath.Join(t.TempDir(), "x.zip"), nil, "zip")
	if err == nil {
		t.Fatal("plaintext http to a remote host must be rejected before any request is made")
	}
	if !strings.Contains(err.Error(), "not safe") || !strings.Contains(err.Error(), "https") {
		t.Errorf("rejection should cite the product-standard safety policy, got: %v", err)
	}
}

// F3：私网/保留地址（CI runner 的元数据端点即此类）必须被拒绝。
func TestFillDownloadAndHashOnce_RejectsLinkLocalMetadataEndpoint(t *testing.T) {
	_, err := downloadAndHashOnce("http://169.254.169.254/latest/meta-data/", filepath.Join(t.TempDir(), "x"), nil, "")
	if err == nil {
		t.Fatal("link-local metadata endpoint must be rejected (SSRF)")
	}
	if !strings.Contains(err.Error(), "not safe") {
		t.Errorf("expected SSRF rejection, got: %v", err)
	}
}

// F3 正例：localhost 的 http 测试起点必须保留（产品统一语义，本地服务/测试夹具）。
func TestFillDownloadAndHashOnce_AllowsLocalhostFixture(t *testing.T) {
	payload := "PK\x03\x04" + strings.Repeat("l", 16)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "fixture.zip")
	if _, err := downloadAndHashOnce(srv.URL+"/a.zip", dest, nil, "zip"); err != nil {
		t.Fatalf("localhost http fixture must keep working: %v", err)
	}
	// sidecar 已写（F6 正例）。
	sidecar, err := os.ReadFile(dest + ".url")
	if err != nil || strings.TrimSpace(string(sidecar)) != srv.URL+"/a.zip" {
		t.Errorf("url sidecar missing/mismatched: err=%v content=%q", err, string(sidecar))
	}
}

// F6：缓存字节完好且魔数正确，但来源 URL 不同 → 必须重新下载，不得复用旧字节。
func TestFillDownloadAndHash_CacheHitRequiresURLMatch(t *testing.T) {
	payloadV1 := "PK\x03\x04" + strings.Repeat("one", 16)
	payloadV2 := "PK\x03\x04" + strings.Repeat("two", 16)

	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte(payloadV2))
	}))
	defer srv.Close()

	cache := t.TempDir()
	dest := filepath.Join(cache, "tool-1.0.0-linux-x86_64.zip")

	// 第一轮：预置「来自 URL-A 的旧缓存」（魔数正确 + sidecar 指向 URL-A）——
	// 直接落盘而不打服务器（hits 从 0 开始），模拟上游 yank 重传前的旧字节。
	if err := os.WriteFile(dest, []byte(payloadV1), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest+".url", []byte(srv.URL+"/a.zip"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 第二轮：同一 (tool,version,os,arch,format) 但 URL-B。
	// 旧实现无条件复用魔数正确的旧字节——同元组换 URL（上游 yank 重传/拼写修正）
	// 时把旧字节哈希进要签名的 manifest。F6 后 sidecar 不匹配 → 重新下载。
	hash, err := downloadAndHash(srv.URL+"/b.zip", dest, nil, "zip", false, 0)
	if err != nil {
		t.Fatalf("changed URL must re-download, not fail: %v", err)
	}
	if hits != 1 {
		t.Errorf("changed URL must bypass the cache (server hits = %d, want 1)", hits)
	}
	want := sha256HexForTest(t, []byte(payloadV2))
	if hash != want {
		t.Errorf("hash = %s, want hash of the NEW payload %s", hash, want)
	}
} // F7：manifest 供给的分量含路径分隔符/".." 时必须在下载前被拒绝。
func TestFillCacheFilename_RejectsPathEscapeComponents(t *testing.T) {
	bad := [][5]string{
		{"../evil", "1.0.0", "linux", "x86_64", "zip"},
		{"tool", "1.0.0", "../../..", "x86_64", "zip"},
		{"tool", "..%2F..%2F", "linux", "x86_64", "zip"}, // 变体：拼接处仍是 ".."
		{"tool", "1.0.0", "linux", "x86_64", "../../evil.zip"},
		{"tool", "1.0.0", "linux", "x86_64", ""},
		{"", "1.0.0", "linux", "x86_64", "zip"},
		{"t:ool", "1.0.0", "linux", "x86_64", "zip"}, // 盘符冒号
		{".", "1.0.0", "linux", "x86_64", "zip"},
		{"tool", "..", "linux", "x86_64", "zip"},
	}
	for _, c := range bad {
		if _, err := fillCacheFilename(c[0], c[1], c[2], c[3], c[4]); err == nil {
			t.Errorf("fillCacheFilename(%q) must be rejected", c)
		}
	}
	if _, err := fillCacheFilename("tool", "1.0.0", "linux", "x86_64", "tar.gz"); err != nil {
		t.Errorf("legitimate components must pass: %v", err)
	}
	// Join 后的包含性断言。
	if err := ensureUnderDir(filepath.Join("cache", "a-b.zip"), "cache"); err != nil {
		t.Errorf("in-dir path must pass containment: %v", err)
	}
}

// F7：validateManifestPathName 拒绝逃逸 manifests 目录的工具名。
func TestValidateManifestPathName_RejectsEscape(t *testing.T) {
	for _, name := range []string{"", "..", "../x", "a/b", `a\b`, "a:b", "a..b"} {
		if err := validateManifestPathName(name); err == nil {
			t.Errorf("validateManifestPathName(%q) must be rejected", name)
		}
	}
	for _, name := range []string{"go", "node", "mingw-w64", "d.sh", "jdk-17"} {
		if err := validateManifestPathName(name); err != nil {
			t.Errorf("legitimate tool name %q must pass: %v", name, err)
		}
	}
}

// F10：写回失败时，本工具的 filled 条目必须降级为 failed，报告不得宣称未落盘的哈希成功。
func TestFillDowngradeFilledEntries(t *testing.T) {
	entries := []fillEntry{
		{Tool: "a", Version: "1", Status: "filled", Hash: "h1"},
		{Tool: "a", Version: "2", Status: "skipped", Hash: "h2"},
		{Tool: "b", Version: "1", Status: "filled", Hash: "h3"},
	}
	downgradeFilledEntries(entries, "write manifest: boom")
	if entries[0].Status != "failed" || entries[0].Error != "write manifest: boom" {
		t.Errorf("a@1 must be downgraded with the reason, got %+v", entries[0])
	}
	if entries[1].Status != "skipped" {
		t.Errorf("skipped entries must be left alone, got %+v", entries[1])
	}
	if entries[2].Status != "failed" {
		t.Errorf("a@2... b@1 filled entry must be downgraded, got %+v", entries[2])
	}
	if entries[0].Hash != "h1" {
		t.Errorf("downgrade should keep the computed hash for forensics, got %+v", entries[0])
	}
}

func sha256HexForTest(t *testing.T, b []byte) string {
	t.Helper()
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
