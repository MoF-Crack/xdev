package manifest

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/xmdemt/xdev/internal/util"
	"golang.org/x/sync/errgroup"
)

// Each load takes one snapshot of the files it will parse. Cache keys use
// these same bytes, so an edit during parsing cannot poison the next load.
type manifestFile struct {
	source, relative string
	data             []byte
	readErr          error
}

// fs.ReadDir sorts each directory by name. Visit exactly two levels, in
// precedence order: root first, then explicitly registered third-party files.
func collectSource(root fs.FS, source string) ([]manifestFile, error) {
	entries, err := fs.ReadDir(root, ".")
	if err != nil {
		return nil, fmt.Errorf("read %s manifest directory: %w", source, err)
	}
	var files []manifestFile
	appendYAML := func(dir string, entries []fs.DirEntry) {
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".yaml") {
				files = append(files, manifestFile{source: source, relative: path.Join(dir, e.Name())})
			}
		}
	}
	appendYAML(".", entries)
	for _, e := range entries {
		if e.Name() != thirdPartySubdir {
			continue
		}
		info, err := fs.Stat(root, thirdPartySubdir)
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", thirdPartySubdir, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("%s manifest collection is not a directory", thirdPartySubdir)
		}
		children, err := fs.ReadDir(root, thirdPartySubdir)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", thirdPartySubdir, err)
		}
		appendYAML(thirdPartySubdir, children)
		break
	}
	var group errgroup.Group
	group.SetLimit(parallelLoadConcurrency)
	for i := range files {
		group.Go(func() error {
			f := &files[i]
			f.data, f.readErr = fs.ReadFile(root, f.relative)
			return nil
		})
	}
	_ = group.Wait()
	return files, nil
}

func collectDiskSource(dir, kind string) ([]manifestFile, error) {
	root, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	return collectSource(os.DirFS(root), kind+":"+filepath.ToSlash(root))
}

// Compatibility entry points use the same snapshot and content identity as
// production cache loading; no independent metadata-only fingerprint remains.
func computeFingerprint(dir string) string {
	files, err := collectDiskSource(dir, "disk")
	if err != nil {
		return fmt.Sprintf("error:%v", err)
	}
	fp, ok := catalogFingerprint(nil, files)
	if !ok {
		return "error:unreadable manifest"
	}
	return fp
}

func loadFromEmbedded() (*Catalog, error) {
	files, err := collectSource(embeddedFS, "embedded")
	if err != nil {
		return nil, err
	}
	catalog, _, err := parseSource(files, false)
	return catalog, err
}

func catalogSources(dir string) (base, disk []manifestFile, err error) {
	if embeddedFS != nil {
		base, err = collectSource(embeddedFS, "embedded")
		if err != nil {
			return nil, nil, err
		}
	}
	if dir != "" {
		// Only a missing root is optional. A failure enumerating an existing
		// directory is distinct from a successful enumeration with zero files.
		if info, statErr := os.Stat(dir); statErr == nil {
			if !info.IsDir() {
				return nil, nil, fmt.Errorf("manifest root %s is not a directory", dir)
			}
			disk, err = collectDiskSource(dir, "disk")
			if err != nil {
				return nil, nil, fmt.Errorf("load %s: %w", dir, err)
			}
		} else if !errors.Is(statErr, fs.ErrNotExist) {
			return nil, nil, statErr
		}
	}
	if embeddedFS == nil && len(disk) == 0 {
		if repo, ok := repoManifestDir(); ok {
			disk, err = collectDiskSource(repo, "repo")
		}
	}
	return base, disk, err
}

// Lenient parsing is reserved for disk overlays over an embedded baseline.
// Incomplete overlays remain visible via warnings and are never cached.
func parseSource(files []manifestFile, lenient bool) (*Catalog, bool, error) {
	tools := make([]*Tool, len(files))
	errs := make([]error, len(files))
	var group errgroup.Group
	group.SetLimit(parallelLoadConcurrency)
	for i := range files {
		group.Go(func() error {
			f := &files[i]
			tools[i], errs[i] = loadOneFile(path.Base(f.relative), func() ([]byte, error) { return f.data, f.readErr })
			return nil
		})
	}
	_ = group.Wait()
	catalog := &Catalog{tools: map[string]*Tool{}}
	complete := true
	for i, tool := range tools {
		if err := errs[i]; err != nil {
			if !lenient {
				return nil, false, fmt.Errorf("%s %s: %w", files[i].source, files[i].relative, err)
			}
			reason := "unparseable"
			if files[i].readErr != nil {
				reason = "unreadable"
			}
			log.Printf("[manifest] WARNING: skip %s %s: %v", reason, files[i].relative, err)
			complete = false
			continue
		}
		catalog.tools[tool.Name] = tool
	}
	catalog.markInstallability()
	return catalog, complete, nil
}

// The ordered source tuples distinguish equal bytes at different paths, and
// platform identifies the cached, host-dependent Tool.Installable field.
func catalogFingerprint(base, disk []manifestFile) (string, bool) {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00%t\x00", runtime.GOOS, runtime.GOARCH, embeddedFS != nil)
	ordered := make([]manifestFile, 0, len(base)+len(disk))
	ordered = append(ordered, base...)
	ordered = append(ordered, disk...)
	slices.SortFunc(ordered, func(a, b manifestFile) int {
		if c := strings.Compare(a.source, b.source); c != 0 {
			return c
		}
		return strings.Compare(a.relative, b.relative)
	})
	for _, f := range ordered {
		if f.readErr != nil {
			return "", false
		}
		fmt.Fprintf(h, "%s\x00%s\x00%x\n", f.source, f.relative, sha256.Sum256(f.data))
	}
	return fmt.Sprintf("%x", h.Sum(nil)), true
}

const catalogCacheSchema = 2

type catalogCacheEntry struct {
	Schema      int    `json:"schema"`
	Fingerprint string `json:"fingerprint"`
	Catalog     struct {
		Tools []*Tool `json:"tools"`
	} `json:"catalog"`
}

// LoadCatalogCached caches YAML parsing across processes. An absent schema,
// old fingerprint, or incomplete cache triggers a fresh parse of the snapshot.
func LoadCatalogCached(dir, cacheDir string) (*Catalog, error) {
	base, disk, err := catalogSources(dir)
	if err != nil {
		return nil, err
	}
	var fp string
	cacheable := false
	cachePath := filepath.Join(cacheDir, "manifest-cache.dat")
	if cacheDir != "" {
		fp, cacheable = catalogFingerprint(base, disk)
	}
	if cacheable {
		if data, err := os.ReadFile(cachePath); err == nil {
			var entry catalogCacheEntry
			if json.Unmarshal(data, &entry) == nil && entry.Schema == catalogCacheSchema && entry.Fingerprint == fp && entry.Catalog.Tools != nil {
				c := &Catalog{tools: map[string]*Tool{}}
				for _, t := range entry.Catalog.Tools {
					if t == nil || t.Name == "" {
						break
					}
					c.tools[t.Name] = t
				}
				if len(c.tools) == len(entry.Catalog.Tools) {
					return c, nil
				}
			}
		}
	}
	cat, complete, err := parseSource(disk, embeddedFS != nil)
	if err != nil {
		return nil, err
	}
	if embeddedFS != nil {
		embedded, _, err := parseSource(base, false)
		if err != nil {
			return nil, err
		}
		cat = mergeCatalogs(embedded, cat)
	}
	if cacheable && complete {
		entry := catalogCacheEntry{Schema: catalogCacheSchema, Fingerprint: fp}
		entry.Catalog.Tools = cat.Tools()
		if err := os.MkdirAll(cacheDir, 0755); err == nil {
			if data, err := json.Marshal(entry); err == nil {
				if err := util.AtomicWriteFile(cachePath, data, 0644); err != nil {
					log.Printf("[manifest] failed to write cache %s: %v", cachePath, err)
				}
			}
		}
	}
	return cat, nil
}
