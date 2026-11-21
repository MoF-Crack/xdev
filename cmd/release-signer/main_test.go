package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/xmdemt/xdev/internal/signer"
)

func TestRunSignsExactReleaseChecksumInput(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	checksumsPath := filepath.Join(dir, "SHA256SUMS")
	checksums := []byte("abc123  xdev-windows-amd64-v1.0.0-rc.3.exe\n")
	if err := os.WriteFile(checksumsPath, checksums, 0o600); err != nil {
		t.Fatal(err)
	}
	key := base64.StdEncoding.EncodeToString(priv)
	publicKeyPath := filepath.Join(dir, "pub.key")
	if err := os.WriteFile(publicKeyPath, []byte(base64.StdEncoding.EncodeToString(pub)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"v1.0.0-rc.3", checksumsPath, publicKeyPath}, key); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(checksumsPath + ".sig")
	if err != nil {
		t.Fatal(err)
	}
	signature, err := signer.ParseReleaseChecksumsSignature(data)
	if err != nil {
		t.Fatal(err)
	}
	if !signer.VerifyReleaseChecksums(pub, signature, repository, "v1.0.0-rc.3", checksums) {
		t.Fatal("written signature did not authenticate original inputs")
	}
	if signer.VerifyReleaseChecksums(pub, signature, repository, "v1.0.0-rc.4", checksums) {
		t.Fatal("signature unexpectedly authenticated another tag")
	}
	if signer.VerifyReleaseChecksums(pub, signature, repository, "v1.0.0-rc.3", append(checksums, 'x')) {
		t.Fatal("signature unexpectedly authenticated changed checksum bytes")
	}
	wrongPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	wrongPublicKeyPath := filepath.Join(dir, "wrong-pub.key")
	if err := os.WriteFile(wrongPublicKeyPath, []byte(base64.StdEncoding.EncodeToString(wrongPub)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"v1.0.0-rc.3", checksumsPath, wrongPublicKeyPath}, key); err == nil {
		t.Fatal("signer accepted a private key that differs from the published root key")
	}
}

func TestRunRejectsInvalidKeyAndArguments(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		key  string
	}{
		{name: "missing args", args: []string{"v1.0.0-rc.3"}, key: "unused"},
		{name: "invalid tag", args: []string{"../rc.3", "SHA256SUMS", "pub.key"}, key: "unused"},
		{name: "missing key", args: []string{"v1.0.0-rc.3", "SHA256SUMS", "pub.key"}},
		{name: "invalid key", args: []string{"v1.0.0-rc.3", "SHA256SUMS", "pub.key"}, key: "not-base64"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := run(tc.args, tc.key); err == nil {
				t.Fatal("run unexpectedly succeeded")
			}
		})
	}
}
