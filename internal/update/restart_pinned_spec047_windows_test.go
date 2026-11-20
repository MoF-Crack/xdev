//go:build windows

package update

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// Reproduce the former hash-to-rename gap using the exact handle that the
// production helper pins before it backs up the current executable. Attempts
// to mutate either bytes or path must be denied through publication.
func TestSpec047PinnedPayloadCannotChangeAfterVerification(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "payload.exe")
	target := filepath.Join(dir, "xdev.exe")
	good := []byte("verified immutable update payload")
	if err := os.WriteFile(source, good, 0600); err != nil {
		t.Fatal(err)
	}
	file, err := openRestartMovable(source)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	want := fmt.Sprintf("%x", sha256.Sum256(good))
	if err := verifyOpenedRestartInput(context.Background(), file, want); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("changed during backup"), 0600); err == nil {
		t.Fatal("verified payload was writable while update helper held its handle")
	}
	if err := os.Rename(source, filepath.Join(dir, "substituted.exe")); err == nil {
		t.Fatal("verified payload path was replaceable while helper held its handle")
	}
	if err := os.Remove(source); err == nil {
		t.Fatal("verified payload was deletable before publication")
	}
	if err := renameRestartInput(file, target, false); err != nil {
		t.Fatalf("publish same verified object: %v", err)
	}
	// Destination now refers to the same protected object, not a reopened path.
	if err := os.WriteFile(target, []byte("changed after publication"), 0600); err == nil {
		t.Fatal("published executable became writable before launch")
	}
	// Read through the pinned object: a new read handle without DELETE sharing
	// would correctly conflict with its outstanding DELETE access.
	if err := verifyOpenedRestartInput(context.Background(), file, want); err != nil {
		t.Fatalf("published unverified bytes: %v", err)
	}
	published, err := os.Lstat(target)
	if err != nil {
		t.Fatal(err)
	}
	pinned, err := file.Stat()
	if err != nil || !os.SameFile(published, pinned) {
		t.Fatalf("publication did not retain the verified object: %v", err)
	}
}

func TestSpec047HandlePublicationDoesNotOverwriteForeignDestination(t *testing.T) {
	dir := t.TempDir()
	payload := filepath.Join(dir, "payload.exe")
	target := filepath.Join(dir, "xdev.exe")
	if err := os.WriteFile(payload, []byte("new payload"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("foreign executable"), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := openRestartMovable(payload)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := renameRestartInput(file, target, false); err == nil {
		t.Fatal("unexpected overwrite of a preexisting foreign executable")
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != "foreign executable" {
		t.Fatalf("foreign executable changed: %q %v", got, err)
	}
}
