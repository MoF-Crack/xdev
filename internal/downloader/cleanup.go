package downloader

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/xmdemt/xdev/internal/util"
)

// ErrPartialBusy identifies a generation which another writer/cleaner owns.
var ErrPartialBusy = errors.New("partial download is busy")

// CleanupResult counts files actually removed and generations not touched.
// Lock files are permanent synchronization identities, not removable cache data.
type CleanupResult struct {
	Deleted     int `json:"deleted"`
	SkippedBusy int `json:"skippedBusy"`
	Failed      int `json:"failed"`
}

func (r *CleanupResult) Add(other CleanupResult) {
	r.Deleted += other.Deleted
	r.SkippedBusy += other.SkippedBusy
	r.Failed += other.Failed
}

// CleanupError preserves the cause while making partial results visible to UI
// callers that retain the historical error-only API.
type CleanupError struct {
	Result CleanupResult
	Cause  error
}

func (e *CleanupError) Error() string {
	return fmt.Sprintf("缓存清理：已删除 %d，忙碌跳过 %d，失败 %d: %v", e.Result.Deleted, e.Result.SkippedBusy, e.Result.Failed, e.Cause)
}
func (e *CleanupError) Unwrap() error { return e.Cause }
func (r CleanupResult) WithError(err error) error {
	if err == nil {
		return nil
	}
	return &CleanupError{r, err}
}

var archiveCacheSuffixes = []string{".tar.gz", ".tar.xz", ".tar.bz2", ".tar.zst", ".tar.lz4", ".zip", ".tgz", ".txz", ".tbz2", ".tar", ".gz", ".xz", ".bz2", ".zst", ".7z", ".rar", ".exe", ".msi", ".dmg", ".pkg", ".jar", ".sh", ".bin"}

// ArchiveCacheSuffixes returns an independent copy for filename parsing.
func ArchiveCacheSuffixes() []string { return append([]string(nil), archiveCacheSuffixes...) }

// IsArchiveCacheName is shared by cache classification and partial-name parsing.
func IsArchiveCacheName(name string) bool {
	for _, suffix := range archiveCacheSuffixes {
		if strings.HasSuffix(strings.ToLower(name), suffix) {
			return true
		}
	}
	return false
}

// PartialCachePath maps a managed part/meta/lock entry to its part and archive.
// Only a 16-lowercase-hex generation or a legacy archive/name without extension
// is accepted. A malformed generation must never be treated as a legacy archive.
func PartialCachePath(path string) (part, dest string, ok bool) {
	part = path
	if strings.HasSuffix(part, ".meta") {
		part = strings.TrimSuffix(part, ".meta")
	} else if strings.HasSuffix(part, ".lock") {
		part = strings.TrimSuffix(part, ".lock")
	}
	if !strings.HasSuffix(part, ".part") {
		return "", "", false
	}
	base := strings.TrimSuffix(filepath.Base(part), ".part")
	if idx := strings.LastIndexByte(base, '.'); idx >= 0 && isPartGeneration(base[idx+1:]) && idx > 0 {
		return part, filepath.Join(filepath.Dir(part), base[:idx]), true
	}
	if base != "" && (IsArchiveCacheName(base) || !strings.Contains(base, ".")) {
		return part, filepath.Join(filepath.Dir(part), base), true
	}
	return "", "", false
}

// ReadCacheDir enumerates the literal cache root. A missing directory is empty.
// On Windows ReadDir on a regular file can return an empty listing.
func ReadCacheDir(dir string) ([]os.DirEntry, error) {
	info, err := os.Stat(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("cache path is not a directory: %s", dir)
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return entries, nil
}

func partialPathsForCleanup(dir, dest string) ([]string, error) {
	entries, err := ReadCacheDir(dir)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		var part string
		if dest != "" {
			name := entry.Name()
			if strings.HasSuffix(name, ".meta") {
				name = strings.TrimSuffix(name, ".meta")
			} else {
				name = strings.TrimSuffix(name, ".lock")
			}
			legacy := filepath.Base(dest) + ".part"
			prefix := filepath.Base(dest) + "."
			if name == legacy {
				part = filepath.Join(dir, name)
			} else if strings.HasPrefix(name, prefix) && strings.HasSuffix(name, ".part") && isPartGeneration(strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".part")) {
				part = filepath.Join(dir, name)
			}
		} else {
			part, _, _ = PartialCachePath(path)
		}
		if part != "" {
			seen[part] = true
		}
	}
	out := make([]string, 0, len(seen))
	for part := range seen {
		out = append(out, part)
	}
	sort.Strings(out)
	return out, nil
}

// cleanupGeneration holds the SAME exclusive lock as downloadOnce through both
// removals. Never unlink its path: an existing waiter may already hold that inode.
func cleanupGeneration(part string) (CleanupResult, error) {
	return cleanupGenerationWith(part, os.Remove)
}

func cleanupGenerationWith(part string, remove func(string) error) (result CleanupResult, err error) {
	// Skip absent data without creating new stable lock files. Recheck both
	// names under lock before deleting anything.
	_, dataErr := os.Lstat(part)
	_, metaErr := os.Lstat(partValidatorFor(part))
	if os.IsNotExist(dataErr) && os.IsNotExist(metaErr) {
		return result, nil
	}
	lock, err := util.AcquireFileLock(part+".lock", 0)
	if err != nil {
		if strings.HasPrefix(err.Error(), "file is locked by another process:") {
			result.SkippedBusy = 1
			return result, fmt.Errorf("%w: %s", ErrPartialBusy, part)
		}
		result.Failed = 1
		return result, fmt.Errorf("lock partial generation %s: %w", part, err)
	}
	defer func() {
		if releaseErr := lock.Release(); releaseErr != nil {
			result.Failed++
			err = errors.Join(err, releaseErr)
		}
	}()
	for _, path := range []string{part, partValidatorFor(part)} {
		// Keep directories (even empty ones) outside this file-only API.
		fi, statErr := os.Lstat(path)
		if os.IsNotExist(statErr) {
			continue
		}
		if statErr != nil {
			result.Failed++
			err = errors.Join(err, statErr)
			continue
		}
		if fi.IsDir() {
			result.Failed++
			err = errors.Join(err, fmt.Errorf("refusing to delete partial directory %s", path))
			continue
		}
		if removeErr := remove(path); removeErr == nil {
			result.Deleted++
		} else if !os.IsNotExist(removeErr) {
			result.Failed++
			err = errors.Join(err, removeErr)
		}
	}
	return result, err
}

// CleanupPartialPath cleans exactly one recognized generation, including orphan
// metadata. It returns ErrPartialBusy without touching either file when locked.
func CleanupPartialPath(path string) (CleanupResult, error) {
	part, _, ok := PartialCachePath(path)
	if !ok {
		return CleanupResult{Failed: 1}, fmt.Errorf("invalid partial generation path %q", path)
	}
	result, err := cleanupGeneration(part)
	return result, result.WithError(err)
}

func cleanupPartialPaths(paths []string) (result CleanupResult, err error) {
	for _, part := range paths {
		r, e := cleanupGeneration(part)
		result.Add(r)
		err = errors.Join(err, e)
	}
	return result, result.WithError(err)
}

// CleanupPartialFileResult cleans every known generation for one destination.
func CleanupPartialFileResult(dest string) (CleanupResult, error) {
	paths, err := partialPathsForCleanup(filepath.Dir(dest), dest)
	if err != nil {
		return CleanupResult{Failed: 1}, err
	}
	return cleanupPartialPaths(paths)
}

// CleanupAllPartialsInDirResult enumerates a literal directory, not a glob.
func CleanupAllPartialsInDirResult(dir string) (CleanupResult, error) {
	paths, err := partialPathsForCleanup(dir, "")
	if err != nil {
		return CleanupResult{Failed: 1}, err
	}
	return cleanupPartialPaths(paths)
}
