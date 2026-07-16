// Package util provides common utility functions for the xdev project.
//
// This file (hmac_key.go) implements machine-specific HMAC key derivation
// for integrity verification of config and lockfile data. The key is
// derived from the current user's identity and machine name, ensuring
// that tampered files are detected on the same machine where they were
// created, while not preventing legitimate cross-machine config transfer
// (the HMAC simply won't verify, which is logged as a warning).
package util

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/user"
	"runtime"
)

// MachineHMACKey derives a 32-byte HMAC-SHA256 key from the current
// machine and user identity. The key is deterministic for a given
// (username, hostname) pair, which means:
//   - Files protected with this key will verify on the same machine/user
//   - Moving the config to a different machine will cause verification to
//     fail (logged as a WARNING, not an error — the user can re-save)
//   - An attacker who can read config.yaml but cannot guess both the
//     username and hostname cannot forge the HMAC
func MachineHMACKey() []byte {
	var username, hostname string

	if u, err := user.Current(); err == nil && u.Username != "" {
		username = u.Username
	} else {
		// Fallback: read from environment
		username = os.Getenv("USER")
		if username == "" {
			username = os.Getenv("USERNAME")
		}
	}

	if h, err := os.Hostname(); err == nil {
		hostname = h
	}

	// On Windows, also incorporate the user SID if available for stronger binding
	sid := ""
	if runtime.GOOS == "windows" {
		if u, err := user.Current(); err == nil && u.Uid != "" {
			sid = u.Uid
		}
	}

	// Derive key using SHA-256 over the identity components
	// The salt "xdev-integrity-v1" ensures this key is distinct from
	// any other SHA-256 usage in the codebase
	h := sha256.New()
	h.Write([]byte("xdev-integrity-v1"))
	h.Write([]byte(username))
	h.Write([]byte{0}) // separator
	h.Write([]byte(hostname))
	h.Write([]byte{0})
	h.Write([]byte(sid))
	h.Write([]byte{0})

	return h.Sum(nil)
}

// ComputeHMAC computes the HMAC-SHA256 of data using the machine-specific
// key. Returns the hex-encoded HMAC string.
// Security (N-12): uses the standard library crypto/hmac instead of a
// custom implementation to ensure cryptographic correctness.
func ComputeHMAC(data []byte) string {
	key := MachineHMACKey()
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyHMAC checks whether the given hex-encoded HMAC matches the
// computed HMAC of data using the machine-specific key.
// Security (N-11): uses hmac.Equal for constant-time comparison to
// prevent timing attacks on the HMAC verification.
func VerifyHMAC(data []byte, hexHMAC string) bool {
	expected, err := hex.DecodeString(hexHMAC)
	if err != nil {
		return false
	}
	key := MachineHMACKey()
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return hmac.Equal(mac.Sum(nil), expected)
}
