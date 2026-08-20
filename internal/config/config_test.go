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

// --- MigrationConfig defaults ---

func TestGetMigrationMode_Default(t *testing.T) {
	tmp := t.TempDir()
	cfg := defaults(tmp)
	cfg.Migration.MigrationMode = ""
	got := cfg.GetMigrationMode()
	if got != "junction" {
		t.Errorf("GetMigrationMode() = %q, want %q for empty mode", got, "junction")
	}
}

func TestGetMigrationMode_Junction(t *testing.T) {
	tmp := t.TempDir()
	cfg := defaults(tmp)
	cfg.Migration.MigrationMode = "junction"
	if got := cfg.GetMigrationMode(); got != "junction" {
		t.Errorf("GetMigrationMode() = %q, want 'junction'", got)
	}
}

func TestGetMigrationMode_Move(t *testing.T) {
	tmp := t.TempDir()
	cfg := defaults(tmp)
	cfg.Migration.MigrationMode = "move"
	if got := cfg.GetMigrationMode(); got != "move" {
		t.Errorf("GetMigrationMode() = %q, want 'move'", got)
	}
}

func TestGetMigrationMode_Invalid(t *testing.T) {
	tmp := t.TempDir()
	cfg := defaults(tmp)
	cfg.Migration.MigrationMode = "invalid"
	if got := cfg.GetMigrationMode(); got != "junction" {
		t.Errorf("GetMigrationMode() = %q, want 'junction' for invalid mode", got)
	}
}

func TestSetMigrationMode_InvalidValue(t *testing.T) {
	tmp := t.TempDir()
	cfg := defaults(tmp)
	cfg.Migration.MigrationMode = "junction"
	if err := cfg.SetMigrationMode("invalid"); err == nil {
		t.Error("SetMigrationMode with invalid value should return an error")
	}
	if cfg.Migration.MigrationMode != "junction" {
		t.Errorf("SetMigrationMode with invalid value should not change mode, got %q", cfg.Migration.MigrationMode)
	}
}

func TestLoadMigrationModeDefault(t *testing.T) {
	tmp := t.TempDir()
	// Config without any migration section -> default "junction"
	noMigrationYAML := `core:
  xmdevenv_dir: ` + tmp + `
`
	if err := os.WriteFile(filepath.Join(tmp, "config.yaml"), []byte(noMigrationYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.GetMigrationMode() != "junction" {
		t.Errorf("GetMigrationMode() = %q, want 'junction'", cfg.GetMigrationMode())
	}
}

func TestLoadMigrationModeExplicit(t *testing.T) {
	tmp := t.TempDir()
	migrationYAML := `core:
  xmdevenv_dir: ` + tmp + `
migration:
  migration_mode: move
`
	if err := os.WriteFile(filepath.Join(tmp, "config.yaml"), []byte(migrationYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.GetMigrationMode() != "move" {
		t.Errorf("GetMigrationMode() = %q, want 'move'", cfg.GetMigrationMode())
	}
}

// --- SniffConfig defaults ---

func TestSniffConfigDefaults(t *testing.T) {
	tmp := t.TempDir()
	// Config without any sniff section
	noSniffYAML := `core:
  xmdevenv_dir: ` + tmp + `
`
	if err := os.WriteFile(filepath.Join(tmp, "config.yaml"), []byte(noSniffYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.Sniff.Strategy != "interval" {
		t.Errorf("Sniff.Strategy = %q, want 'interval'", cfg.Sniff.Strategy)
	}
	if cfg.Sniff.Interval != "24h" {
		t.Errorf("Sniff.Interval = %q, want '24h'", cfg.Sniff.Interval)
	}
	if cfg.Sniff.Concurrency != 3 {
		t.Errorf("Sniff.Concurrency = %d, want 3", cfg.Sniff.Concurrency)
	}
	if cfg.Sniff.RateLimitPerHr != 300 {
		t.Errorf("Sniff.RateLimitPerHr = %d, want 300", cfg.Sniff.RateLimitPerHr)
	}
	if !cfg.Sniff.AutoArchiveFill {
		t.Error("Sniff.AutoArchiveFill should default to true")
	}
}

func TestSetProxy_RejectsUnsafeScheme(t *testing.T) {
	tmp := t.TempDir()
	cfg := defaults(tmp)
	if err := cfg.SetProxy("file:///etc/passwd", "", "", true); err == nil {
		t.Fatal("SetProxy should reject file:// scheme")
	}
	if err := cfg.SetProxy("http://127.0.0.1:7890", "ftp://evil", "", true); err == nil {
		t.Fatal("SetProxy should reject ftp:// https proxy")
	}
	if err := cfg.SetProxy("http://127.0.0.1:7890", "socks5://127.0.0.1:1080", "localhost", true); err != nil {
		t.Fatalf("valid proxy should be accepted: %v", err)
	}
	if cfg.Proxy.HTTP != "http://127.0.0.1:7890" {
		t.Errorf("HTTP proxy not persisted: %q", cfg.Proxy.HTTP)
	}
}

func TestSniffConfigExplicitValues(t *testing.T) {
	tmp := t.TempDir()
	// Config with explicit sniff section (partial)
	sniffYAML := `core:
  xmdevenv_dir: ` + tmp + `
sniff:
  strategy: startup
  auto_archive_fill: false
`
	if err := os.WriteFile(filepath.Join(tmp, "config.yaml"), []byte(sniffYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.Sniff.Strategy != "startup" {
		t.Errorf("Sniff.Strategy = %q, want 'startup'", cfg.Sniff.Strategy)
	}
	// Interval should get default since not explicitly set
	if cfg.Sniff.Interval != "24h" {
		t.Errorf("Sniff.Interval = %q, want '24h'", cfg.Sniff.Interval)
	}
	if cfg.Sniff.Concurrency != 3 {
		t.Errorf("Sniff.Concurrency = %d, want 3", cfg.Sniff.Concurrency)
	}
	if cfg.Sniff.RateLimitPerHr != 300 {
		t.Errorf("Sniff.RateLimitPerHr = %d, want 300", cfg.Sniff.RateLimitPerHr)
	}
	// Explicit false should be preserved
	if cfg.Sniff.AutoArchiveFill {
		t.Error("Sniff.AutoArchiveFill should be false (explicitly set)")
	}
}
