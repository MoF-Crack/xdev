// Package signer provides Ed25519 manifest signing and verification.
//
// Security model:
//   - Developer signs manifests with a private Ed25519 key at release time
//   - The corresponding public key is embedded in the xdev binary at compile time
//   - At download time, xdev verifies the manifest signature before trusting
//     the SHA-256 hashes declared in the manifest
//   - This prevents MITM attacks that could inject malicious archives even
//     if the download channel (TLS) is compromised
package signer

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// ManifestSignature represents a signed manifest.
type ManifestSignature struct {
	Tool      string    `json:"tool"`
	SHA256Map string    `json:"sha256_map"` // JSON: {"os/arch": "sha256:..."}
	Timestamp time.Time `json:"timestamp"`
	Signature string    `json:"signature"` // Base64-encoded Ed25519 signature
}

// signaturePayload builds the bytes that get signed.
// The payload is: tool + "\n" + sha256_map + "\n" + timestamp RFC3339
func signaturePayload(tool, sha256Map string, ts time.Time) []byte {
	return []byte(tool + "\n" + sha256Map + "\n" + ts.Format(time.RFC3339))
}

// GenerateKeyPair creates a new Ed25519 key pair and writes them to files.
// The private key file is written with 0600 permissions.
func GenerateKeyPair(pubPath, privPath string) error {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("generate key pair: %w", err)
	}

	// Write public key (base64-encoded, 32 bytes)
	if err := os.WriteFile(pubPath, []byte(base64.StdEncoding.EncodeToString(pub)), 0o644); err != nil {
		return fmt.Errorf("write public key: %w", err)
	}

	// Write private key (base64-encoded, 64 bytes) with restricted permissions
	if err := os.WriteFile(privPath, []byte(base64.StdEncoding.EncodeToString(priv)), 0o600); err != nil {
		return fmt.Errorf("write private key: %w", err)
	}

	return nil
}

// LoadPrivateKey reads a base64-encoded Ed25519 private key from file.
func LoadPrivateKey(path string) (ed25519.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read private key: %w", err)
	}
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
	if err != nil {
		return nil, fmt.Errorf("decode private key: %w", err)
	}
	if len(seed) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid private key size: got %d, want %d", len(seed), ed25519.PrivateKeySize)
	}
	return ed25519.PrivateKey(seed), nil
}

// LoadPublicKey reads a base64-encoded Ed25519 public key from file.
func LoadPublicKey(path string) (ed25519.PublicKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read public key: %w", err)
	}
	pub, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
	if err != nil {
		return nil, fmt.Errorf("decode public key: %w", err)
	}
	if len(pub) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid public key size: got %d, want %d", len(pub), ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(pub), nil
}

// SignManifest signs a manifest's SHA-256 map with the given private key.
// sha256Map is a JSON-encoded map of "os/arch" → "sha256hash".
func SignManifest(privKey ed25519.PrivateKey, tool, sha256Map string) (*ManifestSignature, error) {
	ts := time.Now().UTC()
	payload := signaturePayload(tool, sha256Map, ts)
	sig := ed25519.Sign(privKey, payload)

	return &ManifestSignature{
		Tool:      tool,
		SHA256Map: sha256Map,
		Timestamp: ts,
		Signature: base64.StdEncoding.EncodeToString(sig),
	}, nil
}

// VerifySignature verifies a manifest signature against a public key.
func VerifySignature(pubKey ed25519.PublicKey, ms *ManifestSignature) bool {
	sigBytes, err := base64.StdEncoding.DecodeString(ms.Signature)
	if err != nil {
		return false
	}
	if len(sigBytes) != ed25519.SignatureSize {
		return false
	}
	payload := signaturePayload(ms.Tool, ms.SHA256Map, ms.Timestamp)
	return ed25519.Verify(pubKey, payload, sigBytes)
}

// WriteSignatureFile writes a ManifestSignature to a JSON file.
func WriteSignatureFile(path string, ms *ManifestSignature) error {
	data, err := json.MarshalIndent(ms, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal signature: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
}

// ReadSignatureFile reads a ManifestSignature from a JSON file.
func ReadSignatureFile(path string) (*ManifestSignature, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read signature file: %w", err)
	}
	var ms ManifestSignature
	if err := json.Unmarshal(data, &ms); err != nil {
		return nil, fmt.Errorf("parse signature file: %w", err)
	}
	return &ms, nil
}

// BuildSHA256Map extracts SHA-256 hashes from a manifest's archives
// and builds the JSON map used for signing.
// Input: map of "version" → map of "os/arch" → "sha256"
func BuildSHA256Map(versions map[string]map[string]string) string {
	data, _ := json.Marshal(versions)
	return string(data)
}

// ParseSHA256Map parses the JSON SHA-256 map from a signature.
func ParseSHA256Map(sha256Map string) (map[string]map[string]string, error) {
	var result map[string]map[string]string
	if err := json.Unmarshal([]byte(sha256Map), &result); err != nil {
		return nil, fmt.Errorf("parse sha256 map: %w", err)
	}
	return result, nil
}
