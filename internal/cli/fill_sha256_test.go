// fill_sha256_test.go — A 方案风险评估 R5/R6/R7 的守卫：
//   - 内容魔数校验（挡住"200 的错误页被当成资产哈希"）；
//   - 失败默认非零退出（CI 不能把部分填充当成功）；
//   - --report 机器可读结果；--tool 过滤（增量填充）。
//
// 全部用例只写 t.TempDir()，用 httptest 假服务器，不触网、不碰真实 home。
package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- 魔数校验 ---------------------------------------------------------------

func TestValidateArchiveFile_Magic(t *testing.T) {
	cases := []struct {
		name    string
		format  string
		content string
		wantOK  bool
	}{
		{"zip ok", "zip", "PK\x03\x04rest-of-archive", true},
		{"zip empty-archive ok", "zip", "PK\x05\x06rest", true},
		{"zip html rejected", "zip", "<!DOCTYPE html><html>login</html>", false},
		{"gzip ok", "tar.gz", "\x1f\x8b\x08\x00payload", true},
		{"gzip plain-text rejected", "tar.gz", "<html>Not Found</html>", false},
		{"xz ok", "tar.xz", "\xfd7zXZ\x00payload", true},
		{"xz rejected", "tar.xz", "PK\x03\x04", false},
		{"7z ok", "7z", "7z\xbc\xaf\x27\x1crest", true},
		{"7z rejected", "7z", "\x1f\x8b", false},
		{"exe (PE) ok", "exe", "MZ\x90\x00rest", true},
		{"exe (MSI/OLE) ok", "exe", "\xd0\xcf\x11\xe0rest", true},
		{"exe html rejected", "exe", "<html>download page</html>", false},
		{"sh ok", "sh", "#!/bin/sh\necho hi\n", true},
		{"sh rejected", "sh", "\x00\x01\x02", false},
		{"unknown format skipped", "weird", "\x00\x01", true},
		{"empty format skipped", "", "\x00\x01", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "sample")
			if err := os.WriteFile(p, []byte(tc.content), 0o644); err != nil {
				t.Fatal(err)
			}
			err := validateArchiveFile(p, tc.format)
			if tc.wantOK && err != nil {
				t.Errorf("validateArchiveFile(%q) = %v, want nil", tc.format, err)
			}
			if !tc.wantOK && err == nil {
				t.Errorf("validateArchiveFile(%q) = nil, want rejection", tc.format)
			}
		})
	}
}

func TestValidateArchiveFile_RejectsEmptyAndMissing(t *testing.T) {
	empty := filepath.Join(t.TempDir(), "empty")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateArchiveFile(empty, "zip"); err == nil {
		t.Error("empty download must be rejected")
	}
	if err := validateArchiveFile(filepath.Join(t.TempDir(), "nope"), "zip"); err == nil {
		t.Error("missing file must be rejected")
	}
}

// dmg 的 magic 在尾部（koly），必须能通过；非 DMG 必须被拒。
func TestValidateArchiveFile_DMGTailMagic(t *testing.T) {
	dir := t.TempDir()
	body := make([]byte, 600)
	copy(body[600-512:], "koly"+strings.Repeat("\x00", 508))
	p := filepath.Join(dir, "ok.dmg")
	if err := os.WriteFile(p, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateArchiveFile(p, "dmg"); err != nil {
		t.Errorf("koly tail must be accepted: %v", err)
	}

	bad := filepath.Join(dir, "bad.dmg")
	if err := os.WriteFile(bad, []byte(strings.Repeat("x", 600)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateArchiveFile(bad, "dmg"); err == nil {
		t.Error("non-DMG must be rejected")
	}
}

// --- 命令级行为 -------------------------------------------------------------

// writeFillManifest 造一个最小工具 manifest（带一个待填归档）。
func writeFillManifest(t *testing.T, dir, tool, format, url string) string {
	t.Helper()
	body := "name: " + tool + "\n" +
		"display_name: " + tool + "\n" +
		"category: build\n" +
		"versions:\n" +
		"  - version: \"1.0.0\"\n" +
		"    stable: true\n" +
		"    archives:\n" +
		"      - os: linux\n" +
		"        arch: x86_64\n" +
		"        format: " + format + "\n" +
		"        url: " + url + "\n" +
		"        sha256: \"\"\n"
	p := filepath.Join(dir, tool+".yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// runFill 执行 fill-sha256 命令（不经 root，避免任何 home/配置副作用）。
func runFill(args ...string) error {
	cmd := newFillSHA256Cmd()
	cmd.SetArgs(args)
	cmd.SetOut(&strings.Builder{})
	cmd.SetErr(&strings.Builder{})
	return cmd.Execute()
}

func TestFillSHA256Cmd_FillsValidAndRejectsErrorPages(t *testing.T) {
	zipBody := "PK\x03\x04" + strings.Repeat("z", 128)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/good.zip":
			w.WriteHeader(200)
			_, _ = w.Write([]byte(zipBody))
		case "/bad.zip":
			// 端点是 200，但内容是错误页 —— 修复前会被当成资产哈希并进入签名图。
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(200)
			_, _ = w.Write([]byte("<!DOCTYPE html><html><body>Sign in to download</body></html>"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	manifests := t.TempDir()
	good := writeFillManifest(t, manifests, "good", "zip", srv.URL+"/good.zip")
	bad := writeFillManifest(t, manifests, "bad", "zip", srv.URL+"/bad.zip")
	writeFillManifest(t, manifests, "missing", "zip", srv.URL+"/404.zip")

	cache := t.TempDir()
	report := filepath.Join(t.TempDir(), "fill-report.json")

	// 默认：有失败 → 必须返回错误（R5）
	err := runFill("--manifests-dir", manifests, "--cache-dir", cache, "--report", report, "--retries", "0")
	if err == nil {
		t.Fatal("fill-sha256 must fail when some archives cannot be filled (exit code is the CI gate)")
	}
	if !strings.Contains(err.Error(), "failed for") {
		t.Errorf("error = %v, want an aggregate failure message", err)
	}

	// 好条目：哈希已写入，且等于真实内容哈希
	data, rerr := os.ReadFile(good)
	if rerr != nil {
		t.Fatal(rerr)
	}
	sum := sha256.Sum256([]byte(zipBody))
	want := hex.EncodeToString(sum[:])
	if !strings.Contains(string(data), want) {
		t.Errorf("good.yaml 未写入正确哈希 %s:\n%s", want, string(data))
	}

	// 坏条目：绝不写入任何哈希（错误页不得成为信任锚）
	badData, _ := os.ReadFile(bad)
	if !strings.Contains(string(badData), `sha256: ""`) {
		t.Errorf("bad.yaml 必须保持空哈希（错误页内容被拒）:\n%s", string(badData))
	}

	// 报告：逐条可机读，失败项带原因
	raw, rerr := os.ReadFile(report)
	if rerr != nil {
		t.Fatalf("report not written: %v", rerr)
	}
	var parsed struct {
		Filled, Skipped, Failed int
		Entries                 []map[string]string
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("report is not valid JSON: %v", err)
	}
	if parsed.Filled != 1 || parsed.Failed != 2 {
		t.Errorf("report counts filled=%d failed=%d, want 1/2\n%s", parsed.Filled, parsed.Failed, raw)
	}
	var sawValidation bool
	for _, e := range parsed.Entries {
		if e["tool"] == "bad" && strings.Contains(e["error"], "validation failed") {
			sawValidation = true
		}
	}
	if !sawValidation {
		t.Errorf("report must record the content-validation failure for bad.zip:\n%s", raw)
	}

	// --allow-partial：显式接受缺口时返回 nil，但报告仍记录失败
	if err := runFill("--manifests-dir", manifests, "--cache-dir", cache, "--retries", "0", "--allow-partial"); err != nil {
		t.Fatalf("--allow-partial must not fail the command: %v", err)
	}
}

func TestFillSHA256Cmd_ToolFilter(t *testing.T) {
	body := "PK\x03\x04" + strings.Repeat("a", 64)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	manifests := t.TempDir()
	one := writeFillManifest(t, manifests, "one", "zip", srv.URL+"/one.zip")
	two := writeFillManifest(t, manifests, "two", "zip", srv.URL+"/two.zip")

	if err := runFill("--manifests-dir", manifests, "--cache-dir", t.TempDir(), "--tool", "one"); err != nil {
		t.Fatalf("filtered fill failed: %v", err)
	}
	oneData, _ := os.ReadFile(one)
	twoData, _ := os.ReadFile(two)
	if strings.Contains(string(oneData), `sha256: ""`) {
		t.Errorf("--tool one 应填充 one.yaml:\n%s", string(oneData))
	}
	if !strings.Contains(string(twoData), `sha256: ""`) {
		t.Errorf("--tool one 不应改动 two.yaml:\n%s", string(twoData))
	}
}

func TestFillSHA256Cmd_NoCacheIgnoresStaleCache(t *testing.T) {
	good := "PK\x03\x04" + strings.Repeat("g", 32)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(good))
	}))
	defer srv.Close()

	manifests := t.TempDir()
	path := writeFillManifest(t, manifests, "cachetest", "zip", srv.URL+"/x.zip")
	cache := t.TempDir()
	// 预置一个"损坏的缓存文件"（非 zip）：无 --no-cache 时会被校验拒掉并重下。
	stale := filepath.Join(cache, "cachetest-1.0.0-linux-x86_64.zip")
	if err := os.WriteFile(stale, []byte("<html>stale</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runFill("--manifests-dir", manifests, "--cache-dir", cache, "--retries", "0"); err != nil {
		t.Fatalf("stale cache must be re-downloaded, not fatal: %v", err)
	}
	data, _ := os.ReadFile(path)
	sum := sha256.Sum256([]byte(good))
	if !strings.Contains(string(data), hex.EncodeToString(sum[:])) {
		t.Errorf("stale cache 被复用（应重新下载）:\n%s", string(data))
	}
}
