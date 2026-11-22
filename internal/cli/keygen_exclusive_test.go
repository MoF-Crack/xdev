package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// Exercise the actual Cobra entry, not just signer.GenerateKeyPair.
func TestKeygenNeverRotatesAnExistingPair(t *testing.T) {
	d := t.TempDir()
	p, s := filepath.Join(d, "pub"), filepath.Join(d, "priv")
	run := func(pub, priv string) error {
		c := newKeygenCmd()
		c.SetArgs([]string{"--pub-key", pub, "--priv-key", priv})
		c.SilenceUsage = true
		c.SilenceErrors = true
		return c.Execute()
	}
	if err := run(p, s); err != nil {
		t.Fatal(err)
	}
	secret, err := os.ReadFile(s)
	if err != nil {
		t.Fatal(err)
	}
	if err = run(p, s); err == nil {
		t.Fatal("implicit key rotation")
	}
	got, err := os.ReadFile(s)
	if err != nil || !bytes.Equal(got, secret) {
		t.Fatal("existing private key changed")
	}
	a := filepath.Join(d, "same")
	if err = run(a, a); err == nil {
		t.Fatal("same name accepted")
	}
	if _, err = os.Lstat(a); !os.IsNotExist(err) {
		t.Fatalf("created alias after rejection: %v", err)
	}
}
