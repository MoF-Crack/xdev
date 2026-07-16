package mirror

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestApplyNoMirror(t *testing.T) {
	tbl := &Table{m: map[string][]string{}}
	if got := tbl.Apply("python", "https://example.com/foo"); got != "https://example.com/foo" {
		t.Errorf("expected no rewrite, got %s", got)
	}
}

func TestApplyNodeMirror(t *testing.T) {
	tbl := &Table{m: map[string][]string{
		"node": {"https://npmmirror.com/mirrors/node"},
	}}
	got := tbl.Apply("node", "https://nodejs.org/dist/v20.10.0/node-v20.10.0.tar.xz")
	// We replace the entire upstream path with the mirror's path
	// plus the leaf below the first upstream component.  The npm
	// mirror exposes the same layout one level below "/mirrors/node".
	want := "https://npmmirror.com/mirrors/node/v20.10.0/node-v20.10.0.tar.xz"
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestApplyPythonMirror(t *testing.T) {
	tbl := &Table{m: map[string][]string{
		"python": {"https://mirrors.aliyun.com/python"},
	}}
	got := tbl.Apply("python", "https://www.python.org/ftp/python/3.12.7/Python-3.12.7.tar.xz")
	want := "https://mirrors.aliyun.com/python/3.12.7/Python-3.12.7.tar.xz"
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestApplyWrongTool(t *testing.T) {
	tbl := &Table{m: map[string][]string{
		"node": {"https://npmmirror.com/mirrors/node"},
	}}
	got := tbl.Apply("python", "https://www.python.org/ftp/python/3.12.7/Python-3.12.7.tar.xz")
	if got != "https://www.python.org/ftp/python/3.12.7/Python-3.12.7.tar.xz" {
		t.Errorf("expected no rewrite when tool not in table, got %s", got)
	}
}

func TestEmptyURL(t *testing.T) {
	tbl := &Table{m: map[string][]string{
		"python": {"https://mirrors.aliyun.com/python"},
	}}
	if got := tbl.Apply("python", ""); got != "" {
		t.Errorf("expected empty string, got %s", got)
	}
}

func TestApplyGoSDKDoesNotUseGoProxy(t *testing.T) {
	tbl := &Table{m: map[string][]string{"go": {"https://goproxy.cn"}}}
	original := "https://go.dev/dl/go1.23.2.windows-amd64.zip"
	if got := tbl.Apply("go", original); got != original {
		t.Fatalf("Go SDK archive rewritten through module proxy: %s", got)
	}
}

func TestApplyAndroidFamiliesUseConcreteRepositoryArtifact(t *testing.T) {
	tbl := &Table{m: map[string][]string{"android": {"https://redirector.gvt1.com/edgedl/android/repository"}}}
	original := "https://dl.google.com/android/repository/platform-tools_r35.0.2-win.zip"
	for _, family := range []string{"android", "android-platform-tools", "android-ndk", "android-emulator"} {
		want := "https://redirector.gvt1.com/edgedl/android/repository/platform-tools_r35.0.2-win.zip"
		if got := tbl.Apply(family, original); got != want {
			t.Errorf("Apply(%q) = %q, want %q", family, got, want)
		}
	}
}

func TestApplyRustUsesRustupDistStructure(t *testing.T) {
	tbl := &Table{m: map[string][]string{"rust": {"https://rsproxy.cn"}}}
	original := "https://static.rust-lang.org/rustup/dist/x86_64-pc-windows-msvc/rustup-init.exe"
	want := "https://rsproxy.cn/rustup/dist/x86_64-pc-windows-msvc/rustup-init.exe"
	if got := tbl.Apply("rust", original); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestArchiveProbeRejects404AndFallsBackToRange(t *testing.T) {
	var paths []string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path+" "+r.Method)
		if r.URL.Path == "/missing.zip" {
			http.NotFound(w, r)
			return
		}
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.Header.Get("Range") != "bytes=0-7" {
			t.Fatalf("Range = %q", r.Header.Get("Range"))
		}
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("PK\x03\x04data"))
	}))
	defer server.Close()

	oldClient := defaultClient
	defaultClient = server.Client()
	defer func() { defaultClient = oldClient }()

	ctx := context.Background()
	if headCheck(ctx, server.URL+"/missing.zip", time.Second) {
		t.Fatal("404 archive reported healthy")
	}
	if !headCheck(ctx, server.URL+"/archive.zip", time.Second) {
		t.Fatal("valid Range payload reported unhealthy")
	}
	if strings.Contains(strings.Join(paths, "\n"), "/ HEAD") {
		t.Fatalf("base root was probed: %v", paths)
	}
}
