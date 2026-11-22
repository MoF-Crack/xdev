// Command checkpair verifies release environment secrets without echoing or
// persisting either value. Run only in the secret-scoped release step.
package main

import (
	"crypto/ed25519"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"os"
)

func validPair(pubText, privText string) bool {
	decode := func(text string, n int) []byte {
		b, err := base64.StdEncoding.Strict().DecodeString(text)
		if err != nil || len(b) != n || base64.StdEncoding.EncodeToString(b) != text {
			return nil
		}
		return b
	}
	pub := decode(pubText, ed25519.PublicKeySize)
	priv := decode(privText, ed25519.PrivateKeySize)
	if pub == nil || priv == nil {
		return false
	}
	derived := ed25519.NewKeyFromSeed(priv[:ed25519.SeedSize])
	return subtle.ConstantTimeCompare(derived, priv) == 1 && subtle.ConstantTimeCompare(derived[ed25519.SeedSize:], pub) == 1
}
func main() {
	if !validPair(os.Getenv("XDEV_SIGNING_PUBLIC_KEY"), os.Getenv("XDEV_SIGNING_PRIVATE_KEY")) {
		fmt.Fprintln(os.Stderr, "release signing keys invalid or do not match")
		os.Exit(1)
	}
}
