package manifest

import "testing"

// Linux/macOS manifests spell x86-64 as "x86_64" while runtime.GOARCH is
// "amd64". Comparing them verbatim made markInstallability report almost every
// tool as "not installable on this platform" on linux/amd64 (1/54 instead of
// 48/54) — a defect invisible on Windows, where both spellings are "amd64".

func TestCanonicalArch(t *testing.T) {
	cases := []struct {
		goos, goarch, want string
	}{
		{"linux", "amd64", "x86_64"},
		{"darwin", "amd64", "x86_64"},
		{"windows", "amd64", "amd64"},
		{"linux", "arm64", "arm64"},
		{"windows", "386", "386"},
		{"linux", "riscv64", "riscv64"},
	}
	for _, c := range cases {
		if got := CanonicalArch(c.goos, c.goarch); got != c.want {
			t.Errorf("CanonicalArch(%q,%q) = %q, want %q", c.goos, c.goarch, got, c.want)
		}
	}
}

func TestVersionInstallable_LinuxSpellingMatches(t *testing.T) {
	linuxX86 := Version{Archives: []Archive{{
		OS: "linux", Arch: "x86_64", Format: "tar.gz", URL: "https://example.test/x.tar.gz",
	}}}
	if !linuxX86.Installable("linux", "amd64") {
		t.Fatal("linux/x86_64 archive must be installable on linux/amd64")
	}
	if !linuxX86.Installable("linux", "x86_64") {
		t.Fatal("manifest-convention spelling must keep working")
	}
	if linuxX86.Installable("windows", "amd64") {
		t.Fatal("linux archive must not be installable on windows")
	}

	linuxArm := Version{Archives: []Archive{{
		OS: "linux", Arch: "arm64", Format: "tar.gz", URL: "https://example.test/x.tar.gz",
	}}}
	if !linuxArm.Installable("linux", "arm64") {
		t.Fatal("linux/arm64 archive must be installable on linux/arm64")
	}
	if linuxArm.Installable("linux", "amd64") {
		t.Fatal("arm64 archive must not be installable on linux/amd64")
	}

	anyArch := Version{Archives: []Archive{{
		OS: "any", Arch: "any", Format: "zip", URL: "https://example.test/x.zip",
	}}}
	if !anyArch.Installable("linux", "amd64") {
		t.Fatal(`"any"/"any" archive must be installable everywhere`)
	}
}
