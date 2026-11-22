package plugin

import (
	"github.com/xmdemt/xdev/internal/manifest"
	"strings"
	"testing"
)

func TestD18_GenericOptionalVersionComparer(t *testing.T) {
	var p Plugin = NewGeneric(&manifest.Tool{Name: "android-ndk"})
	comparer, ok := p.(VersionComparer)
	if !ok {
		t.Fatal("generic lacks release comparer")
	}
	if order, ok := comparer.CompareVersions("r29", "27.2.12479018"); !ok || order != 1 {
		t.Fatalf("NDK precedence = %d,%v", order, ok)
	}
}

func TestD18_GenericUnpinnedLatestUsesReleaseScheme(t *testing.T) {
	g := NewGeneric(&manifest.Tool{Name: "android-ndk", VersionList: []manifest.Version{{Version: "27.2.12479018"}, {Version: "unknown"}, {Version: "r29"}}})
	for _, spec := range []string{"latest", "lts"} {
		got, err := g.ResolveVersion(spec)
		if err != nil || got != "r29" {
			t.Errorf("unconfigured %s = %q,%v; want r29", spec, got, err)
		}
	}
}

func TestD23_GenericArchiveDiagnostic(t *testing.T) {
	g := NewGeneric(&manifest.Tool{Name: "placeholder", VersionList: []manifest.Version{{Version: "1", Archives: []manifest.Archive{{OS: "any", Arch: "any"}}}}})
	a, err := g.ArchiveFor("1")
	if a != nil || err == nil || !strings.Contains(err.Error(), "download URL and format") {
		t.Fatalf("placeholder archive = %v,%v", a, err)
	}
}
