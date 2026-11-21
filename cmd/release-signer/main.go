package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xmdemt/xdev/internal/signer"
)

const repository = "github.com/MoF-Crack/xdev"

func main() {
	if err := run(os.Args[1:], os.Getenv("XDEV_SIGNING_PRIVATE_KEY")); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, privateKeyB64 string) error {
	if len(args) != 3 || args[0] == "" || args[1] == "" || args[2] == "" {
		return errors.New("usage: xdev-release-signer <release-tag> <SHA256SUMS-path> <expected-public-key-path>")
	}
	tag := strings.TrimSpace(args[0])
	if !strings.HasPrefix(tag, "v") || strings.ContainsAny(tag, "\\/\x00") {
		return fmt.Errorf("invalid release tag %q", tag)
	}
	keyBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(privateKeyB64))
	if err != nil || len(keyBytes) != ed25519.PrivateKeySize {
		return errors.New("XDEV_SIGNING_PRIVATE_KEY must be a base64-encoded Ed25519 private key")
	}
	publicKeyPath, err := filepath.Abs(args[2])
	if err != nil {
		return fmt.Errorf("resolve expected public key path: %w", err)
	}
	expectedPublicKey, err := signer.LoadPublicKey(publicKeyPath)
	if err != nil {
		return fmt.Errorf("read expected public key: %w", err)
	}
	privateKey := ed25519.PrivateKey(keyBytes)
	if !privateKey.Public().(ed25519.PublicKey).Equal(expectedPublicKey) {
		return errors.New("XDEV_SIGNING_PRIVATE_KEY does not match the release binary's expected public key")
	}
	checksumsPath, err := filepath.Abs(args[1])
	if err != nil {
		return fmt.Errorf("resolve SHA256SUMS path: %w", err)
	}
	checksums, err := os.ReadFile(checksumsPath)
	if err != nil {
		return fmt.Errorf("read SHA256SUMS: %w", err)
	}
	if len(checksums) == 0 || len(checksums) > 1<<20 {
		return fmt.Errorf("SHA256SUMS size is outside the accepted range")
	}
	signature, err := signer.SignReleaseChecksums(privateKey, repository, tag, checksums)
	if err != nil {
		return err
	}
	if !signer.VerifyReleaseChecksums(expectedPublicKey, signature, repository, tag, checksums) {
		return errors.New("new release checksum signature failed verification against the release public key")
	}
	data, err := signer.MarshalReleaseChecksumsSignature(signature)
	if err != nil {
		return err
	}
	out := checksumsPath + ".sig"
	if err := os.WriteFile(out, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write checksum signature: %w", err)
	}
	return nil
}
