package manifest

import (
	"regexp"

	"github.com/xmdemt/xdev/internal/util"
)

// Numeric releases may have one or more components (including vendor build
// numbers). Arbitrary labels cannot establish upgrade ordering: CompareSemver
// deliberately tolerates them for display sorting, not for default promotion.
var catalogOrderedVersion = regexp.MustCompile(`^v?[0-9]+(?:\.[0-9]+)*(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)

func canPromoteCatalogLatest(versions []Version, candidate, current string) bool {
	if !catalogOrderedVersion.MatchString(candidate) {
		return false
	}
	if current != "" && !catalogOrderedVersion.MatchString(current) {
		return false
	}
	allowPre := IsPrerelease(current) && util.PrereleaseChannel(candidate) == util.PrereleaseChannel(current)
	// Keep the current version's ordering even if a stale embedded latest/LTS
	// does not occur in the list. The general promotion helper intentionally
	// repairs missing current entries; catalog overlays must not use that to
	// downgrade an existing default.
	if current != "" {
		if _, ok := findVersion(versions, current); !ok {
			versions = append(append([]Version(nil), versions...), Version{Version: current})
		}
	}
	return CanPromoteLatest(versions, candidate, current, allowPre)
}
