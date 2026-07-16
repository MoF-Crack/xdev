package java

import (
	"regexp"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/plugin"
)

// temurinTestManifest returns a manifest pinned to fixed Adoptium Temurin
// assets (URL + SHA256) for Java 17 and 21 across all 5 platforms.
// Java 11 is left with empty archives as a placeholder to exercise the
// "no archive -> error" security path.
func temurinTestManifest() *manifest.Tool {
	return &manifest.Tool{
		Name:        "java",
		DisplayName: "Eclipse Temurin",
		Latest:      "21",
		LTS:         "17",
		VersionList: []manifest.Version{
			{
				Version: "21",
				Archives: []manifest.Archive{
					{
						OS:     "windows",
						Arch:   "amd64",
						Format: "zip",
						URL:    "https://github.com/adoptium/temurin21-binaries/releases/download/jdk-21.0.11%2B10/OpenJDK21U-jdk_x64_windows_hotspot_21.0.11_10.zip",
						SHA256: "d3625e7cadf23787ea540229544b6e2ab494b3b54da1801879e583e1dfee0a64",
						Strip:  1,
					},
					{
						OS:     "linux",
						Arch:   "x86_64",
						Format: "tar.gz",
						URL:    "https://github.com/adoptium/temurin21-binaries/releases/download/jdk-21.0.11%2B10/OpenJDK21U-jdk_x64_linux_hotspot_21.0.11_10.tar.gz",
						SHA256: "4b2220e232a97997b436ca6ab15cbf70171ecff52958a46159dfa5a8c44ca4de",
						Strip:  1,
					},
					{
						OS:     "linux",
						Arch:   "arm64",
						Format: "tar.gz",
						URL:    "https://github.com/adoptium/temurin21-binaries/releases/download/jdk-21.0.11%2B10/OpenJDK21U-jdk_aarch64_linux_hotspot_21.0.11_10.tar.gz",
						SHA256: "8d498ec88e1c1989fab95c6784240ab92d011e29c54d20a3f9c324b13476f9ad",
						Strip:  1,
					},
					{
						OS:     "darwin",
						Arch:   "x86_64",
						Format: "tar.gz",
						URL:    "https://github.com/adoptium/temurin21-binaries/releases/download/jdk-21.0.11%2B10/OpenJDK21U-jdk_x64_mac_hotspot_21.0.11_10.tar.gz",
						SHA256: "34180eb03e6d207c388cce3da668f6cc7cd7508c185c24782fadac2c9c0e66f9",
						Strip:  1,
					},
					{
						OS:     "darwin",
						Arch:   "arm64",
						Format: "tar.gz",
						URL:    "https://github.com/adoptium/temurin21-binaries/releases/download/jdk-21.0.11%2B10/OpenJDK21U-jdk_aarch64_mac_hotspot_21.0.11_10.tar.gz",
						SHA256: "6ebcf221c9b41507b14c098e93c6ead6440b8d9bd154f8ec666c4c73abbdb201",
						Strip:  1,
					},
				},
			},
			{
				Version: "17",
				Archives: []manifest.Archive{
					{
						OS:     "windows",
						Arch:   "amd64",
						Format: "zip",
						URL:    "https://github.com/adoptium/temurin17-binaries/releases/download/jdk-17.0.19%2B10/OpenJDK17U-jdk_x64_windows_hotspot_17.0.19_10.zip",
						SHA256: "b5b235c48adf6a081874b812c630b9f4b5f637b7a5ed18b9174d08a41ec4c235",
						Strip:  1,
					},
					{
						OS:     "linux",
						Arch:   "x86_64",
						Format: "tar.gz",
						URL:    "https://github.com/adoptium/temurin17-binaries/releases/download/jdk-17.0.19%2B10/OpenJDK17U-jdk_x64_linux_hotspot_17.0.19_10.tar.gz",
						SHA256: "d8afc263758141a66e0e3aafc321e783f7016696f4eaea067d340a269037d331",
						Strip:  1,
					},
					{
						OS:     "linux",
						Arch:   "arm64",
						Format: "tar.gz",
						URL:    "https://github.com/adoptium/temurin17-binaries/releases/download/jdk-17.0.19%2B10/OpenJDK17U-jdk_aarch64_linux_hotspot_17.0.19_10.tar.gz",
						SHA256: "83a52172678ec8975164648654869cb2e71d7c748b47aca94b29bbfa10c18e81",
						Strip:  1,
					},
					{
						OS:     "darwin",
						Arch:   "x86_64",
						Format: "tar.gz",
						URL:    "https://github.com/adoptium/temurin17-binaries/releases/download/jdk-17.0.19%2B10/OpenJDK17U-jdk_x64_mac_hotspot_17.0.19_10.tar.gz",
						SHA256: "03632d1fbf139ab3719a9f4b47dc206251449b87557143c822336dbf8c06560f",
						Strip:  1,
					},
					{
						OS:     "darwin",
						Arch:   "arm64",
						Format: "tar.gz",
						URL:    "https://github.com/adoptium/temurin17-binaries/releases/download/jdk-17.0.19%2B10/OpenJDK17U-jdk_aarch64_mac_hotspot_17.0.19_10.tar.gz",
						SHA256: "8fa1eff40bb637a33613b2ccb8b12c70dc3661cc22cf8e784943715769a05336",
						Strip:  1,
					},
				},
			},
			// Java 11: placeholder — no fixed assets, must error.
			{Version: "11", Archives: nil},
		},
	}
}

// hex64 matches exactly 64 lowercase hex characters.
var hex64 = regexp.MustCompile(`^[0-9a-f]{64}$`)

// TestJavaPlugin_ImplementsPluginInterface verifies at compile time that
// *Plugin satisfies plugin.Plugin.
func TestJavaPlugin_ImplementsPluginInterface(t *testing.T) {
	var _ plugin.Plugin = (*Plugin)(nil)
	var _ plugin.Plugin = New(temurinTestManifest())
}

// TestArchiveForPlatform_17_ReturnsPinnedTemurin checks that Java 17 on
// windows/amd64 returns the correct pinned Temurin asset with a valid
// 64-hex SHA-256.
func TestArchiveForPlatform_17_ReturnsPinnedTemurin(t *testing.T) {
	p := New(temurinTestManifest())
	a, err := p.ArchiveForPlatform("17", "windows", "amd64")
	if err != nil {
		t.Fatalf("ArchiveForPlatform(17, windows, amd64): %v", err)
	}
	if a == nil {
		t.Fatal("archive is nil")
	}
	if !strings.Contains(a.URL, "adoptium/temurin") {
		t.Errorf("URL = %q, want containing adoptium/temurin", a.URL)
	}
	if !strings.Contains(a.URL, "temurin17-binaries") {
		t.Errorf("URL = %q, want temurin17-binaries", a.URL)
	}
	if a.SHA256 == "" {
		t.Error("SHA256 is empty")
	}
	if !hex64.MatchString(a.SHA256) {
		t.Errorf("SHA256 = %q, want 64-char hex", a.SHA256)
	}
	wantSHA := "b5b235c48adf6a081874b812c630b9f4b5f637b7a5ed18b9174d08a41ec4c235"
	if a.SHA256 != wantSHA {
		t.Errorf("SHA256 = %q, want %q", a.SHA256, wantSHA)
	}
	if a.EffectiveKind() != manifest.InstallKindArchive {
		t.Errorf("EffectiveKind() = %q, want archive", a.EffectiveKind())
	}
}

// TestArchiveForPlatform_21_ReturnsPinnedTemurin checks that Java 21 on
// windows/amd64 returns the correct pinned Temurin asset with a valid
// 64-hex SHA-256.
func TestArchiveForPlatform_21_ReturnsPinnedTemurin(t *testing.T) {
	p := New(temurinTestManifest())
	a, err := p.ArchiveForPlatform("21", "windows", "amd64")
	if err != nil {
		t.Fatalf("ArchiveForPlatform(21, windows, amd64): %v", err)
	}
	if a == nil {
		t.Fatal("archive is nil")
	}
	if !strings.Contains(a.URL, "adoptium/temurin") {
		t.Errorf("URL = %q, want containing adoptium/temurin", a.URL)
	}
	if !strings.Contains(a.URL, "temurin21-binaries") {
		t.Errorf("URL = %q, want temurin21-binaries", a.URL)
	}
	if !hex64.MatchString(a.SHA256) {
		t.Errorf("SHA256 = %q, want 64-char hex", a.SHA256)
	}
	wantSHA := "d3625e7cadf23787ea540229544b6e2ab494b3b54da1801879e583e1dfee0a64"
	if a.SHA256 != wantSHA {
		t.Errorf("SHA256 = %q, want %q", a.SHA256, wantSHA)
	}
}

// TestArchiveForPlatform_AllPlatforms verifies that every platform
// returns a non-empty archive with a Temurin URL and valid SHA for
// both Java 17 and 21.
func TestArchiveForPlatform_AllPlatforms(t *testing.T) {
	p := New(temurinTestManifest())
	platforms := []struct {
		os, arch string
	}{
		{"windows", "amd64"},
		{"linux", "x86_64"},
		{"linux", "arm64"},
		{"darwin", "x86_64"},
		{"darwin", "arm64"},
	}
	for _, v := range []string{"17", "21"} {
		for _, pl := range platforms {
			a, err := p.ArchiveForPlatform(v, pl.os, pl.arch)
			if err != nil {
				t.Errorf("ArchiveForPlatform(%s, %s, %s): %v", v, pl.os, pl.arch, err)
				continue
			}
			if a == nil {
				t.Errorf("ArchiveForPlatform(%s, %s, %s): nil archive", v, pl.os, pl.arch)
				continue
			}
			if !strings.Contains(a.URL, "adoptium/temurin") {
				t.Errorf("[%s/%s/%s] URL = %q, want adoptium/temurin", v, pl.os, pl.arch, a.URL)
			}
			if !hex64.MatchString(a.SHA256) {
				t.Errorf("[%s/%s/%s] SHA256 = %q, want 64-char hex", v, pl.os, pl.arch, a.SHA256)
			}
		}
	}
}

// TestArchiveForPlatform_PlaceholderErrors verifies that a version
// with empty archives (placeholder) returns an error mentioning
// "placeholder" rather than synthesising a dynamic URL. This is the
// security-critical path: never return an archive without a pinned SHA.
func TestArchiveForPlatform_PlaceholderErrors(t *testing.T) {
	p := New(temurinTestManifest())
	_, err := p.ArchiveForPlatform("11", "windows", "amd64")
	if err == nil {
		t.Fatal("expected error for placeholder (empty archives) version 11, got nil")
	}
	if !strings.Contains(err.Error(), "placeholder") {
		t.Errorf("error %q should mention placeholder", err)
	}
}

// TestArchiveForPlatform_UnknownVersionErrors verifies that an unknown
// version returns a clear error.
func TestArchiveForPlatform_UnknownVersionErrors(t *testing.T) {
	p := New(temurinTestManifest())
	_, err := p.ArchiveForPlatform("99", "windows", "amd64")
	if err == nil {
		t.Fatal("expected error for unknown version 99, got nil")
	}
}

// TestArchiveFor_DelegatesToHostPlatform verifies that ArchiveFor
// (the Plugin interface method) delegates to ArchiveForPlatform with
// the host's OS/Arch and does not panic.
func TestArchiveFor_DelegatesToHostPlatform(t *testing.T) {
	p := New(temurinTestManifest())
	// Should not panic; on the test host this may succeed or error
	// depending on platform, but must never panic.
	_, _ = p.ArchiveFor("21")
}
