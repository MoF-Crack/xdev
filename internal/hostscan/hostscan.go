// Package hostscan is the host-scan module: find tools on PATH / a
// supplied path list while skipping xdev shim copies. Callers pass the
// shim directory from App config — never config.Load("").
//
// Interface: Query in, []Hit out. Managed vs PATH ordering is a caller
// concern; this module only reports where each binary actually lives.
//
// Two adapters at this seam:
//   - production: os.Getenv("PATH") + os.ReadDir
//   - tests: Scan with an explicit PathList that points at a temp tree
package hostscan

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Hit is one binary found on a PATH snapshot.
type Hit struct {
	Path string // cleaned path to the executable
	Dir  string // directory that contained it
}

// Query is the single host-scan input.
type Query struct {
	// Names are candidate basenames (e.g. "pnpm", "pnpm.exe").
	// Empty Names is a no-op.
	Names []string
	// ShimDir, when non-empty, is skipped: those files are xdev
	// dispatcher copies, not the real tool.
	ShimDir string
	// PathList, when non-empty, is used instead of os.Getenv("PATH").
	// Tests inject a temp tree here so production PATH is never required.
	PathList []string
}

// readDir lists a directory. Tests may replace it to count calls.
var readDir = os.ReadDir

// LookPath finds cmd on PATH, skipping any hit inside shimDir (those
// are xdev dispatcher copies, not the real tool). Empty shimDir
// disables the skip. Unlike exec.LookPath, a shim-dir hit does not
// hide a later real binary (shimDir is typically first on PATH).
func LookPath(cmd, shimDir string) (string, bool) {
	hits := Scan(Query{Names: []string{cmd}, ShimDir: shimDir})
	if len(hits) == 0 {
		return "", false
	}
	return hits[0].Path, true
}

// FirstOnPATH returns the first of names that resolves outside shimDir.
// Alias order is preserved. PATH directories are ReadDir once (Spec 031).
func FirstOnPATH(names []string, shimDir string) (string, bool) {
	if len(names) == 0 {
		return "", false
	}
	hits := Scan(Query{Names: names, ShimDir: shimDir})
	if len(hits) == 0 {
		return "", false
	}
	byBase := make(map[string]string, len(hits))
	for _, h := range hits {
		key := lookupKey(filepath.Base(h.Path))
		if _, ok := byBase[key]; !ok {
			byBase[key] = h.Path
		}
	}
	for _, n := range expandNames(names) {
		if path, ok := byBase[lookupKey(filepath.Base(n))]; ok {
			return path, true
		}
	}
	return hits[0].Path, true
}

// ScanPATH returns every distinct hit for names on the process PATH,
// skipping shimDir.
func ScanPATH(names []string, shimDir string) []Hit {
	return Scan(Query{Names: names, ShimDir: shimDir})
}

// IndexByBasename maps lookupKey(basename) → first PATH-order Hit.
func IndexByBasename(hits []Hit) map[string]Hit {
	out := make(map[string]Hit, len(hits))
	for _, h := range hits {
		key := lookupKey(filepath.Base(h.Path))
		if _, ok := out[key]; !ok {
			out[key] = h
		}
	}
	return out
}

// FirstInIndex returns the first of names present in a basename index.
func FirstInIndex(names []string, byBase map[string]Hit) (string, bool) {
	if len(byBase) == 0 {
		return "", false
	}
	for _, n := range expandNames(names) {
		if h, ok := byBase[lookupKey(filepath.Base(n))]; ok {
			return h.Path, true
		}
	}
	return "", false
}

// Scan walks Query.PathList (or the process PATH) and returns every
// distinct file that matches Names, skipping ShimDir. Order is PATH
// order; duplicate paths (case-insensitive on Windows) are dropped.
//
// Each directory is ReadDir once and matched in memory (Spec 031).
// On Windows each name is also tried with PATHEXT (or .exe/.cmd/.bat)
// so "pnpm" finds pnpm.exe and pnpm.cmd without the caller expanding.
func Scan(q Query) []Hit {
	if len(q.Names) == 0 {
		return nil
	}
	dirs := q.PathList
	if len(dirs) == 0 {
		dirs = filepath.SplitList(os.Getenv("PATH"))
	}
	want := candidateSet(q.Names)
	var hits []Hit
	seen := make(map[string]bool)
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		if q.ShimDir != "" && InsideDir(dir, q.ShimDir) {
			continue
		}
		entries, err := readDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if !want[lookupKey(name)] {
				continue
			}
			cand := filepath.Join(dir, name)
			if q.ShimDir != "" && InsideDir(cand, q.ShimDir) {
				continue
			}
			key := normalizeKey(cand)
			if seen[key] {
				continue
			}
			seen[key] = true
			hits = append(hits, Hit{Path: cand, Dir: dir})
		}
	}
	return hits
}

// SearchNames expands a tool alias for the current OS (Windows adds .exe).
func SearchNames(alias string) []string {
	if runtime.GOOS == "windows" {
		return []string{alias, alias + ".exe"}
	}
	return []string{alias}
}

// InsideDir reports whether path is inside dir (Windows case-insensitive).
func InsideDir(path, dir string) bool {
	if path == "" || dir == "" {
		return false
	}
	cleanPath := filepath.Clean(path)
	cleanDir := filepath.Clean(dir)
	if runtime.GOOS == "windows" {
		cleanPath = strings.ToLower(cleanPath)
		cleanDir = strings.ToLower(cleanDir)
	}
	if cleanPath == cleanDir {
		return true
	}
	sep := string(os.PathSeparator)
	if !strings.HasSuffix(cleanDir, sep) {
		cleanDir += sep
	}
	return strings.HasPrefix(cleanPath, cleanDir)
}

func normalizeKey(path string) string {
	clean := filepath.Clean(path)
	if runtime.GOOS == "windows" {
		return strings.ToLower(clean)
	}
	return clean
}

func lookupKey(name string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(name)
	}
	return name
}

func candidateSet(names []string) map[string]bool {
	set := make(map[string]bool, len(names)*4)
	for _, n := range expandNames(names) {
		if n == "" {
			continue
		}
		set[lookupKey(n)] = true
	}
	return set
}

func expandNames(names []string) []string {
	if runtime.GOOS != "windows" {
		return names
	}
	exts := windowsPathExts()
	out := make([]string, 0, len(names)*len(exts)+len(names))
	seen := make(map[string]bool)
	add := func(n string) {
		key := strings.ToLower(n)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, n)
	}
	for _, n := range names {
		add(n)
		if hasWindowsExt(n, exts) {
			continue
		}
		for _, ext := range exts {
			// One casing per extension: NTFS is case-insensitive.
			// Prefer lowercase so the returned ReadDir name still matches.
			add(n + strings.ToLower(ext))
		}
	}
	return out
}

func windowsPathExts() []string {
	raw := os.Getenv("PATHEXT")
	if raw == "" {
		return []string{".COM", ".EXE", ".BAT", ".CMD"}
	}
	parts := strings.Split(raw, ";")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if !strings.HasPrefix(p, ".") {
			p = "." + p
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return []string{".COM", ".EXE", ".BAT", ".CMD"}
	}
	return out
}

func hasWindowsExt(name string, exts []string) bool {
	lower := strings.ToLower(name)
	for _, ext := range exts {
		if strings.HasSuffix(lower, strings.ToLower(ext)) {
			return true
		}
	}
	return false
}
