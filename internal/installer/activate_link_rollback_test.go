// activate_link_rollback_test.go — F-22 guards.
//
// Pre-fix Activate unconditionally removed current/<tool> before creating the
// replacement link, and when every link method failed it returned WITHOUT
// restoring the old link — so a failed activation left the tool permanently
// un-activated even though a perfectly good previous version was installed.
//
// The old fallback chain also ended in util.CopyDir, which produced a real
// directory COPY at current/<tool>. util.ActiveVersion reads that path with
// os.Readlink, so it reported ("", false) forever: the tool looked broken and
// no later `xdev use` could repair it without manual deletion.
package installer

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/xmdemt/xdev/internal/util"
)

// TestActivate_LinkFailureRestoresPreviousLink asserts that when every link
// creation method fails, the previous current/<tool> link is restored instead
// of being destroyed.
func TestActivate_LinkFailureRestoresPreviousLink(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)

	p := &fakePlugin{name: "fakego", shims: []string{"fakego"}}
	vA := makeFakeInstalledVersion(t, cfg.Core.InstallDir, "fakego", "1.0.0")
	vB := makeFakeInstalledVersion(t, cfg.Core.InstallDir, "fakego", "2.0.0")

	// vA is the active version.
	if err := ins.Activate(p, vA, "1.0.0"); err != nil {
		t.Fatalf("Activate vA: %v", err)
	}
	if got, ok := util.ActiveVersion(cfg.Core.XMDevDir, "fakego"); !ok || got != "1.0.0" {
		t.Fatalf("precondition: active version = %q (ok=%v), want 1.0.0", got, ok)
	}

	// Force every link method to fail (this is otherwise only reachable on a
	// hostile filesystem / locked binary).
	orig := linkCurrent
	linkCurrent = func(target, link string) error {
		return errors.New("forced: all link methods failed")
	}
	defer func() { linkCurrent = orig }()

	err := ins.Activate(p, vB, "2.0.0")
	if err == nil {
		t.Fatal("Activate reported success although link creation failed")
	}

	// F-22 core assertion: the OLD link must still resolve to vA.
	got, ok := util.ActiveVersion(cfg.Core.XMDevDir, "fakego")
	if !ok || got != "1.0.0" {
		t.Errorf("after a failed activation the previous link was not restored: version=%q ok=%v, want 1.0.0", got, ok)
	}
	// And it must still be a LINK (not a directory copy).
	link := filepath.Join(cfg.Core.XMDevDir, "current", "fakego")
	if _, rerr := os.Readlink(link); rerr != nil {
		t.Errorf("current/fakego is not a link after rollback (Readlink: %v)", rerr)
	}
}

// TestActivate_LinkFailureWithNoPreviousLink asserts the no-previous-link case
// stays clean: the failure is reported and nothing is left behind.
func TestActivate_LinkFailureWithNoPreviousLink(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)

	p := &fakePlugin{name: "fresh", shims: []string{"fresh"}}
	vA := makeFakeInstalledVersion(t, cfg.Core.InstallDir, "fresh", "1.0.0")

	orig := linkCurrent
	linkCurrent = func(target, link string) error { return errors.New("forced") }
	defer func() { linkCurrent = orig }()

	if err := ins.Activate(p, vA, "1.0.0"); err == nil {
		t.Fatal("Activate reported success although link creation failed")
	}
	link := filepath.Join(cfg.Core.XMDevDir, "current", "fresh")
	if _, err := os.Lstat(link); err == nil {
		t.Error("a dangling current/fresh entry was left behind after a failed activation")
	}
}

// TestFallbackLink_NeverCopiesDirectory asserts the CopyDir last resort is
// gone: fallbackLink must either create a real LINK or fail — never
// materialise a plain directory at the link path (which ActiveVersion can
// never resolve).
//
// Written to be environment-independent: on hosts without an `ln` binary the
// call simply fails, which is the assertion; where `ln` exists it may succeed
// only by creating a link.
func TestFallbackLink_NeverCopiesDirectory(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "payload.txt"), []byte("p"), 0o644); err != nil {
		t.Fatal(err)
	}
	// dst already exists as a non-empty directory: mklink /J refuses, and the
	// link must not be created INSIDE it.
	dst := filepath.Join(dir, "dst")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "keep.txt"), []byte("k"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := fallbackLink(src, dst)

	// The core contract: the source tree was never copied in.
	if _, statErr := os.Stat(filepath.Join(dst, "payload.txt")); statErr == nil {
		t.Error("fallbackLink copied payload.txt into dst: the CopyDir fallback is still present")
	}
	if _, statErr := os.Stat(filepath.Join(dst, "src")); statErr == nil {
		t.Error("fallbackLink created the link INSIDE dst (dst/src) instead of at dst")
	}
	if err == nil {
		// Success is only acceptable when dst itself became a link.
		if _, rerr := os.Readlink(dst); rerr != nil {
			t.Errorf("fallbackLink reported success but dst is not a link (Readlink: %v)", rerr)
		}
	} else {
		// Failure must leave the pre-existing directory untouched.
		if _, statErr := os.Stat(filepath.Join(dst, "keep.txt")); statErr != nil {
			t.Errorf("fallbackLink destroyed pre-existing dst content: %v", statErr)
		}
	}
}

// TestActivate_CurrentLinkIsAlwaysALink pins the contract that motivated
// removing the CopyDir fallback: after a successful Activate the current entry
// must be resolvable with os.Readlink (what util.ActiveVersion uses).
func TestActivate_CurrentLinkIsAlwaysALink(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)

	p := &fakePlugin{name: "fakego", shims: []string{"fakego"}}
	vA := makeFakeInstalledVersion(t, cfg.Core.InstallDir, "fakego", "1.0.0")

	if err := ins.Activate(p, vA, "1.0.0"); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	link := filepath.Join(cfg.Core.XMDevDir, "current", "fakego")
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("current/fakego is not a link (Readlink: %v) — a directory copy would break ActiveVersion", err)
	}
	if filepath.Base(target) != "1.0.0" {
		t.Errorf("link target = %q, want it to end in 1.0.0", target)
	}
	if got, ok := util.ActiveVersion(cfg.Core.XMDevDir, "fakego"); !ok || got != "1.0.0" {
		t.Errorf("ActiveVersion = %q (ok=%v), want 1.0.0", got, ok)
	}
	if runtime.GOOS == "windows" {
		// A junction must be reported as a link by Lstat, never as a plain dir.
		fi, lerr := os.Lstat(link)
		if lerr != nil {
			t.Fatalf("Lstat: %v", lerr)
		}
		if fi.IsDir() && fi.Mode()&os.ModeSymlink == 0 && fi.Mode()&os.ModeIrregular == 0 {
			t.Error("current/fakego looks like a plain directory, not a junction/symlink")
		}
	}
}
