// Package manifest describes the metadata of supported tools and provides
// loading, caching, and version enumeration capabilities.
//
// A manifest is a YAML file shipped in this repository under
// /manifests/<tool>.yaml, describing:
//
//   - how to enumerate remote versions (e.g. scrape a JSON API)
//   - what artefacts (archives / installers) each version ships
//   - what shims the tool needs (e.g. python, python3)
//   - what environment variables to set on activation
//
// # Loading
//
// Manifests can be loaded from three sources (in priority order):
//  1. Embedded FS — manifests baked into the binary via go:embed, so
//     the tool works even without the manifests/ directory on disk
//  2. Disk directory — $XMDEV_HOME/manifests/ for user-customized manifests
//  3. Repository-relative — manifests/ in the source tree (dev mode)
//
// LoadCatalogCached() loads from the first available source and caches
// the result in memory for the process lifetime.
//
// # Remote Version Discovery
//
// When a manifest has a RemoteSource configured (e.g. type: "github",
// url: "golang/go"), the refresh command can fetch the latest version
// list from the upstream source and merge it into the local manifest.
//
// # Template Variables
//
// The EnvVars map supports "{{install}}" template variables that are
// replaced at activation time with the actual installation path.
// For example: {"GOROOT": "{{install}}"} becomes {"GOROOT": "E:/XDev/install/go/1.22.0"}.
//
// # SHA-256 Placeholders
//
// Archive SHA-256 fields that are all zeros (e.g. "000000...") are
// treated as placeholders — verification is skipped for these entries.
// This allows shipping manifests before all hashes have been computed.
// Use `xdev fill-sha256` to populate them before release.
package manifest

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/xmdemt/xdev/internal/util"
	"gopkg.in/yaml.v3"
)

// embeddedFS is set by InitEmbedded to provide manifests baked
// into the binary so the tool works even when the manifests/
// directory is not present on disk.
var embeddedFS fs.FS

// Tool describes one supported tool (python, node, go, ...).
type Tool struct {
	Name        string            `yaml:"name"`
	DisplayName string            `yaml:"display_name"`
	Description string            `yaml:"description"`
	Homepage    string            `yaml:"homepage"`
	Category    string            `yaml:"category"` // language | mobile | build | system
	Shims       []string          `yaml:"shims"`
	EnvVars     map[string]string `yaml:"env_vars"`
	VersionList []Version         `yaml:"versions"`
	Remote      RemoteSource      `yaml:"remote"`
	Latest      string            `yaml:"latest"`
	LTS         string            `yaml:"lts"`
	InstallRoot string            `yaml:"install_root"`
	RequiredPlugin  []string       `yaml:"required_plugins,omitempty"` // required plugin name list
}

// Version describes a single release of a tool.
type Version struct {
	Version  string    `yaml:"version"` // e.g. "3.12.0"
	Stable   bool      `yaml:"stable"`
	Release  string    `yaml:"release"` // ISO date
	Archives []Archive `yaml:"archives"`
}

// Install kind constants. An Archive's InstallKind field controls how the
// installer treats the downloaded file:
//
//   - "archive"    (default): extract the file (zip/tar.gz/...) into the
//                 install directory. The file is never executed.
//   - "executable": the downloaded file is a self-installing binary
//                 (e.g. rustup-init.exe). After SHA-256 verification it is
//                 run with InstallArgs; it is expected to install itself.
//   - "script":    the downloaded file is a shell script (e.g. sh.rustup.rs).
//                 After SHA-256 verification it is executed via "sh".
//
// Unknown values always fall back to "archive" so that a typo or a future
// kind string can never accidentally cause code execution (trust model:
// running untrusted code requires explicit opt-in).
const (
	InstallKindArchive    = "archive"
	InstallKindExecutable = "executable"
	InstallKindScript     = "script"
)

// Archive is one downloadable file (the manifest can list several
// archives per version, one per OS/arch).
type Archive struct {
	OS          string   `yaml:"os"`     // windows | darwin | linux | any
	Arch        string   `yaml:"arch"`   // amd64 | arm64 | 386 | any
	Format      string   `yaml:"format"` // zip | tar.xz | tar.gz | msi | pkg | dmg | exe | deb
	URL         string   `yaml:"url"`
	MirrorURL   string   `yaml:"mirror_url"`  // optional preferred mirror (singular, legacy)
	MirrorURLs  []string `yaml:"mirror_urls"` // optional multiple mirror candidates (plural, preferred)
	SHA256      string   `yaml:"sha256"`
	Size        int64    `yaml:"size"`
	Strip       int      `yaml:"strip"`
	InstallArgs []string `yaml:"install_args"`
	InstallKind string  `yaml:"install_kind,omitempty"` // archive|executable|script; empty => archive
}

// EffectiveKind returns the resolved install kind, defaulting to
// InstallKindArchive when the field is empty or holds an unrecognised
// value. Only "executable" and "script" are returned as-is; everything
// else collapses to the safe "archive" default so that execution of
// downloaded code always requires an explicit manifest declaration.
func (a Archive) EffectiveKind() string {
	switch a.InstallKind {
	case InstallKindExecutable, InstallKindScript:
		return a.InstallKind
	default:
		return InstallKindArchive
	}
}

// EffectiveMirrorURLs returns the list of candidate mirror URLs for this
// archive.
//
// Priority:
//  1. mirror_urls (plural) — the new preferred field
//  2. mirror_url (singular) — legacy field, wrapped in a single-element slice
//  3. nil — no mirror configured
//
// This ensures backward compatibility: manifests that only set mirror_url
// (singular) continue to work without modification.
func (a Archive) EffectiveMirrorURLs() []string {
	if len(a.MirrorURLs) > 0 {
		return a.MirrorURLs
	}
	if a.MirrorURL != "" {
		return []string{a.MirrorURL}
	}
	return nil
}

// RemoteSource describes how to discover new versions dynamically.
type RemoteSource struct {
	Type    string `yaml:"type"`    // github | html | json | none
	URL     string `yaml:"url"`     // GitHub "owner/repo" or full URL
	Path    string `yaml:"path"`    // JSON dot-separated field path or HTML selector
	Pattern string `yaml:"pattern"` // regex to extract version from tag/text
}

// Catalog is the full collection of tools, loaded from disk.
type Catalog struct {
	tools map[string]*Tool
}

// LoadCatalog loads every manifests/<tool>.yaml in the given directory.
//
// In a real install the manifests ship inside the binary as embedded
// data; for now we read from disk for easy iteration.
//
// If the requested directory does not exist OR is empty, we
// transparently fall back to the repo-relative manifests/ directory
// (development mode).  This keeps the first-run experience working
// even before the user has populated their personal manifest cache.
func LoadCatalog(dir string) (*Catalog, error) {
	// When embedded manifests are available, always prefer them.
	// The embedded FS is baked into the binary at compile time and
	// is guaranteed to be complete.  On-disk manifests may be
	// incomplete (e.g. only a few files copied during development)
	// or stale.
	if embeddedFS != nil {
		return loadFromEmbedded()
	}

	c := &Catalog{tools: map[string]*Tool{}}

	needsFallback := false
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		needsFallback = true
	} else {
		// Directory exists - is it empty?
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, fmt.Errorf("read manifest dir %s: %w", dir, err)
		}
		hasYAML := false
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".yaml") {
				hasYAML = true
				break
			}
		}
		if !hasYAML {
			needsFallback = true
		}
	}

	if needsFallback {
		if alt, ok := repoManifestDir(); ok {
			dir = alt
		} else {
			// No manifests anywhere - return an empty catalog so
			// the rest of the program still works (e.g. `available`
			// prints an empty table rather than crashing).
			return c, nil
		}
	}

	return loadFromDir(dir)
}

// loadFromDir reads every .yaml file in dir and returns a Catalog.
func loadFromDir(dir string) (*Catalog, error) {
	c := &Catalog{tools: map[string]*Tool{}}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read manifest dir %s: %w", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var t Tool
		if err := yaml.Unmarshal(data, &t); err != nil {
			return nil, fmt.Errorf("parse %s: %w", e.Name(), err)
		}
		if t.Name == "" {
			t.Name = strings.TrimSuffix(e.Name(), ".yaml")
		}
		c.tools[t.Name] = &t
	}
	return c, nil
}

// Tool returns a tool by name.
func (c *Catalog) Tool(name string) (*Tool, bool) {
	t, ok := c.tools[name]
	return t, ok
}

// Tools returns every tool in the catalog, sorted by name.
func (c *Catalog) Tools() []*Tool {
	out := make([]*Tool, 0, len(c.tools))
	for _, t := range c.tools {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Search returns tools whose name, display name or description match
// the query (case-insensitive substring).
func (c *Catalog) Search(q string) []*Tool {
	q = strings.ToLower(q)
	out := make([]*Tool, 0)
	for _, t := range c.tools {
		if strings.Contains(strings.ToLower(t.Name), q) ||
			strings.Contains(strings.ToLower(t.DisplayName), q) ||
			strings.Contains(strings.ToLower(t.Description), q) {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Versions returns the sorted list of known versions for a tool.
func (t *Tool) Versions() []string {
	out := make([]string, 0, len(t.VersionList))
	for _, v := range t.VersionList {
		out = append(out, v.Version)
	}
	return out
}

// FindVersion looks up a concrete version struct.
func (t *Tool) FindVersion(v string) (*Version, bool) {
	for i := range t.VersionList {
		if t.VersionList[i].Version == v {
			return &t.VersionList[i], true
		}
	}
	return nil, false
}

// ArchiveForCurrent picks the archive whose OS/Arch matches the
// current platform, falling back to "any" if no exact match exists.
func (v *Version) ArchiveForCurrent(osName, arch string) (*Archive, bool) {
	// 1. exact os+arch
	for i := range v.Archives {
		a := &v.Archives[i]
		if a.OS == osName && a.Arch == arch {
			return a, true
		}
	}
	// 2. any arch
	for i := range v.Archives {
		a := &v.Archives[i]
		if a.OS == osName && a.Arch == "any" {
			return a, true
		}
	}
	// 3. any os
	for i := range v.Archives {
		a := &v.Archives[i]
		if a.OS == "any" {
			return a, true
		}
	}
	return nil, false
}

// DefaultVersion returns Latest, falling back to LTS, then the first
// listed version, then "".
func (t *Tool) DefaultVersion() string {
	if t.Latest != "" {
		return t.Latest
	}
	if t.LTS != "" {
		return t.LTS
	}
	if len(t.VersionList) > 0 {
		return t.VersionList[0].Version
	}
	return ""
}

// MarshalTool serialises a Tool back to YAML bytes.
func MarshalTool(t *Tool) ([]byte, error) {
	return yaml.Marshal(t)
}

// repoManifestDir tries to find a sibling /manifests directory when
// running from a development checkout.  This is best-effort and only
// used as a fallback when the user's $XMDEV_HOME/manifests is empty.
func repoManifestDir() (string, bool) {
	// Try the executable path first (works in production where the
	// binary is shipped as <root>/bin/xdev).  Walk up to 6
	// parents to cover common layouts (bin/, cmd/, ...).
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		for i := 0; i < 6; i++ {
			candidate := filepath.Join(dir, "manifests")
			if info, err := os.Stat(candidate); err == nil && info.IsDir() {
				return candidate, true
			}
			dir = filepath.Dir(dir)
		}
	}
	// Fallback: walk up from the current working directory.  This
	// makes `go test` (whose binary lives in the build cache) and
	// `xdev run --manifest=...` work without extra wiring.
	if cwd, err := os.Getwd(); err == nil {
		dir := cwd
		for i := 0; i < 6; i++ {
			candidate := filepath.Join(dir, "manifests")
			if info, err := os.Stat(candidate); err == nil && info.IsDir() {
				return candidate, true
			}
			dir = filepath.Dir(dir)
		}
	}
	return "", false
}

// Time helpers - mostly so manifest code can express "released on".

// ParseRelease parses t.Release into time.Time, returning ok=false
// when empty.
func (v *Version) ParseRelease() (time.Time, bool) {
	if v.Release == "" {
		return time.Time{}, false
	}
	t, err := time.Parse("2006-01-02", v.Release)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// --- Catalog caching ---

// cachedCatalog is the JSON-serializable form of a Catalog used for
// disk-based caching.  The internal map is converted to a sorted slice
// so the serialization is deterministic.
type cachedCatalog struct {
	Tools []*Tool `json:"tools"`
}

// LoadCatalogCached works like LoadCatalog but uses a disk-based cache
// to avoid re-parsing YAML files on every invocation.
//
// If cacheDir is empty, caching is skipped and LoadCatalog is called
// directly (backward compatible).  The cache file is stored at
// <cacheDir>/manifest-cache.dat and is validated against a fingerprint
// of the manifest directory (file names + mod times + sizes).
func LoadCatalogCached(manifestDir string, cacheDir string) (*Catalog, error) {
	if cacheDir == "" {
		return LoadCatalog(manifestDir)
	}

	// When embedded manifests are available, use the executable's
	// modification time as the fingerprint.  This is cheap and
	// correctly invalidates the cache when the binary is updated
	// (which is the only time the embedded manifests change).
	var fp string
	if embeddedFS != nil {
		if exe, err := os.Executable(); err == nil {
			if info, err := os.Stat(exe); err == nil {
				fp = fmt.Sprintf("embedded:%d:%d", info.ModTime().UnixNano(), info.Size())
			}
		}
		if fp == "" {
			fp = "embedded:unknown"
		}
	} else {
		// Resolve the actual directory that will be used for loading,
		// so the fingerprint matches the real source of truth.
		actualDir := resolveManifestDir(manifestDir)
		fp = fingerprintDir(actualDir)
	}
	cachePath := filepath.Join(cacheDir, "manifest-cache.dat")

	// Try to read the cached catalog.
	if data, err := os.ReadFile(cachePath); err == nil {
		var entry struct {
			Fingerprint string        `json:"fingerprint"`
			Catalog     cachedCatalog `json:"catalog"`
		}
		if json.Unmarshal(data, &entry) == nil && entry.Fingerprint == fp {
			// Cache hit — reconstruct the Catalog.
			c := &Catalog{tools: map[string]*Tool{}}
			for _, t := range entry.Catalog.Tools {
				c.tools[t.Name] = t
			}
			return c, nil
		}
	}

	// Cache miss — load from disk the slow way.
	cat, err := LoadCatalog(manifestDir)
	if err != nil {
		return nil, err
	}

	// Write cache atomically.
	if err := os.MkdirAll(cacheDir, 0o755); err == nil {
		tools := cat.Tools() // already sorted by name
		entry := struct {
			Fingerprint string        `json:"fingerprint"`
			Catalog     cachedCatalog `json:"catalog"`
		}{
			Fingerprint: fp,
			Catalog:     cachedCatalog{Tools: tools},
		}
		if data, err := json.Marshal(entry); err == nil {
			if err := util.AtomicWriteFile(cachePath, data, 0o644); err != nil {
				log.Printf("[manifest] failed to write cache %s: %v", cachePath, err)
			}
		}
	}

	return cat, nil
}

// resolveManifestDir returns the actual directory from which manifests
// will be loaded, mirroring the fallback logic in LoadCatalog without
// actually reading the files.
func resolveManifestDir(dir string) string {
	needsFallback := false
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		needsFallback = true
	} else {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return dir
		}
		hasYAML := false
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".yaml") {
				hasYAML = true
				break
			}
		}
		if !hasYAML {
			needsFallback = true
		}
	}
	if needsFallback {
		if alt, ok := repoManifestDir(); ok {
			return alt
		}
	}
	return dir
}

// loadFromEmbedded reads all YAML manifests baked into the binary.
func loadFromEmbedded() (*Catalog, error) {
	c := &Catalog{tools: map[string]*Tool{}}
	entries, err := fs.ReadDir(embeddedFS, ".")
	if err != nil {
		return nil, fmt.Errorf("read embedded manifests: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		data, err := fs.ReadFile(embeddedFS, e.Name())
		if err != nil {
			return nil, fmt.Errorf("read embedded %s: %w", e.Name(), err)
		}
		var t Tool
		if err := yaml.Unmarshal(data, &t); err != nil {
			return nil, fmt.Errorf("parse embedded %s: %w", e.Name(), err)
		}
		if t.Name == "" {
			t.Name = strings.TrimSuffix(e.Name(), ".yaml")
		}
		c.tools[t.Name] = &t
	}
	return c, nil
}

// fingerprintDir computes a short hash that changes whenever the set of
// .yaml files or their contents change.  It uses file name + mod time +
// size so we don't need to read every file.
func fingerprintDir(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		// If we can't read the dir, the fingerprint will be unique
		// each time, effectively disabling the cache.
		return fmt.Sprintf("err:%v:%d", err, time.Now().UnixNano())
	}

	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".yaml") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	var sb strings.Builder
	for _, name := range names {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			sb.WriteString(name)
			sb.WriteString("err")
			continue
		}
		sb.WriteString(name)
		sb.WriteString(fmt.Sprintf("%d:%d", info.ModTime().UnixNano(), info.Size()))
	}

	h := sha256.Sum256([]byte(sb.String()))
	return fmt.Sprintf("%x", h[:16])
}
