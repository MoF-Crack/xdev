package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultsWhenMissing(t *testing.T) {
	tmp := t.TempDir()
	// Load with explicit empty -> default under tmp
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	t.Setenv("XMDEV_FORCE_HOME", "1") // bypass the smart-fallback heuristic

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.Core.XMDevDir == "" {
		t.Errorf("expected non-empty xmdevenv_dir")
	}
	if cfg.Core.InstallDir == "" {
		t.Errorf("expected non-empty install_dir")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	cfg := defaults(tmp)
	cfg.Mirror["python"] = "https://example.com/python"
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "config.yaml")); err != nil {
		t.Fatalf("config file not created: %v", err)
	}
	loaded, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if loaded.Mirror["python"] != "https://example.com/python" {
		t.Errorf("mirror not persisted, got %v", loaded.Mirror)
	}
}

func TestEnsureDirs(t *testing.T) {
	tmp := t.TempDir()
	cfg := defaults(tmp)
	if err := cfg.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs error: %v", err)
	}
	for _, d := range []string{
		cfg.Core.DownloadDir, cfg.Core.InstallDir, cfg.Core.ShimDir,
		cfg.Core.CacheDir, cfg.Core.EnvDir, cfg.Core.ManifestDir,
	} {
		info, err := os.Stat(d)
		if err != nil || !info.IsDir() {
			t.Errorf("expected dir %s, got err=%v", d, err)
		}
	}
}

func TestExplicitOverride(t *testing.T) {
	tmp := t.TempDir()
	cfg, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.Core.XMDevDir != tmp {
		t.Errorf("expected xmdevenv_dir=%s, got %s", tmp, cfg.Core.XMDevDir)
	}
}

func TestExplicitDirWins(t *testing.T) {
	// Even when XMDEV_FORCE_HOME=1 and the smart-fallback would
	// redirect to D:\, an explicit argument always wins.
	t.Setenv("XMDEV_FORCE_HOME", "1")
	got, err := resolveXMDevDir("C:\\some\\where")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if got != "C:\\some\\where" {
		t.Errorf("explicit dir was overridden, got %s", got)
	}
}

func TestXMDevHomeEnvWins(t *testing.T) {
	t.Setenv("XMDEV_HOME", "C:\\custom\\home")
	got, err := resolveXMDevDir("")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if got != "C:\\custom\\home" {
		t.Errorf("XMDEV_HOME was overridden, got %s", got)
	}
}

func TestMinFreeBytesDefault(t *testing.T) {
	// 20 GiB = 20 * 2^30 bytes
	want := int64(20) * (1 << 30)
	if MinFreeBytesDefault != want {
		t.Errorf("MinFreeBytesDefault = %d, want %d", MinFreeBytesDefault, want)
	}
}

// --- GetPathMode ---

func TestGetPathMode_Default(t *testing.T) {
	tmp := t.TempDir()
	cfg := defaults(tmp)
	cfg.Path.Mode = ""
	got := cfg.GetPathMode()
	if got != "shim" {
		t.Errorf("GetPathMode() = %q, want %q for empty mode", got, "shim")
	}
}

func TestGetPathMode_Shim(t *testing.T) {
	tmp := t.TempDir()
	cfg := defaults(tmp)
	cfg.Path.Mode = "shim"
	got := cfg.GetPathMode()
	if got != "shim" {
		t.Errorf("GetPathMode() = %q, want %q", got, "shim")
	}
}

func TestGetPathMode_Direct(t *testing.T) {
	tmp := t.TempDir()
	cfg := defaults(tmp)
	cfg.Path.Mode = "direct"
	got := cfg.GetPathMode()
	if got != "direct" {
		t.Errorf("GetPathMode() = %q, want %q", got, "direct")
	}
}

func TestGetPathMode_Invalid(t *testing.T) {
	tmp := t.TempDir()
	cfg := defaults(tmp)
	cfg.Path.Mode = "invalid"
	got := cfg.GetPathMode()
	if got != "shim" {
		t.Errorf("GetPathMode() = %q, want %q for invalid mode", got, "shim")
	}
}

// --- SetPathMode ---

func TestSetPathMode_InvalidValue(t *testing.T) {
	tmp := t.TempDir()
	cfg := defaults(tmp)
	cfg.Path.Mode = "shim"
	cfg.SetPathMode("invalid")
	if cfg.Path.Mode != "shim" {
		t.Errorf("SetPathMode with invalid value should not change mode, got %q", cfg.Path.Mode)
	}
}
