package gui

import (
	"bytes"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestGUILoggerPreservesFirstRunStartupAndRestoresOutput(t *testing.T) {
	CloseLogger()
	original := log.Writer()
	var caller bytes.Buffer
	log.SetOutput(&caller)
	dir := t.TempDir()
	t.Cleanup(func() { CloseLogger(); log.SetOutput(original) })
	InitLogger(dir)
	log.Print("before-setup-marker")
	InitLogger(dir)
	log.Print("after-setup-marker")
	data, err := os.ReadFile(filepath.Join(dir, "xdev-gui.log"))
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"before-setup-marker", "after-setup-marker"} {
		if strings.Count(string(data), marker) != 1 {
			t.Fatalf("startup log lost or duplicated %q: %s", marker, data)
		}
	}
	CloseLogger()
	if log.Writer() != &caller {
		t.Fatal("standard logger was not restored")
	}
	log.Print("caller-restored-marker")
	if !strings.Contains(caller.String(), "caller-restored-marker") {
		t.Fatal("caller logs discarded after GUI shutdown")
	}
}

func TestGUILoggerHomeChangeClosesOldHandle(t *testing.T) {
	CloseLogger()
	original := log.Writer()
	oldDir, newDir := t.TempDir(), t.TempDir()
	t.Cleanup(func() { CloseLogger(); log.SetOutput(original) })
	InitLogger(oldDir)
	log.Print("old-home-marker")
	InitLogger(newDir)
	oldPath := filepath.Join(oldDir, "xdev-gui.log")
	if err := os.Rename(oldPath, oldPath+".closed"); err != nil {
		t.Fatalf("old log handle leaked after setup changed home: %v", err)
	}
	log.Print("new-home-marker")
	data, err := os.ReadFile(filepath.Join(newDir, "xdev-gui.log"))
	if err != nil || !strings.Contains(string(data), "new-home-marker") {
		t.Fatalf("new home log missing: %s %v", data, err)
	}
}

// heldStandardLogWriter pauses one real standard-log call while log's output
// mutex is held. Cancellation returns an error instead of entering logFileMu,
// so even a lock-order regression can release the standard mutex and be joined.
// The normal path still uses logWriter and its immediate file Sync.
type heldStandardLogWriter struct {
	entered   chan struct{}
	released  chan struct{}
	once      sync.Once
	enterOnce sync.Once
	forward   bool // Published by closing released.
	next      io.Writer
}

func (w *heldStandardLogWriter) Write(p []byte) (int, error) {
	w.enterOnce.Do(func() { close(w.entered) })
	<-w.released
	if !w.forward {
		return 0, errors.New("controlled standard log write canceled")
	}
	return w.next.Write(p)
}

func (w *heldStandardLogWriter) release(forward bool) {
	w.once.Do(func() {
		w.forward = forward
		close(w.released)
	})
}

func TestGUILoggerReinitializesDuringStandardLogging(t *testing.T) {
	testGUILoggerWithHeldStandardWriter(t, false)
}

func TestGUILoggerClosesDuringStandardLogging(t *testing.T) {
	testGUILoggerWithHeldStandardWriter(t, true)
}

func testGUILoggerWithHeldStandardWriter(t *testing.T, closing bool) {
	t.Helper()
	CloseLogger()
	original, originalGUIWriter := log.Writer(), standardLogOutput
	first, second := t.TempDir(), t.TempDir()
	var caller bytes.Buffer
	log.SetOutput(&caller)
	gate := &heldStandardLogWriter{
		entered: make(chan struct{}), released: make(chan struct{}), next: originalGUIWriter,
	}
	standardLogOutput = gate
	var workers sync.WaitGroup
	// Register after TempDir: stop/join both workers and close both files before
	// Windows tries to remove their directories, including every Fatal path.
	t.Cleanup(func() {
		gate.release(false)
		workers.Wait()
		CloseLogger()
		standardLogOutput = originalGUIWriter
		log.SetOutput(original)
	})
	InitLogger(first)
	logFileMu.Lock()
	old := logFile
	logFileMu.Unlock()
	if old == nil {
		t.Fatal("initial log file was not opened")
	}
	workers.Add(1)
	go func() {
		defer workers.Done()
		log.Print("concurrent-GUI-log-marker")
	}()
	select {
	case <-gate.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("standard log writer did not start")
	}

	done := make(chan struct{})
	workers.Add(1)
	go func() {
		defer workers.Done()
		defer close(done)
		if closing {
			CloseLogger()
		} else {
			InitLogger(second)
		}
	}()
	// Observe the file transition while the standard mutex is STILL held. A
	// lifecycle operation must release logFileMu before asking for that mutex.
	// TryLock lets a regression fail without blocking the test's cleanup.
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		transitioned := false
		if logFileMu.TryLock() {
			if closing {
				transitioned = logFile == nil
			} else {
				transitioned = logFile != old && logPath == filepath.Join(second, "xdev-gui.log")
			}
			logFileMu.Unlock()
		}
		if transitioned {
			break
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("file transition did not release logFileMu while standard writer was held")
		}
	}
	select {
	case <-done:
		t.Fatal("lifecycle bypassed the held standard logger mutex")
	default:
	}
	gate.release(true)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("logger deadlocked after releasing the standard writer")
	}
	workers.Wait()
	if err := old.Close(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("old log handle was not closed: %v", err)
	}
	if closing {
		if log.Writer() != &caller {
			t.Fatal("standard logger was not restored after concurrent close")
		}
		// The in-flight GUI write after CloseLogger is buffered for the next
		// session, where it must still be emitted exactly once.
		standardLogOutput = originalGUIWriter
		InitLogger(second)
	}
	data, err := os.ReadFile(filepath.Join(second, "xdev-gui.log"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), "concurrent-GUI-log-marker") != 1 {
		t.Fatalf("in-flight log lost or duplicated: %s", data)
	}
}
