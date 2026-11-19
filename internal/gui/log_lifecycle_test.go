package gui

import (
	"bytes"
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
	t.Cleanup(func() { CloseLogger(); log.SetOutput(original) })
	dir := t.TempDir()
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

func TestGUILoggerReinitializesDuringStandardLogging(t *testing.T) {
	CloseLogger()
	original := log.Writer()
	t.Cleanup(func() { CloseLogger(); log.SetOutput(original) })
	first, second := t.TempDir(), t.TempDir()
	InitLogger(first)
	stop := make(chan struct{})
	var writers sync.WaitGroup
	writers.Add(1)
	go func() {
		defer writers.Done()
		for {
			select {
			case <-stop:
				return
			default:
				log.Print("concurrent-GUI-log-marker")
			}
		}
	}()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 20; i++ {
			if i%2 == 0 {
				InitLogger(second)
			} else {
				InitLogger(first)
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("logger deadlocked reinitializing while standard logger held its writer lock")
	}
	close(stop)
	writers.Wait()
	CloseLogger()
}
