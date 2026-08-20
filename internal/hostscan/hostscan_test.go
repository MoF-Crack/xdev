package hostscan

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestInsideDir(t *testing.T) {
	dir := filepath.Join("E:", "XDev", "shims")
	hit := filepath.Join(dir, "pnpm.exe")
	miss := filepath.Join("D:", "Env", "nodejs", "pnpm.exe")
	if !InsideDir(hit, dir) {
		t.Fatalf("expected %q inside %q", hit, dir)
	}
	if InsideDir(miss, dir) {
		t.Fatalf("did not expect %q inside %q", miss, dir)
	}
	if InsideDir(dir, dir) != true {
		t.Fatal("dir should be inside itself")
	}
}

func TestLookPathSkipsShimDir(t *testing.T) {
	tmp := t.TempDir()
	shimDir := filepath.Join(tmp, "shims")
	realDir := filepath.Join(tmp, "real")
	if err := os.MkdirAll(shimDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "xdevhostscanbin"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	shimBin := filepath.Join(shimDir, name)
	realBin := filepath.Join(realDir, name)
	if err := os.WriteFile(shimBin, []byte("shim"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(realBin, []byte("real"), 0o755); err != nil {
		t.Fatal(err)
	}

	orig := os.Getenv("PATH")
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+orig)
	if runtime.GOOS == "windows" {
		t.Setenv("PATHEXT", ".COM;.EXE;.BAT;.CMD")
	}
	if _, ok := LookPath(name, shimDir); ok {
		t.Fatal("LookPath should skip the shim-dir hit")
	}

	t.Setenv("PATH", realDir+string(os.PathListSeparator)+shimDir+string(os.PathListSeparator)+orig)
	got, ok := LookPath(name, shimDir)
	if !ok {
		t.Fatal("LookPath should find the real binary")
	}
	if !InsideDir(got, realDir) {
		t.Errorf("got %q, want under %q", got, realDir)
	}

	// Production PATH puts shimDir first. Skipping the shim must still
	// return the later real binary — not "not found".
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+realDir+string(os.PathListSeparator)+orig)
	got, ok = LookPath(name, shimDir)
	if !ok {
		t.Fatal("LookPath should walk past the shim-dir hit")
	}
	if !InsideDir(got, realDir) {
		t.Errorf("got %q, want under %q after skipping shim", got, realDir)
	}
}

func writeBin(t *testing.T, dir, name string, body string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestScanReturnsEveryPATHInstanceSkippingShim(t *testing.T) {
	tmp := t.TempDir()
	shimDir := filepath.Join(tmp, "shims")
	dDrive := filepath.Join(tmp, "D-Env-nodejs")
	eDrive := filepath.Join(tmp, "E-XDev-node")
	name := "pnpm"
	if runtime.GOOS == "windows" {
		name = "pnpm.cmd"
	}
	writeBin(t, shimDir, name, "shim")
	dHit := writeBin(t, dDrive, name, "stale-d")
	eHit := writeBin(t, eDrive, name, "xdev-e")

	hits := Scan(Query{
		Names:    []string{"pnpm"},
		ShimDir:  shimDir,
		PathList: []string{shimDir, dDrive, eDrive},
	})
	if len(hits) != 2 {
		t.Fatalf("hits = %d (%v), want 2 (shim skipped, D: and E: kept)", len(hits), hits)
	}
	if !InsideDir(hits[0].Path, dDrive) {
		t.Errorf("first hit = %q, want under %q (PATH order)", hits[0].Path, dDrive)
	}
	if !InsideDir(hits[1].Path, eDrive) {
		t.Errorf("second hit = %q, want under %q", hits[1].Path, eDrive)
	}
	for _, h := range hits {
		if InsideDir(h.Path, shimDir) {
			t.Errorf("shim copy leaked: %q", h.Path)
		}
	}
	_ = dHit
	_ = eHit
}

func TestScanDedupesCaseOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows case-insensitive PATH only")
	}
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "nodejs")
	writeBin(t, dir, "pnpm.cmd", "one")
	hits := Scan(Query{
		Names:    []string{"pnpm", "pnpm.cmd"},
		PathList: []string{dir, dir},
	})
	if len(hits) != 1 {
		t.Fatalf("hits = %d (%v), want 1 after case/path dedupe", len(hits), hits)
	}
}

func TestScanEmptyNames(t *testing.T) {
	if hits := Scan(Query{}); hits != nil {
		t.Fatalf("empty Names should be a no-op, got %v", hits)
	}
}
