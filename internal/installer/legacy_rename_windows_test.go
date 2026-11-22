//go:build windows

package installer

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xmdemt/xdev/internal/util"
	"golang.org/x/sys/windows"
)

func legacyRenameFixture(t *testing.T) (string, string, os.FileInfo) {
	t.Helper()
	root := t.TempDir()
	src, dst := filepath.Join(root, "legacy"), filepath.Join(root, "archived")
	if err := os.Mkdir(src, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "marker.txt"), []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(src)
	if err != nil {
		t.Fatal(err)
	}
	return src, dst, fi
}

func TestLegacyRenameRetryErrorContract(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		failure                   error
		successesAfter, wantCalls int
	}{
		{"transient-access", windows.ERROR_ACCESS_DENIED, 2, 3},
		{"transient-sharing", windows.ERROR_SHARING_VIOLATION, 1, 2},
		{"persistent-access", windows.ERROR_ACCESS_DENIED, -1, 10},
		{"persistent-sharing", windows.ERROR_SHARING_VIOLATION, -1, 10},
		{"nonretryable", windows.ERROR_INVALID_PARAMETER, -1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src, dst, fi := legacyRenameFixture(t)
			calls, waits := 0, 0
			var delay time.Duration
			err := renameLegacyDirectoryWith(src, dst, fi, func(a, b string) error {
				calls++
				if tc.successesAfter >= 0 && calls > tc.successesAfter {
					return os.Rename(a, b)
				}
				return &os.LinkError{Op: "rename", Old: a, New: b, Err: tc.failure}
			}, func(d time.Duration) { waits++; delay += d })
			if calls != tc.wantCalls || waits != tc.wantCalls-1 {
				t.Fatalf("calls=%d waits=%d", calls, waits)
			}
			if delay > 45*time.Millisecond {
				t.Fatalf("unbounded delay: %s", delay)
			}
			if tc.successesAfter >= 0 {
				if err != nil {
					t.Fatal(err)
				}
				if _, e := os.Stat(filepath.Join(dst, "marker.txt")); e != nil {
					t.Fatal(e)
				}
			} else {
				if !errors.Is(err, tc.failure) {
					t.Fatalf("cause lost: %v", err)
				}
				if b, e := os.ReadFile(filepath.Join(src, "marker.txt")); e != nil || string(b) != "keep" {
					t.Fatalf("source not preserved: %q %v", b, e)
				}
				if _, e := os.Lstat(dst); !os.IsNotExist(e) {
					t.Fatalf("destination created despite failure: %v", e)
				}
			}
		})
	}
}

func TestLegacyRenameStopsIfDirectoryChangesDuringRetry(t *testing.T) {
	src, dst, fi := legacyRenameFixture(t)
	calls := 0
	err := renameLegacyDirectoryWith(src, dst, fi, func(a, b string) error { calls++; return windows.ERROR_ACCESS_DENIED }, func(time.Duration) {
		if e := os.Rename(src, src+"-original"); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(src, []byte("replacement"), 0644); e != nil {
			t.Fatal(e)
		}
	})
	if err == nil || calls != 1 {
		t.Fatalf("changed entry retried: calls=%d err=%v", calls, err)
	}
	if b, e := os.ReadFile(src); e != nil || string(b) != "replacement" {
		t.Fatalf("replacement changed: %q %v", b, e)
	}
	if b, e := os.ReadFile(filepath.Join(src+"-original", "marker.txt")); e != nil || string(b) != "keep" {
		t.Fatalf("original changed: %q %v", b, e)
	}
}

func TestLegacyRenameStopsIfDirectoryBecomesJunction(t *testing.T) {
	src, dst, fi := legacyRenameFixture(t)
	original := src + "-original"
	calls := 0
	err := renameLegacyDirectoryWith(src, dst, fi, func(a, b string) error { calls++; return windows.ERROR_SHARING_VIOLATION }, func(time.Duration) {
		if e := os.Rename(src, original); e != nil {
			t.Fatal(e)
		}
		if e := util.CreateJunction(src, original); e != nil {
			t.Fatal(e)
		}
	})
	if err == nil || calls != 1 {
		t.Fatalf("junction replacement retried: calls=%d err=%v", calls, err)
	}
	oldRemove := removeAllResidue
	removeAllResidue = func(string) error { t.Error("junction reached recursive residue deletion"); return nil }
	t.Cleanup(func() { removeAllResidue = oldRemove })
	removeLinkEntry(src, filepath.Dir(src))
	if _, e := os.Lstat(src); !os.IsNotExist(e) {
		t.Fatalf("junction not removed: %v", e)
	}
	if b, e := os.ReadFile(filepath.Join(original, "marker.txt")); e != nil || string(b) != "keep" {
		t.Fatalf("junction target changed: %q %v", b, e)
	}
}

func TestLegacyRenameRealMemberHandle(t *testing.T) {
	for _, release := range []bool{true, false} {
		name := "persistent"
		if release {
			name = "released"
		}
		t.Run(name, func(t *testing.T) {
			src, dst, fi := legacyRenameFixture(t)
			path, e := windows.UTF16PtrFromString(filepath.Join(src, "marker.txt"))
			if e != nil {
				t.Fatal(e)
			}
			h, e := windows.CreateFile(path, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
			if e != nil {
				t.Fatal(e)
			}
			open := true
			t.Cleanup(func() {
				if open {
					windows.CloseHandle(h)
				}
			})
			calls := 0
			err := renameLegacyDirectoryWith(src, dst, fi, func(a, b string) error { calls++; return os.Rename(a, b) }, func(time.Duration) {
				if release && open {
					if e := windows.CloseHandle(h); e != nil {
						t.Fatal(e)
					}
					open = false
				}
			})
			if release {
				if err != nil || calls < 2 {
					t.Fatalf("real transient handle: calls=%d err=%v", calls, err)
				}
			} else {
				if !errors.Is(err, windows.ERROR_ACCESS_DENIED) || calls != 10 {
					t.Fatalf("persistent handle: calls=%d err=%v", calls, err)
				}
				if _, e := os.Stat(filepath.Join(src, "marker.txt")); e != nil {
					t.Fatal(e)
				}
			}
		})
	}
}
