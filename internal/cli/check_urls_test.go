// check_urls_test.go — `xdev manifest check-urls` 的行为守卫（A 方案风险 R4/R9 的检测面）。
//
// 用 httptest 假服务器，不触网；只写 t.TempDir()。
package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeURLManifest(t *testing.T, dir, tool, rawURL string) {
	t.Helper()
	body := "name: " + tool + "\n" +
		"display_name: " + tool + "\n" +
		"category: build\n" +
		"versions:\n" +
		"  - version: \"1.0.0\"\n" +
		"    archives:\n" +
		"      - os: linux\n" +
		"        arch: x86_64\n" +
		"        format: zip\n" +
		"        url: " + rawURL + "\n" +
		"        sha256: \"\"\n"
	if err := os.WriteFile(filepath.Join(dir, tool+".yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestManifestCheckURLs_DetectsDeadLinksAndAllowlist(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ok.zip" {
			w.WriteHeader(200)
			_, _ = w.Write([]byte("PK\x03\x04"))
			return
		}
		http.NotFound(w, r) // 上游改名 → 404
	}))
	defer srv.Close()

	dir := t.TempDir()
	writeURLManifest(t, dir, "alive", srv.URL+"/ok.zip")
	writeURLManifest(t, dir, "renamed", srv.URL+"/dead.zip") // 上游改名 → 404

	report := filepath.Join(t.TempDir(), "urls.json")

	// 有死链 → 非零退出（CI 门禁）
	cmd := newManifestCheckURLsCmd()
	cmd.SetArgs([]string{"--manifests-dir", dir, "--json", report, "--quiet"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("check-urls must fail when an archive URL is dead")
	}
	if !strings.Contains(err.Error(), "dead") {
		t.Errorf("error = %v, want a dead-URL message", err)
	}

	raw, rerr := os.ReadFile(report)
	if rerr != nil {
		t.Fatalf("json report not written: %v", rerr)
	}
	var parsed struct {
		Total, OK, Dead, Error, Allowlisted int
		Results                             []urlProbeResult
	}
	if jerr := json.Unmarshal(raw, &parsed); jerr != nil {
		t.Fatalf("report is not valid JSON: %v", jerr)
	}
	if parsed.OK != 1 || parsed.Dead != 1 {
		t.Errorf("report ok=%d dead=%d, want 1/1\n%s", parsed.OK, parsed.Dead, raw)
	}
	if len(parsed.Results) != 2 {
		t.Errorf("results len = %d, want 2", len(parsed.Results))
	}

	// 白名单豁免：死链被承认后命令通过（并且报告标为 allowlisted）
	allow := filepath.Join(t.TempDir(), "allow.txt")
	if werr := os.WriteFile(allow, []byte("# known: upstream renamed, tracked in ticket 17\n"+srv.URL+"/dead.zip\n"), 0o644); werr != nil {
		t.Fatal(werr)
	}
	cmd2 := newManifestCheckURLsCmd()
	cmd2.SetArgs([]string{"--manifests-dir", dir, "--allow-file", allow, "--quiet", "--json", report})
	if err2 := cmd2.Execute(); err2 != nil {
		t.Fatalf("allowlisted dead link must not fail the command: %v", err2)
	}
	raw2, _ := os.ReadFile(report)
	var parsed2 struct {
		Dead, Allowlisted int
	}
	if jerr := json.Unmarshal(raw2, &parsed2); jerr != nil {
		t.Fatal(jerr)
	}
	if parsed2.Dead != 0 || parsed2.Allowlisted != 1 {
		t.Errorf("after allowlist: dead=%d allowlisted=%d, want 0/1", parsed2.Dead, parsed2.Allowlisted)
	}
}

// 网络不可达默认只报告、不失败（本机被墙的主机不应让 CI 变红），
// 但 --fail-on-network-error 时应失败。
func TestManifestCheckURLs_NetworkErrorSemantics(t *testing.T) {
	dir := t.TempDir()
	// 127.0.0.1:1 上必然没有监听 → 连接被拒
	writeURLManifest(t, dir, "unreachable", "http://127.0.0.1:1/x.zip")

	cmd := newManifestCheckURLsCmd()
	cmd.SetArgs([]string{"--manifests-dir", dir, "--quiet", "--timeout", "3s"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("network errors must not fail by default: %v", err)
	}

	cmd2 := newManifestCheckURLsCmd()
	cmd2.SetArgs([]string{"--manifests-dir", dir, "--quiet", "--timeout", "3s", "--fail-on-network-error"})
	if err := cmd2.Execute(); err == nil {
		t.Fatal("--fail-on-network-error must fail on unreachable URLs")
	}
}

// H-2（本批复核新增）：非 http(s) scheme 一律判 error，绝不能让 http.Client
// 去碰 file:// / ftp:// 等协议（manifest URL 可被第三方改写，不能默认它安全）。
func TestManifestCheckURLs_RejectsNonHTTPScheme(t *testing.T) {
	dir := t.TempDir()
	writeURLManifest(t, dir, "fileurl", "file:///etc/passwd")
	writeURLManifest(t, dir, "ftpurl", "ftp://example.com/x.zip")

	report := filepath.Join(t.TempDir(), "schemes.json")
	cmd := newManifestCheckURLsCmd()
	cmd.SetArgs([]string{"--manifests-dir", dir, "--quiet", "--json", report})
	// 非 http(s) 是 error 不是 dead → 默认退出 0（错误是"我方/数据问题"，由报告暴露）
	if err := cmd.Execute(); err != nil {
		t.Fatalf("scheme 错误默认不失败（应由报告与 --fail-on-network-error 控制）: %v", err)
	}
	raw, err := os.ReadFile(report)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Error   int `json:"Error"`
		Results []urlProbeResult
	}
	if jerr := json.Unmarshal(raw, &parsed); jerr != nil {
		t.Fatal(jerr)
	}
	if parsed.Error != 2 {
		t.Fatalf("error 计数 = %d, want 2（file:// 与 ftp:// 都应判 error）", parsed.Error)
	}
	for _, r := range parsed.Results {
		if !strings.Contains(r.Error, "unsupported URL scheme") {
			t.Errorf("URL %s 的错误 = %q, want scheme 拒绝说明", r.URL, r.Error)
		}
	}
}
