package version

import (
	"regexp"
	"strconv"
	"strings"
)

// numericVersion accepts the repository's dotted numeric versions, including
// missing/extra numeric segments, with SemVer prerelease/build identifiers.
// Unknown tails ("22oops", empty segments, underscores) are never parsed as zero.
var numericVersion = regexp.MustCompile(`^[vV]?[0-9]+(?:\.[0-9]+)*(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)
var ndkRelease = regexp.MustCompile(`^r([0-9]+)([a-z]?)$`)

type releaseKey struct {
	parts []string
	pre   []string
}

// CompareForTool returns precedence and whether BOTH strings belong to the
// tool's version scheme. An order of zero with comparable=false is unknown,
// never evidence of equality. android-ndk uses Android release/revision keys;
// other tools use numeric/SemVer precedence. Build metadata is ignored.
func CompareForTool(tool, a, b string) (order int, comparable bool) {
	ka, oka := parseForTool(tool, a)
	kb, okb := parseForTool(tool, b)
	if !oka || !okb {
		return 0, false
	}
	return compareKeys(ka, kb), true
}

// IsComparableForTool reports whether v belongs to the tool's known scheme.
func IsComparableForTool(tool, v string) bool { _, ok := parseForTool(tool, v); return ok }

// NewestForTool chooses the highest recognized version. Unknown values are
// excluded; semantic ties retain their input order rather than inventing an
// upgrade from spelling, date or build metadata.
func NewestForTool(tool string, values []string) (string, bool) {
	best := ""
	var bestKey releaseKey
	for _, v := range values {
		key, ok := parseForTool(tool, v)
		if ok && (best == "" || compareKeys(key, bestKey) > 0) {
			best, bestKey = v, key
		}
	}
	return best, best != ""
}

func parseForTool(tool, raw string) (releaseKey, bool) {
	s := strings.TrimSpace(raw)
	if tool == "android-ndk" {
		// Android's public release alias r27c corresponds to Pkg.Revision
		// 27.2.<build>. No suffix / 'a' is revision 0, 'b' is 1, etc.
		// The final SDK build number identifies the artifact, not another public
		// revision. Ignoring it for ALL NDK comparisons preserves equivalence and
		// transitivity (r27c == 27.2.12479018; r29 == 29.0).
		if m := ndkRelease.FindStringSubmatch(s); m != nil {
			revision := "0"
			if m[2] != "" {
				revision = strconv.Itoa(int(m[2][0] - 'a'))
			}
			return releaseKey{parts: []string{m[1], revision}}, true
		}
	}
	if !numericVersion.MatchString(s) {
		return releaseKey{}, false
	}
	s = strings.TrimPrefix(strings.TrimPrefix(s, "v"), "V")
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i]
	}
	key := releaseKey{}
	if i := strings.IndexByte(s, '-'); i >= 0 {
		key.pre = strings.Split(s[i+1:], ".")
		s = s[:i]
	}
	key.parts = strings.Split(s, ".")
	if tool == "android-ndk" {
		// Only major[.revision[.SDK-build]] is a known Android version form.
		if len(key.parts) > 3 {
			return releaseKey{}, false
		}
		if len(key.parts) > 2 {
			key.parts = key.parts[:2]
		}
	}
	return key, true
}

func compareKeys(a, b releaseKey) int {
	n := max(len(a.parts), len(b.parts))
	for i := 0; i < n; i++ {
		av, bv := "0", "0"
		if i < len(a.parts) {
			av = a.parts[i]
		}
		if i < len(b.parts) {
			bv = b.parts[i]
		}
		if c := compareDigits(av, bv); c != 0 {
			return c
		}
	}
	if len(a.pre) == 0 && len(b.pre) == 0 {
		return 0
	}
	if len(a.pre) == 0 {
		return 1
	}
	if len(b.pre) == 0 {
		return -1
	}
	for i := 0; i < min(len(a.pre), len(b.pre)); i++ {
		av, bv := a.pre[i], b.pre[i]
		an, bn := digitsOnly(av), digitsOnly(bv)
		if an && bn {
			if c := compareDigits(av, bv); c != 0 {
				return c
			}
			continue
		}
		if an != bn {
			if an {
				return -1
			}
			return 1
		}
		if c := strings.Compare(av, bv); c != 0 {
			return c
		}
	}
	if len(a.pre) < len(b.pre) {
		return -1
	}
	if len(a.pre) > len(b.pre) {
		return 1
	}
	return 0
}

// Comparing normalized decimal strings avoids both integer overflow and a
// saturation ceiling that would equate distinct large version identifiers.
func compareDigits(a, b string) int {
	a = strings.TrimLeft(a, "0")
	b = strings.TrimLeft(b, "0")
	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return 1
	}
	return strings.Compare(a, b)
}
func digitsOnly(s string) bool {
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
