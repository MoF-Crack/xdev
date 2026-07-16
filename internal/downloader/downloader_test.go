package downloader

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/util"
)

func TestDownloadAggregatesSanitizedFallbackFailures(t *testing.T) {
	server := httptest.NewTLSServer(http.NotFoundHandler())
	defer server.Close()
	err := Download(context.Background(), Options{
		URL:          server.URL + "/primary.zip?token=secret",
		FallbackURLs: []string{server.URL + "/fallback.zip?key=secret"},
		Dest:         filepath.Join(t.TempDir(), "archive.zip"),
		Client:       server.Client(),
		MaxRetries:   1,
	})
	if err == nil {
		t.Fatal("expected all candidates to fail")
	}
	message := err.Error()
	for _, want := range []string{"/primary.zip", "/fallback.zip", "HTTP 404"} {
		if !strings.Contains(message, want) {
			t.Errorf("error %q does not contain %q", message, want)
		}
	}
	if strings.Contains(message, "secret") {
		t.Fatalf("error leaked URL credentials: %s", message)
	}
}

func TestIsAllZeroHex(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", false}, // empty is not a placeholder
		{"0000000000000000000000000000000000000000000000000000000000000000", true},
		{"0x0000000000000000000000000000000000000000000000000000000000000000", true},
		{"0000000000000000000000000000000000000000000000000000000000000001", false},
		{"f5cea43414cc33024bbe5867f208d1c9c915d6a38e92abeee07ed9e5563662297", false},
		{"abc", false},
		{"0", true},
	}
	for _, c := range cases {
		if got := util.IsAllZeroHash(c.in); got != c.want {
			t.Errorf("IsAllZeroHash(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
