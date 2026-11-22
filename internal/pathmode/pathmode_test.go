package pathmode

import "testing"

func TestModeContract(t *testing.T) {
	for _, tc := range []struct {
		mode, normalized string
		valid, shims     bool
	}{
		{Shim, Shim, true, true}, {Auto, Auto, true, true}, {Direct, Direct, true, false}, {"", Shim, false, true}, {"legacy", Shim, false, true},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			if Valid(tc.mode) != tc.valid || Normalize(tc.mode) != tc.normalized || UsesShims(tc.mode) != tc.shims {
				t.Errorf("mode %q: valid=%v normalized=%q shims=%v", tc.mode, Valid(tc.mode), Normalize(tc.mode), UsesShims(tc.mode))
			}
		})
	}
}
