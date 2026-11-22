package cli

import (
	"crypto/ed25519"
	"github.com/xmdemt/xdev/internal/pm"
)

func signPMCatalog(dir, out string, key ed25519.PrivateKey) (bool, error) {
	return pm.SignFile(dir, out, key)
}
