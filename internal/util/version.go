package util

import "strings"

// IsVersionNewer reports whether version a is newer than version b.
// Uses simple semver comparison: split by ".", compare numeric segments.
// Non-numeric segments (e.g. "r29") return false — conservative skip.
// Handles null bytes and malformed input safely (returns false, no panic).
func IsVersionNewer(a, b string) bool {
	// Reject strings containing null bytes.
	if strings.ContainsRune(a, 0) || strings.ContainsRune(b, 0) {
		return false
	}

	partsA := strings.Split(a, ".")
	partsB := strings.Split(b, ".")

	maxLen := len(partsA)
	if len(partsB) > maxLen {
		maxLen = len(partsB)
	}

	numsA := make([]int, maxLen)
	numsB := make([]int, maxLen)

	for i, p := range partsA {
		n, ok := parseNumeric(p)
		if !ok {
			return false
		}
		numsA[i] = n
	}
	for i, p := range partsB {
		n, ok := parseNumeric(p)
		if !ok {
			return false
		}
		numsB[i] = n
	}

	for i := 0; i < maxLen; i++ {
		if numsA[i] != numsB[i] {
			return numsA[i] > numsB[i]
		}
	}

	return false // equal
}

// parseNumeric parses a purely numeric string into an int.
// Returns false if the string is empty or contains non-digit characters.
func parseNumeric(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}
