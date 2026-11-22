package config

import (
	"encoding/json"
	"errors"
	"github.com/xmdemt/xdev/internal/util"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

func switchFixture(t *testing.T) *Config {
	t.Helper()
	c, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Save(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XMDEV_HOME", c.Core.XMDevDir)
	t.Setenv("LOCALAPPDATA", t.TempDir())
	t.Cleanup(ResetHomeDirForTest)
	return c
}

func TestRootSwitchRejectsUnsafeTargets(t *testing.T) {
	for _, kind := range []string{"nested", "occupied", "stale"} {
		t.Run(kind, func(t *testing.T) {
			c := switchFixture(t)
			old := c.Core.XMDevDir
			dest := filepath.Join(t.TempDir(), "new")
			switch kind {
			case "nested":
				dest = filepath.Join(old, "child")
			case "occupied":
				if err := os.MkdirAll(dest, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dest, "sentinel"), []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			case "stale":
				other := c.Clone()
				if err := other.SwitchRoot(filepath.Join(t.TempDir(), "winner")); err != nil {
					t.Fatal(err)
				}
			}
			if err := c.SwitchRoot(dest); err == nil {
				t.Fatal("unsafe root switch accepted")
			}
			if c.Core.XMDevDir != old {
				t.Fatal("failed switch changed memory")
			}
		})
	}
}

func TestRootSwitchPublicationFailurePreservesOld(t *testing.T) {
	c := switchFixture(t)
	old := c.Core.XMDevDir
	before, _ := os.ReadFile(filepath.Join(old, "config.yaml"))
	previous := writeRootSelection
	writeRootSelection = func(path string, data []byte, mode os.FileMode) error { return errors.New("injected commit failure") }
	t.Cleanup(func() { writeRootSelection = previous })
	dest := filepath.Join(t.TempDir(), "new")
	if err := c.SwitchRoot(dest); err == nil {
		t.Fatal("expected publication failure")
	}
	after, _ := os.ReadFile(filepath.Join(old, "config.yaml"))
	if string(before) != string(after) || c.Core.XMDevDir != old {
		t.Fatal("old config changed")
	}
	if _, err := os.Stat(filepath.Join(dest, "config.yaml")); !os.IsNotExist(err) {
		t.Fatal("uncommitted target config remains")
	}
}

func TestRootSwitchIndependentInstancesSingleWinner(t *testing.T) {
	c := switchFixture(t)
	d := c.Clone()
	base := t.TempDir()
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i, cfg := range []*Config{c, d} {
		wg.Add(1)
		go func(i int, cfg *Config) {
			defer wg.Done()
			results <- cfg.SwitchRoot(filepath.Join(base, []string{"a", "b"}[i]))
		}(i, cfg)
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("successful competing commits=%d want 1", success)
	}
}

func TestRootSwitchRejectsCorruptRedirects(t *testing.T) {
	c := switchFixture(t)
	root := c.Core.XMDevDir
	for _, kind := range []string{"bad-json", "bad-hmac", "cycle", "missing-target"} {
		t.Run(kind, func(t *testing.T) {
			record := rootRedirect{Schema: 1, From: root, Target: root}
			if kind == "missing-target" {
				record.Target = filepath.Join(t.TempDir(), "missing")
			}
			signed, _ := json.Marshal(record)
			record.HMAC = util.ComputeHMAC(signed)
			if kind == "bad-hmac" {
				record.HMAC = "bad"
			}
			data, _ := json.Marshal(record)
			if kind == "bad-json" {
				data = []byte("{")
			}
			if err := os.WriteFile(filepath.Join(root, rootRedirectFile), data, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(root); err == nil {
				t.Fatal("invalid redirect accepted")
			}
			ResetHomeDirForTest()
			if got := ResolveHomeDir(); got != "" {
				t.Fatalf("shim accepted invalid redirect: %s", got)
			}
		})
	}
}

func TestRootSwitchMigrationCommitFailureKeepsSource(t *testing.T) {
	c := switchFixture(t)
	old := c.Core.XMDevDir
	file := filepath.Join(old, "valuable")
	if err := os.WriteFile(file, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	previous := writeRootSelection
	writeRootSelection = func(string, []byte, os.FileMode) error { return errors.New("commit failure") }
	t.Cleanup(func() { writeRootSelection = previous })
	dest := filepath.Join(t.TempDir(), "copy")
	if err := c.MigrateRoot(dest); err == nil {
		t.Fatal("expected error")
	}
	if c.Core.XMDevDir != old {
		t.Fatal("failed migration published memory")
	}
	if b, e := os.ReadFile(file); e != nil || string(b) != "keep" {
		t.Fatal("source damaged", e)
	}
	if _, e := os.Stat(filepath.Join(dest, "config.yaml")); !os.IsNotExist(e) {
		t.Fatal("uncommitted config left")
	}
	if _, e := LoadReadOnly(old); e != nil {
		t.Fatal("old root unusable", e)
	}
}

func TestRootSwitchCrossProcessSingleWinner(t *testing.T) {
	if os.Getenv("XDEV_SWITCH_LOCK_CHILD") == "1" {
		c := defaults(os.Getenv("XMDEV_HOME"))
		if err := c.SwitchRoot(os.Getenv("XDEV_SWITCH_LOCK_DEST")); err != nil {
			t.Fatal(err)
		}
		return
	}
	c := switchFixture(t)
	old := c.Core.XMDevDir
	base := t.TempDir()
	var commands []*exec.Cmd
	for _, name := range []string{"one", "two"} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestRootSwitchCrossProcessSingleWinner$")
		cmd.Env = append(os.Environ(), "XMDEV_HOME="+old, "XDEV_SWITCH_LOCK_CHILD=1", "XDEV_SWITCH_LOCK_DEST="+filepath.Join(base, name))
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		commands = append(commands, cmd)
	}
	successes := 0
	for _, cmd := range commands {
		if err := cmd.Wait(); err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("cross-process commit successes=%d", successes)
	}
	if _, err := LoadReadOnly(old); err != nil {
		t.Fatal(err)
	}
}

func TestRootSwitchConcurrentCacheInvalidation(t *testing.T) {
	switchFixture(t)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				InvalidateHomeCache()
				ResolveHomeDir()
			}
		}()
	}
	wg.Wait()
}

func TestRootSwitchFreshProcessDiscovery(t *testing.T) {
	if os.Getenv("XDEV_ROOT_SWITCH_CHILD") == "1" {
		c, err := Load("")
		if err != nil {
			t.Fatal(err)
		}
		want := os.Getenv("XDEV_ROOT_SWITCH_WANT")
		if c.Core.XMDevDir != want || ResolveHomeDir() != want {
			t.Fatalf("fresh child root=%s shim=%s want=%s", c.Core.XMDevDir, ResolveHomeDir(), want)
		}
		return
	}
	c := switchFixture(t)
	old := c.Core.XMDevDir
	dest := filepath.Join(t.TempDir(), "new")
	if err := os.MkdirAll(filepath.Join(old, "install"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(old, "install", "sentinel"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := c.SwitchRoot(dest); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestRootSwitchFreshProcessDiscovery$")
	cmd.Env = append(os.Environ(), "XDEV_ROOT_SWITCH_CHILD=1", "XDEV_ROOT_SWITCH_WANT="+dest)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(old, "install", "sentinel")); err != nil {
		t.Fatal(err)
	}
}
