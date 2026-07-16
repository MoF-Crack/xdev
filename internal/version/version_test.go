package version

import "testing"

func TestVersion(t *testing.T) {
	if Version == "" {
		t.Error("Version constant must not be empty")
	}
}

func TestGet(t *testing.T) {
	if got := Get(); got != Version {
		t.Errorf("Get() = %q, want %q", got, Version)
	}
}

func TestVersionFormat(t *testing.T) {
	if len(Version) < 5 {
		t.Errorf("Version %q looks too short", Version)
	}
}
