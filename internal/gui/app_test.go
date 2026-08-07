package gui

import "testing"

func TestExtractVersion(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"go1.22.0", "1.22.0"},
		{"v3.12.7", "3.12.7"},
		{"java 25", ""}, // "25" has no dot → not version-like per isVersionLike
		{"git version 2.47.0.windows.1", "2.47.0"},
		{"1.22.0", "1.22.0"},
		{"not a version", ""},
		{"Python 3.12.7", "3.12.7"},
		{"node v20.11.0", "v20.11.0"}, // extractVersion returns the original field
		{"rustc 1.77.0", "1.77.0"},
		{"go1.22", "1.22"},
		{"v1.0.0", "1.0.0"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := extractVersion(tt.input)
			if got != tt.expected {
				t.Errorf("extractVersion(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestStripWindowsSuffix(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"2.47.0.windows.1", "2.47.0"},
		{"2.47.0", "2.47.0"}, // no .windows. suffix → returns original string
		{"1.0.0.windows.2", "1.0.0"},
		{"3.12.0.windows.0", "3.12.0"},
		{"", ""}, // empty string, no .windows. found
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := stripWindowsSuffix(tt.input)
			if got != tt.expected {
				t.Errorf("stripWindowsSuffix(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestIsVersionLike(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"1.22.0", true},
		{"3.12.7", true},
		{"v1.0.0", true},
		{"go1.22", true},
		{"2.47.0.windows.1", true}, // .windows. is stripped before checking dots
		{"25", false},              // no dot
		{"hello", false},           // doesn't start with digit
		{"1", false},               // no dot
		{"1.2.3.4.5", false},       // too many dots (>3)
		{"0.1", true},              // 1 dot is ok
		{"1.2.3.4", true},          // 3 dots is ok
		{"v3.12", true},            // v prefix stripped, then 1 dot
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isVersionLike(tt.input)
			if got != tt.expected {
				t.Errorf("isVersionLike(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}
