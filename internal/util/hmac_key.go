// Package util provides common utility functions for the xdev project.
//
// This file (hmac_key.go) implements machine-specific HMAC key derivation
// for integrity verification of config and lockfile data. The key is
// derived from the current user's identity and machine name, ensuring
// that tampered files are detected on the same machine where they were
// created, while not preventing legitimate cross-machine config transfer
// (the HMAC simply won't verify, which is logged as a warning).
//
// P2-22 / F-14 —— 如实声明强度（仅注释，不改逻辑，保持与既有配置/锁文件
// 的兼容）：这不是"机密强度"的 MAC。密钥的全部输入（用户名、主机名、Windows
// SID）都是本机公开信息，盐 "xdev-integrity-v1" 是源码里的固定常量。因此：
//   - 它能防的是**误改**：手工编辑/脚本重写/传输损坏导致的字段变更会被发现；
//   - 它防不住**能写文件的攻击者**：对方在本机同样能读取用户名/主机名并自行
//     用本函数重算 HMAC。凡涉及真实攻击面（第三方 manifest、签名、下载完整性）
//     都必须走 Spec 013/021 的 Ed25519 签名链，而不是这个 HMAC。
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
// (username, hostname, SID) triple, which means:
//   - Files protected with this key will verify on the same machine/user
//   - Moving the config to a different machine will cause verification to
//     fail (logged as a WARNING, not an error — the user can re-save)
//
// 强度口径（P2-22 / F-14）：密钥与"用户环境"绑定，不是机密——username/
// hostname/SID 均可被本机任何进程读到，盐是固定常量。修复前此处的注释声称
// "读得到 config.yaml 但猜不到用户名+主机名的攻击者无法伪造"，该声明不成立
// （两者都不是秘密）。实际保证只有一条：文件的**非预期改动**（误编辑、脚本
// 覆盖、传输损坏）会在校验时暴露。防伪造请依赖 Ed25519 签名链。
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
