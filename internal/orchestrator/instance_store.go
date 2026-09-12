package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Ticket 35 / C-08 (audit-fixes B5): one persisted store for remote instances.
//
// Before this change SaveInstance wrote a bare <home>/remote/<ID> file whose
// content was just the status string, while the GUI's ListRemoteInstances read
// <home>/instances/<id>.json — so `xdev remote list` (which only ran
// `docker ps`) and the GUI each saw a different half of the world, and neither
// could see the other's SSH instances. Instances now land in
// <home>/instances/<id>.json as an orchestrator.Instance (exactly the shape the
// GUI unmarshals); legacy markers stay readable for compatibility and are
// reported so the user knows how to migrate.
const (
	instancesDirName = "instances"
	legacyDirName    = "remote"
)

// sensitiveMetadataKeys never reach the persisted JSON, matched case-
// insensitively as substrings (deliberately broad). The SSH target carries the
// plaintext password, and Instance.Metadata is written to a world-readable
// file: SaveInstance is the choke point that keeps a credential out of it even
// if a caller ever stuffs one into Metadata.
var sensitiveMetadataKeys = []string{
	"password", "passwd", "passphrase", "secret", "token", "credential", "privatekey",
}

// SaveInstance persists inst as <xmdeDir>/instances/<id>.json.
func SaveInstance(xmdeDir string, inst *Instance) error {
	if inst == nil || inst.ID == "" {
		return fmt.Errorf("instance id is required to persist an instance")
	}
	if xmdeDir == "" {
		return fmt.Errorf("xdev home directory is required to persist an instance")
	}
	// The id becomes a filename (defense in depth: SSH ids are built from
	// user-controlled host/user, which sshFieldLooksUnsafe already restricts).
	if err := validateInstanceID(inst.ID); err != nil {
		return err
	}

	dir := filepath.Join(xmdeDir, instancesDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create instance dir: %w", err)
	}
	data, err := json.MarshalIndent(sanitizedInstance(inst), "", "  ")
	if err != nil {
		return fmt.Errorf("encode instance %s: %w", inst.ID, err)
	}
	if err := os.WriteFile(filepath.Join(dir, inst.ID+".json"), data, 0o644); err != nil {
		return fmt.Errorf("write instance %s: %w", inst.ID, err)
	}
	return nil
}

// ListInstances merges the live Docker containers with the persisted instances
// so `xdev remote list` and the GUI's ListRemoteInstances show the same set
// (C-08). Docker being unavailable is not fatal: persisted instances stay
// valid, and the reason is logged once.
func ListInstances(ctx context.Context, xmdeDir string) []Instance {
	out, err := ListContainers(ctx)
	if err != nil {
		log.Printf("remote: docker ps unavailable (%v) — listing persisted instances only", err)
		out = nil
	}
	for _, inst := range ListSavedInstances(xmdeDir) {
		if containsInstance(out, inst.ID) {
			continue
		}
		out = append(out, inst)
	}
	return out
}

// ListSavedInstances returns the persisted instances: the JSON store plus the
// legacy <home>/remote/<ID> markers (read-only compatibility). Entries are
// deduped by ID (the JSON store wins) and sorted for stable output.
func ListSavedInstances(xmdeDir string) []Instance {
	if xmdeDir == "" {
		return nil
	}
	saved := readInstancesDir(filepath.Join(xmdeDir, instancesDirName))
	seen := make(map[string]bool, len(saved))
	for _, inst := range saved {
		seen[strings.ToLower(inst.ID)] = true
	}
	for _, inst := range readLegacyMarkers(filepath.Join(xmdeDir, legacyDirName)) {
		if seen[strings.ToLower(inst.ID)] {
			continue // already migrated to the JSON store
		}
		// Migration hint (C-08): the legacy marker is still honoured, but the
		// user should recreate the instance so it lands in instances/.
		log.Printf("remote: legacy instance marker %q found (pre-C-08 layout in %s/) — listed for compatibility only; re-create the instance to migrate it to %s/%s.json",
			inst.ID, legacyDirName, instancesDirName, inst.ID)
		saved = append(saved, inst)
		seen[strings.ToLower(inst.ID)] = true
	}
	sort.Slice(saved, func(i, j int) bool { return saved[i].ID < saved[j].ID })
	return saved
}

// sanitizedInstance copies inst, dropping credential-bearing metadata keys so
// nothing secret is written to disk (C-08 hard constraint: SSHTarget, whose
// Password is plaintext, must never be persisted).
func sanitizedInstance(inst *Instance) *Instance {
	cp := *inst
	if inst.Addresses != nil {
		cp.Addresses = append([]string(nil), inst.Addresses...)
	}
	cp.Metadata = make(map[string]string, len(inst.Metadata))
	for k, v := range inst.Metadata {
		if isSensitiveMetadataKey(k) {
			log.Printf("remote: dropping metadata key %q from instance %s before persisting (credentials are never written to disk)", k, inst.ID)
			continue
		}
		cp.Metadata[k] = v
	}
	return &cp
}

func isSensitiveMetadataKey(key string) bool {
	k := strings.ToLower(key)
	compact := strings.NewReplacer("_", "", "-", "", " ", "", ".", "").Replace(k)
	for _, s := range sensitiveMetadataKeys {
		if strings.Contains(compact, s) {
			return true
		}
	}
	return false
}

// validateInstanceID keeps the id usable as a single filename inside the
// instance directory: no separators, no drive/ADS ":" and no "..".
func validateInstanceID(id string) error {
	if id == "" || id == "." || id == ".." {
		return fmt.Errorf("invalid instance id %q", id)
	}
	for _, r := range id {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-'
		if !ok {
			return fmt.Errorf("invalid instance id %q: only letters, digits, '.', '_' and '-' are allowed", id)
		}
	}
	return nil
}

// readInstancesDir reads the current <home>/instances/*.json store.
func readInstancesDir(dir string) []Instance {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil // absent store = no persisted instances
	}
	var out []Instance
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var inst Instance
		if err := json.Unmarshal(data, &inst); err != nil {
			log.Printf("remote: ignoring unreadable instance file %s: %v", e.Name(), err)
			continue
		}
		if inst.ID == "" {
			inst.ID = strings.TrimSuffix(e.Name(), ".json")
		}
		out = append(out, inst)
	}
	return out
}

// readLegacyMarkers reads pre-C-08 <home>/remote/<ID> markers: one file per
// instance whose content was the status string. They are read, never migrated
// in place — the caller logs the migration hint.
func readLegacyMarkers(dir string) []Instance {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Instance
	for _, e := range entries {
		if e.IsDir() || strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		status := strings.TrimSpace(string(data))
		if status == "" {
			status = "unknown"
		}
		out = append(out, Instance{
			ID:       e.Name(),
			Status:   status,
			Metadata: map[string]string{"adapter": "legacy", "legacyMarker": "true"},
		})
	}
	return out
}

// containsInstance reports whether list already covers instID. Docker reports
// 12-char short ids while `docker run -d` returns the full 64-char one, so an
// instance persisted by `remote create` is matched by prefix (>= 8 chars, the
// same tolerance docker's CLI uses) and never listed twice.
func containsInstance(list []Instance, instID string) bool {
	for _, inst := range list {
		if inst.ID == instID {
			return true
		}
		if len(inst.ID) >= 8 && len(instID) >= 8 {
			if strings.HasPrefix(inst.ID, instID) || strings.HasPrefix(instID, inst.ID) {
				return true
			}
		}
	}
	return false
}
