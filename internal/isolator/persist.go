package isolator

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// persistedState is the on-disk isolator session (Spec 032). It lets
// `xdev isolate setup` and `xdev isolate exec` share state across
// processes instead of losing the container ID / flake dir.
type persistedState struct {
	ContainerID string `json:"containerId,omitempty"`
	FlakeDir    string `json:"flakeDir,omitempty"`
	ProjectDir  string `json:"projectDir,omitempty"`
	Image       string `json:"image,omitempty"`

	// WrittenFiles maps a normalized path of every file this isolator wrote
	// during Setup to the hex SHA-256 of the exact bytes written
	// (C-07 / P1-34). Teardown deletes a path only while its on-disk content
	// still hashes to the recorded value, so a file the user edited or
	// replaced in the meantime survives — the pre-fix code removed
	// flake.nix blindly, destroying hand-written user flakes.
	//
	// Setup and Teardown run in separate processes, so this record (not
	// in-memory state) is the only proof of ownership. A state file written
	// before C-07 carries no map at all: Teardown then degrades to
	// "delete nothing + one log line" instead of guessing.
	WrittenFiles map[string]string `json:"writtenFiles,omitempty"`
}

func statePath(xmdeDir, mode string) string {
	return filepath.Join(xmdeDir, "isolate", mode+".json")
}

func saveState(xmdeDir, mode string, s persistedState) {
	if xmdeDir == "" || mode == "" {
		return
	}
	p := statePath(xmdeDir, mode)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	data, _ := json.Marshal(s)
	_ = os.WriteFile(p, data, 0o644)
}

func loadState(xmdeDir, mode string) persistedState {
	var s persistedState
	if xmdeDir == "" || mode == "" {
		return s
	}
	data, err := os.ReadFile(statePath(xmdeDir, mode))
	if err != nil {
		return s
	}
	_ = json.Unmarshal(data, &s)
	return s
}

func clearState(xmdeDir, mode string) {
	if xmdeDir == "" || mode == "" {
		return
	}
	_ = os.Remove(statePath(xmdeDir, mode))
}

// contentSHA256 returns the hex SHA-256 of content.
func contentSHA256(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// stateKey normalizes a path for use as a WrittenFiles key. Setup and
// Teardown run in separate processes and may see the same directory through
// differently-cased spellings (Windows paths are case-insensitive), so keys
// are folded on Windows.
func stateKey(path string) string {
	key := filepath.Clean(path)
	if runtime.GOOS == "windows" {
		key = strings.ToLower(key)
	}
	return key
}

// recordWrittenFile remembers that this isolator wrote path with content and
// returns the (possibly newly created) map.
func recordWrittenFile(files map[string]string, path string, content []byte) map[string]string {
	if path == "" {
		return files
	}
	if files == nil {
		files = map[string]string{}
	}
	files[stateKey(path)] = contentSHA256(content)
	return files
}

// writtenHash returns the recorded hash for path ("" when unknown).
func writtenHash(files map[string]string, path string) string {
	if len(files) == 0 || path == "" {
		return ""
	}
	return files[stateKey(path)]
}

// removeIfContentUnchanged deletes path when its current content still hashes
// to want. Nothing is deleted when the hash differs (the user took the file
// over) or when the file is gone already; both cases are logged so a
// surprising leftover is diagnosable.
func removeIfContentUnchanged(path, want string) bool {
	if path == "" || want == "" {
		return false
	}
	current, err := os.ReadFile(path)
	if err != nil {
		return false // already gone, or unreadable → never delete blindly
	}
	if contentSHA256(current) != want {
		log.Printf("isolate: leaving %s in place — its content changed since setup, so xdev no longer owns it", path)
		return false
	}
	if err := os.Remove(path); err != nil {
		log.Printf("isolate: could not remove %s: %v", path, err)
		return false
	}
	return true
}

// logLegacyTeardownSkip reports the C-07 degradation for state files written
// before the content hashes existed: ownership cannot be proven, so the file
// is left alone (deleting it would risk a user's own flake.nix).
func logLegacyTeardownSkip(mode, path string) {
	log.Printf("isolate teardown (%s): %s has no recorded content hash (state predates C-07) — skipping removal to avoid deleting a user file", mode, path)
}
