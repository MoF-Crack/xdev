//go:build windows

package env

import (
	"strings"
	"testing"
)

// TestSetEnvVar_GuardValidation (L-3) verifies the validation front of the
// generic SetEnvVar before any registry access: protected names are
// rejected, machine-level writes require the whitelist, values are
// null-byte checked and length-capped, and PATH with an invalid level is
// rejected by the routing switch (no registry write happens in any of
// these paths).
func TestSetEnvVar_GuardValidation(t *testing.T) {
	cases := []struct {
		name, varName, value, level, wantSub string
	}{
		{"protected name", "SystemRoot", "C:\\Windows", "user", "protected system env var"},
		{"path invalid level", "path", "C:\\bin", "bogus", "unsupported environment variable level"},
		{"machine non-whitelisted", "MY_RANDOM_VAR", "x", "machine", "non-whitelisted machine env var"},
		{"null bytes", "MYTOOL_HOME", "a\x00b", "user", "null bytes"},
		{"value too long", "MYTOOL_HOME", strings.Repeat("x", 32768), "user", "exceeds Windows registry limit"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := SetEnvVar(tc.varName, tc.value, tc.level)
			if err == nil {
				t.Fatalf("SetEnvVar(%q, ...) must fail, got nil", tc.varName)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("error %q should contain %q", err.Error(), tc.wantSub)
			}
		})
	}
}

// TestDeleteEnvVar_GuardValidation (L-3) verifies the delete guards:
// protected names (incl. PATH) are refused and machine-level deletes
// require the whitelist — all before any registry access.
func TestDeleteEnvVar_GuardValidation(t *testing.T) {
	cases := []struct {
		name, varName, level, wantSub string
	}{
		{"protected name", "SystemRoot", "user", "protected system env var"},
		{"path delete refused", "path", "user", "protected system env var"},
		{"machine non-whitelisted", "MY_RANDOM_VAR", "machine", "non-whitelisted machine env var"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := DeleteEnvVar(tc.varName, tc.level)
			if err == nil {
				t.Fatalf("DeleteEnvVar(%q) must fail, got nil", tc.varName)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("error %q should contain %q", err.Error(), tc.wantSub)
			}
		})
	}
}