package tui

import (
	"github.com/xmdemt/xdev/internal/util"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestD18_TUIRetainsCachedNDKUpdate(t *testing.T) {
	m := newTUITestModelWithTools(t, "android-ndk")
	cfg := m.config.Core
	m.config.Core.CacheDir = t.TempDir()
	current := filepath.Join(cfg.XMDevDir, "current")
	if err := os.MkdirAll(current, 0755); err != nil {
		t.Fatal(err)
	}
	for name, version := range map[string]string{"android-ndk": "r26", "unknown": "broken9", "equal": "1.2.0+local"} {
		target := filepath.Join(cfg.InstallDir, name, version)
		if err := os.MkdirAll(target, 0755); err != nil {
			t.Fatal(err)
		}
		if err := util.CreateJunction(filepath.Join(current, name), target); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(m.config.Core.CacheDir, "sniff-cache.json"), []byte(`{"tools":{"android-ndk":{"status":"failed","error":"offline","versions":["bogus","27.2.12479018","r29"]},"unknown":{"versions":["999"]},"equal":{"versions":["1.2+other"]}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	m = m.enterUpdatesView()
	if len(m.updates) != 1 || m.updates[0].Latest != "r29" {
		t.Fatalf("updates=%+v", m.updates)
	}
	view := m.updatesViewString()
	for _, want := range []string{"android-ndk", "r26", "r29", "offline"} {
		if !strings.Contains(view, want) {
			t.Errorf("missing %s in %s", want, view)
		}
	}
}
