package installer

import (
	"path/filepath"
	"runtime"
	"testing"
)

// TestIsWithinDir covers L12: the old check used ToLower+HasPrefix, so
// /home/u/xdev-evil counted as inside /home/u/xdev and case differences were
// ignored on Linux.
func TestIsWithinDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		parent := `C:\xdev`
		cases := []struct {
			child string
			want  bool
		}{
			{`C:\xdev`, true},
			{`C:\xdev\install\go\1.27`, true},
			{`C:\xdev-evil`, false},
			{`C:\other`, false},
		}
		for _, c := range cases {
			if got := isWithinDir(c.child, parent); got != c.want {
				t.Errorf("isWithinDir(%q,%q) = %v, want %v", c.child, parent, got, c.want)
			}
		}
		return
	}
	parent := filepath.Join(string(filepath.Separator), "home", "u", "xdev")
	cases := []struct {
		child string
		want  bool
	}{
		{parent, true},
		{filepath.Join(parent, "install", "go", "1.27"), true},
		{parent + "-evil", false},
		{parent + "evil", false},
		{filepath.Join(string(filepath.Separator), "home", "u"), false},
	}
	for _, c := range cases {
		if got := isWithinDir(c.child, parent); got != c.want {
			t.Errorf("isWithinDir(%q,%q) = %v, want %v", c.child, parent, got, c.want)
		}
	}
}
