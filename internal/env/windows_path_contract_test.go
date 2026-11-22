package env

import "testing"

func TestReportD02_ComparisonDoesNotChangeLiteralSpelling(t *testing.T) {
	tests := []struct{ raw, written, key string }{
		{`C:\`, `C:\`, `c:\`},
		{`C:/../A//B/`, `C:/../A/B/`, `c:\a\b`},
		{`\\?\C:\A\..\B `, `\\?\C:\A\..\B `, `\\?\C:\A\..\B `},
		{`"D:\folder with spaces"`, `"D:\folder with spaces"`, `"D:\folder with spaces"`},
		{`C:\%MY_VAR%\\bin`, `C:\%MY_VAR%\\bin`, `C:\%MY_VAR%\\bin`},
	}
	for _, tc := range tests {
		if got := NormalizeWindowsPathEntry(tc.raw); got != tc.written {
			t.Errorf("written %q = %q, want %q", tc.raw, got, tc.written)
		}
		if got := WindowsPathComparisonKey(tc.raw); got != tc.key {
			t.Errorf("comparison key for %q = %q, want %q", tc.raw, got, tc.key)
		}
	}
}
