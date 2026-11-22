// Package signer provides Ed25519 manifest signature verification for
// supply-chain security.
//
// # Overview
//
// When manifest YAML files are signed with the private key (via
// `xdev sign-manifests`), each tool gets a .yaml.sig file containing
// a JSON-encoded ManifestSignature. The downloader verifies this
// signature against the embedded root public key before accepting a
// download, ensuring that the SHA-256 hash and download URL have not
// been tampered with.
//
// # Key Management
//
// The root public key is hardcoded in rootPublicKeyB64 to prevent an
// attacker who gains write access to the config directory from replacing
// it. To rotate the key, a new binary must be released.
//
// Generate a key pair with:
//
//	xdev keygen --pub-key manifests/pub.key --priv-key keys/priv.key
//
// Then replace rootPublicKeyB64 with the contents of pub.key.
//
// # Development builds
//
// Only a genuinely empty root selects the unkeyed development mode. A
// configured but malformed root is a configuration error, never an opt-out.
// Expected archive hashes are independent of this setting.
//
// # Signature Format
//
// ManifestSignature is a JSON structure containing the tool name, version,
// SHA-256 hashes, and an Ed25519 signature. Verification checks that the
// signature was produced by the private key corresponding to the embedded
// public key, and that the downloaded archive's hash matches the signed hash.
package signer

import (
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
)

// rootPublicKeyB64 is the base64-encoded Ed25519 public key.
// This value can be set at compile time via -ldflags:
//
//	go build -ldflags "-X github.com/xmdemt/xdev/internal/signer.rootPublicKeyB64=<base64_pub_key>" .
//
// The -X target must be the FULL import path. A short path such as
// "-X internal/signer.rootPublicKeyB64=..." is silently ignored by the Go
// linker: the build succeeds, this variable stays empty, and signature
// verification is silently disabled.
//
// An empty value selects unkeyed development mode.
var rootPublicKeyB64 = ""

// embeddedSignaturesFS can be set at compile time to embed .sig files
// from the manifests directory. When nil, signatures are read from disk.
var embeddedSignaturesFS embed.FS

// SetEmbeddedSignaturesFS sets the embedded FS for signature files.
// Called during init from cmd/xmdevenvmt/main.go.
func SetEmbeddedSignaturesFS(fsys embed.FS) {
	embeddedSignaturesFS = fsys
}

// RootPublicKey is the legacy accessor: only an absent key returns nil.
// New consumers should propagate errors from RootPublicKeyChecked.
func RootPublicKey() []byte {
	pub, err := RootPublicKeyChecked()
	if err != nil {
		log.Printf("[signer] ERROR: %v", err)
		// Compatibility with callers which used nil to select unsigned mode.
		// An invalid-size, non-nil key cannot verify any signature.
		return []byte{}
	}
	return pub
}

// RootPublicKeyChecked distinguishes an unkeyed development build (nil, nil)
// from a broken trust configuration. Even a textual placeholder is an error.
func RootPublicKeyChecked() ([]byte, error) {
	if rootPublicKeyB64 == "" {
		return nil, nil
	}
	pub, err := base64.StdEncoding.Strict().DecodeString(rootPublicKeyB64)
	if err != nil || len(pub) != 32 || base64.StdEncoding.EncodeToString(pub) != rootPublicKeyB64 {
		return nil, fmt.Errorf("invalid embedded Ed25519 root public key; refusing to trust signatures (expected canonical base64 of 32 bytes)")
	}
	return pub, nil
}

// HasRootKey reports whether the build requires verification (also true for
// malformed nonempty roots; they must never relax integrity requirements).
func HasRootKey() bool { return rootPublicKeyB64 != "" }

// SetRootPublicKeyForTesting overrides the embedded root public key at
// runtime (base64-encoded 32-byte Ed25519 public key). Tests use it to
// exercise the signature-gated code paths that production builds enable
// via -ldflags; passing "" restores the "no root key embedded" state.
//
// Audit-fixes ticket 13: the signature gates (signed-map membership,
// per-URL coverage, L-10 refusal) all short-circuit when no root key is
// embedded, so without this hook those paths would be untestable on a
// default (placeholder-key) build.
func SetRootPublicKeyForTesting(b64 string) {
	rootPublicKeyB64 = b64
}

// ReadEmbeddedSignature reads a manifest signature from the embedded FS.
// Returns nil, nil if no embedded signature is found.
func ReadEmbeddedSignature(toolName string) (*ManifestSignature, error) {
	if embeddedSignaturesFS == (embed.FS{}) {
		return nil, nil
	}
	sigPath := "manifests/" + toolName + ".yaml.sig"
	data, err := fs.ReadFile(embeddedSignaturesFS, sigPath)
	if err != nil {
		return nil, nil // Not found is not an error
	}
	var ms ManifestSignature
	if err := json.Unmarshal(data, &ms); err != nil {
		return nil, fmt.Errorf("parse embedded signature for %s: %w", toolName, err)
	}
	return &ms, nil
}
