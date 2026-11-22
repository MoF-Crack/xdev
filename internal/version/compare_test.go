package version

import (
	"strings"
	"testing"
)

func TestD18ReleaseScheme(t *testing.T) {
	huge := strings.Repeat("9", 50)
	for _, tc := range []struct {
		tool, a, b string
		want       int
	}{
		{"android-ndk", "r29", "r26", 1}, {"android-ndk", "r27", "r27a", 0}, {"android-ndk", "r27c", "r27b", 1},
		{"android-ndk", "r27c", "27.2.12479018", 0}, {"android-ndk", "r29", "27.2.12479018", 1},
		{"android-ndk", "27.2.2", "27.2.1", 0}, {"android-ndk", "r" + huge + "b", "r" + huge + "a", 1},
		{"editor", "v2026.2", "2026.2.0.0", 0}, {"editor", "2026.2.0.1", "2026.2", 1},
		{"editor", "1.0.0+build.4", "1", 0}, {"editor", "1-rc.10+foo", "1-rc.2", 1}, {"editor", "1-rc.1", "1", -1},
		{"editor", "1-" + huge, "1-" + huge + "0", -1}, {"editor", huge + "0", huge, 1},
	} {
		got, ok := CompareForTool(tc.tool, tc.a, tc.b)
		if !ok || got != tc.want {
			t.Errorf("%s: %s vs %s = %d,%v; want %d,true", tc.tool, tc.a, tc.b, got, ok, tc.want)
		}
	}
	for _, v := range []string{"", "r27c", "go1.22", "1..2", "1-", "1.2junk", "foo"} {
		if _, ok := CompareForTool("node", v, "0"); ok {
			t.Errorf("recognized unknown %q for ordinary tool", v)
		}
	}
	if got, ok := NewestForTool("android-ndk", []string{"unknown", "27.2.100", "r27c", "r29", "r26"}); !ok || got != "r29" {
		t.Errorf("newest = %q,%v", got, ok)
	}
	if _, ok := NewestForTool("node", []string{"unknown", "r99"}); ok {
		t.Error("all-unknown list yielded winner")
	}
}

func TestD18NDKAntisymmetryAndTransitivity(t *testing.T) {
	values := []string{"r26", "r27", "r27a", "27", "27.0.1234", "r27b", "27.2.2-rc.1", "r27c", "27.2.1", "27.2.9099", "r29"}
	for _, a := range values {
		for _, b := range values {
			ab, ok := CompareForTool("android-ndk", a, b)
			ba, okBA := CompareForTool("android-ndk", b, a)
			if !ok || !okBA || ab != -ba {
				t.Fatalf("antisymmetry for %s %s: %d,%d", a, b, ab, ba)
			}
			for _, c := range values {
				bc, _ := CompareForTool("android-ndk", b, c)
				ac, _ := CompareForTool("android-ndk", a, c)
				if ab >= 0 && bc >= 0 && ac < 0 {
					t.Fatalf("nontransitive: %s >= %s >= %s", a, b, c)
				}
			}
		}
	}
}
