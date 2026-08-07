package util

import "testing"

func TestIsVersionNewer_True(t *testing.T) {
	cases := []struct {
		a, b string
	}{
		{"2.55.0", "2.47.0"},  // T2B.6
		{"4.4.1", "3.31.0"},   // T2B.9
		{"7.0.2", "5.7.3"},    // T2B.10
		{"26", "25"},          // extra: pure numeric
		{"3.14.7", "3.14.6"},  // patch bump
		{"2.0.0", "1.9.9"},    // major bump
		{"10.0.0", "9.99.99"}, // multi-digit major
	}
	for _, tc := range cases {
		if !IsVersionNewer(tc.a, tc.b) {
			t.Errorf("IsVersionNewer(%q, %q) = false, want true", tc.a, tc.b)
		}
	}
}

func TestIsVersionNewer_False(t *testing.T) {
	cases := []struct {
		a, b string
	}{
		{"2.47.0", "2.55.0"},              // T2B.7
		{"r29", "27.2.12479018"},          // T2B.8: non-numeric segment
		{"3.14.6", "3.14.6"},              // T2B.11: equal
		{"2.55.0\x00malicious", "2.47.0"}, // extra: null byte safety
		{"", "2.47.0"},                    // extra: empty string
		{"1.2.3", "1.2.3"},                // equal semver
		{"abc", "1.0.0"},                  // non-numeric
		{"1.0.0", ""},                     // b is empty
	}
	for _, tc := range cases {
		if IsVersionNewer(tc.a, tc.b) {
			t.Errorf("IsVersionNewer(%q, %q) = true, want false", tc.a, tc.b)
		}
	}
}

func TestIsVersionNewer_NullByteSafety(t *testing.T) {
	// Both versions containing null bytes.
	if IsVersionNewer("2.55.0\x00", "2.47.0") {
		t.Error("IsVersionNewer with null byte in a should return false")
	}
	if IsVersionNewer("2.55.0", "2.47.0\x00") {
		t.Error("IsVersionNewer with null byte in b should return false")
	}
	if IsVersionNewer("2.55.0\x00evil", "2.47.0\x00more") {
		t.Error("IsVersionNewer with null bytes in both should return false")
	}
}

func TestIsVersionNewer_NonNumeric(t *testing.T) {
	cases := []struct {
		a, b string
	}{
		{"r29", "27.2.12479018"},
		{"27.2.12479018", "r29"},
		{"v1.0.0", "1.0.0"},     // "v" prefix is non-numeric segment
		{"1.0.0-beta", "1.0.0"}, // pre-release has non-numeric
	}
	for _, tc := range cases {
		if IsVersionNewer(tc.a, tc.b) {
			t.Errorf("IsVersionNewer(%q, %q) = true, want false (non-numeric segment)", tc.a, tc.b)
		}
	}
}
