// Package hostsnapshot provides suite-level host-state capture and diff
// for proving automated tests do not pollute persistent user environment
// (Constitution principle II / Spec 005).
package hostsnapshot

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/env"
)

// Snapshot is a point-in-time capture of selected host-persistent state.
type Snapshot struct {
	Env   map[string]string
	Files map[string]FileMeta
}

// FileMeta records whether a path exists and a simple size/mode fingerprint.
type FileMeta struct {
	Exists bool
	Size   int64
	Mode   os.FileMode
}

// Change is one difference between two snapshots.
type Change struct {
	Kind   string // env|file
	Key    string
	Before string
	After  string
}

// Capture records the current host-persistent state described by opts.
func Capture(opts Options) (Snapshot, error) {
	s := Snapshot{
		Env:   make(map[string]string, len(opts.EnvKeys)),
		Files: make(map[string]FileMeta, len(opts.Paths)),
	}
	for _, k := range opts.EnvKeys {
		if k == "" {
			continue
		}
		v, err := env.GetUserEnvVar(k)
		if err != nil {
			// Missing key is represented as empty string.
			v = ""
		}
		s.Env[k] = v
	}
	for _, p := range opts.Paths {
		if p == "" {
			continue
		}
		clean := filepath.Clean(p)
		if excluded(clean, opts.ExcludePathPrefixes) {
			continue
		}
		fi, err := os.Lstat(clean)
		if err != nil {
			if os.IsNotExist(err) {
				s.Files[clean] = FileMeta{Exists: false}
				continue
			}
			return Snapshot{}, fmt.Errorf("hostsnapshot: stat %s: %w", clean, err)
		}
		s.Files[clean] = FileMeta{
			Exists: true,
			Size:   fi.Size(),
			Mode:   fi.Mode(),
		}
	}
	return s, nil
}

// Diff returns ordered changes from before to after.
func Diff(before, after Snapshot) []Change {
	var out []Change

	keys := make(map[string]struct{})
	for k := range before.Env {
		keys[k] = struct{}{}
	}
	for k := range after.Env {
		keys[k] = struct{}{}
	}
	var envKeys []string
	for k := range keys {
		envKeys = append(envKeys, k)
	}
	sort.Strings(envKeys)
	for _, k := range envKeys {
		b := before.Env[k]
		a := after.Env[k]
		if b != a {
			out = append(out, Change{
				Kind:   "env",
				Key:    k,
				Before: b,
				After:  a,
			})
		}
	}

	fkeys := make(map[string]struct{})
	for k := range before.Files {
		fkeys[k] = struct{}{}
	}
	for k := range after.Files {
		fkeys[k] = struct{}{}
	}
	var fileKeys []string
	for k := range fkeys {
		fileKeys = append(fileKeys, k)
	}
	sort.Strings(fileKeys)
	for _, k := range fileKeys {
		b := before.Files[k]
		a := after.Files[k]
		if b != a {
			out = append(out, Change{
				Kind:   "file",
				Key:    k,
				Before: formatFile(b),
				After:  formatFile(a),
			})
		}
	}
	return out
}

// RunWithHostSnapshot wraps m.Run with before/after host snapshot checks.
// Set XDEV_HOST_SNAPSHOT=0 to skip (CI and default suites must not set this).
func RunWithHostSnapshot(m *testing.M, opts Options) int {
	if os.Getenv("XDEV_HOST_SNAPSHOT") == "0" {
		return m.Run()
	}

	before, err := Capture(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "host state snapshot: capture before failed: %v\n", err)
		return 1
	}

	code := m.Run()

	after, err := Capture(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "host state snapshot: capture after failed: %v\n", err)
		if code == 0 {
			return 1
		}
		return code
	}

	changes := Diff(before, after)
	if len(changes) == 0 {
		fmt.Fprintln(os.Stderr, "host state snapshot: no changes detected")
		return code
	}

	fmt.Fprintln(os.Stderr, "host state snapshot: CHANGES DETECTED (blocking)")
	for _, c := range changes {
		fmt.Fprintf(os.Stderr, "  [%s] %s\n    before: %q\n    after:  %q\n", c.Kind, c.Key, c.Before, c.After)
	}
	if code == 0 {
		return 1
	}
	return code
}

func formatFile(m FileMeta) string {
	if !m.Exists {
		return "missing"
	}
	return fmt.Sprintf("exists size=%d mode=%v", m.Size, m.Mode)
}

func excluded(path string, prefixes []string) bool {
	clean := filepath.Clean(path)
	for _, p := range prefixes {
		p = filepath.Clean(p)
		if p == "" {
			continue
		}
		if clean == p || strings.HasPrefix(clean, p+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}
