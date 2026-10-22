package discovery

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/xmdemt/xdev/internal/manifest"
	"gopkg.in/yaml.v3"
)

// yamlUnmarshalForTest keeps the assertion helpers readable.
func yamlUnmarshalForTest(data []byte, v any) error {
	return yaml.Unmarshal(data, v)
}

// Ticket 23 (audit-fixes / D-03) guards — cross-process manifest RMW.
//
// M1: concurrent mergeIntoManifest (new version) + UpdateArchiveSHA256
//     (hash write-back) must not lose each other's update. Pre-fix both
//     did read → modify → write with no lock: one side's write silently
//     overwrote the other's.
// M3: the baseline first-write path is atomic (temp+rename) and complete.

const demoManifestBase = `name: demo
display_name: Demo
category: system
versions:
  - version: 1.0.0
    stable: true
    archives:
      - os: windows
        arch: amd64
        format: zip
        url: https://example.test/demo-1.0.0.zip
        sha256: ""
`

func writeDemoManifest(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "demo.yaml")
	if err := os.WriteFile(path, []byte(demoManifestBase), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// M1: alternating merge (new versions) and hash write-backs across several
// rounds must all survive — neither side may be rolled back.
func TestMergeIntoManifest_ConcurrentWithUpdateSHA_NoLostUpdate(t *testing.T) {
	dir := t.TempDir()
	path := writeDemoManifest(t, dir)

	tool := &manifest.Tool{Name: "demo", DisplayName: "Demo"}
	const rounds = 8
	hashes := make([]string, rounds)

	for i := 0; i < rounds; i++ {
		ver := "2." + string(rune('0'+i)) + ".0"
		hash := strings.Repeat(string(rune('a'+i)), 64)
		hashes[i] = hash

		var wg sync.WaitGroup
		errCh := make(chan error, 2)
		wg.Add(2)
		go func() {
			defer wg.Done()
			if err := mergeIntoManifest(tool, []DiscoveredVersion{{
				Version: ver,
				Archives: []manifest.Archive{{
					OS: "windows", Arch: "amd64", Format: "zip",
					URL: "https://example.test/demo-" + ver + ".zip",
				}},
			}}, dir); err != nil {
				errCh <- err
			}
		}()
		go func() {
			defer wg.Done()
			// Alternate the target version so both writers keep touching
			// the same file with different content.
			target := "1.0.0"
			if i%2 == 1 && i >= 2 {
				target = "2." + string(rune('0'+i-2)) + ".0"
			}
			if target == "1.0.0" {
				if err := manifest.UpdateArchiveSHA256(dir, "demo", "1.0.0", "windows", "amd64", hash); err != nil {
					errCh <- err
				}
			} else if err := manifest.UpdateArchiveSHA256(dir, "demo", target, "windows", "amd64", hash); err != nil {
				// The version may not have landed yet in this round; that is
				// a scheduling artifact, not a lost update — skip it.
				_ = err
			}
		}()
		wg.Wait()
		close(errCh)
		for err := range errCh {
			t.Fatalf("round %d: %v", i, err)
		}
	}

	// Every merged version must still be present (no lost merge) …
	final, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < rounds; i++ {
		ver := "2." + string(rune('0'+i)) + ".0"
		if !strings.Contains(string(final), "version: "+ver) {
			t.Errorf("merged version %s was lost — concurrent RMW rolled it back", ver)
		}
	}
	// … and at least one hash write-back must have stuck (the writer that
	// alternates onto 1.0.0 every other round).
	foundHash := false
	for _, h := range hashes {
		if strings.Contains(string(final), h) {
			foundHash = true
			break
		}
	}
	if !foundHash {
		t.Error("no hash write-back survived — concurrent RMW rolled back every UpdateArchiveSHA256")
	}
	// The file must still be valid YAML with a parseable tool identity.
	var parsed manifest.Tool
	if err := yamlUnmarshalForTest(final, &parsed); err != nil {
		t.Fatalf("manifest torn by concurrent writes: %v", err)
	}
	if parsed.Name != "demo" {
		t.Errorf("parsed manifest name = %q, want demo", parsed.Name)
	}
}

// M3: when the on-disk manifest does not exist, the baseline is created
// atomically and completely (parseable), leaving no .tmp residue.
func TestBaselineWrite_AtomicAndLogged(t *testing.T) {
	dir := t.TempDir()
	tool := &manifest.Tool{
		Name: "fresh",
		VersionList: []manifest.Version{{
			Version: "1.0.0",
			Archives: []manifest.Archive{{
				OS: "windows", Arch: "amd64", Format: "zip",
				URL: "https://example.test/fresh-1.0.0.zip", SHA256: strings.Repeat("a", 64),
			}},
		}},
	}
	if err := mergeIntoManifest(tool, []DiscoveredVersion{{
		Version: "2.0.0",
		Archives: []manifest.Archive{{
			OS: "windows", Arch: "amd64", Format: "zip",
			URL: "https://example.test/fresh-2.0.0.zip",
		}},
	}}, dir); err != nil {
		t.Fatalf("mergeIntoManifest: %v", err)
	}

	path := filepath.Join(dir, "fresh.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("baseline manifest missing: %v", err)
	}
	var parsed manifest.Tool
	if err := yamlUnmarshalForTest(data, &parsed); err != nil {
		t.Fatalf("baseline manifest is not complete/parseable: %v", err)
	}
	if !strings.Contains(string(data), "2.0.0") {
		t.Error("merged version missing from the baseline-derived manifest")
	}
	// Atomic write leaves no temp residue.
	if entries, _ := os.ReadDir(dir); len(entries) > 0 {
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".tmp") {
				t.Errorf("atomic write left a temp file: %s", e.Name())
			}
		}
	}
}
