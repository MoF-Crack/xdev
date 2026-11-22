package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"testing"
)

func TestReleaseKeyPair(t *testing.T) {
	p, s, _ := ed25519.GenerateKey(nil)
	q, _, _ := ed25519.GenerateKey(nil)
	e := base64.StdEncoding.EncodeToString
	if !validPair(e(p), e(s)) || validPair(e(q), e(s)) || validPair("", e(s)) || validPair(e(s), e(s)) || validPair(e(p)+"\n", e(s)) {
		t.Fatal("keypair validation")
	}
	s[63] ^= 1
	if validPair(e(p), e(s)) {
		t.Fatal("inconsistent private seed and public half")
	}
}
