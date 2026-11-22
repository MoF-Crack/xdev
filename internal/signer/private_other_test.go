//go:build !windows

package signer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestKeygenPrivateMode(t *testing.T) {
	d := t.TempDir()
	s := filepath.Join(d, "secret")
	if e := GenerateKeyPair(filepath.Join(d, "public"), s); e != nil {
		t.Fatal(e)
	}
	fi, e := os.Stat(s)
	if e != nil {
		t.Fatal(e)
	}
	if fi.Mode().Perm()&0077 != 0 {
		t.Fatalf("private permissions %v", fi.Mode())
	}
}
