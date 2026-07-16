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
// # Placeholder Key
//
// The default value of rootPublicKeyB64 is "REPLACE_WITH_REAL_PUBLIC_KEY".
// When this placeholder is detected, RootPublicKey() returns nil and a
// WARNING is logged. The downloader skips signature verification in this
// case, which means downloads are NOT integrity-verified — this is
// acceptable for development but a security risk in production.
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
//	go build -ldflags "-X internal/signer.rootPublicKeyB64=<base64_pub_key>" ./cmd/xmdevenvmt/
//
// When empty or equal to the placeholder "REPLACE_WITH_REAL_PUBLIC_KEY",
// RootPublicKey() returns nil and signature verification is disabled.
// Generate a key pair with:
//
//	xdev keygen --pub-key manifests/pub.key --priv-key keys/priv.key
var rootPublicKeyB64 = ""

// embeddedSignaturesFS can be set at compile time to embed .sig files
// from the manifests directory. When nil, signatures are read from disk.
var embeddedSignaturesFS embed.FS

// SetEmbeddedSignaturesFS sets the embedded FS for signature files.
// Called during init from cmd/xmdevenvmt/main.go.
func SetEmbeddedSignaturesFS(fsys embed.FS) {
	embeddedSignaturesFS = fsys
}

// RootPublicKey returns the embedded Ed25519 public key.
// Returns nil if the key has not been set (empty string or placeholder).
// When nil is returned, manifest signature verification is skipped,
// which means downloads are NOT integrity-verified — this is a
// security risk in production deployments.
//
// The key can be injected at build time via -ldflags:
//
//	go build -ldflags "-X internal/signer.rootPublicKeyB64=<key>" ./cmd/xmdevenvmt/
func RootPublicKey() []byte {
	if rootPublicKeyB64 == "" || rootPublicKeyB64 == "REPLACE_WITH_REAL_PUBLIC_KEY" {
		if rootPublicKeyB64 == "" {
			log.Printf("[signer] WARNING: no public key embedded (rootPublicKeyB64 is empty); set via -ldflags or code — manifest signature verification is DISABLED")
		} else {
			log.Printf("[signer] WARNING: no real public key embedded (placeholder detected); manifest signature verification is DISABLED — downloads will not be integrity-verified")
		}
		return nil
	}
	pub, err := base64.StdEncoding.DecodeString(rootPublicKeyB64)
	if err != nil || len(pub) != 32 {
		return nil
	}
	return pub
}

// HasRootKey reports whether a real root public key is embedded.
func HasRootKey() bool {
	return RootPublicKey() != nil
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
