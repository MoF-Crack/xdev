package downloader

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
)

// Ticket 23 (audit-fixes / D-08) guards — atomic sidecar (TOFU pin) writes.
//
// M4: overwriting an existing 0o444 (read-only) pin succeeds.
// M5: the all-zero placeholder first-write branch writes a complete pin.
// M6: a concurrent reader never observes a half-written pin.

// M4: an existing read-only pin is replaced, not rejected.
func TestWriteSidecarHash_Overwrites0444Target(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "archive.zip")
	sidecar := dest + ".sha256"
	oldHash := strings.Repeat("1", 64)
	newHash := strings.Repeat("2", 64)

	writeSidecarHash(sidecar, oldHash, dest)
	if err := os.Chmod(sidecar, 0o444); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	// Confirm the attribute really is read-only on this platform.
	if fi, err := os.Stat(sidecar); err == nil && fi.Mode().Perm()&0o200 != 0 {
		t.Logf("note: read-only attribute not enforced by this filesystem (mode=%v)", fi.Mode())
	}

	writeSidecarHash(sidecar, newHash, dest)

	got, err := os.ReadFile(sidecar)
	if err != nil {
		t.Fatalf("sidecar unreadable after overwrite: %v", err)
	}
	if !strings.HasPrefix(string(got), newHash) {
		t.Errorf("sidecar content = %q, want the new pin %s", string(got), newHash[:16]+"...")
	}
	// No temp residue left behind.
	if _, err := os.Stat(sidecar + ".tmp"); err == nil {
		t.Error("temp file left behind after a successful sidecar write")
	}
}

// M5: the all-zero placeholder branch (downloader.go "no real hash" path)
// establishes a complete pin on first use.
func TestWriteSidecarHash_AllZeroFirstWrite(t *testing.T) {
	const payload = "tofu first write payload"
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "archive.zip")
	err := Download(context.Background(), Options{
		URL: srv.URL + "/archive.zip", Dest: dest, Client: srv.Client(),
		ExpectedSHA: strings.Repeat("0", 64),
		IsThirdParty: true, // TOFU-eligible: no root key embedded in tests
		MaxRetries:   1,
	})
	if err != nil {
		t.Fatalf("download with all-zero placeholder failed: %v", err)
	}

	pin, err := os.ReadFile(dest + ".sha256")
	if err != nil {
		t.Fatalf("first-write pin missing: %v", err)
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}  archive\.zip\n$`).Match(pin) {
		t.Errorf("pin content malformed (possible partial write): %q", string(pin))
	}
	if strings.HasPrefix(string(pin), strings.Repeat("0", 64)) {
		t.Error("pin still holds the all-zero placeholder instead of the computed hash")
	}
}

// M6: while a writer repeatedly replaces the pin, a concurrent reader only
// ever sees a complete pin (or no file) — never a torn one.
func TestConcurrentDownload_NoHalfPin(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "archive.zip")
	sidecar := dest + ".sha256"
	writeSidecarHash(sidecar, strings.Repeat("0", 64), dest)

	const iterations = 200
	var wg sync.WaitGroup
	stop := make(chan struct{})
	badReads := make(chan string, 8)

	// Writer: flip between two valid pins.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			h := strings.Repeat("a", 64)
			if i%2 == 1 {
				h = strings.Repeat("b", 64)
			}
			writeSidecarHash(sidecar, h, dest)
		}
		close(stop)
	}()

	// Reader: every observed state must be a complete pin.
	validPin := regexp.MustCompile(`^[0-9a-f]{64}  archive\.zip\n$`)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			data, err := os.ReadFile(sidecar)
			if err != nil {
				continue // missing during the rename window is acceptable
			}
			if !validPin.Match(data) {
				select {
				case badReads <- string(data):
				default:
				}
				return
			}
		}
	}()

	wg.Wait()
	close(badReads)
	for bad := range badReads {
		t.Errorf("reader observed a torn pin: %q", bad)
	}
}
