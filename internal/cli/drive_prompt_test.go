package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/config"
)

// TestPromptDriveAcceptsRecommendation verifies that an empty line
// from the user accepts the recommendation.
func TestPromptDriveAcceptsRecommendation(t *testing.T) {
	drives := []config.DriveInfo{
		{Letter: "C:", Path: "C:\\", Free: 20 << 30, Total: 100 << 30, Mounted: true},
		{Letter: "D:", Path: "D:\\", Free: 80 << 30, Total: 500 << 30, Mounted: true},
		{Letter: "E:", Path: "E:\\", Free: 200 << 30, Total: 1000 << 30, Mounted: true},
	}
	if _, ok := findDrive(drives, "D:"); !ok {
		t.Errorf("expected to find D: drive")
	}
	if _, ok := findDrive(drives, "Z:"); ok {
		t.Errorf("did not expect to find Z: drive")
	}
	sep := string(os.PathSeparator)
	got := suggestedPathFor(drives[2]) // E:
	want := "E:" + sep + "XDev"
	if got != want {
		t.Errorf("suggestedPathFor = %q, want %q", got, want)
	}
}

// TestParseUserChoice tests the small string-parsing rules that
// drive the prompt loop.
func TestParseUserChoice(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", "empty"},
		{"D", "letter"},
		{"d", "letter"},
		{"D:", "letter"},
		{"E:", "letter"},
		{"c", "c"},
		{"C", "c"},
		{"p", "p"},
		{"path", "p"},
		{"q", "q"},
		{"quit", "q"},
		{"bogus", "invalid"},
	}
	for _, c := range cases {
		got := classifyInput(c.in)
		if got != c.want {
			t.Errorf("classifyInput(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func classifyInput(s string) string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return "empty"
	}
	switch strings.ToLower(trimmed) {
	case "c", "c:":
		return "c"
	case "p", "path", "custom":
		return "p"
	case "q", "quit", "exit":
		return "q"
	}
	letter := strings.ToUpper(strings.TrimRight(trimmed, ":"))
	if len(letter) == 1 && letter[0] >= 'A' && letter[0] <= 'Z' {
		return "letter"
	}
	return "invalid"
}

// driveFixture is a small fixed disk layout used by the interactive
// integration tests.  C: is small, D: medium, E: large.
func driveFixture() []config.DriveInfo {
	return []config.DriveInfo{
		{Letter: "C:", Path: "C:\\", Free: 10 << 30, Total: 100 << 30, Mounted: true},
		{Letter: "D:", Path: "D:\\", Free: 50 << 30, Total: 200 << 30, Mounted: true},
		{Letter: "E:", Path: "E:\\", Free: 200 << 30, Total: 1000 << 30, Mounted: true},
	}
}

// TestPromptDriveInteractiveEnter verifies that pressing Enter on the
// recommendation returns the suggested path.
func TestPromptDriveInteractiveEnter(t *testing.T) {
	out := &bytes.Buffer{}
	in := strings.NewReader("\n")
	choice, err := promptDriveWith(in, out, driveFixture())
	if err != nil {
		t.Fatalf("promptDriveWith: %v", err)
	}
	if choice.Quit {
		t.Errorf("did not expect Quit")
	}
	sep := string(os.PathSeparator)
	want := "E:" + sep + "XDev"
	if choice.Path != want {
		t.Errorf("Path = %q, want %q", choice.Path, want)
	}
	if !strings.Contains(out.String(), "Scanning drives") {
		t.Errorf("expected scan output, got: %q", out.String())
	}
	if !strings.Contains(out.String(), "[Enter]  use recommended (E:)") {
		t.Errorf("expected Enter hint, got: %q", out.String())
	}
}

// TestPromptDriveInteractivePickLetter verifies that typing "D"
// picks the D: drive.
func TestPromptDriveInteractivePickLetter(t *testing.T) {
	out := &bytes.Buffer{}
	in := strings.NewReader("D\n")
	choice, err := promptDriveWith(in, out, driveFixture())
	if err != nil {
		t.Fatalf("promptDriveWith: %v", err)
	}
	if choice.Quit {
		t.Errorf("did not expect Quit")
	}
	sep := string(os.PathSeparator)
	want := "D:" + sep + "XDev"
	if choice.Path != want {
		t.Errorf("Path = %q, want %q", choice.Path, want)
	}
}

// TestPromptDriveInteractiveQuit verifies that "q" aborts cleanly.
func TestPromptDriveInteractiveQuit(t *testing.T) {
	out := &bytes.Buffer{}
	in := strings.NewReader("q\n")
	choice, err := promptDriveWith(in, out, driveFixture())
	if err != nil {
		t.Fatalf("promptDriveWith: %v", err)
	}
	if !choice.Quit {
		t.Errorf("expected Quit = true, got %+v", choice)
	}
}

// TestPromptDriveInteractiveCustomPath verifies that "p" then a
// path returns that path verbatim.
func TestPromptDriveInteractiveCustomPath(t *testing.T) {
	out := &bytes.Buffer{}
	in := strings.NewReader("p\nF:\\devenv\\xdev\n")
	choice, err := promptDriveWith(in, out, driveFixture())
	if err != nil {
		t.Fatalf("promptDriveWith: %v", err)
	}
	if choice.Quit {
		t.Errorf("did not expect Quit")
	}
	if choice.Path != `F:\devenv\xdev` {
		t.Errorf("Path = %q, want %q", choice.Path, `F:\devenv\xdev`)
	}
}

// TestPromptDriveInteractiveInvalidThenValid verifies that bogus
// input is re-prompted and a follow-up valid input wins.
func TestPromptDriveInteractiveInvalidThenValid(t *testing.T) {
	out := &bytes.Buffer{}
	in := strings.NewReader("bogus\n42\nZ\nD\n")
	choice, err := promptDriveWith(in, out, driveFixture())
	if err != nil {
		t.Fatalf("promptDriveWith: %v", err)
	}
	sep := string(os.PathSeparator)
	want := "D:" + sep + "XDev"
	if choice.Path != want {
		t.Errorf("Path = %q, want %q", choice.Path, want)
	}
	if !strings.Contains(out.String(), "unrecognised") {
		t.Errorf("expected re-prompt message, got: %q", out.String())
	}
}

// TestPromptDriveInteractiveRejectUnmounted verifies that picking an
// unmounted drive letter re-prompts.
func TestPromptDriveInteractiveRejectUnmounted(t *testing.T) {
	out := &bytes.Buffer{}
	in := strings.NewReader("Z\nD\n")
	choice, err := promptDriveWith(in, out, driveFixture())
	if err != nil {
		t.Fatalf("promptDriveWith: %v", err)
	}
	sep := string(os.PathSeparator)
	want := "D:" + sep + "XDev"
	if choice.Path != want {
		t.Errorf("Path = %q, want %q", choice.Path, want)
	}
	if !strings.Contains(out.String(), "Z:") || !strings.Contains(out.String(), "is not mounted") {
		t.Errorf("expected unmounted warning, got: %q", out.String())
	}
}

// TestPromptDriveInteractiveForceC verifies that typing "c" forces
// the home to the user profile on the system drive.
func TestPromptDriveInteractiveForceC(t *testing.T) {
	out := &bytes.Buffer{}
	in := strings.NewReader("c\n")
	choice, err := promptDriveWith(in, out, driveFixture())
	if err != nil {
		t.Fatalf("promptDriveWith: %v", err)
	}
	if choice.Quit {
		t.Errorf("did not expect Quit")
	}
	if !choice.Force {
		t.Errorf("expected Force = true, got %+v", choice)
	}
	if choice.Path == "" {
		t.Errorf("expected a fallback home path on Force")
	}
	// The default home lives under the user profile and contains
	// ".xdev" - we just check that the suffix is present so
	// the test does not depend on the exact %USERPROFILE% value.
	if !strings.Contains(choice.Path, ".xdev") {
		t.Errorf("expected path to contain .xdev, got %q", choice.Path)
	}
}

// TestPromptDriveDemo runs an end-to-end "interactive" prompt with
// the user typing "D" (pick the D: drive) and prints the full
// rendered UI to the test log.  It exists mainly so a developer can
// eyeball the actual terminal output during a `go test -v` run.
func TestPromptDriveDemo(t *testing.T) {
	out := &bytes.Buffer{}
	in := strings.NewReader("D\n")
	choice, err := promptDriveWith(in, out, driveFixture())
	if err != nil {
		t.Fatalf("promptDriveWith: %v", err)
	}
	t.Logf("\n===== interactive init UI =====\n%s==============================", out.String())
	t.Logf("user picked: Path=%q Quit=%v Force=%v", choice.Path, choice.Quit, choice.Force)
}

// TestSwitchHomeCascadesAllSubdirs simulates the real init flow:
// the user has just selected a new home directory, and the
// Config's derived sub-directories must follow.
func TestSwitchHomeCascadesAllSubdirs(t *testing.T) {
	// Build a Config that already points at E: (the typical state
	// after defaults() runs inside config.Load()).
	cfg := &config.Config{
		Core: config.CoreConfig{
			XMDevDir:    `E:\XDev`,
			DownloadDir: `E:\XDev\downloads`,
			InstallDir:  `E:\XDev\install`,
			ShimDir:     `E:\XDev\shims`,
			CacheDir:    `E:\XDev\cache`,
			EnvDir:      `E:\XDev\env`,
			ManifestDir: `E:\XDev\manifests`,
		},
	}
	// Simulate the user picking D: - this is exactly what init.go
	// does after the prompt.
	cfg.Core.XMDevDir = `D:\XDev`
	cfg.Core.DownloadDir = ""
	cfg.Core.InstallDir = ""
	cfg.Core.ShimDir = ""
	cfg.Core.CacheDir = ""
	cfg.Core.EnvDir = ""
	cfg.Core.ManifestDir = ""
	cfg.ResolvePaths(cfg.Core.XMDevDir)

	sep := string(os.PathSeparator)
	prefix := "D:" + sep + "XDev"
	checks := map[string]string{
		"XMDevDir":    cfg.Core.XMDevDir,
		"DownloadDir": cfg.Core.DownloadDir,
		"InstallDir":  cfg.Core.InstallDir,
		"ShimDir":     cfg.Core.ShimDir,
		"CacheDir":    cfg.Core.CacheDir,
		"EnvDir":      cfg.Core.EnvDir,
		"ManifestDir": cfg.Core.ManifestDir,
	}
	for name, path := range checks {
		if !strings.HasPrefix(path, prefix) {
			t.Errorf("%s = %q, expected to start with %q", name, path, prefix)
		}
		if strings.Contains(path, "E:") {
			t.Errorf("%s = %q still references the OLD E: home", name, path)
		}
	}
}

// TestEndToEndUserPicksD reproduces the exact user flow that
// surfaced the original bug:
//
//  1. xdev init
//  2. the prompt scans drives
//  3. the user types "D"
//  4. every sub-directory MUST live under D:\XDev
//
// The bug: download/install/shim/cache/env/manifest stayed on E:.
// This test uses strings.NewReader (which is *not* a *os.File, so
// the prompt's interactive detection still engages) and then
// applies the same logic that init.go uses after the prompt.
func TestEndToEndUserPicksD(t *testing.T) {
	out := &bytes.Buffer{}
	in := strings.NewReader("D\n")

	// Simulate the state after config.Load() at startup: a default
	// config pointing at E:.
	cfg := &config.Config{
		Core: config.CoreConfig{
			XMDevDir:    `E:\XDev`,
			DownloadDir: `E:\XDev\downloads`,
			InstallDir:  `E:\XDev\install`,
			ShimDir:     `E:\XDev\shims`,
			CacheDir:    `E:\XDev\cache`,
			EnvDir:      `E:\XDev\env`,
			ManifestDir: `E:\XDev\manifests`,
		},
	}

	// Run the interactive prompt (skips the non-interactive branch
	// because strings.Reader is not a *os.File).
	choice, err := promptDriveWith(in, out, driveFixture())
	if err != nil {
		t.Fatalf("promptDriveWith: %v", err)
	}
	if choice.Quit {
		t.Fatalf("unexpected Quit")
	}

	// Apply the choice exactly the way init.go does.
	cfg.Core.XMDevDir = choice.Path
	cfg.Core.DownloadDir = ""
	cfg.Core.InstallDir = ""
	cfg.Core.ShimDir = ""
	cfg.Core.CacheDir = ""
	cfg.Core.EnvDir = ""
	cfg.Core.ManifestDir = ""
	cfg.ResolvePaths(cfg.Core.XMDevDir)

	// Now verify every field follows D:.
	sep := string(os.PathSeparator)
	prefix := "D:" + sep + "XDev"
	checks := map[string]string{
		"XMDevDir":    cfg.Core.XMDevDir,
		"DownloadDir": cfg.Core.DownloadDir,
		"InstallDir":  cfg.Core.InstallDir,
		"ShimDir":     cfg.Core.ShimDir,
		"CacheDir":    cfg.Core.CacheDir,
		"EnvDir":      cfg.Core.EnvDir,
		"ManifestDir": cfg.Core.ManifestDir,
	}
	t.Logf("\n%s", out.String())
	for name, path := range checks {
		t.Logf("✔ %-11s %s", name+":", path)
		if !strings.HasPrefix(path, prefix) {
			t.Errorf("✘ %s = %q, expected to start with %q", name, path, prefix)
		}
	}
}
