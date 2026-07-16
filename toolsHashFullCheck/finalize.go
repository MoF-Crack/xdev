// finalize.go — writes all "hashed" entries to manifests and cleans up.
// This is run after downloads to flush the pending hash updates.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/xmdemt/xdev/internal/manifest"
)

// Finalize writes all "hashed" entries to their manifest YAML files and
// marks them as "cleaned". This handles the case where downloads succeeded
// but the pipeline was interrupted before manifests were written.
func (p *Pipeline) Finalize() error {
	cat, err := manifest.LoadCatalog(p.manifestsDir)
	if err != nil {
		return fmt.Errorf("load manifests: %w", err)
	}

	// Group hashed entries by tool
	toolHashes := make(map[string]map[string]string) // tool -> "version/os/arch" -> hash
	for _, e := range p.progress.Entries {
		if e.Status != stateHashed && e.Status != stateCleaned {
			continue
		}
		if e.Hash == "" {
			continue
		}
		key := fmt.Sprintf("%s/%s/%s", e.Version, e.OS, e.Arch)
		if toolHashes[e.Tool] == nil {
			toolHashes[e.Tool] = make(map[string]string)
		}
		toolHashes[e.Tool][key] = e.Hash
	}

	written := 0
	for toolName, hashes := range toolHashes {
		tool, ok := cat.Tool(toolName)
		if !ok {
			fmt.Fprintf(os.Stderr, "⚠ tool %q not found in catalog, skipping\n", toolName)
			continue
		}

		anyUpdated := false
		for vi, v := range tool.VersionList {
			for ai, a := range v.Archives {
				key := fmt.Sprintf("%s/%s/%s", v.Version, a.OS, a.Arch)
				if hash, ok := hashes[key]; ok {
					if a.SHA256 != hash {
						tool.VersionList[vi].Archives[ai].SHA256 = hash
						anyUpdated = true
					}
				}
			}
		}

		if anyUpdated {
			yamlPath := filepath.Join(p.manifestsDir, toolName+".yaml")
			if err := writeManifestYAML(yamlPath, tool); err != nil {
				fmt.Fprintf(os.Stderr, "❌ %s: write failed: %v\n", toolName, err)
			} else {
				fmt.Printf("📝 Updated %s\n", yamlPath)
				written++
			}
		}
	}

	// Clean up downloaded files for hashed entries
	cleaned := 0
	for _, e := range p.progress.Entries {
		if e.Status != stateHashed {
			continue
		}
		// Find the tool to get the format
		tool, ok := cat.Tool(e.Tool)
		if !ok {
			continue
		}
		format := ""
		for _, v := range tool.VersionList {
			if v.Version == e.Version {
				for _, a := range v.Archives {
					if a.OS == e.OS && a.Arch == e.Arch {
						format = a.Format
						break
					}
				}
				break
			}
		}
		if format == "" {
			continue
		}
		dest := filepath.Join(p.cacheDir, fmt.Sprintf("%s-%s-%s-%s.%s",
			e.Tool, e.Version, e.OS, e.Arch, format))
		if err := os.Remove(dest); err != nil && !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "⚠ cleanup %s: %v\n", dest, err)
		} else {
			cleaned++
		}
		_ = p.progress.UpdateEntry(e.Tool, e.Version, e.OS, e.Arch, stateCleaned, "", "")
	}

	fmt.Printf("\n📊 Manifests written: %d, Files cleaned: %d\n", written, cleaned)
	p.printStats()
	return nil
}
