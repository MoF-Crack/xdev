package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunHelpAndArgumentErrors(t *testing.T) {
	var out bytes.Buffer
	if code := run([]string{"--help"}, &out, &out); code != 0 {
		t.Fatalf("help exit = %d", code)
	}
	for _, flag := range []string{"-tool", "-version", "-role"} {
		if !strings.Contains(out.String(), flag) {
			t.Fatalf("help missing %s: %s", flag, out.String())
		}
	}
	if code := run([]string{"-mode", "bad"}, &out, &out); code != 2 {
		t.Fatalf("bad mode exit = %d", code)
	}
}

func TestRunFilterWithNoMatchesIsAnError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "demo.yaml"), []byte("name: demo\nversions:\n  - version: '1'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if code := run([]string{"-manifests", dir, "-tool", "missing"}, &output, &output); code != 2 || !strings.Contains(output.String(), "no manifest entries matched") {
		t.Fatalf("exit = %d, output = %s", code, output.String())
	}
}

func TestRunRejectsInvalidRole(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "demo.yaml"), []byte("name: demo\nversions:\n  - version: '1'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if code := run([]string{"-manifests", dir, "-role", "primary"}, &output, &output); code != 2 || !strings.Contains(output.String(), "expected official or mirror") {
		t.Fatalf("exit = %d, output = %s", code, output.String())
	}
}

func TestWriteReportIsAtomic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := writeReport(path, func(w io.Writer) error { _, _ = w.Write([]byte("partial")); return os.ErrInvalid })
	if err == nil {
		t.Fatal("expected write error")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "old" {
		t.Fatalf("report = %q", data)
	}
	if err := writeReport(path, func(w io.Writer) error { _, err := w.Write([]byte("new")); return err }); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(path)
	if err != nil || string(data) != "new" {
		t.Fatalf("successful report = %q, err=%v", data, err)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp") {
			t.Fatalf("temporary file remains: %s", entry.Name())
		}
	}
}

func TestRunWritesBothReportsBeforeDeterministicExit(t *testing.T) {
	dir := t.TempDir()
	manifest := "name: demo\nversions:\n  - version: '1'\n    archives:\n      - os: windows\n        arch: amd64\n        format: zip\n        mirror_url: https://mirror.invalid/base/\n        sha256: ''\n"
	if err := os.WriteFile(filepath.Join(dir, "demo.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	jsonPath, mdPath := filepath.Join(dir, "out.json"), filepath.Join(dir, "out.md")
	var output bytes.Buffer
	code := run([]string{"-manifests", dir, "-platform", "windows/amd64", "-mode", "full", "-json", jsonPath, "-markdown", mdPath, "-fail-on-deterministic"}, &output, &output)
	if code != 1 {
		t.Fatalf("exit = %d, output=%s", code, output.String())
	}
	for _, path := range []string{jsonPath, mdPath} {
		data, err := os.ReadFile(path)
		if err != nil || !bytes.Contains(data, []byte("MISSING_CHECKSUM")) {
			t.Fatalf("report %s: %q, err=%v", path, data, err)
		}
	}
}
