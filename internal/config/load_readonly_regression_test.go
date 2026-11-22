package config

import (
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestLoadReadOnlyDoesNotSaveOrConsumeMigration(t *testing.T) {
	root := t.TempDir()
	c, err := LoadReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Save(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(root, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	migrateOnce = sync.Once{}
	t.Cleanup(func() { migrateOnce = sync.Once{} })
	if _, err = LoadReadOnly(root); err != nil {
		t.Fatal(err)
	}
	available := false
	migrateOnce.Do(func() { available = true })
	if !available {
		t.Fatal("read-only load consumed migration hook")
	}
	after, err := os.ReadFile(filepath.Join(root, "config.yaml"))
	if err != nil || string(before) != string(after) {
		t.Fatal("read-only load wrote config", err)
	}
	missing := filepath.Join(t.TempDir(), "absent")
	if _, err = LoadReadOnly(missing); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("read-only load created root")
	}
}

func TestLoadReadOnlyPreservesIntegrityValidation(t *testing.T) {
	root := t.TempDir()
	c, err := LoadReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Save(); err != nil {
		t.Fatal(err)
	}
	c.Proxy = ProxyConfig{HTTP: "http://127.0.0.1:9999", Enabled: true}
	data, err := yaml.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "config.yaml")
	if err = os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	read, err := LoadReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	if read.Proxy.Enabled || read.Integrity != "" {
		t.Fatal("read-only load bypassed HMAC validation")
	}
	after, err := os.ReadFile(file)
	if err != nil || string(data) != string(after) {
		t.Fatal("integrity handling changed disk", err)
	}
}
