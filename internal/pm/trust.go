package pm

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/xmdemt/xdev/internal/signer"
	"github.com/xmdemt/xdev/internal/util"
)

// Source and approval are assigned by the loader, never deserialized from YAML.
type Origin struct{ Kind, Path, Digest string }

func (o Origin) Authorize(accepted string) error {
	switch o.Kind {
	case "builtin", "official":
		return nil
	case "local", "third-party":
		if len(o.Digest) == 64 && o.Digest == accepted {
			return nil
		}
		return fmt.Errorf("%s PM definition %s requires explicit trust for digest %s", o.Kind, o.Path, o.Digest)
	default:
		return errors.New("PM definition has unknown origin")
	}
}

const signatureDomain = "xdev:package-managers:ed25519:v1\x00"

func payload(data []byte) []byte { return append([]byte(signatureDomain), data...) }
func sum(data []byte) string     { return fmt.Sprintf("%x", sha256.Sum256(data)) }

// SignFile signs only the official source, never user-local or third-party definitions.
func SignFile(dir, out string, key ed25519.PrivateKey) (bool, error) {
	path := filepath.Join(dir, FileName)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if _, err = parse(data, path); err != nil {
		return false, err
	}
	if len(key) != ed25519.PrivateKeySize {
		return false, errors.New("invalid PM signing key")
	}
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(key, payload(data)))
	return true, util.AtomicWriteFile(filepath.Join(out, FileName+".sig"), []byte(sig+"\n"), 0644)
}

func official(dir string) ([]Def, error) {
	path := filepath.Join(dir, FileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	key, err := signer.RootPublicKeyChecked()
	if err != nil {
		return nil, err
	}
	sig, err := os.ReadFile(path + ".sig")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && len(key) == 0 {
			defs, parseErr := parse(data, path)
			orig := builtinDefs()
			if parseErr == nil && reflect.DeepEqual(defs, orig) {
				stamp(orig, "builtin", "xdev binary (unsigned identical disk copy ignored)", "")
				return orig, nil
			}
		}
		return nil, fmt.Errorf("official PM %s: signature required: %v", path, err)
	}
	if len(key) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("official PM %s: no release public key", path)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sig)))
	if err != nil || !ed25519.Verify(key, payload(data), raw) {
		return nil, fmt.Errorf("official PM %s: invalid signature", path)
	}
	defs, err := parse(data, path)
	if err != nil {
		return nil, err
	}
	stamp(defs, "official", path, sum(data))
	return defs, nil
}

func stamp(defs []Def, kind, path, digest string) {
	for i := range defs {
		d := &defs[i]
		if kind == "local" || kind == "third-party" {
			d.Name = kind + "/" + d.Name
		}
		hash := digest
		if hash == "" {
			b, _ := json.Marshal(d)
			hash = sum(b)
		}
		d.Origin = Origin{kind, path, hash}
	}
}

// Load uses the binary catalog when no external official file exists. Unkeyed
// development builds may also ignore a semantically identical unsigned copy.
// An invalid signature is always an error. Parse and execute the verified snapshot.
func Load(dir string) ([]Def, error) {
	if dir == "" {
		return Builtin(), nil
	}
	defs, err := official(dir)
	if errors.Is(err, os.ErrNotExist) {
		defs = Builtin()
	} else if err != nil {
		return nil, err
	}
	for _, kind := range []string{"local", "third-party"} {
		path := filepath.Join(dir, kind, FileName)
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		extra, err := parse(data, path)
		if err != nil {
			return nil, err
		}
		stamp(extra, kind, path, "")
		defs = append(defs, extra...)
	}
	return defs, nil
}

// Summary quotes all executable inputs, including detector arguments, for review.
func (d Def) Summary() string {
	b, _ := json.MarshalIndent(d, "", "  ")
	return fmt.Sprintf("source: %s\npath: %q\ndigest: %s\nshell: %s\nrequires_admin: %t\n%s", d.Origin.Kind, d.Origin.Path, d.Origin.Digest, d.ExecutionEnv, d.RequiresAdmin, b)
}
