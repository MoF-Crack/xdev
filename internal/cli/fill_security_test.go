// fill_security_test.go — 本批复核（回归+安全第二轮）新增的守卫：
//
//	H-1 fill-sha256 的下载必须带 SecureRedirectCheck（拒绝 HTTPS→HTTP 降级）——
//	   这个下载产出的是"要被签名的哈希"，安全基线必须与 downloader/update 一致；
//	H-3 writeManifestYAML 必须原子写（崩溃不留半截 manifest，对齐票 23 的承诺）。
package cli

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/xmdemt/xdev/internal/manifest"
)

func yamlUnmarshalForTest(data []byte, out *manifest.Tool) error {
	return yaml.Unmarshal(data, out)
}

// H-1：https → http 降级必须被拒（之前是裸 http.Client，默认跟跳）。
// 通过 fillHTTPTransportHook 注入信任 httptest 自签证书的 Transport ——
// 重定向策略本身仍然走生产代码路径（这正是本用例要验证的对象）。
func TestDownloadAndHash_RejectsHTTPSDowngrade(t *testing.T) {
	// 目标服务器：http 明文端点（扮演降级后的跳转目的地）
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("PK\x03\x04" + strings.Repeat("x", 64)))
	}))
	defer plain.Close()

	// 入口：https 服务器 302 跳到 http 明文端点
	redirectSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL+"/evil.zip", http.StatusFound)
	}))
	defer redirectSrv.Close()

	old := fillHTTPTransportHook
	fillHTTPTransportHook = func() http.RoundTripper { return redirectSrv.Client().Transport }
	t.Cleanup(func() { fillHTTPTransportHook = old })

	dest := filepath.Join(t.TempDir(), "downgrade.zip")
	_, err := downloadAndHash(redirectSrv.URL+"/a.zip", dest, nil, "zip", true, 0)
	if err == nil {
		t.Fatal("HTTPS→HTTP 降级必须被拒绝（该函数产出要被签名的哈希，是信任锚）")
	}
	if !strings.Contains(err.Error(), "downgrade") && !strings.Contains(err.Error(), "insecure redirect") {
		t.Errorf("error = %v, want the downgrade rejection message", err)
	}
	// 降级被拒时不得留下产物（半截下载即等于可被误用的输入）。
	if _, statErr := os.Stat(dest); statErr == nil {
		t.Error("被拒的下载不得在缓存目录留下文件")
	}
}

// H-1 辅证：正常的 https 直连仍然工作（安全收紧不能把正常路径收死）。
func TestDownloadAndHash_PlainHTTPSStillWorks(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("PK\x03\x04" + strings.Repeat("y", 64)))
	}))
	defer srv.Close()

	old := fillHTTPTransportHook
	fillHTTPTransportHook = func() http.RoundTripper { return srv.Client().Transport }
	t.Cleanup(func() { fillHTTPTransportHook = old })

	dest := filepath.Join(t.TempDir(), "ok.zip")
	hash, err := downloadAndHash(srv.URL+"/ok.zip", dest, nil, "zip", true, 0)
	if err != nil {
		t.Fatalf("正常 https 下载不应被安全策略误伤: %v", err)
	}
	if len(hash) != 64 {
		t.Errorf("hash len = %d, want 64", len(hash))
	}
}

// H-3：writeManifestYAML 必须原子（落盘后内容完整；实现细节上不得再用
// 固定 path+".tmp" 之外的非原子路径——此处以行为验证：写入成功且文件可再次解析）。
func TestWriteManifestYAML_RoundTripAndAtomicPath(t *testing.T) {
	dir := t.TempDir()
	tool := &manifest.Tool{Name: "atomic-check", DisplayName: "Atomic Check", Category: "build"}
	tool.VersionList = []manifest.Version{{
		Version: "1.0.0",
		Archives: []manifest.Archive{{
			OS: "linux", Arch: "x86_64", Format: "zip",
			URL: "https://example.com/a.zip", SHA256: strings.Repeat("a", 64),
		}},
	}}
	p := filepath.Join(dir, "atomic-check.yaml")
	if err := writeManifestYAML(p, tool); err != nil {
		t.Fatalf("writeManifestYAML: %v", err)
	}
	// 写完后不得残留 .tmp 中间文件（原子写的可观察副作用之一）。
	if _, err := os.Stat(p + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("残留临时文件 %s.tmp（原子写应当已 rename 走）", p)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var back manifest.Tool
	if err := yamlUnmarshalForTest(data, &back); err != nil {
		t.Fatalf("写出的 YAML 必须可再次解析（原子性验证的最低要求）: %v", err)
	}
	if back.Name != "atomic-check" || len(back.VersionList) != 1 {
		t.Errorf("round-trip 内容缺失: %+v", back)
	}
}
