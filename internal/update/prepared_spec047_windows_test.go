//go:build windows

package update

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSpec047PrepareRawAndLegacyZIPNeverPublishBeforeRestart(t *testing.T) {
	peBytes := spec047OwnExecutable(t)
	for _, archive := range []bool{false, true} {
		t.Run(map[bool]string{false: "direct-exe", true: "legacy-zip"}[archive], func(t *testing.T) {
			bytes := peBytes
			if archive {
				bytes = updateArchiveFixture(t, peBytes, binaryNameInArchive())
			}
			server, _ := spec047FixtureTLS(t, map[string][]byte{"ok": bytes}, archive)
			result := spec047CheckResult(server.URL, "ok", archive)
			target := filepath.Join(t.TempDir(), "xdev.exe")
			old := []byte("current executable left untouched")
			if err := os.WriteFile(target, old, 0600); err != nil {
				t.Fatal(err)
			}
			ctx := context.WithValue(context.Background(), networkContextKey{}, updateNetwork{transport: server.Client().Transport})
			var phases []string
			ready, err := PrepareForRestart(ctx, result, target, func(p Progress) {
				if len(phases) == 0 || phases[len(phases)-1] != p.Phase {
					phases = append(phases, p.Phase)
				}
			})
			if err != nil {
				t.Fatal(err)
			}
			staged := ready.stage
			want := []string{"downloading", "verifying", "ready"}
			if archive {
				want = []string{"downloading", "verifying", "extracting", "ready"}
			}
			if !equalStrings(phases, want) {
				t.Fatalf("download misreported installation: %v want %v", phases, want)
			}
			assertRunningBinaryUnchanged(t, target, old)
			got, err := os.ReadFile(filepath.Join(staged, restartPayloadName))
			if err != nil || sha256.Sum256(got) != sha256.Sum256(peBytes) {
				t.Fatalf("wrong prepared PE: %v", err)
			}
			if err := ready.Discard(); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(staged); !os.IsNotExist(err) {
				t.Fatalf("discard left verified payload: %v", err)
			}
			assertRunningBinaryUnchanged(t, target, old)
		})
	}
}

func TestSpec047PrepareRejectsChecksumAndInvalidPEWithoutChangingTarget(t *testing.T) {
	for _, tc := range []struct {
		name      string
		payload   []byte
		wrongSHA  bool
		errorText string
	}{
		{"bad-sha", spec047OwnExecutable(t), true, "hash mismatch"},
		{"not-a-pe", []byte("downloaded text cannot be executed"), false, "PE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			name := rawExeNameForTarget("1.0.0-rc.2", runtime.GOARCH)
			server := useUpdateTLSFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/payload" {
					_, _ = w.Write(tc.payload)
					return
				}
				sum := sha256.Sum256(tc.payload)
				if tc.wrongSHA {
					sum[0] ^= 0xff
				}
				fmt.Fprintf(w, "%x  %s\n", sum, name)
			})
			result := &CheckResult{LatestVersion: "1.0.0-rc.2", HasUpdate: true, Release: &ReleaseInfo{TagName: "v1.0.0-rc.2", Assets: []Asset{{Name: name, BrowserDownloadURL: server.URL + "/payload", Size: int64(len(tc.payload))}, {Name: "SHA256SUMS", BrowserDownloadURL: server.URL + "/sums", Size: 100}}}}
			target := filepath.Join(t.TempDir(), "xdev.exe")
			old := []byte("old program")
			if err := os.WriteFile(target, old, 0600); err != nil {
				t.Fatal(err)
			}
			ready, err := PrepareForRestart(context.Background(), result, target, nil)
			if err == nil || ready != nil || !strings.Contains(err.Error(), tc.errorText) {
				t.Fatalf("invalid preparation accepted: %v %+v", err, ready)
			}
			assertRunningBinaryUnchanged(t, target, old)
			entries, _ := os.ReadDir(filepath.Dir(target))
			if len(entries) != 1 {
				t.Fatalf("failed preparation left staging: %+v", entries)
			}
		})
	}
}
