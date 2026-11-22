package manifest

import (
	"sort"

	"github.com/xmdemt/xdev/internal/version"
)

// CompareToolVersions returns release precedence. A false comparable result
// means one or both spellings are outside the tool's declared scheme.
func CompareToolVersions(toolName, a, b string) (order int, comparable bool) {
	return version.CompareForTool(toolName, a, b)
}

// IsComparableToolVersion reports whether v can participate in upgrade decisions.
func IsComparableToolVersion(toolName, v string) bool {
	return version.IsComparableForTool(toolName, v)
}

// NewestToolVersion returns the highest comparable version. Semantic ties
// preserve input order, and an all-unknown or empty list returns false.
func NewestToolVersion(toolName string, values []string) (string, bool) {
	return version.NewestForTool(toolName, values)
}

// SortToolVersionsByRelease orders recognized releases first, newest first.
// Dates and raw strings break display ties. They never authorize an upgrade.
func SortToolVersionsByRelease(toolName string, versions []Version) {
	sort.SliceStable(versions, func(i, j int) bool { return toolReleaseNewer(toolName, versions[i], versions[j]) })
}

func toolReleaseNewer(toolName string, a, b Version) bool {
	if cmp, ok := CompareToolVersions(toolName, a.Version, b.Version); ok {
		if cmp != 0 {
			return cmp > 0
		}
	} else {
		ak, bk := IsComparableToolVersion(toolName, a.Version), IsComparableToolVersion(toolName, b.Version)
		if ak != bk {
			return ak
		}
	}
	ta, oka := parseReleaseDate(a.Release)
	tb, okb := parseReleaseDate(b.Release)
	if oka != okb {
		return oka
	}
	if oka && !ta.Equal(tb) {
		return ta.After(tb)
	}
	return a.Version > b.Version
}
