package env

import (
	"path/filepath"
	"testing"
)

// --- extractVersionFromOutput tests ---

func TestExtractVersionFromOutput_Node(t *testing.T) {
	got := extractVersionFromOutput("v20.18.0")
	if got != "20.18.0" {
		t.Errorf("got %q, want %q", got, "20.18.0")
	}
}

func TestExtractVersionFromOutput_Python(t *testing.T) {
	got := extractVersionFromOutput("Python 3.12.7")
	if got != "3.12.7" {
		t.Errorf("got %q, want %q", got, "3.12.7")
	}
}

func TestExtractVersionFromOutput_Yarn(t *testing.T) {
	got := extractVersionFromOutput("1.22.22")
	if got != "1.22.22" {
		t.Errorf("got %q, want %q", got, "1.22.22")
	}
}

func TestExtractVersionFromOutput_Pnpm(t *testing.T) {
	got := extractVersionFromOutput("9.1.2")
	if got != "9.1.2" {
		t.Errorf("got %q, want %q", got, "9.1.2")
	}
}

func TestExtractVersionFromOutput_Gradle(t *testing.T) {
	got := extractVersionFromOutput("Gradle 9.4.1")
	if got != "9.4.1" {
		t.Errorf("got %q, want %q", got, "9.4.1")
	}
}

func TestExtractVersionFromOutput_Java(t *testing.T) {
	got := extractVersionFromOutput("java 25.0.3")
	if got != "25.0.3" {
		t.Errorf("got %q, want %q", got, "25.0.3")
	}
}

func TestExtractVersionFromOutput_Empty(t *testing.T) {
	got := extractVersionFromOutput("")
	if got != "" {
		t.Errorf("got %q, want %q", got, "")
	}
}

// --- isInsideXdevDir tests ---

func TestIsInsideXdevDir_True(t *testing.T) {
	xdevDir := filepath.Join("C:", "Users", "dev", ".xmdev")
	path := filepath.Join("C:", "Users", "dev", ".xmdev", "install", "node", "v20")
	if !isInsideXdevDir(path, xdevDir) {
		t.Errorf("isInsideXdevDir(%q, %q) = false, want true", path, xdevDir)
	}
}

func TestIsInsideXdevDir_False(t *testing.T) {
	xdevDir := filepath.Join("C:", "Users", "dev", ".xmdev")
	path := filepath.Join("C:", "Program Files", "nodejs")
	if isInsideXdevDir(path, xdevDir) {
		t.Errorf("isInsideXdevDir(%q, %q) = true, want false", path, xdevDir)
	}
}

// --- isVersionLike tests ---

func TestIsVersionLike_Valid(t *testing.T) {
	cases := []struct {
		input string
	}{
		{"1.22.0"},
		{"20.18.0"},
		{"3.12.7"},
		{"9.4.1"},
	}
	for _, tc := range cases {
		if !isVersionLike(tc.input) {
			t.Errorf("isVersionLike(%q) = false, want true", tc.input)
		}
	}
}

func TestIsVersionLike_Invalid(t *testing.T) {
	cases := []struct {
		input string
	}{
		{"abc"},
		{"1"},
		{""},
	}
	for _, tc := range cases {
		if isVersionLike(tc.input) {
			t.Errorf("isVersionLike(%q) = true, want false", tc.input)
		}
	}
}
