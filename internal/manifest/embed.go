package manifest

import (
	"io/fs"
	"strings"
)

// InitEmbedded registers an embedded filesystem containing the
// built-in YAML manifests.  The caller is expected to embed the
// manifests/ directory at the main package level.
//
//	e.g. //go:embed manifests/*.yaml
//	var manifestFS embed.FS
//	manifest.InitEmbedded(manifestFS)
//
// The embed directive creates a virtual "manifests/" sub-directory
// inside the FS.  We strip that prefix so that loadFromEmbedded can
// read files from the root of the embedded FS.
func InitEmbedded(efs fs.FS) {
	// Check if the YAML files live under a "manifests/" sub-directory.
	// If so, sub-root the FS so that ReadDir(".") lists the YAML files.
	if entries, err := fs.ReadDir(efs, "."); err == nil {
		for _, e := range entries {
			if e.IsDir() && e.Name() == "manifests" {
				sub, err := fs.Sub(efs, "manifests")
				if err == nil {
					embeddedFS = sub
					return
				}
			}
			// If we find a .yaml file at the root, the FS is already
			// correctly structured.
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".yaml") {
				embeddedFS = efs
				return
			}
		}
	}
	embeddedFS = efs
}

// HasEmbeddedManifest reports whether a built-in (embedded) manifest
// exists for the given tool name.
//
// Audit-fixes ticket 13 (N-03): the installer's same-name trust guard uses
// this to decide whether a disk manifest under third-party/ may downgrade
// a tool to the third-party (TOFU) trust model. It must be false for
// genuinely third-party tools and true for anything shipped in the binary.
func HasEmbeddedManifest(toolName string) bool {
	if embeddedFS == nil || toolName == "" {
		return false
	}
	_, err := fs.ReadFile(embeddedFS, toolName+".yaml")
	return err == nil
}
