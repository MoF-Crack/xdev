package signer

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateKeyPairRefusesExistingAndAliases(t *testing.T) {
	for _, kind := range []string{"existing-public", "existing-private", "same", "case-alias", "symlink", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			pub := filepath.Join(dir, "pub")
			priv := filepath.Join(dir, "priv")
			switch kind {
			case "existing-public":
				os.WriteFile(pub, []byte("old"), 0600)
			case "existing-private":
				os.WriteFile(priv, []byte("old"), 0600)
			case "same":
				priv = pub
			case "case-alias":
				priv = filepath.Join(dir, "PUB")
			case "symlink", "hardlink":
				os.WriteFile(pub, []byte("old"), 0600)
				var err error
				if kind == "symlink" {
					err = os.Symlink(pub, priv)
				} else {
					err = os.Link(pub, priv)
				}
				if err != nil {
					t.Skip(err)
				}
			}
			beforePub, _ := os.ReadFile(pub)
			beforePriv, _ := os.ReadFile(priv)
			if err := GenerateKeyPair(pub, priv); err == nil {
				t.Error("must refuse existing or aliased output")
			}
			afterPub, _ := os.ReadFile(pub)
			afterPriv, _ := os.ReadFile(priv)
			if !bytes.Equal(beforePub, afterPub) || !bytes.Equal(beforePriv, afterPriv) {
				t.Error("refusal modified outputs")
			}
		})
	}
}

func TestGenerateKeyPairFailureCleansOnlyNewFiles(t *testing.T) {
	dir := t.TempDir()
	pub := filepath.Join(dir, "missing", "pub")
	priv := filepath.Join(dir, "priv")
	if err := GenerateKeyPair(pub, priv); err == nil {
		t.Fatal("expected unavailable parent error")
	}
	if _, err := os.Lstat(priv); !os.IsNotExist(err) {
		t.Fatalf("private file left: %v", err)
	}
}

func TestMalformedRootNeverDisablesVerification(t *testing.T) {
	for _, value := range []string{"bad!", "REPLACE_WITH_REAL_PUBLIC_KEY", "YQ=="} {
		SetRootPublicKeyForTesting(value)
		if !HasRootKey() {
			t.Errorf("malformed root %q became absent", value)
		}
		if RootPublicKey() == nil {
			t.Errorf("malformed root %q entered no-key compatibility branch", value)
		}
	}
	SetRootPublicKeyForTesting("")
}
