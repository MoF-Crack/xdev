package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xmdemt/xdev/internal/config"
)

func updateArchiveFixture(t *testing.T, payload []byte, member string) []byte {
	t.Helper()
	var buf bytes.Buffer
	if runtime.GOOS == "windows" {
		zw := zip.NewWriter(&buf)
		w, err := zw.CreateHeader(&zip.FileHeader{Name: member, Method: zip.Store})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(payload); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
	} else {
		gz, err := gzip.NewWriterLevel(&buf, gzip.NoCompression)
		if err != nil {
			t.Fatal(err)
		}
		tw := tar.NewWriter(gz)
		if err := tw.WriteHeader(&tar.Header{Name: member, Typeflag: tar.TypeReg, Mode: 0o755, Size: int64(len(payload))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(payload); err != nil {
			t.Fatal(err)
		}
		if err := tw.Close(); err != nil {
			t.Fatal(err)
		}
		if err := gz.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return buf.Bytes()
}

func updateTargetFixture(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("XMDEV_HOME", home)
	config.ResetHomeDirForTest()
	t.Cleanup(config.ResetHomeDirForTest)
	exe := filepath.Join(home, binaryNameInArchive())
	if err := os.WriteFile(exe, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	return exe
}

func updateResultFixture(serverURL string, archive []byte) *CheckResult {
	return &CheckResult{
		CurrentVersion: "1.0.0-beta.1", LatestVersion: "1.0.0-beta.2", HasUpdate: true,
		Release: &ReleaseInfo{TagName: "v1.0.0-beta.2", Prerelease: true, Assets: []Asset{
			{Name: assetNameForPlatform("1.0.0-beta.2"), BrowserDownloadURL: serverURL + "/archive", Size: int64(len(archive))},
			{Name: "SHA256SUMS", BrowserDownloadURL: serverURL + "/sums"},
		}},
	}
}

func assertOldUpdateTarget(t *testing.T, exe string) {
	t.Helper()
	got, err := os.ReadFile(exe)
	if err != nil || string(got) != "old binary" {
		t.Fatalf("failed/cancelled update changed live binary: bytes=%q err=%v", got, err)
	}
	entries, err := os.ReadDir(filepath.Dir(exe))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".xdev-update-") || strings.HasSuffix(entry.Name(), ".new") {
			t.Errorf("update staging leaked after failure: %s", entry.Name())
		}
	}
}

func assertNoUpdateCompletion(t *testing.T, events []Progress) {
	t.Helper()
	for _, event := range events {
		if event.Phase == "completed" || event.Percent == 100 {
			t.Fatalf("error reported fake completion: %+v", event)
		}
	}
}

func TestApplyToWithProgressRealBytesAndCommit(t *testing.T) {
	exe := updateTargetFixture(t)
	payload := bytes.Repeat([]byte("real new binary bytes"), 8192)
	archive := updateArchiveFixture(t, payload, binaryNameInArchive())
	assetName := assetNameForPlatform("1.0.0-beta.2")
	server := useUpdateTLSFixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/archive":
			w.Header().Set("Content-Length", strconv.Itoa(len(archive)))
			for start := 0; start < len(archive); start += 32768 {
				end := min(start+32768, len(archive))
				if _, err := w.Write(archive[start:end]); err != nil {
					return
				}
				w.(http.Flusher).Flush()
				if end < len(archive) {
					time.Sleep(120 * time.Millisecond)
				}
			}
		case "/sums":
			_, _ = fmt.Fprintf(w, "%x  %s\n", sha256.Sum256(archive), assetName)
		default:
			http.NotFound(w, r)
		}
	})
	result := updateResultFixture(server.URL, archive)
	result.Release.Assets[0].Size = 1 // Content-Length, then actual EOF, correct stale metadata.
	var events []Progress
	err := ApplyToWithProgress(context.Background(), result, exe, func(p Progress) {
		events = append(events, p)
		if p.Phase != "completed" {
			got, readErr := os.ReadFile(exe)
			if readErr != nil || string(got) != "old binary" {
				t.Errorf("binary published before completion: phase=%s err=%v", p.Phase, readErr)
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	var phases []string
	partial := false
	var previous int64
	for i, event := range events {
		if i == 0 || event.Phase != events[i-1].Phase {
			phases = append(phases, event.Phase)
		}
		if event.DownloadedBytes < previous || event.DownloadedBytes > int64(len(archive)) {
			t.Fatalf("not actual/monotonic archive bytes: %+v", events)
		}
		previous = event.DownloadedBytes
		if event.Phase == "downloading" && event.DownloadedBytes > 0 && event.DownloadedBytes < int64(len(archive)) {
			partial = true
			if event.TotalBytes != int64(len(archive)) || event.Percent != min(99, int(event.DownloadedBytes*100/int64(len(archive)))) {
				t.Errorf("download progress not derived from bytes: %+v", event)
			}
		}
		if event.Phase != "completed" && event.Percent == 100 {
			t.Errorf("reported completion before install: %+v", event)
		}
	}
	if !reflect.DeepEqual(phases, []string{"downloading", "verifying", "extracting", "installing", "completed"}) || !partial {
		t.Fatalf("want real streaming progress and ordered phases, got %v (partial=%v)", phases, partial)
	}
	last := events[len(events)-1]
	if last.Percent != 100 || last.DownloadedBytes != int64(len(archive)) || last.TotalBytes != int64(len(archive)) {
		t.Fatalf("completion bytes do not match downloaded archive: %+v", last)
	}
	got, err := os.ReadFile(exe)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("successful apply did not install verified binary: %v", err)
	}
}

func TestApplyToWithProgressFailuresDoNotReplace(t *testing.T) {
	for _, mode := range []string{"hash mismatch", "missing sums", "sums HTTP failure", "archive HTTP failure", "truncated download", "invalid archive", "missing root binary", "replace failure"} {
		t.Run(mode, func(t *testing.T) {
			exe := updateTargetFixture(t)
			member := binaryNameInArchive()
			if mode == "missing root binary" {
				member = "gui/" + member
			}
			archive := updateArchiveFixture(t, []byte("new binary"), member)
			if mode == "invalid archive" {
				archive = []byte("not an archive")
			}
			server := useUpdateTLSFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/archive" {
					if mode == "archive HTTP failure" {
						http.Error(w, "unavailable", http.StatusNotFound)
						return
					}
					if mode == "truncated download" {
						w.Header().Set("Content-Length", strconv.Itoa(len(archive)+5000))
					}
					_, _ = w.Write(archive)
					return
				}
				if mode == "sums HTTP failure" {
					http.Error(w, "unavailable", http.StatusServiceUnavailable)
					return
				}
				hash := sha256.Sum256(archive)
				if mode == "hash mismatch" {
					hash = sha256.Sum256([]byte("different archive"))
				}
				_, _ = fmt.Fprintf(w, "%x  %s\n", hash, assetNameForPlatform("1.0.0-beta.2"))
			})
			result := updateResultFixture(server.URL, archive)
			if mode == "missing sums" {
				result.Release.Assets = result.Release.Assets[:1]
			}
			var events []Progress
			err := ApplyToWithProgress(context.Background(), result, exe, func(p Progress) {
				events = append(events, p)
				if mode == "replace failure" && p.Phase == "installing" {
					entries, readErr := os.ReadDir(filepath.Dir(exe))
					if readErr != nil {
						t.Fatal(readErr)
					}
					for _, entry := range entries {
						if strings.HasPrefix(entry.Name(), ".xdev-update-bin-") {
							if removeErr := os.Remove(filepath.Join(filepath.Dir(exe), entry.Name())); removeErr != nil {
								t.Fatal(removeErr)
							}
						}
					}
				}
			})
			if err == nil {
				t.Fatal("fixture failure must return an error")
			}
			if mode == "hash mismatch" && !strings.Contains(err.Error(), "hash mismatch") {
				t.Fatalf("want SHA verification failure, got %v", err)
			}
			if mode == "hash mismatch" {
				for _, event := range events {
					if event.Phase == "extracting" || event.Phase == "installing" {
						t.Fatalf("hash failure reached application: %+v", event)
					}
				}
				last := events[len(events)-1]
				if last.Phase != "verifying" || last.DownloadedBytes != int64(len(archive)) {
					t.Fatalf("hash error lost actual download progress: %+v", last)
				}
			}
			assertNoUpdateCompletion(t, events)
			assertOldUpdateTarget(t, exe)
		})
	}
}

func TestApplyToWithProgressCancellationBeforeCommit(t *testing.T) {
	for _, phase := range []string{"downloading", "verifying", "extracting", "installing"} {
		t.Run(phase, func(t *testing.T) {
			exe := updateTargetFixture(t)
			archive := updateArchiveFixture(t, bytes.Repeat([]byte("new binary"), 65536), binaryNameInArchive())
			server := useUpdateTLSFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/archive" {
					w.Header().Set("Content-Length", strconv.Itoa(len(archive)))
					_, _ = w.Write(archive)
					return
				}
				_, _ = fmt.Fprintf(w, "%x  %s\n", sha256.Sum256(archive), assetNameForPlatform("1.0.0-beta.2"))
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var events []Progress
			err := ApplyToWithProgress(ctx, updateResultFixture(server.URL, archive), exe, func(p Progress) {
				events = append(events, p)
				if p.Phase == phase && (phase != "downloading" || p.DownloadedBytes > 0) {
					cancel()
				}
			})
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("want context cancellation, got %v", err)
			}
			assertNoUpdateCompletion(t, events)
			assertOldUpdateTarget(t, exe)
		})
	}
}

func TestApplyToLegacyDelegatesAndAcceptsNilProgress(t *testing.T) {
	exe := updateTargetFixture(t)
	archive := updateArchiveFixture(t, []byte("new binary"), binaryNameInArchive())
	server := useUpdateTLSFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/archive" {
			_, _ = w.Write(archive)
			return
		}
		_, _ = fmt.Fprintf(w, "%x  %s\n", sha256.Sum256(archive), assetNameForPlatform("1.0.0-beta.2"))
	})
	if err := ApplyTo(context.Background(), updateResultFixture(server.URL, archive), exe); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(exe)
	if err != nil || string(got) != "new binary" {
		t.Fatalf("legacy ApplyTo no longer applies update: %q / %v", got, err)
	}
	if err := ApplyToWithProgress(context.Background(), nil, exe, nil); err == nil {
		t.Fatal("nil result must fail without a panic")
	}
}

// Cancel through the real context at a selected Err checkpoint. This makes
// cancellation between Windows backup/commit and after fallback staging
// deterministic, without replacing filesystem primitives or sleeping.
type cancelAtCheckpoint struct {
	context.Context
	cancel context.CancelFunc
	after  int
	calls  int
}

func (c *cancelAtCheckpoint) Err() error {
	c.calls++
	if c.calls == c.after {
		c.cancel()
	}
	return c.Context.Err()
}

func TestReplacementCancellationAtPublication(t *testing.T) {
	t.Run("Windows backup rolls back on cancel", func(t *testing.T) {
		exe := updateTargetFixture(t)
		staged := filepath.Join(filepath.Dir(exe), "staged")
		if err := os.WriteFile(staged, []byte("new binary"), 0o755); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		checkpoint := &cancelAtCheckpoint{Context: ctx, cancel: cancel, after: 3}
		if err := replaceBinaryWindowsWithContext(checkpoint, exe, staged); !errors.Is(err, context.Canceled) {
			t.Fatalf("want cancellation after backup, got %v", err)
		}
		assertOldUpdateTarget(t, exe)
		if _, err := os.Stat(staged); err != nil {
			t.Fatal("cancelled replacement consumed new binary")
		}
	})
	t.Run("copy fallback cancels after staging", func(t *testing.T) {
		exe := updateTargetFixture(t)
		staged := filepath.Join(filepath.Dir(exe), "staged")
		if err := os.WriteFile(staged, []byte("new binary"), 0o755); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		checkpoint := &cancelAtCheckpoint{Context: ctx, cancel: cancel, after: 4}
		if err := copyAndReplaceWithContext(checkpoint, exe, staged); !errors.Is(err, context.Canceled) {
			t.Fatalf("want cancellation before fallback commit, got %v (checks=%d)", err, checkpoint.calls)
		}
		assertOldUpdateTarget(t, exe)
	})
}

func TestDownloadProgressUnknownLength(t *testing.T) {
	payload := bytes.Repeat([]byte("chunked bytes"), 4096)
	server := useUpdateTLSFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.(http.Flusher).Flush() // Force chunking without a guessed Content-Length.
		_, _ = w.Write(payload)
	})
	var events []Progress
	reporter := &progressReporter{callback: func(p Progress) { events = append(events, p) }}
	var out bytes.Buffer
	if err := downloadAssetWithProgress(context.Background(), server.URL, &out, 0, reporter); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out.Bytes(), payload) || len(events) < 2 {
		t.Fatalf("chunked download failed: bytes=%d events=%+v", out.Len(), events)
	}
	if events[0].TotalBytes != 0 || events[0].Percent != 0 {
		t.Fatal("unknown length invented a total or percentage")
	}
	last := events[len(events)-1]
	if last.DownloadedBytes != int64(len(payload)) || last.TotalBytes != int64(len(payload)) || last.Percent != 99 {
		t.Fatalf("EOF must report counted bytes without application success: %+v", last)
	}
	assertNoUpdateCompletion(t, events)
}

func TestProgressJSONAndThrottle(t *testing.T) {
	p := Progress{Phase: "downloading", DownloadedBytes: 25, TotalBytes: 100, Percent: 25}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"phase":"downloading","downloadedBytes":25,"totalBytes":100,"percent":25}` {
		t.Fatalf("GUI progress JSON contract changed: %s", data)
	}
	var events []Progress
	reporter := &progressReporter{callback: func(p Progress) { events = append(events, p) }, value: Progress{Phase: "downloading", TotalBytes: 100}}
	// Pin the notification time ahead to make throttling independent of CI speed.
	reporter.lastNotify = time.Now().Add(time.Hour)
	for i := 0; i < 100; i++ {
		reporter.downloaded(1)
	}
	if len(events) != 0 {
		t.Fatal("unthrottled per-byte callbacks")
	}
	reporter.notify(true)
	if len(events) != 1 || events[0].DownloadedBytes != 100 || events[0].Percent != 99 {
		t.Fatalf("throttling lost real bytes or falsely completed: %+v", events)
	}
}
