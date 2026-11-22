package cli

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/pm"
	"github.com/xmdemt/xdev/internal/pm/pmtest"
	"github.com/xmdemt/xdev/internal/signer"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPMActualTrustAndSignCommand(t *testing.T) {
	dir := t.TempDir()
	keyDir := t.TempDir()
	prior := globalCtx
	t.Cleanup(func() { globalCtx = prior })
	globalCtx = runCtx{initialized: true, cfg: &config.Config{Core: config.CoreConfig{ManifestDir: dir}}}
	journal := pmtest.Commands(t, "bash", "pwsh", "net", "probe-detector")
	if err := os.Mkdir(filepath.Join(dir, "local"), 0700); err != nil {
		t.Fatal(err)
	}
	source := "package_managers:\n - name: probe\n   install_command: inert fixture\n   execution_env: bash\n   detect_cmd: probe-detector\n   requires_admin: true\n"
	path := filepath.Join(dir, "local", pm.FileName)
	write := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(path, source)
	run := func(args ...string) error {
		cmd := newPMInstallCmd()
		cmd.SetArgs(args)
		cmd.SetOut(&bytes.Buffer{})
		cmd.SilenceErrors = true
		cmd.SilenceUsage = true
		return cmd.Execute()
	}
	if err := run("local/probe", "--dry-run"); err != nil {
		t.Fatal(err)
	}
	if err := run("local/probe"); err == nil {
		t.Fatal("unapproved detector allowed")
	}
	if journal() != "" {
		t.Fatal(journal())
	}
	source = strings.Replace(source, "probe-detector", "missing-pm-detector", 1)
	write(path, source)
	digest := func() string {
		defs, err := pm.Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		d, _ := pm.Find(defs, "local/probe")
		return d.Origin.Digest
	}
	old := digest()
	if runtime.GOOS == "windows" {
		if err := run("local/probe", "--trust-digest", old); err == nil || !strings.Contains(err.Error(), "administrator") {
			t.Fatal(err)
		}
		if !strings.Contains(journal(), "net.exe session") || strings.Contains(journal(), "inert fixture") {
			t.Fatal(journal())
		}
	}
	source = strings.Replace(source, "requires_admin: true", "requires_admin: false", 1)
	write(path, source)
	if err := run("local/probe", "--trust-digest", old); err == nil {
		t.Fatal("stale approval")
	}
	if err := run("local/probe", "--trust-digest", digest()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(journal(), "-c inert fixture") {
		t.Fatal(journal())
	}
	write(filepath.Join(dir, pm.FileName), source)
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer.SetRootPublicKeyForTesting(base64.StdEncoding.EncodeToString(pub))
	t.Cleanup(func() { signer.SetRootPublicKeyForTesting("") })
	privPath := filepath.Join(keyDir, "key")
	write(privPath, base64.StdEncoding.EncodeToString(priv))
	if err := run("probe", "--dry-run"); err == nil {
		t.Fatal("unsigned official accepted")
	}
	cmd := newSignManifestsCmd()
	cmd.SetArgs([]string{"--manifests-dir", dir, "--priv-key", privPath})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if err := run("probe", "--dry-run"); err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(dir, pm.FileName), source+"# edited\n")
	if err := run("probe"); err == nil {
		t.Fatal("bad signature accepted by CLI")
	}
}

func TestPMDryRunWithoutShellOrPrivileges(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	prior := globalCtx
	t.Cleanup(func() { globalCtx = prior })
	globalCtx = runCtx{initialized: true, cfg: &config.Config{Core: config.CoreConfig{ManifestDir: t.TempDir()}}}
	name := "brew"
	if runtime.GOOS == "windows" {
		name = "choco"
	}
	cmd := newPMInstallCmd()
	cmd.SetArgs([]string{name, "--dry-run"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"builtin", "shell", "admin", "digest"} {
		if !strings.Contains(out.String(), key) {
			t.Errorf("missing %q in plan %s", key, out.String())
		}
	}
}
