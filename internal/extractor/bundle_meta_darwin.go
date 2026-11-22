//go:build darwin

package extractor

import (
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
)

// The content-only copier must not silently omit resource forks or signatures
// stored in xattrs (instead of Mach-O/Contents/_CodeSignature files). A safe
// metadata-preserving implementation can extend this list and charge metadata
// as well. The normal data-fork/symlink form remains supported.
func checkBundleMetadata(path string) error {
	for _, name := range []string{"com.apple.ResourceFork", "com.apple.FinderInfo", "com.apple.cs.CodeDirectory", "com.apple.cs.CodeRequirements", "com.apple.cs.CodeSignature"} {
		n, e := unix.Lgetxattr(path, name, nil)
		if errors.Is(e, unix.ENOATTR) || errors.Is(e, unix.ENOTSUP) {
			continue
		}
		if e != nil {
			return e
		}
		if n > 0 {
			return fmt.Errorf("unsupported bundle metadata %s on %s; refusing lossy copy", name, path)
		}
	}
	return nil
}
