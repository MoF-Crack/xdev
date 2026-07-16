package util

import "strings"

// CompareSemver compares two version strings using simple major.minor.patch
// rules. Returns 1 if a > b, 0 if equal, -1 if a < b.
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
	return 0
}

// ParseSemverParts parses a version string into [3]int{major, minor, patch}.
// Strips leading "v", pre-release suffixes (everything after "-"), and
// extracts numeric prefixes from each component.
func ParseSemverParts(s string) [3]int {
	var result [3]int
	s = strings.TrimPrefix(s, "v")
	if idx := strings.Index(s, "-"); idx >= 0 {
		s = s[:idx]
	}
	parts := strings.SplitN(s, ".", 3)
	for i, p := range parts {
		if i >= 3 {
			break
		}
		n := 0
		for _, c := range p {
			if c >= '0' && c <= '9' {
				n = n*10 + int(c-'0')
			} else {
				break
			}
		}
		result[i] = n
	}
	return result
}
