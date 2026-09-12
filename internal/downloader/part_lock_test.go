package downloader

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Ticket 21 (audit-fixes / P1-01 + P1-02) guards — .part mutual exclusion
// and the Pause/Cancel state machine.
//
// DL1 (red line): two downloads into the same destination — exactly one may
// succeed, the other must report an explicit "another download in progress"
// error. Pre-fix both wrote into the same .part and both "succeeded".
// DL4 (red line): Pause followed immediately by Resume must work — pre-fix
// Resume raced the still-running goroutine and could report
// "cannot resume task in status downloading".
// DL5: a paused download must keep its .part file.
// DL7: a second Pause is rejected by the status gate (no pausedAt overwrite).

// slowServer serves payload with pauses so Pause/Cancel can act mid-transfer.
func slowServer(t *testing.T, payload []byte, chunkDelay time.Duration) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(payload)))
		half := len(payload) / 2
		_, _ = w.Write(payload[:half])
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		time.Sleep(chunkDelay)
		_, _ = w.Write(payload[half:])
	}))
	t.Cleanup(srv.Close)
	return srv
}

// DL1: concurrent downloads to the same dest — one wins, the other errors.
//
// The winner must hold the .part lock long enough for the loser's 1s
// acquisition timeout to expire, hence the 2s server delay: a download that
// finishes first legitimately releases the lock and the next one proceeds.
func TestDownload_ConcurrentSameDest_SecondErrors(t *testing.T) {
	payload := []byte(strings.Repeat("x", 4096))
	srv := slowServer(t, payload, 2*time.Second)
	dest := filepath.Join(t.TempDir(), "archive.zip")

	opts := Options{
		URL: srv.URL + "/archive.zip", Dest: dest, Client: srv.Client(),
		ProgressDesc: "demo@1.0.0", MaxRetries: 1,
	}

	type outcome struct {
		err error
	}
	results := make(chan outcome, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results <- outcome{err: Download(context.Background(), opts)}
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	var ok, failed int
	var errMsg string
	for r := range results {
		if r.err == nil {
			ok++
		} else {
			failed++
			errMsg = r.err.Error()
		}
	}
	if ok != 1 || failed != 1 {
		t.Fatalf("concurrent same-dest downloads: ok=%d failed=%d, want exactly one of each (err=%q)", ok, failed, errMsg)
	}
	if !strings.Contains(errMsg, "another download is already in progress") {
		t.Errorf("loser error = %q, want an explicit in-progress error", errMsg)
	}
	// The winner's archive is intact.
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("winner's file missing: %v", err)
	}
	if string(got) != string(payload) {
		t.Errorf("winner's archive is corrupted (len=%d, want %d)", len(got), len(payload))
	}
}

// DL2: a leftover .part from a crashed process (plus its stale .lock file)
// is taken over and resumed.
func TestDownload_CrashResume_TakesOver(t *testing.T) {
	payload := []byte(strings.Repeat("y", 2048))
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rangeHdr := r.Header.Get("Range")
		if rangeHdr == "" {
			_, _ = w.Write(payload)
			return
		}
		// Serve the requested range (simple slicing).
		var startByte int
		_, _ = fmt.Sscanf(rangeHdr, "bytes=%d-", &startByte)
		if startByte >= len(payload) {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(payload)-startByte))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload[startByte:])
	}))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "archive.zip")
	// Crash residue: half the payload plus a stale lock file.
	if err := os.WriteFile(dest+".part", payload[:1024], 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest+".part.lock", nil, 0o644); err != nil {
		t.Fatal(err)
	}

	err := Download(context.Background(), Options{
		URL: srv.URL + "/archive.zip", Dest: dest, Client: srv.Client(),
		ExpectedSHA: sha256Of(payload), ProgressDesc: "demo@1.0.0", MaxRetries: 1,
	})
	if err != nil {
		t.Fatalf("crash-resume download failed (stale lock must not block takeover): %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Errorf("resumed archive len=%d, want %d", len(got), len(payload))
	}
}

// DL3: a .part that is a SYMLINK is cleared and rebuilt inside the lock —
// the M-4 hardening must not be swallowed by the new "in progress" error.
func TestDownload_SymlinkPartFile_ClearedUnderLock(t *testing.T) {
	payload := []byte("symlink part payload")
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "archive.zip")
	victim := filepath.Join(dir, "victim.txt")
	if err := os.WriteFile(victim, []byte("do-not-touch"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, dest+".part"); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}

	if err := Download(context.Background(), Options{
		URL: srv.URL + "/archive.zip", Dest: dest, Client: srv.Client(),
		ExpectedSHA: sha256Of(payload), MaxRetries: 1,
	}); err != nil {
		t.Fatalf("download with symlinked .part failed: %v", err)
	}
	if got, err := os.ReadFile(victim); err != nil || string(got) != "do-not-touch" {
		t.Errorf("symlink target was written through: %q err=%v", got, err)
	}
	if got, err := os.ReadFile(dest); err != nil || string(got) != string(payload) {
		t.Errorf("archive not written correctly: %q err=%v", got, err)
	}
}

// managerFor builds a Manager with a single running task.
func managerFor(t *testing.T, srv *httptest.Server, dest string) (*Manager, string) {
	t.Helper()
	m := NewManager()
	id := "task-1"
	if err := m.StartDownload(id, "demo", "1.0.0", Options{
		URL: srv.URL + "/archive.zip", Dest: dest, Client: srv.Client(),
		ProgressDesc: "demo@1.0.0", MaxRetries: 1,
	}); err != nil {
		t.Fatalf("StartDownload: %v", err)
	}
	return m, id
}

// DL4 (red line): Pause → 立即 Resume must succeed.
func TestPause_ImmediatelyResume_Works(t *testing.T) {
	payload := []byte(strings.Repeat("z", 8192))
	srv := slowServer(t, payload, 250*time.Millisecond)
	dest := filepath.Join(t.TempDir(), "archive.zip")
	m, id := managerFor(t, srv, dest)

	// Give the download a moment to start streaming.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if p := GetPartialFileSize(dest); p > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if err := m.Pause(id); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	// The pre-fix failure mode: the status was still "downloading" here
	// because the goroutine had not yet processed the cancellation.
	if err := m.Resume(id); err != nil {
		t.Fatalf("immediate Resume after Pause failed: %v (pre-fix race: status still downloading)", err)
	}

	// Wait for completion; the resumed generation finishes the file.
	deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if st := mustStatus(t, m, id); st == StatusCompleted || st == StatusFailed {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if st := mustStatus(t, m, id); st != StatusCompleted {
		t.Fatalf("final status = %s, want completed", st)
	}
}

// DL5: a paused download keeps its .part file (no cross-generation delete).
func TestPause_DoesNotDeletePart(t *testing.T) {
	payload := []byte(strings.Repeat("w", 8192))
	srv := slowServer(t, payload, 400*time.Millisecond)
	dest := filepath.Join(t.TempDir(), "archive.zip")
	m, id := managerFor(t, srv, dest)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if GetPartialFileSize(dest) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := m.Pause(id); err != nil {
		t.Fatalf("Pause: %v", err)
	}

	// Let the cancelled goroutine unwind completely.
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if mustStatus(t, m, id) == StatusPaused {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond) // extra settling window

	if _, err := os.Stat(dest + ".part"); err != nil {
		t.Errorf("paused download lost its .part file: %v", err)
	}
	if st := mustStatus(t, m, id); st != StatusPaused {
		t.Errorf("status = %s, want paused (a paused download must not be reported as cancelled)", st)
	}
}

// DL6: Cancel while a writer holds the handle must not panic.
func TestCancel_DoesNotPanicWithActiveWriter(t *testing.T) {
	payload := []byte(strings.Repeat("v", 8192))
	srv := slowServer(t, payload, 200*time.Millisecond)
	dest := filepath.Join(t.TempDir(), "archive.zip")
	m, id := managerFor(t, srv, dest)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if GetPartialFileSize(dest) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := m.Cancel(id); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	if st := mustStatus(t, m, id); st != StatusCancelled {
		t.Errorf("status = %s, want cancelled", st)
	}
}

// DL7: the second Pause is rejected by the status gate and cannot overwrite
// pausedAt with a later byte count.
func TestPauseTwice_RejectedByStatusGate(t *testing.T) {
	payload := []byte(strings.Repeat("u", 16384))
	srv := slowServer(t, payload, 300*time.Millisecond)
	dest := filepath.Join(t.TempDir(), "archive.zip")
	m, id := managerFor(t, srv, dest)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if GetPartialFileSize(dest) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := m.Pause(id); err != nil {
		t.Fatalf("first Pause: %v", err)
	}
	task, _ := m.GetTask(id)
	task.mu.Lock()
	firstPausedAt := task.pausedAt
	task.mu.Unlock()

	if err := m.Pause(id); err == nil {
		t.Error("second Pause must be rejected by the status gate")
	}
	task.mu.Lock()
	secondPausedAt := task.pausedAt
	task.mu.Unlock()
	if firstPausedAt != secondPausedAt {
		t.Errorf("pausedAt overwritten by the rejected second Pause: %d → %d", firstPausedAt, secondPausedAt)
	}
}

// DL9 (regression): the normal Pause → wait → Resume flow still completes.
func TestManagerResume_StillWorks(t *testing.T) {
	payload := []byte(strings.Repeat("t", 8192))
	srv := slowServer(t, payload, 150*time.Millisecond)
	dest := filepath.Join(t.TempDir(), "archive.zip")
	m, id := managerFor(t, srv, dest)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if GetPartialFileSize(dest) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := m.Pause(id); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	// Wait for the paused state to settle (the production flow polls the
	// status before offering Resume).
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if mustStatus(t, m, id) == StatusPaused {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if st := mustStatus(t, m, id); st != StatusPaused {
		t.Fatalf("status after Pause = %s, want paused", st)
	}
	if err := m.Resume(id); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if st := mustStatus(t, m, id); st == StatusCompleted || st == StatusFailed {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if st := mustStatus(t, m, id); st != StatusCompleted {
		t.Fatalf("final status = %s, want completed", st)
	}
}

func mustStatus(t *testing.T, m *Manager, id string) DownloadStatus {
	t.Helper()
	task, err := m.GetTask(id)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	return task.GetStatus()
}
