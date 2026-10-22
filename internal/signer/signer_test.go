package signer

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGenerateKeyPair(t *testing.T) {
	dir := t.TempDir()
	pubPath := filepath.Join(dir, "pub.key")
	privPath := filepath.Join(dir, "priv.key")

	if err := GenerateKeyPair(pubPath, privPath); err != nil {
		t.Fatalf("GenerateKeyPair failed: %v", err)
	}

	// Verify files exist
	if _, err := os.Stat(pubPath); err != nil {
		t.Fatalf("public key file not created: %v", err)
	}
	if _, err := os.Stat(privPath); err != nil {
		t.Fatalf("private key file not created: %v", err)
	}

	// Verify public key is valid base64
	pubData, err := os.ReadFile(pubPath)
	if err != nil {
		t.Fatalf("read public key: %v", err)
	}
	pubBytes, err := base64.StdEncoding.DecodeString(string(pubData))
	if err != nil {
		t.Fatalf("decode public key: %v", err)
	}
	if len(pubBytes) != ed25519.PublicKeySize {
		t.Errorf("public key size: got %d, want %d", len(pubBytes), ed25519.PublicKeySize)
	}

	// Verify private key is valid base64
	privData, err := os.ReadFile(privPath)
	if err != nil {
		t.Fatalf("read private key: %v", err)
	}
	privBytes, err := base64.StdEncoding.DecodeString(string(privData))
	if err != nil {
		t.Fatalf("decode private key: %v", err)
	}
	if len(privBytes) != ed25519.PrivateKeySize {
		t.Errorf("private key size: got %d, want %d", len(privBytes), ed25519.PrivateKeySize)
	}
}

func TestLoadPrivateKey(t *testing.T) {
	dir := t.TempDir()
	pubPath := filepath.Join(dir, "pub.key")
	privPath := filepath.Join(dir, "priv.key")

	if err := GenerateKeyPair(pubPath, privPath); err != nil {
		t.Fatalf("GenerateKeyPair failed: %v", err)
	}

	privKey, err := LoadPrivateKey(privPath)
	if err != nil {
		t.Fatalf("LoadPrivateKey failed: %v", err)
	}
	if len(privKey) != ed25519.PrivateKeySize {
		t.Errorf("private key size: got %d, want %d", len(privKey), ed25519.PrivateKeySize)
	}
}

func TestLoadPublicKey(t *testing.T) {
	dir := t.TempDir()
	pubPath := filepath.Join(dir, "pub.key")
	privPath := filepath.Join(dir, "priv.key")

	if err := GenerateKeyPair(pubPath, privPath); err != nil {
		t.Fatalf("GenerateKeyPair failed: %v", err)
	}

	pubKey, err := LoadPublicKey(pubPath)
	if err != nil {
		t.Fatalf("LoadPublicKey failed: %v", err)
	}
	if len(pubKey) != ed25519.PublicKeySize {
		t.Errorf("public key size: got %d, want %d", len(pubKey), ed25519.PublicKeySize)
	}
}

func TestLoadPrivateKey_InvalidSize(t *testing.T) {
	dir := t.TempDir()
	privPath := filepath.Join(dir, "bad_priv.key")
	// Write a key with wrong size
	os.WriteFile(privPath, []byte(base64.StdEncoding.EncodeToString([]byte("short"))), 0o600)

	_, err := LoadPrivateKey(privPath)
	if err == nil {
		t.Error("expected error for invalid private key size")
	}
}

func TestSignAndVerify(t *testing.T) {
	dir := t.TempDir()
	pubPath := filepath.Join(dir, "pub.key")
	privPath := filepath.Join(dir, "priv.key")

	if err := GenerateKeyPair(pubPath, privPath); err != nil {
		t.Fatalf("GenerateKeyPair failed: %v", err)
	}

	privKey, _ := LoadPrivateKey(privPath)
	pubKey, _ := LoadPublicKey(pubPath)

	sha256Map := BuildSHA256Map(map[string]map[string]string{
		"20.18.0": {
			"windows/amd64": "abc123def456",
			"linux/amd64":   "789ghi012jkl",
		},
	})

	sig, err := SignManifest(privKey, "node", sha256Map)
	if err != nil {
		t.Fatalf("SignManifest failed: %v", err)
	}

	if sig.Tool != "node" {
		t.Errorf("sig.Tool: got %q, want %q", sig.Tool, "node")
	}
	if sig.Signature == "" {
		t.Error("signature is empty")
	}
	if sig.Timestamp.IsZero() {
		t.Error("timestamp is zero")
	}

	// Verify with correct key
	if !VerifySignature(pubKey, sig) {
		t.Error("VerifySignature failed with correct key")
	}

	// Verify with wrong key
	wrongPub, _, _ := ed25519.GenerateKey(nil)
	if VerifySignature(wrongPub, sig) {
		t.Error("VerifySignature should fail with wrong key")
	}
}

func TestVerifySignature_TamperedPayload(t *testing.T) {
	dir := t.TempDir()
	pubPath := filepath.Join(dir, "pub.key")
	privPath := filepath.Join(dir, "priv.key")

	GenerateKeyPair(pubPath, privPath)
	privKey, _ := LoadPrivateKey(privPath)
	pubKey, _ := LoadPublicKey(pubPath)

	sha256Map := BuildSHA256Map(map[string]map[string]string{
		"3.12.0": {"windows/amd64": "original_hash"},
	})

	sig, _ := SignManifest(privKey, "python", sha256Map)

	// Tamper with the SHA-256 map
	sig.SHA256Map = BuildSHA256Map(map[string]map[string]string{
		"3.12.0": {"windows/amd64": "tampered_hash"},
	})

	if VerifySignature(pubKey, sig) {
		t.Error("VerifySignature should fail with tampered payload")
	}
}

func TestWriteAndReadSignatureFile(t *testing.T) {
	dir := t.TempDir()
	sigPath := filepath.Join(dir, "node.yaml.sig")

	original := &ManifestSignature{
		Tool:      "node",
		SHA256Map: `{"20.18.0":{"windows/amd64":"abc123"}}`,
		Timestamp: time.Now().UTC().Truncate(time.Second),
		Signature: "test_signature_base64",
	}

	if err := WriteSignatureFile(sigPath, original); err != nil {
		t.Fatalf("WriteSignatureFile failed: %v", err)
	}

	loaded, err := ReadSignatureFile(sigPath)
	if err != nil {
		t.Fatalf("ReadSignatureFile failed: %v", err)
	}

	if loaded.Tool != original.Tool {
		t.Errorf("Tool: got %q, want %q", loaded.Tool, original.Tool)
	}
	if loaded.SHA256Map != original.SHA256Map {
		t.Errorf("SHA256Map: got %q, want %q", loaded.SHA256Map, original.SHA256Map)
	}
	if loaded.Signature != original.Signature {
		t.Errorf("Signature: got %q, want %q", loaded.Signature, original.Signature)
	}
}

func TestBuildAndParseSHA256Map(t *testing.T) {
	original := map[string]map[string]string{
		"20.18.0": {
			"windows/amd64": "abc123",
			"linux/amd64":   "def456",
		},
		"18.20.4": {
			"windows/amd64": "ghi789",
		},
	}

	jsonStr := BuildSHA256Map(original)
	parsed, err := ParseSHA256Map(jsonStr)
	if err != nil {
		t.Fatalf("ParseSHA256Map failed: %v", err)
	}

	if len(parsed) != 2 {
		t.Errorf("parsed versions: got %d, want 2", len(parsed))
	}
	if parsed["20.18.0"]["windows/amd64"] != "abc123" {
		t.Errorf("parsed hash: got %q, want %q", parsed["20.18.0"]["windows/amd64"], "abc123")
	}
}

func TestRootPublicKey_Placeholder(t *testing.T) {
	// Default build should have placeholder key
	if RootPublicKey() != nil {
		t.Error("RootPublicKey should return nil for placeholder key")
	}
	if HasRootKey() {
		t.Error("HasRootKey should return false for placeholder key")
	}
}

// TestRootPublicKey_CorruptValueLogsAndReturnsNil (F-48): a corrupt
// embedded key (bad base64, or decodable but not 32 bytes) must still
// return nil — the documented "verification disabled" semantics — and
// must log an ERROR-level warning instead of failing silently.
func TestRootPublicKey_CorruptValueLogsAndReturnsNil(t *testing.T) {
	cases := []struct {
		name string
		b64  string
	}{
		{"invalid base64", "!!!not-base64!!!"},
		{"wrong length", base64.StdEncoding.EncodeToString([]byte("too-short"))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			SetRootPublicKeyForTesting(tc.b64)
			defer SetRootPublicKeyForTesting("")

			var buf bytes.Buffer
			prevWriter := log.Writer()
			prevFlags := log.Flags()
			log.SetOutput(&buf)
			log.SetFlags(0)
			defer func() {
				log.SetOutput(prevWriter)
				log.SetFlags(prevFlags)
			}()

			if got := RootPublicKey(); got != nil {
				t.Errorf("RootPublicKey() = %v, want nil for corrupt key", got)
			}
			if HasRootKey() {
				t.Error("HasRootKey() = true, want false for corrupt key")
			}
			out := buf.String()
			if !strings.Contains(out, "ERROR") {
				t.Errorf("corrupt root key produced no ERROR log; got %q", out)
			}
		})
	}
}

// TestRootPublicKey_ValidKeyReturnsKey guards the happy path against a
// regression where the new corrupt-key logging swallows valid keys.
func TestRootPublicKey_ValidKeyReturnsKey(t *testing.T) {
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = byte(i)
	}
	SetRootPublicKeyForTesting(base64.StdEncoding.EncodeToString(raw))
	defer SetRootPublicKeyForTesting("")

	got := RootPublicKey()
	if len(got) != 32 {
		t.Fatalf("RootPublicKey() length = %d, want 32", len(got))
	}
	if !bytes.Equal(got, raw) {
		t.Error("RootPublicKey() returned different bytes than injected")
	}
}

func TestSignatureRoundTrip(t *testing.T) {
	// Full round-trip: generate → sign → write → read → verify
	dir := t.TempDir()
	pubPath := filepath.Join(dir, "pub.key")
	privPath := filepath.Join(dir, "priv.key")
	sigPath := filepath.Join(dir, "node.yaml.sig")

	GenerateKeyPair(pubPath, privPath)
	privKey, _ := LoadPrivateKey(privPath)
	pubKey, _ := LoadPublicKey(pubPath)

	sha256Map := BuildSHA256Map(map[string]map[string]string{
		"20.18.0": {"windows/amd64": "a" + string(make([]byte, 63))},
	})

	sig, _ := SignManifest(privKey, "node", sha256Map)
	WriteSignatureFile(sigPath, sig)

	loaded, _ := ReadSignatureFile(sigPath)
	if !VerifySignature(pubKey, loaded) {
		t.Error("round-trip verification failed")
	}

	// Verify the JSON is valid
	var raw map[string]interface{}
	sigData, _ := os.ReadFile(sigPath)
	if err := json.Unmarshal(sigData, &raw); err != nil {
		t.Errorf("signature file is not valid JSON: %v", err)
	}
}
