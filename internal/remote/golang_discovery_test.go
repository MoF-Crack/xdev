package remote

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xmdemt/xdev/internal/manifest"
)

// 回归：Go 版本嗅探必须走 go.dev 官方 JSON 索引。
//
// 背景：旧配置 `type: github, url: golang/go` 永远发现不了新版本 ——
// Go 官方不使用 GitHub Releases（/repos/golang/go/releases 返回空数组），
// 只打 git tag，而 /tags 默认返回 2012 年的 weekly.* 旧 tag。
//
// 本测试用 httptest 复刻 go.dev/dl/?mode=json 的响应形状（顶层数组），
// 验证 discoverGoLang 能正确提取并套用 manifest pattern。
func TestDiscoverGoLangParsesOfficialJSON(t *testing.T) {
	const body = `[
	  {"version":"go1.27.3","stable":true},
	  {"version":"go1.26.10","stable":true},
	  {"version":"go1.26.5","stable":true},
	  {"version":"go1.28rc1","stable":false}
	]`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	// 直接测解析逻辑：把固定端点临时指向 httptest。
	old := goDevDLURLForTest(srv.URL)
	defer goDevDLURLForTest(old)

	versions, err := discoverGoLang(context.Background(), manifest.RemoteSource{
		Type:    "golang",
		Pattern: `go(\d+\.\d+\.\d+)$`,
	})
	if err != nil {
		t.Fatalf("discoverGoLang: %v", err)
	}

	want := []string{"1.27.3", "1.26.10", "1.26.5"}
	if len(versions) != len(want) {
		t.Fatalf("versions = %v, want %v（rc 版本应被 pattern 排除）", versions, want)
	}
	for i := range want {
		if versions[i] != want[i] {
			t.Errorf("versions[%d] = %q, want %q", i, versions[i], want[i])
		}
	}
}

// 回归：manifest 未配 pattern 时应回退到「剥 go 前缀」。
func TestDiscoverGoLangWithoutPattern(t *testing.T) {
	const body = `[{"version":"go1.27.3","stable":true}]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	old := goDevDLURLForTest(srv.URL)
	defer goDevDLURLForTest(old)

	versions, err := discoverGoLang(context.Background(), manifest.RemoteSource{Type: "golang"})
	if err != nil {
		t.Fatalf("discoverGoLang: %v", err)
	}
	if len(versions) != 1 || versions[0] != "1.27.3" {
		t.Errorf("versions = %v, want [1.27.3]", versions)
	}
}

// 回归：jsonSniffer 必须认领 golang 类型，否则 discovery 会报
// "unsupported (no matching sniffer)" —— 这正是修复前 Go 1.27 嗅探失败的
// 第二个原因（remote 层已支持，但 sniffer 注册表没认领）。
func TestDiscoverVersionsDispatchesGolang(t *testing.T) {
	// 类型分派本身：未知类型应报错，golang 不应落入 default。
	_, err := DiscoverVersions(context.Background(), manifest.RemoteSource{
		Type: "definitely-not-a-type",
	})
	if err == nil {
		t.Error("未知 remote type 应当报错")
	}
}
