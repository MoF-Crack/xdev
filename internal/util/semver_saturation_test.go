package util

import (
	"math"
	"strings"
	"testing"
)

// Ticket 39 (P2-13/P2-14/P2-23) guards — SemVer saturation consistency.

// 39-2a: build metadata never participates in comparison.
func TestCompareSemver_IgnoresBuildMetadata(t *testing.T) {
	if got := CompareSemver("1.0.0-rc.1+build.1", "1.0.0-rc.1"); got != 0 {
		t.Errorf("CompareSemver with build metadata = %d, want 0 (build metadata is ignored)", got)
	}
	if got := CompareSemver("1.0.0+build.9", "1.0.0+build.1"); got != 0 {
		t.Errorf("CompareSemver differing only in build metadata = %d, want 0", got)
	}
}

// 39-2b: pre-release identifiers with huge numeric values must saturate at
// ONE shared ceiling — different magnitudes stay ordered instead of
// comparing equal at a partial-value cut-off.
func TestComparePreRelease_HugeNumericIdentifiersSaturate(t *testing.T) {
	// Two long-but-representable numeric identifiers (18 vs 19 digits, both
	// inside uint64): the old implementation stopped accumulating past
	// 1<<30 and returned the same partial value for both.
	small := "1.0.0-rc.999999999999999999"   // 18 nines ≈ 1e18
	bigger := "1.0.0-rc.9999999999999999999" // 19 nines ≈ 1e19 (still < MaxUint64)
	if got := CompareSemver(small, bigger); got != -1 {
		t.Errorf("CompareSemver(%q, %q) = %d, want -1 (long identifiers must stay ordered)", small, bigger, got)
	}
	// Both beyond the uint64 ceiling → they tie (documented saturation).
	a := "1.0.0-rc." + strings.Repeat("9", 40)
	b := "1.0.0-rc." + strings.Repeat("8", 40)
	if got := CompareSemver(a, b); got != 0 {
		t.Errorf("values beyond the uint64 ceiling should saturate to a tie, got %d", got)
	}
	// A saturating value still outranks a small one.
	if got := CompareSemver("1.0.0-rc."+strings.Repeat("9", 40), "1.0.0-rc.5"); got != 1 {
		t.Errorf("saturated value must outrank a small one, got %d", got)
	}
}

// 39-2c: a version component with a huge number must not wrap around and
// sort BELOW a normal version.
func TestParseSemverParts_NoOverflowWraparound(t *testing.T) {
	parts := ParseSemverParts(strings.Repeat("9", 30) + ".0.0")
	if parts[0] <= 0 {
		t.Fatalf("huge major component wrapped to %d — overflow regression", parts[0])
	}
	if parts[0] != math.MaxInt32 {
		t.Errorf("huge major component = %d, want the MaxInt32 saturation ceiling", parts[0])
	}
	// Ordering stays monotonic: the huge version is newer than a normal one.
	if got := CompareSemver(strings.Repeat("9", 30)+".0.0", "5.0.0"); got != 1 {
		t.Errorf("huge-major version should outrank 5.0.0, got %d", got)
	}
}

// 39-2d: numericValue saturates instead of returning a partial value.
func TestNumericValue_Saturates(t *testing.T) {
	if got := numericValue("0"); got != 0 {
		t.Errorf("numericValue(0) = %d, want 0", got)
	}
	if got := numericValue("12345678901234567890"); got != 12345678901234567890 {
		t.Errorf("numericValue(20-digit) = %d, want the exact value", got)
	}
	// Beyond uint64 → ceiling.
	huge := strings.Repeat("9", 30)
	if got := numericValue(huge); got != math.MaxUint64 {
		t.Errorf("numericValue(30-digit) = %d, want math.MaxUint64 (saturation)", got)
	}
}
