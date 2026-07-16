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