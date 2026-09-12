// discover_json_limit_test.go 守卫票 16（P2-15）：JSON 版本发现的响应体
// 必须限额解码（16 MiB），恶意/异常的超大 body 只能得到错误，不允许无界
// 缓冲（OOM）或挂死。
package remote

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/xmdemt/xdev/internal/manifest"
)

// TestDiscoverJSON_LimitsBody 用略超上限（17 MiB）的合法 JSON 片段驱动
// discoverJSON：解码必须在 16 MiB 处停止并返回错误，且调用有界返回。
func TestDiscoverJSON_LimitsBody(t *testing.T) {
	if jsonBodyLimit != 16<<20 {
		t.Errorf("jsonBodyLimit = %d, want %d (16 MiB)", jsonBodyLimit, 16<<20)
	}

	// 17 MiB 的字符串值：JSON 结构本身合法，但远超限额——限额生效时
	// 解码器在 16 MiB 处遇到 EOF（unexpected EOF）。
	pad := strings.Repeat("0", 17<<20)
	body := `{"versions":["` + pad + `"]}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	origClient := httpClient
	httpClient = server.Client()
	defer func() { httpClient = origClient }()

	done := make(chan error, 1)
	start := time.Now()
	go func() {
		_, err := discoverJSON(context.Background(), manifest.RemoteSource{
			Type: "json",
			URL:  server.URL,
			Path: "versions",
		})
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatalf("discoverJSON accepted a %d MiB body — the %d MiB limit is not enforced",
				len(body)>>20, jsonBodyLimit>>20)
		}
		if !strings.Contains(err.Error(), "decode JSON") {
			t.Errorf("error = %v, want a wrapped JSON decode error", err)
		}
		t.Logf("rejected %d MiB body in %s: %v", len(body)>>20, time.Since(start), err)
	case <-time.After(60 * time.Second):
		t.Fatalf("discoverJSON did not return within 60s for a %d MiB body — unbounded read/hang",
			len(body)>>20)
	}
}
