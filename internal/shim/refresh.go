package shim

import (
	"log"
	"os"
	"path/filepath"
	"strings"
)

// RefreshDispatchers refreshes existing entries recorded by InstallShim. Both
// the managed alias and its source identity must match; a filename extension
// or a symlink alone never proves ownership. Legacy unrecorded entries are
// preserved with a diagnostic: re-activate the tool to register its aliases.
// The existing API is shared by the GUI and self-update entry points.
// refreshed counts actual replacements; failed counts entry or setup failures.
func RefreshDispatchers(shimDir, selfPath string) (refreshed, failed int) {
	entries, err := os.ReadDir(shimDir)
	if os.IsNotExist(err) {
		return 0, 0
	}
	if err != nil {
		log.Printf("[shim] read dispatcher directory: %v", err)
		return 0, 1
	}
	if len(entries) == 0 {
		return 0, 0
	}
	lock, err := lockDispatchers(shimDir)
	if err != nil {
		log.Printf("[shim] lock dispatchers: %v", err)
		return 0, 1
	}
	defer lock.Release()
	selfPath, err = filepath.Abs(selfPath)
	if err != nil {
		log.Printf("[shim] resolve dispatcher source: %v", err)
		return 0, 1
	}
	digest, err := dispatcherDigest(selfPath)
	if err != nil {
		log.Printf("[shim] read dispatcher source: %v", err)
		return 0, 1
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || dispatcherResidue(name) {
			continue
		}
		r, err := readDispatcherRecord(shimDir, name)
		if os.IsNotExist(err) {
			log.Printf("[shim] preserve unregistered entry %s (re-activate managed tools to register legacy dispatchers)", name)
			continue
		}
		if err != nil {
			failed++
			log.Printf("[shim] ownership record %s: %v", name, err)
			continue
		}
		dst := filepath.Join(shimDir, name)
		owned, err := dispatcherOwned(dst, r, selfPath)
		if err != nil || !owned {
			failed++
			log.Printf("[shim] preserve dispatcher %s: ownership identity mismatch or unreadable (%v)", name, err)
			continue
		}
		changed := true
		if os.PathSeparator == '\\' {
			// Hash equality also makes a copy-only volume idempotent.
			current, hashErr := dispatcherDigest(dst)
			if hashErr != nil {
				failed++
				log.Printf("[shim] read dispatcher %s: %v", name, hashErr)
				continue
			}
			changed = current != digest
		} else if target, linkErr := os.Readlink(dst); linkErr == nil && target == selfPath {
			changed = false
		}
		if changed {
			if err := replaceDispatcher(selfPath, dst); err != nil {
				failed++
				log.Printf("[shim] refresh dispatcher %s: %v", name, err)
				continue
			}
			refreshed++
		}
		r.Source = selfPath
		r.SHA256 = digest
		if err := writeDispatcherRecord(shimDir, r); err != nil {
			failed++
			log.Printf("[shim] record refreshed dispatcher %s: %v", name, err)
		}
	}
	return refreshed, failed
}

func dispatcherResidue(name string) bool {
	name = strings.ToLower(name)
	return strings.HasPrefix(name, ".xdev-dispatcher-") || strings.HasSuffix(name, ".old") || strings.HasSuffix(name, ".tmp") || strings.Contains(name, ".tmp-")
}
