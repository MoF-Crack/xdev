package gui

import (
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
