// sanitize_test.go tests the shim/env-var name validators added for
// Spec 021 Phase 3 (D11 shim name validation / D12 env var validation).
package util

import (
	"path/filepath"
	"testing"
)

func TestValidateShimName(t *testing.T) {
	cases := []struct {
		name    string
		wantErr bool
	}{
		{"go", false},
		{"bunx", false},
		{"python3", false},
		{"tool-name", false}, // hyphens are fine
		{"", true},
		{".", true},
		{"..", true},
		{"../outside", true},
		{`..\outside`, true},
		{"a/b", true},
		{"a\\b", true},
		{"C:evil", true}, // drive letter prefix
		{"CON", true},    // Windows reserved device name
		{"bad\x00name", true},
		{"trailing. ", true}, // trailing dot/space (Windows collision)
	}
	for _, c := range cases {
		err := ValidateShimName(c.name)
		if (err != nil) != c.wantErr {
			t.Errorf("ValidateShimName(%q) err = %v, wantErr %v", c.name, err, c.wantErr)
		}
	}
}

func TestValidateEnvVarName(t *testing.T) {
	cases := []struct {
		name    string
		wantErr bool
	}{
		{"GOROOT", false},
		{"MYTOOL_HOME", false},
		{"_FOO", false},
		{"NODE_HOME", false},
		{"", true},
		{"1HOME", true},      // must start with letter/underscore
		{"A B", true},        // space
		{"FOO;BAR", true},    // shell injection
		{"HOME-", true},      // hyphen not allowed
		{"PATH", true},       // protected system var
		{"Path", true},       // protected (case-insensitive)
		{"path", true},       // protected (case-insensitive)
		{"SystemRoot", true}, // protected
		{"TEMP", true},       // protected
		{"USERPROFILE", true},
		{"COMSPEC", true},
	}
	for _, c := range cases {
		err := ValidateEnvVarName(c.name)
		if (err != nil) != c.wantErr {
			t.Errorf("ValidateEnvVarName(%q) err = %v, wantErr %v", c.name, err, c.wantErr)
		}
	}
}

// TestValidateAbsPathUnder covers the containment helper used to confine
// migration/profile paths to xdev-owned directories (F-10/F-11).
func TestValidateAbsPathUnder(t *testing.T) {
	base := filepath.Join(string(filepath.Separator), "xdev", "install")
	ok := []string{
		filepath.Join(base, "go", "1.26.5"),
		filepath.Join(base, "..", "install", "node"),
		base + string(filepath.Separator) + "tool",
	}
	for _, p := range ok {
		if err := ValidateAbsPathUnder(base, p); err != nil {
			t.Errorf("ValidateAbsPathUnder(%q, %q) should pass, got: %v", base, p, err)
		}
	}

	bad := []string{
		filepath.Join(base, "..", "outside"),
		filepath.Join(base, ".."),
		base,
		"relative/path",
	}
	for _, p := range bad {
		if err := ValidateAbsPathUnder(base, p); err == nil {
			t.Errorf("ValidateAbsPathUnder(%q, %q) should be rejected", base, p)
		}
	}
}
