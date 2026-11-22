package signer

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Reserve both names before writing either key. Only newly-created files are
// removed on failure. This is not a cross-directory atomic transaction.
func createPair(pubPath, privPath string, pub ed25519.PublicKey, priv ed25519.PrivateKey) (err error) {
	canonical := func(path string) (string, error) {
		abs, e := filepath.Abs(path)
		if e != nil {
			return "", e
		}
		parent, e := filepath.EvalSymlinks(filepath.Dir(abs))
		if e != nil {
			return "", e
		}
		return filepath.Join(parent, filepath.Base(abs)), nil
	}
	p, err := canonical(pubPath)
	if err != nil {
		return err
	}
	s, err := canonical(privPath)
	if err != nil {
		return err
	}
	if strings.EqualFold(p, s) {
		return fmt.Errorf("public and private key destinations must be distinct")
	}
	secret, err := createPrivateExclusive(s)
	if err != nil {
		return fmt.Errorf("create private key: %w", err)
	}
	files := []*os.File{secret}
	defer func() {
		identities := make([]os.FileInfo, len(files))
		for i, f := range files {
			var e error
			identities[i], e = f.Stat()
			err = errors.Join(err, e, f.Close())
		}
		if err == nil {
			return
		}
		for i, f := range files {
			current, e := os.Lstat(f.Name())
			if e == nil && identities[i] != nil && os.SameFile(current, identities[i]) {
				err = errors.Join(err, os.Remove(f.Name()))
			}
		}
	}()
	public, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return fmt.Errorf("create public key: %w", err)
	}
	files = append(files, public)
	if _, err = secret.WriteString(base64.StdEncoding.EncodeToString(priv)); err != nil {
		return err
	}
	if err = secret.Sync(); err != nil {
		return err
	}
	if _, err = public.WriteString(base64.StdEncoding.EncodeToString(pub)); err != nil {
		return err
	}
	return public.Sync()
}
