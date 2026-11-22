package pm

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/signer"
)

const fixture = `package_managers:
  - name: probe
    detect_cmd: probe-version
    detect_args: [--probe-version]
    install_command: fixture install
    execution_env: bash
    requires_admin: true
    platform: unix
    enable_command: fixture enable
    parent_tool: fixture-parent
    tool_name: fixture-tool
    install_methods:
      - name: fixture-method
        command: fixture add
        detect_cmd: fixture-detect
`

func writeFixture(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}
func testKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer.SetRootPublicKeyForTesting(base64.StdEncoding.EncodeToString(pub))
	t.Cleanup(func() { signer.SetRootPublicKeyForTesting("") })
	return priv
}
func TestOfficialUnsignedRejected(t *testing.T) {
	testKey(t)
	dir := t.TempDir()
	writeFixture(t, filepath.Join(dir, FileName), fixture)
	if defs, err := Load(dir); err == nil || len(defs) != 0 {
		t.Fatal("unsigned official executable definition accepted")
	}
}
func TestPMCompleteSignedContent(t *testing.T) {
	key := testKey(t)
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	writeFixture(t, path, fixture)
	if ok, err := SignFile(dir, dir, key); err != nil || !ok {
		t.Fatal(ok, err)
	}
	defs, err := Load(dir)
	if err != nil || len(defs) != 1 {
		t.Fatal(defs, err)
	}
	d := defs[0]
	if d.Origin.Kind != "official" || d.Origin.Path != path || d.Origin.Digest != sum([]byte(fixture)) || d.Origin.Authorize("") != nil {
		t.Fatal(d.Origin)
	}
	for _, s := range []string{"name: probe", "probe-version", "--probe-version", "fixture install", "bash", "true", "unix", "fixture enable", "fixture-parent", "fixture-tool", "fixture-method", "fixture add", "fixture-detect"} {
		t.Run(s, func(t *testing.T) {
			writeFixture(t, path, strings.Replace(fixture, s, s+"x", 1))
			if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "invalid signature") {
				t.Fatalf("changed %s: %v", s, err)
			}
		})
	}
	writeFixture(t, path, fixture)
	writeFixture(t, path+".sig", base64.StdEncoding.EncodeToString(ed25519.Sign(key, []byte(fixture))))
	if _, err := Load(dir); err == nil {
		t.Fatal("accepted signature outside PM domain")
	}
}
func TestPMApprovalBoundToContentsAndNamespace(t *testing.T) {
	dir := t.TempDir()
	for _, kind := range []string{"local", "third-party"} {
		if err := os.Mkdir(filepath.Join(dir, kind), 0700); err != nil {
			t.Fatal(err)
		}
		writeFixture(t, filepath.Join(dir, kind, FileName), strings.Replace(fixture, "name: probe", "name: scoop", 1))
	}
	defs, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	official, _ := Find(defs, "scoop")
	custom, _ := Find(defs, "local/scoop")
	third, _ := Find(defs, "third-party/scoop")
	if official.Origin.Kind != "builtin" || custom.Origin.Kind != "local" || third.Origin.Kind != "third-party" || custom.Origin.Authorize("") == nil || custom.Origin.Authorize(custom.Origin.Digest) != nil || third.Origin.Authorize(custom.Origin.Digest) == nil {
		t.Fatal(official.Origin, custom.Origin, third.Origin)
	}
	writeFixture(t, custom.Origin.Path, strings.Replace(strings.Replace(fixture, "name: probe", "name: scoop", 1), "fixture install", "fixture changed", 1))
	defs, err = Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	changed, _ := Find(defs, "local/scoop")
	if changed.Origin.Authorize(custom.Origin.Digest) == nil {
		t.Fatal("stale digest accepted")
	}
}
func TestPMUnkeyedShippedCopy(t *testing.T) {
	defs, err := Load(repoManifestsDir(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != len(Builtin()) || defs[0].Origin.Kind != "builtin" {
		t.Fatal("unsigned shipped data must not claim official identity")
	}
}
