package installer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/xmdemt/xdev/internal/manifest"
)

func TestInstallerExtractFailurePreservesPriorTree(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)
	ins.SetNoEnvWrite(true)
	if err := os.MkdirAll(cfg.Core.CacheDir, 0700); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(cfg.Core.CacheDir, "extracttool-1.0.0.zip")
	if err := os.WriteFile(src, []byte("invalid archive"), 0600); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(cfg.Core.InstallDir, "extracttool", "1.0.0")
	if err := os.MkdirAll(dest, 0700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(dest, "previous")
	if err := os.WriteFile(sentinel, []byte("prior tree"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ins.extract("extracttool", &manifest.Archive{Format: "zip"}, "1.0.0", dest); err == nil {
		t.Fatal("invalid/nonempty target must fail")
	}
	if b, err := os.ReadFile(sentinel); err != nil || string(b) != "prior tree" {
		t.Fatalf("prior tree damaged: %q %v", b, err)
	}
}

func TestInstallerExtractUsesCancellationAndConfiguredBudget(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)
	ins.SetNoEnvWrite(true)
	if err := os.MkdirAll(cfg.Core.CacheDir, 0700); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(cfg.Core.CacheDir, "demobudget-1.0.0.zip")
	makeFakeZip(t, src, "demobudget")
	a := &manifest.Archive{Format: "zip", Strip: 1}
	p := &fakePlugin{name: "demobudget", shims: []string{"demobudget"}, archive: a}
	dest := filepath.Join(cfg.Core.InstallDir, p.Name(), "1.0.0")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ins.installByKind(ctx, p, a, "1.0.0", dest); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel err=%v", err)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("cancel published target: %v", err)
	}
	t.Setenv("XDEV_EXTRACT_MAX_FILE_BYTES", "1")
	t.Setenv("XDEV_EXTRACT_MAX_TOTAL_BYTES", "1")
	if err := ins.installByKind(context.Background(), p, a, "1.0.0", dest); err == nil {
		t.Fatal("configured budget ignored")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("over-budget published target: %v", err)
	}
}
