package update

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/signer"
)

const signedFixtureTag = "v1.0.0-rc.3"

func releaseVerificationKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	old := signer.RootPublicKey()
	t.Cleanup(func() { signer.SetRootPublicKeyForTesting(base64.StdEncoding.EncodeToString(old)) })
	signer.SetRootPublicKeyForTesting(base64.StdEncoding.EncodeToString(pub))
	return priv
}

func signedFixtureChecksums(t *testing.T, key ed25519.PrivateKey, tag string, sums []byte) []byte {
	t.Helper()
	signature, err := signer.SignReleaseChecksums(key, "github.com/MoF-Crack/xdev", tag, sums)
	if err != nil {
		t.Fatal(err)
	}
	data, err := signer.MarshalReleaseChecksumsSignature(signature)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestSignedReleaseIsRefusedWithoutAnEmbeddedRootKey(t *testing.T) {
	priv := releaseVerificationKey(t)
	sums := []byte("abc123  xdev-windows-amd64-v1.0.0-rc.3.exe\n")
	signature := signedFixtureChecksums(t, priv, signedFixtureTag, sums)
	signer.SetRootPublicKeyForTesting("")
	err := verifyReleaseChecksumsSignatureBytes(&ReleaseInfo{TagName: signedFixtureTag}, sums, signature)
	if err == nil || !strings.Contains(err.Error(), "no valid Ed25519 root key") {
		t.Fatalf("unsigned-root build accepted a signed release: %v", err)
	}
}

func TestCheckRequiresChecksumSignatureForRC3AndLater(t *testing.T) {
	legacy := releaseFixture("v1.0.0-rc.2", true)
	legacy["assets"] = legacy["assets"].([]Asset)[:2]
	for _, mode := range []string{"present", "missing", "duplicate", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			next := releaseFixture(signedFixtureTag, true)
			assets := next["assets"].([]Asset)
			want := "1.0.0-rc.2"
			switch mode {
			case "present":
				want = "1.0.0-rc.3"
			case "missing":
				assets = assets[:2]
			case "duplicate":
				assets = append(assets, assets[2])
			case "invalid":
				assets[2].Size = 0
			}
			next["assets"] = assets
			useUpdateTLSFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/repos/MoF-Crack/xdev/releases" {
					t.Errorf("unexpected discovery URL: %s", r.URL.Path)
					return
				}
				_ = json.NewEncoder(w).Encode([]map[string]any{legacy, next})
			})
			ctx := restrictRawUpdateTestTransport(t, func(r *http.Request) bool { return r.URL.Host == "api.github.com" })
			got, err := Check(ctx, "1.0.0-rc.1")
			if err != nil || !got.HasUpdate || got.LatestVersion != want {
				t.Fatalf("Check = %+v, %v; want update %s", got, err, want)
			}
		})
	}
}

// Both installation flows use the actual bounded HTTPS download and signature
// verifier. The fixture is inert text and is never installed or executed.
func TestUpdateConsumersVerifyReleaseChecksumSignatures(t *testing.T) {
	priv := releaseVerificationKey(t)
	payload := []byte("release checksum test fixture")
	for _, tc := range []struct{ name, tag, wantError string }{
		{"valid", signedFixtureTag, ""},
		{"legacy without signature", "v1.0.0-rc.2", ""},
		{"missing", signedFixtureTag, "missing its required"},
		{"changed checksums", signedFixtureTag, "invalid SHA256SUMS.sig signature"},
		{"different tag", signedFixtureTag, "invalid SHA256SUMS.sig signature"},
		{"empty signature on legacy", "v1.0.0-rc.2", "empty SHA256SUMS.sig"},
		{"payload mismatch", signedFixtureTag, "hash mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			name := assetNameForPlatform(tc.tag)
			originalSums := []byte(fmt.Sprintf("%x  %s\n", sha256.Sum256(payload), name))
			sums, signedTag := originalSums, tc.tag
			if tc.name == "changed checksums" {
				sums = append(append([]byte(nil), originalSums...), '\n')
			}
			if tc.name == "different tag" {
				signedTag = "v1.0.0-rc.4"
			}
			signature := signedFixtureChecksums(t, priv, signedTag, originalSums)
			if tc.name == "empty signature on legacy" {
				signature = nil
			}
			server := useUpdateTLSFixture(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/sums":
					_, _ = w.Write(sums)
				case "/signature":
					_, _ = w.Write(signature)
				default:
					http.NotFound(w, r)
				}
			})
			ctx := restrictRawUpdateTestTransport(t, func(r *http.Request) bool { return strings.HasPrefix(r.URL.String(), server.URL+"/") })
			release := &ReleaseInfo{TagName: tc.tag, Assets: []Asset{{Name: "SHA256SUMS", BrowserDownloadURL: server.URL + "/sums", Size: int64(len(sums))}}}
			if tc.name != "missing" && tc.name != "legacy without signature" {
				release.Assets = append(release.Assets, Asset{Name: checksumsSignatureAssetName, BrowserDownloadURL: server.URL + "/signature", Size: int64(max(1, len(signature)))})
			}
			filePath := filepath.Join(t.TempDir(), "fixture")
			contents := payload
			if tc.name == "payload mismatch" {
				contents = []byte("different fixture")
			}
			if err := os.WriteFile(filePath, contents, 0600); err != nil {
				t.Fatal(err)
			}
			file, err := os.Open(filePath)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			_, restartErr := verifyPreparedRestartDownload(ctx, release, name, file)
			for path, err := range map[string]error{"direct": verifySHA256(ctx, release, name, filePath), "restart": restartErr} {
				if tc.wantError == "" && err != nil || tc.wantError != "" && (err == nil || !strings.Contains(err.Error(), tc.wantError)) {
					t.Errorf("%s verification = %v; want %q", path, err, tc.wantError)
				}
			}
		})
	}
}
