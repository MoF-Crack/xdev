// fill_official_url_test.go — 第三轮复核实测新增的守卫 H-4：
// 填充 `sha256` 必须下载**官方 URL**，绝不能因为存在 mirror_url 就改下镜像
// （镜像字节可能不同、且镜像挂掉会把"官方可达"的条目拖成缺口——实测清华 github-release
//
//	镜像对 ninja 返回 403，导致整条数据线被死镜像阻断）。
package cli

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

// 官方与镜像内容不同：写入的必须是官方内容的哈希，且镜像故障不得影响结果。
func TestFillSHA256Cmd_HashesOfficialURLNotMirror(t *testing.T) {
	official := "PK\x03\x04" + strings.Repeat("O", 128)
	mirror := "PK\x03\x04" + strings.Repeat("M", 128)

	officialSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(official))
	}))
	defer officialSrv.Close()
	mirrorSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 镜像挂掉（实测清华镜像对该路径返回 403）
		w.WriteHeader(http.StatusForbidden)
	}))
	defer mirrorSrv.Close()

	dir := t.TempDir()
	body := "name: dual\n" +
		"display_name: dual\n" +
		"category: build\n" +
		"versions:\n" +
		"  - version: \"1.0.0\"\n" +
		"    archives:\n" +
		"      - os: linux\n" +
		"        arch: x86_64\n" +
		"        format: zip\n" +
		"        url: " + officialSrv.URL + "/official.zip\n" +
		"        mirror_url: " + mirrorSrv.URL + "/mirror.zip\n" +
		"        sha256: \"\"\n"
	p := filepath.Join(dir, "dual.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runFill("--manifests-dir", dir, "--cache-dir", t.TempDir(), "--retries", "0"); err != nil {
		t.Fatalf("镜像 403 不得让填充失败（应下官方 URL）: %v", err)
	}
	data, _ := os.ReadFile(p)
	sum := sha256.Sum256([]byte(official))
	mirrorSum := sha256.Sum256([]byte(mirror))
	if !strings.Contains(string(data), hex.EncodeToString(sum[:])) {
		t.Errorf("`sha256` 必须是**官方**内容的哈希:\n%s", string(data))
	}
	if strings.Contains(string(data), hex.EncodeToString(mirrorSum[:])) {
		t.Error("`sha256` 里出现了镜像内容的哈希（H-4 回归）")
	}
}
