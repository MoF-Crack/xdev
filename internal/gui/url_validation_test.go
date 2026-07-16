package gui

import (
	"strings"
	"testing"
)

func TestSetMirrorConfigRejectsUnsafeURLs(t *testing.T) {
	a := newTestApp()
	for _, raw := range []string{"http://example.com", "javascript:alert(1)", "https://127.0.0.1/file"} {
		if err := a.SetMirrorConfig(map[string]string{"node": raw}); err == nil {
			t.Fatalf("SetMirrorConfig(%q) accepted unsafe URL", raw)
		}
	}
}

func TestAddCustomToolRejectsUnsafeDownloadURL(t *testing.T) {
	a := newTestApp()
	for _, raw := range []string{"http://example.com/tool.zip", "file:///tool.zip", "https://127.0.0.1/tool.zip"} {
		body := `{"name":"safe-name","displayName":"Safe","downloadUrl":"` + raw + `"}`
		if err := a.AddCustomTool(body); err == nil || !strings.Contains(strings.ToLower(err.Error()), "url") {
			t.Fatalf("AddCustomTool(%q) error = %v, want URL validation error", raw, err)
		}
	}
}
