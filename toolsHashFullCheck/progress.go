// Package main implements the toolsHashFullCheck hash-filling pipeline.
//
// progress.go manages the JSON state file (progress.json) that tracks the
// download and hash-computation status of every manifest archive entry.
// The state file enables:
//
//   - Break-resume: entries already marked "cleaned" or "hashed" are skipped
//     on subsequent runs, so an interrupted pipeline can be re-run without
//     re-downloading completed entries.
//   - Auditability: each entry records its source URL, computed hash, file
//     size, and any error encountered, providing a full audit trail.
//   - Cleanup tracking: entries transition through states
//     pending → downloaded → hashed → cleaned, so the pipeline knows which
//     downloaded files are safe to delete.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// Entry states. An entry progresses through these states:
//
//	pending → downloaded → hashed → cleaned
//	             ↓
//	          failed (any step can fail)
const (
	statePending    = "pending"
	stateDownloaded = "downloaded"
	stateHashed     = "hashed"
	stateCleaned    = "cleaned"
	stateFailed     = "failed"
)

// EntryState records the processing status of a single manifest archive entry.
type EntryState struct {
	Tool    string `json:"tool"`
	Version string `json:"version"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
	URL     string `json:"url"`
	Status  string `json:"status"`
	Hash    string `json:"hash,omitempty"`
	Error   string `json:"error,omitempty"`
	Size    int64  `json:"size"`
}

// Progress is the top-level state file structure. It is serialized to
// progress.json and updated after every state transition.
type Progress struct {
	mu      sync.Mutex `json:"-"`
	Entries []EntryState `json:"entries"`
	Updated string       `json:"updated"`
	path    string       `json:"-"`
}

// entryKey returns a unique identifier for an entry within the progress map.
func entryKey(tool, version, osName, arch string) string {
	return fmt.Sprintf("%s@%s/%s/%s", tool, version, osName, arch)
}

// LoadProgress reads the state file from disk. If the file does not exist
// or cannot be parsed, an empty Progress is returned (the pipeline will
// rebuild the entry list from the manifests).
func LoadProgress(path string) *Progress {
	p := &Progress{path: path}
	data, err := os.ReadFile(path)
	if err != nil {
		return p
	}
	// Decode into a temporary struct (without the mutex/path fields)
	var raw struct {
		Entries []EntryState `json:"entries"`
		Updated string       `json:"updated"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return p
	}
	p.Entries = raw.Entries
	p.Updated = raw.Updated
	return p
}

// Save writes the state file to disk atomically (write to .tmp, then rename).
// Must be called with the mutex held.
func (p *Progress) Save() error {
	p.Updated = time.Now().Format(time.RFC3339)
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	tmp := p.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p.path)
}

// SetPath sets the on-disk path for the state file.
func (p *Progress) SetPath(path string) {
	p.path = path
}

// FindEntry looks up an entry by its identifying fields. Returns nil if
// not found.
func (p *Progress) FindEntry(tool, version, osName, arch string) *EntryState {
	for i := range p.Entries {
		if p.Entries[i].Tool == tool &&
			p.Entries[i].Version == version &&
			p.Entries[i].OS == osName &&
			p.Entries[i].Arch == arch {
			return &p.Entries[i]
		}
	}
	return nil
}

// EnsureEntry finds an existing entry or creates a new one in "pending" state.
func (p *Progress) EnsureEntry(tool, version, osName, arch, url string, size int64) *EntryState {
	p.mu.Lock()
	defer p.mu.Unlock()
	if e := p.FindEntry(tool, version, osName, arch); e != nil {
		return e
	}
	e := EntryState{
		Tool:    tool,
		Version: version,
		OS:      osName,
		Arch:    arch,
		URL:     url,
		Status:  statePending,
		Size:    size,
	}
	p.Entries = append(p.Entries, e)
	return &p.Entries[len(p.Entries)-1]
}

// UpdateEntry atomically updates an entry's state and persists the progress file.
func (p *Progress) UpdateEntry(tool, version, osName, arch, status, hash, errMsg string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	e := p.FindEntry(tool, version, osName, arch)
	if e == nil {
		return fmt.Errorf("entry not found: %s", entryKey(tool, version, osName, arch))
	}
	e.Status = status
	if hash != "" {
		e.Hash = hash
	}
	if errMsg != "" {
		e.Error = errMsg
	} else if status != stateFailed {
		e.Error = ""
	}
	return p.Save()
}

// IsComplete returns true if the entry has been fully processed (hashed or
// cleaned). These entries are skipped on re-runs.
func (e *EntryState) IsComplete() bool {
	return e.Status == stateHashed || e.Status == stateCleaned
}

// Stats returns counts of entries in each state.
func (p *Progress) Stats() (pending, downloaded, hashed, cleaned, failed int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, e := range p.Entries {
		switch e.Status {
		case statePending:
			pending++
		case stateDownloaded:
			downloaded++
		case stateHashed:
			hashed++
		case stateCleaned:
			cleaned++
		case stateFailed:
			failed++
		}
	}
	return
}

// ResetFailed resets all "failed" entries back to "pending" so they can be
// retried on the next pipeline run. This is useful when failures were caused
// by transient network issues.
func (p *Progress) ResetFailed() {
	p.mu.Lock()
	defer p.mu.Unlock()
	count := 0
	for i := range p.Entries {
		if p.Entries[i].Status == stateFailed {
			p.Entries[i].Status = statePending
			p.Entries[i].Error = ""
			count++
		}
	}
	p.Save()
	fmt.Printf("Reset %d failed entries to pending.\n", count)
}
