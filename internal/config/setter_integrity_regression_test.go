package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestConfigSettersRefreshIntegrity(t *testing.T) {
	for _, mode := range []string{"shim", "auto", "direct"} {
		t.Run(mode, func(t *testing.T) {
			cfg := defaults(t.TempDir())
			cfg.CustomTools = []CustomToolConfig{{Name: "custom", SourceURL: "https://example.com/tool", SourceType: "html"}}
			cfg.Core.MirrorPreference = []string{"https://example.com/mirror"}
			if err := cfg.Save(); err != nil {
				t.Fatal(err)
			}
			if err := cfg.SetProxy("http://127.0.0.1:7890", "socks5://127.0.0.1:1080", "localhost", true); err != nil {
				t.Fatal(err)
			}
			if err := cfg.SetPathMode(mode); err != nil {
				t.Fatal(err)
			}
			loaded, err := Load(cfg.Core.XMDevDir)
			if err != nil {
				t.Fatal(err)
			}
			if loaded.Proxy != cfg.Proxy || !loaded.Proxy.Enabled || loaded.Path.Mode != mode || !reflect.DeepEqual(loaded.CustomTools, cfg.CustomTools) {
				t.Fatalf("setter/load discarded saved fields: proxy=%+v mode=%q custom=%+v", loaded.Proxy, loaded.Path.Mode, loaded.CustomTools)
			}
			if loaded.Integrity == "" || loaded.Integrity != loaded.computeIntegrityHMAC() {
				t.Fatal("saved HMAC does not describe saved snapshot")
			}
		})
	}
}

func TestConfigSetterFailureKeepsMemory(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(*Config) error
	}{
		{"proxy", func(c *Config) error { return c.SetProxy("http://127.0.0.1:7890", "", "", true) }},
		{"mode", func(c *Config) error { return c.SetPathMode("direct") }},
		{"retention", func(c *Config) error { return c.SetArchiveRetention("7d") }},
		{"migration", func(c *Config) error { return c.SetMigrationMode("move") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := defaults(t.TempDir())
			if err := cfg.Save(); err != nil {
				t.Fatal(err)
			}
			// Force a publication error portably, without changing user permissions.
			configPath := filepath.Join(cfg.Core.XMDevDir, "config.yaml")
			old, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			backup := filepath.Join(cfg.Core.XMDevDir, "previous.yaml")
			if err := os.Rename(configPath, backup); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(configPath, 0700); err != nil {
				t.Fatal(err)
			}
			before := cfg.Clone()
			if err := tc.set(cfg); err == nil {
				t.Fatal("expected publication failure")
			}
			if !reflect.DeepEqual(cfg.Clone(), before) {
				t.Fatal("failed setter published an in-memory partial update")
			}
			preserved, err := os.ReadFile(backup)
			if err != nil || string(preserved) != string(old) {
				t.Fatal("previous config was damaged")
			}
		})
	}
}
