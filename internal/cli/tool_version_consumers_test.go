package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/util"
)

// Exercise the Cobra updates and refresh entry points on the shipped NDK layout.
// The fixture disables remote discovery. Refresh can only promote local archives.
func TestD18_CLIUpdateConsumers(t *testing.T) {
	const tool = "android-ndk"
	root := t.TempDir()
	t.Setenv("XMDEV_HOME", root)
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	old := globalCtx
	globalCtx = runCtx{cfg: cfg, initialized: true}
	t.Cleanup(func() { globalCtx = old; config.ResetHomeDirForTest() })
	target := filepath.Join(cfg.Core.InstallDir, "android-sdk", tool, "r26")
	for _, d := range []string{target, filepath.Join(root, "current"), cfg.Core.CacheDir, cfg.Core.ManifestDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := util.CreateJunction(filepath.Join(root, "current", tool), target); err != nil {
		t.Fatal(err)
	}
	body := `name: android-ndk
latest: r26
install_root: android-sdk
auto_sniff: false
versions:
  - version: r26
  - version: 27.2.12479018
  - version: unknown
  - version: r29
    archives:
      - {os: any, arch: any, url: 'https://example.invalid/local-fixture.zip', format: zip}
`
	file := filepath.Join(cfg.Core.ManifestDir, tool+".yaml")
	if err := os.WriteFile(file, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	cacheFile := filepath.Join(cfg.Core.CacheDir, "sniff-cache.json")
	if err := os.WriteFile(cacheFile, []byte(`{"tools":{"android-ndk":{"status":"failed","error":"offline","last_success":"2026-01-01T00:00:00Z","versions":["unknown","27.2.12479018","r29"]}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := newUpdatesCmd()
	cmd.SetArgs([]string{})
	var diag bytes.Buffer
	cmd.SetErr(&diag)
	capture := captureStdout(t)
	err = cmd.Execute()
	output := capture()
	if err != nil || !strings.Contains(output, "r26 → r29") || !strings.Contains(diag.String(), "offline") {
		t.Fatalf("updates: %s / %s / %v", output, diag.String(), err)
	}
	if got := loadSniffUpdates(cfg); len(got) != 1 || got[0].Available != "r29" || got[0].Installed != "r26" {
		t.Fatalf("doctor results %+v", got)
	}
	cache := loadSniffCache(cfg.Core.CacheDir)
	if got := upgradeIndicator(tool, "r26", cache); !strings.Contains(got, "r29") || !strings.Contains(got, "offline") {
		t.Errorf("list indicator %q", got)
	}
	for _, active := range []string{"garbage", "29.0.777", "r30"} {
		if got := upgradeIndicator(tool, active, cache); got != "" {
			t.Errorf("unexpected indicator for %s: %s", active, got)
		}
	}
	cmd = newRefreshCmd()
	cmd.SetArgs([]string{tool})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	catalog, err := manifest.LoadCatalog(cfg.Core.ManifestDir)
	if err != nil {
		t.Fatal(err)
	}
	ndk, ok := catalog.Tool(tool)
	if !ok || ndk.Latest != "r29" {
		t.Fatalf("promoted NDK=%+v", ndk)
	}
	cmd = newUpgradeCmd()
	cmd.SetArgs([]string{tool, "--check"})
	capture = captureStdout(t)
	err = cmd.Execute()
	output = capture()
	if err != nil || !strings.Contains(output, "upgrade available → r29") {
		t.Fatalf("upgrade --check: %q, %v", output, err)
	}
}
