package util

import (
	"math"
	"strconv"
	"strings"
)

// CompareSemver compares two version strings using SemVer 2.0.0 rules:
// major.minor.patch first, then pre-release precedence.
// Returns 1 if a > b, 0 if equal, -1 if a < b.
//
// Pre-release semantics (Spec 039, mirrors internal/resolver):
//   - 1.0.0 > 1.0.0-rc.1          (release beats pre-release)
//   - 1.0.0-rc.2 > 1.0.0-rc.1     (numeric identifiers compare numerically)
//   - 1.0.0-rc.1 > 1.0.0-alpha.9  (alphanumeric identifiers compare lexically)
//   - 1.0.0-rc.1.2 > 1.0.0-rc.1    (more identifiers rank higher)
//   - 1.0.0-1 > 1.0.0-alpha       (numeric identifiers rank below alphanumeric)
//
// Build metadata ("+build") is stripped before comparison and ignored.
func CompareSemver(a, b string) int {
	pa := ParseSemverParts(a)
	pb := ParseSemverParts(b)
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			if pa[i] > pb[i] {
				return 1
			}
			return -1
		}
	}
	return comparePreReleaseCore(preReleaseOf(a), preReleaseOf(b))
}

// ParseSemverParts parses a version string into [3]int{major, minor, patch}.
// Strips leading "v", pre-release suffixes (everything after "-"), and build
// metadata ("+build"), and extracts numeric prefixes from each component.
func ParseSemverParts(s string) [3]int {
	var result [3]int
	s = strings.TrimPrefix(s, "v")
	if idx := strings.Index(s, "+"); idx >= 0 {
		s = s[:idx]
	}
	if idx := strings.Index(s, "-"); idx >= 0 {
		s = s[:idx]
	}
	parts := strings.SplitN(s, ".", 3)
	for i, p := range parts {
		if i >= 3 {
			break
		}
		// Ticket 39 (P2-23): saturating accumulation. A component with a
		// 30-digit number used to WRAP AROUND on int overflow and could then
		// sort below a small version. Clamp at the practical ceiling for a
		// version component so ordering stays monotonic.
		const maxPart = math.MaxInt32
		n := 0
		for _, c := range p {
			if c < '0' || c > '9' {
				break
			}
			if n > (maxPart-int(c-'0'))/10 {
				n = maxPart
				break
			}
			n = n*10 + int(c-'0')
		}
		result[i] = n
	}
	return result
}

// IsPrereleaseVersion reports whether the version string carries a
// pre-release suffix (e.g. "0.1.2-rc.1", "1.0.0-alpha.3", "v2.0.0-beta").
// A "v" prefix is tolerated and build metadata is stripped first, so
// "1.0.0+build" is NOT a pre-release while "1.0.0-rc.1+build" is.
// Non-version-like strings that contain "-" after a non-empty core are
// conservatively treated as pre-releases.
func IsPrereleaseVersion(s string) bool {
	return preReleaseOf(s) != ""
}

// PrereleaseChannel returns the pre-release channel identifier of a
// version string: the identifier before the first "." of the pre-release
// suffix ("0.1.2-rc.1" → "rc", "1.0.0-alpha.5" → "alpha", "2.0.0-beta" →
// "beta"). Stable versions return "".
//
// Spec 039（dsh alpha 问题）: 升级通道语义——npm dist-tags 把 latest 与
// alpha 分成两个通道（dsh 实测 latest=0.1.2-rc.1 而 alpha=0.1.5-alpha.1），
// xdev 的 latest 提升按通道匹配，防止 alpha 通道劫持 latest。
func PrereleaseChannel(s string) string {
	pre := preReleaseOf(s)
	if pre == "" {
		return ""
	}
	if idx := strings.IndexByte(pre, '.'); idx > 0 {
		return pre[:idx]
	}
	return pre
}

// preReleaseOf extracts the pre-release suffix of a version string:
// trims a leading "v", strips build metadata ("+..."), and returns
// everything after the first "-" ("" when there is none).
func preReleaseOf(s string) string {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if idx := strings.Index(s, "+"); idx >= 0 {
		s = s[:idx]
	}
	if idx := strings.Index(s, "-"); idx >= 0 {
		return strings.TrimSpace(s[idx+1:])
	}
	return ""
}

// comparePreReleaseCore compares two pre-release suffixes per SemVer 2.0.0
// ("a" and "b" are the raw suffix strings, "" meaning a stable release).
func comparePreReleaseCore(a, b string) int {
	if a == b {
		return 0
	}
	// No pre-release has higher precedence than any pre-release.
	if a == "" {
		return 1
	}
	if b == "" {
		return -1
	}
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")
	n := len(aParts)
	if len(bParts) > n {
		n = len(bParts)
	}
	for i := 0; i < n; i++ {
		if i >= len(aParts) {
			return -1 // a has fewer identifiers → lower precedence
		}
		if i >= len(bParts) {
			return 1 // b has fewer identifiers → a ranks higher
		}
		aNum := allDigits(aParts[i])
		bNum := allDigits(bParts[i])
		switch {
		case aNum && bNum:
			ai, bi := numericValue(aParts[i]), numericValue(bParts[i])
			if ai != bi {
				if ai < bi {
					return -1
				}
				return 1
			}
		case aNum != bNum:
			// Numeric identifiers always rank below alphanumeric ones.
			if aNum {
				return -1
			}
			return 1
		default:
			if aParts[i] != bParts[i] {
				if aParts[i] < bParts[i] {
					return -1
				}
				return 1
			}
		}
	}
	return 0
}

// allDigits reports whether s is non-empty and consists only of digits.
func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// numericValue parses a digit-only string into a uint64 with SATURATION:
// anything above the uint64 ceiling clamps to math.MaxUint64 (callers gate
// on allDigits first, so non-digit input never reaches here).
//
// Ticket 39 (P2-14/P2-23): the previous version stopped accumulating once
// the value passed 1<<30 and returned that PARTIAL value, so two
// long-but-different numeric identifiers could compare EQUAL (and their
// ordering depended on where the cut-off happened to land). Saturating at
// one shared ceiling keeps the comparison monotonic — different magnitudes
// stay ordered, and only genuinely unrepresentable values tie.
func numericValue(s string) uint64 {
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return math.MaxUint64
	}
	return v
}
