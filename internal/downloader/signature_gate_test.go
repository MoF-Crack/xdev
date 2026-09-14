// Ticket 13 (audit-fixes / P0-02 phase 1 + P0-03) guard tests — signature
// chain integrity and the TOFU gating verdict.
//
// Red lines covered here:
//
//	G1  a NEW version whose mirror hash is not signed is rejected (attack
//	    server never receives a request that is accepted);
//	G2  a new version that REUSES an already-signed hash (cross-version /
//	    cross-os-arch) is rejected — the pre-fix whole-table fallback
//	    accepted it;
//	G6  TOFU write-back followed by a RE-INSTALL of the same version
//	    succeeds — the pre-fix code reported "hash tampering" because the
//	    written-back hash is not in the signed map.
package downloader

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/signer"
)

// --- test signing infrastructure -------------------------------------------

type testSigner struct{ priv ed25519.PrivateKey }

// injectTestRootKey generates an Ed25519 key pair, installs the public half
// as the process root key (HasRootKey()==true) and clears it on cleanup.
func injectTestRootKey(t *testing.T) testSigner {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	signer.SetRootPublicKeyForTesting(base64.StdEncoding.EncodeToString(pub))
	t.Cleanup(func() { signer.SetRootPublicKeyForTesting("") })
	return testSigner{priv: priv}
}

// sign produces the JSON envelope the installer/GUI attach to a download.
func (s testSigner) sign(t *testing.T, tool string, m map[string]map[string]string) []byte {
	t.Helper()
	ms, err := signer.SignManifest(s.priv, tool, signer.BuildSHA256Map(m))
	if err != nil {
		t.Fatalf("SignManifest: %v", err)
	}
	data, err := json.Marshal(ms)
	if err != nil {
		t.Fatalf("marshal signature: %v", err)
	}
	return data
}

func sha256Of(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// routesServer serves different payloads per path over HTTPS.
func routesServer(t *testing.T, routes map[string][]byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(payload)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// --- G1: tampered mirror on a new version ----------------------------------

func TestSignature_NewVersionTamperedMirror_Rejected(t *testing.T) {
	ts := injectTestRootKey(t)

	officialPayload := []byte("official 3.0.0 payload")
	attackPayload := []byte("attacker payload")
	srv := routesServer(t, map[string][]byte{
		"/official.zip": officialPayload,
		"/mirror.zip":   attackPayload,
	})
	officialHash := sha256Of(officialPayload)
	mirrorHash := sha256Of(attackPayload)

	// The version IS signed — for the official hash only. The disk manifest
	// under test was extended after signing with a mirror entry whose
	// mirror_sha256 is the attacker's own hash, so the signed-map
	// MEMBERSHIP check passes (ExpectedSHA matches) and the per-URL
	// coverage filter is the gate that must fire.
	sig := ts.sign(t, "demo", map[string]map[string]string{
		"3.0.0": {"windows/amd64": officialHash},
	})

	// Variant A: the mirror is the only candidate → everything is filtered
	// out and the download is refused outright.
	destA := filepath.Join(t.TempDir(), "a.zip")
	errA := Download(context.Background(), Options{
		URL: srv.URL + "/mirror.zip", Dest: destA, Client: srv.Client(),
		ExpectedSHA:       officialHash,
		URLShaMap:         map[string]string{srv.URL + "/mirror.zip": mirrorHash},
		ManifestSignature: sig, ToolName: "demo", Version: "3.0.0", OSArch: "windows/amd64",
		MaxRetries: 1,
	})
	if errA == nil {
		t.Fatal("mirror-only candidate with an unsigned hash must be rejected")
	}
	if !strings.Contains(errA.Error(), "covered by the manifest signature") {
		t.Errorf("error = %v, want the signed-coverage rejection", errA)
	}
	if _, statErr := os.Stat(destA); statErr == nil {
		t.Error("attacker payload must not have been written to disk")
	}

	// Variant B: mirror + official fallback → the mirror is dropped and the
	// official URL is used, so the attacker payload is never fetched.
	destB := filepath.Join(t.TempDir(), "b.zip")
	errB := Download(context.Background(), Options{
		URL: srv.URL + "/mirror.zip", Dest: destB, Client: srv.Client(),
		FallbackURLs:      []string{srv.URL + "/official.zip"},
		ExpectedSHA:       officialHash,
		URLShaMap:         map[string]string{srv.URL + "/mirror.zip": mirrorHash},
		ManifestSignature: sig, ToolName: "demo", Version: "3.0.0", OSArch: "windows/amd64",
		MaxRetries: 1,
	})
	if errB != nil {
		t.Fatalf("official fallback should have been used: %v", errB)
	}
	got, readErr := os.ReadFile(destB)
	if readErr != nil {
		t.Fatalf("read result: %v", readErr)
	}
	if string(got) != string(officialPayload) {
		t.Errorf("downloaded content = %q, want the official payload", got)
	}

	// Variant C: an entirely unsigned NEW version is refused by the
	// membership check before any URL is considered (defence in depth).
	destC := filepath.Join(t.TempDir(), "c.zip")
	sigOld := ts.sign(t, "demo", map[string]map[string]string{
		"1.0.0": {"windows/amd64": officialHash},
	})
	errC := Download(context.Background(), Options{
		URL: srv.URL + "/official.zip", Dest: destC, Client: srv.Client(),
		ExpectedSHA:       officialHash,
		ManifestSignature: sigOld, ToolName: "demo", Version: "3.0.0", OSArch: "windows/amd64",
		MaxRetries: 1,
	})
	if errC == nil {
		t.Fatal("a version absent from the signed map must be refused")
	}
}

// --- G2 (red line): reusing an already-signed hash --------------------------

func TestSignature_NewVersionReusesSignedHash_Rejected(t *testing.T) {
	ts := injectTestRootKey(t)
	signedA := strings.Repeat("c", 64)
	signedB := strings.Repeat("d", 64)
	sig := ts.sign(t, "demo", map[string]map[string]string{
		"1.0.0": {"windows/amd64": signedA, "linux/amd64": signedB},
	})

	// Cross-version: 3.0.0 is not in the signed map at all, even though its
	// declared hash equals the hash signed for 1.0.0 (the pre-fix
	// whole-table fallback accepted exactly this).
	err := verifyManifestSignature(Options{
		ExpectedSHA: signedA, ManifestSignature: sig,
		ToolName: "demo", Version: "3.0.0", OSArch: "windows/amd64",
	})
	if err == nil {
		t.Fatal("cross-version hash reuse must be rejected (G2 red line)")
	}
	if !strings.Contains(err.Error(), "hash tampering") {
		t.Errorf("error = %v, want a hash-tampering rejection", err)
	}

	// Cross-os/arch inside an unsigned version: same verdict.
	errB := verifyManifestSignature(Options{
		ExpectedSHA: signedB, ManifestSignature: sig,
		ToolName: "demo", Version: "3.0.0", OSArch: "windows/amd64",
	})
	if errB == nil {
		t.Fatal("cross-os-arch reuse inside an unsigned version must be rejected")
	}

	// Control: the version that IS signed still passes on its exact key.
	if errC := verifyManifestSignature(Options{
		ExpectedSHA: signedA, ManifestSignature: sig,
		ToolName: "demo", Version: "1.0.0", OSArch: "windows/amd64",
	}); errC != nil {
		t.Fatalf("exact signed key must pass: %v", errC)
	}
}

// --- G3: the normal official-URL path keeps working -------------------------

func TestSignature_OfficialURLNormal_StillWorks(t *testing.T) {
	ts := injectTestRootKey(t)
	payload := []byte("official normal payload")
	srv := routesServer(t, map[string][]byte{"/official.zip": payload})
	h := sha256Of(payload)
	sig := ts.sign(t, "demo", map[string]map[string]string{"1.0.0": {"windows/amd64": h}})

	dest := filepath.Join(t.TempDir(), "official.zip")
	err := Download(context.Background(), Options{
		URL: srv.URL + "/official.zip", Dest: dest, Client: srv.Client(),
		ExpectedSHA: h, ManifestSignature: sig,
		ToolName: "demo", Version: "1.0.0", OSArch: "windows/amd64", MaxRetries: 1,
	})
	if err != nil {
		t.Fatalf("signed official download failed: %v", err)
	}
	if _, statErr := os.Stat(dest); statErr != nil {
		t.Errorf("downloaded file missing: %v", statErr)
	}
}

// --- G4: old signatures keep working within the same version ----------------

func TestSignature_OldSig_SameVersionCrossPlatform_Passes(t *testing.T) {
	ts := injectTestRootKey(t)
	h := strings.Repeat("e", 64)
	// Old signature style: the same version keyed under a different
	// os/arch than the archive being installed.
	sig := ts.sign(t, "demo", map[string]map[string]string{"1.0.0": {"linux/amd64": h}})

	err := verifyManifestSignature(Options{
		ExpectedSHA: h, ManifestSignature: sig,
		ToolName: "demo", Version: "1.0.0", OSArch: "windows/amd64",
	})
	if err != nil {
		t.Fatalf("same-version cross-platform fallback must pass: %v", err)
	}
}

// --- G5: placeholder-key builds behave exactly as before --------------------

func TestSignature_PlaceholderKeyBuild_Unchanged(t *testing.T) {
	// No root key injected — the default (release) build form.
	payload := []byte("placeholder-key payload")
	srv := routesServer(t, map[string][]byte{"/a.zip": payload})
	h := sha256Of(payload)
	// A signature blob that would FAIL verification if a real key were
	// embedded: the whole signature branch must be skipped instead.
	bogusSig := []byte(`{"tool":"demo","sha256_map":"{\"9.9.9\":{\"windows/amd64\":\"deadbeef\"}}","timestamp":"2026-07-08T12:35:12Z","signature":"AAAA"}`)

	dest := filepath.Join(t.TempDir(), "a.zip")
	err := Download(context.Background(), Options{
		URL: srv.URL + "/a.zip", Dest: dest, Client: srv.Client(),
		ExpectedSHA: h, ManifestSignature: bogusSig,
		ToolName: "demo", Version: "3.0.0", OSArch: "windows/amd64", MaxRetries: 1,
	})
	if err != nil {
		t.Fatalf("placeholder-key build must keep the pre-batch behaviour: %v", err)
	}
	if _, statErr := os.Stat(dest); statErr != nil {
		t.Errorf("downloaded file missing: %v", statErr)
	}
}

// --- G6 (red line): TOFU write-back then re-install -------------------------

func TestTOFU_ReinstallAfterWriteback_Succeeds(t *testing.T) {
	ts := injectTestRootKey(t)
	payload := []byte("sniff-discovered payload")
	srv := routesServer(t, map[string][]byte{"/demo-2.0.0.zip": payload})
	realHash := sha256Of(payload)

	// The signed map covers only the pinned 1.0.0 — the sniff-discovered
	// 2.0.0 is legitimately absent from it.
	sig := ts.sign(t, "demo", map[string]map[string]string{
		"1.0.0": {"windows/amd64": strings.Repeat("a", 64)},
	})
	dest := filepath.Join(t.TempDir(), "demo-2.0.0.zip")
	base := Options{
		URL: srv.URL + "/demo-2.0.0.zip", Dest: dest, Client: srv.Client(),
		ManifestSignature: sig, ToolName: "demo", Version: "2.0.0", OSArch: "windows/amd64",
		UnsignedVersion: true, MaxRetries: 1,
	}

	// First install: the manifest carries no hash → TOFU records the real one.
	first := base
	first.ExpectedSHA = ""
	if err := Download(context.Background(), first); err != nil {
		t.Fatalf("first (TOFU) install failed: %v", err)
	}
	pin, err := os.ReadFile(dest + ".sha256")
	if err != nil || !strings.HasPrefix(string(pin), realHash) {
		t.Fatalf("TOFU pin not written (err=%v pin=%q)", err, string(pin))
	}

	// Re-install with the TOFU-written hash: the membership check and the
	// per-URL filter must be gated by UnsignedVersion, otherwise this is
	// misreported as hash tampering (the pre-fix behaviour).
	second := base
	second.ExpectedSHA = realHash
	if err := Download(context.Background(), second); err != nil {
		t.Fatalf("re-install after TOFU write-back failed: %v — the hash is real but not in the signed map (gate regression)", err)
	}
}

// --- Ticket 17 (P0-02 phase 2): mirror hash coverage -------------------------

// TestFilterSignatureCoveredURLs_MirrorKeyKeepsMirrorCandidate 证明签名图里以
// `os/arch#mirror` 记录的镜像哈希能被**真实过滤函数**消费：
//
//	java/Temurin 形态 = 官方哈希 + 独立的 mirror_sha256（镜像不是同一份字节）。
//	只签官方哈希时，镜像候选会被判「未入签」而剔除，阶段 1 的镜像通路失效；
//	票 17 第 3 项让签名图额外带上 `os/arch#mirror`，验证侧
//	verifySHAInMapForVersion 的同版本值扫描即可命中它。
//
// 反向半例保证用例非假绿：镜像 URL 的生效哈希既不是官方也不是已签镜像哈希时，
// 该候选仍必须被剔除。
func TestFilterSignatureCoveredURLs_MirrorKeyKeepsMirrorCandidate(t *testing.T) {
	s := injectTestRootKey(t)
	const (
		officialURL = "https://official.example/java-21.tar.gz"
		mirrorURL   = "https://mirror.example/java-21.tar.gz"
	)
	official := strings.Repeat("a", 64)
	mirror := strings.Repeat("b", 64)

	sig := s.sign(t, "java", map[string]map[string]string{
		"21.0.1": {
			"linux/amd64":        official,
			"linux/amd64#mirror": mirror,
		},
	})
	base := Options{
		ToolName: "java", Version: "21.0.1", OSArch: "linux/amd64",
		ExpectedSHA: official, ManifestSignature: sig,
		URLShaMap: map[string]string{officialURL: official, mirrorURL: mirror},
	}

	kept := filterSignatureCoveredURLs(base, []string{officialURL, mirrorURL})
	if len(kept) != 2 {
		t.Fatalf("kept = %v, want both URLs —— 已签的 mirror_sha256 必须让镜像候选通过 (P0-02 阶段 2)", kept)
	}

	// 反向前半（＝票 17 修复前的行为）：签名图**不含** #mirror 键时，镜像候选必须被
	// 剔除。这一半把「签名图必须带镜像哈希」写成可执行契约，而不是注释里的约定。
	noMirrorKey := base
	noMirrorKey.ManifestSignature = s.sign(t, "java", map[string]map[string]string{
		"21.0.1": {"linux/amd64": official},
	})
	kept = filterSignatureCoveredURLs(noMirrorKey, []string{officialURL, mirrorURL})
	if len(kept) != 1 || kept[0] != officialURL {
		t.Fatalf("kept = %v, want only the official URL —— 签名图缺 #mirror 键时镜像必被剔除（票 17 修复前）", kept)
	}

	// 反向后半：攻击者/新加的镜像带自己的哈希 → 必须剔除。
	evil := base
	evil.URLShaMap = map[string]string{officialURL: official, mirrorURL: strings.Repeat("c", 64)}
	kept = filterSignatureCoveredURLs(evil, []string{officialURL, mirrorURL})
	if len(kept) != 1 || kept[0] != officialURL {
		t.Fatalf("kept = %v, want only the official URL —— 未入签的镜像哈希必须被剔除", kept)
	}
}

// --- G9: cross-tool signature confusion ------------------------------------

func TestVerifyManifestSignature_ToolMismatchRejected(t *testing.T) {
	ts := injectTestRootKey(t)
	h := strings.Repeat("f", 64)
	sig := ts.sign(t, "other-tool", map[string]map[string]string{"1.0.0": {"windows/amd64": h}})

	err := verifyManifestSignature(Options{
		ExpectedSHA: h, ManifestSignature: sig,
		ToolName: "demo", Version: "1.0.0", OSArch: "windows/amd64",
	})
	if err == nil {
		t.Fatal("a signature issued for another tool must be rejected (N-02)")
	}
	if !strings.Contains(err.Error(), "tool mismatch") {
		t.Errorf("error = %v, want the cross-tool rejection", err)
	}
}
