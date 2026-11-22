package extractor

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExtractionBudgetMatrix(t *testing.T) {
	for _, format := range []string{"zip", "tar.gz", "7z"} {
		for _, tc := range []struct {
			name           string
			file, total    int64
			entries, depth int
			ok             bool
		}{{"exact", 4, 8, 2, 2, true}, {"one-byte-over-file", 3, 8, 2, 2, false}, {"one-byte-over-total", 4, 7, 2, 2, false}, {"entries", 4, 8, 1, 2, false}, {"depth", 4, 8, 2, 1, false}, {"larger-trusted-profile", 8 << 30, 30 << 30, 100, 20, true}} {
			t.Run(format+"/"+tc.name, func(t *testing.T) {
				dir := t.TempDir()
				src := filepath.Join(dir, "sample")
				dst := filepath.Join(dir, "out")
				entries := []string{"bin/a|abcd", "bin/b|1234"}
				if format == "tar.gz" {
					createTestTarGz(t, src, entries)
				} else {
					createTestZip(t, src, entries)
				}
				b := DefaultBudget()
				b.MaxFileBytes = tc.file
				b.MaxTotalBytes = tc.total
				b.MaxEntries = tc.entries
				b.MaxDepth = tc.depth
				e := ExtractWithOptions(context.Background(), src, dst, format, 0, b)
				if (e == nil) != tc.ok {
					t.Fatalf("got %v", e)
				}
				if tc.ok {
					if readFile(t, filepath.Join(dst, "bin", "a")) != "abcd" {
						t.Fatal("content")
					}
				} else if _, e := os.Stat(dst); !os.IsNotExist(e) {
					t.Fatalf("failed output published: %v", e)
				}
				if stages, _ := filepath.Glob(filepath.Join(dir, ".xdev-extract-*")); len(stages) != 0 {
					t.Fatalf("left staging %v", stages)
				}
			})
		}
	}
}
func TestLessDataAndForgedSizeNeverWritePastBudget(t *testing.T) {
	for _, data := range []string{"123", "12345"} {
		d := t.TempDir()
		b := DefaultBudget()
		b.MaxFileBytes = 4
		b.MaxTotalBytes = 4
		x := extraction{ctx: context.Background(), dst: d, Budget: b, buf: make([]byte, 2)}
		out := filepath.Join(d, "f")
		if e := x.file(out, 0600, 4, strings.NewReader(data)); e == nil {
			t.Fatal("invalid size accepted")
		}
		fi, e := os.Stat(out)
		if e != nil || fi.Size() > 4 {
			t.Fatalf("written beyond limit: %v, %v", fi, e)
		}
	}
}
func TestExtractTimeAndSpace(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "t.zip")
	createTestZip(t, src, []string{"bin/a|abcd"})
	for _, kind := range []string{"cancel", "time", "space"} {
		ctx, cancel := context.WithCancel(context.Background())
		b := DefaultBudget()
		switch kind {
		case "cancel":
			cancel()
		case "time":
			b.Timeout = time.Nanosecond
		case "space":
			b.ReserveBytes = ^uint64(0)
		}
		e := ExtractWithOptions(ctx, src, filepath.Join(dir, kind), "zip", 0, b)
		cancel()
		if e == nil {
			t.Fatal("should refuse", kind)
		}
	}
}
func TestLocalBudgetPolicy(t *testing.T) {
	t.Setenv("XDEV_EXTRACT_MAX_FILE_BYTES", "2")
	d := t.TempDir()
	src := filepath.Join(d, "a.zip")
	createTestZip(t, src, []string{"a|123"})
	if e := Extract(src, filepath.Join(d, "out"), "zip", 0); e == nil {
		t.Fatal("production API ignored configured budget")
	}
	t.Setenv("XDEV_EXTRACT_MAX_FILE_BYTES", "3")
	if e := Extract(src, filepath.Join(d, "out"), "zip", 0); e != nil {
		t.Fatal(e)
	}
}
func TestDMGBundleAggregateBudget(t *testing.T) {
	d := t.TempDir()
	mount := filepath.Join(d, "mount")
	stage := filepath.Join(d, "stage")
	os.Mkdir(stage, 0700)
	for _, n := range []string{"One.app", "Two.app"} {
		os.MkdirAll(filepath.Join(mount, n), 0755)
		os.WriteFile(filepath.Join(mount, n, "f"), []byte("abc"), 0644)
	}
	b := DefaultBudget()
	b.MaxTotalBytes = 5
	x := extraction{ctx: context.Background(), dst: stage, Budget: b, buf: make([]byte, 2)}
	if _, e := x.copyAppsFromMount(mount); e == nil || x.total > 5 {
		t.Fatalf("copied beyond shared budget: %v %d", e, x.total)
	}
}
func TestReal7zaArchive(t *testing.T) {
	tool := os.Getenv("XDEV_TEST_7ZA")
	if tool == "" {
		t.Skip("set XDEV_TEST_7ZA to an installed 7za path")
	}
	d := t.TempDir()
	src := filepath.Join(d, "fixture.7z")
	data := bytes.Repeat([]byte{'z'}, 4096)
	os.WriteFile(filepath.Join(d, "safe.txt"), data, 0644)
	cmd := exec.Command(tool, "a", "-t7z", src, "safe.txt")
	cmd.Dir = d
	log, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("make archive: %v %s", e, log)
	}
	t.Logf("fixture generator: %s", log)
	// Extraction must be portable without any external decoder in PATH.
	t.Setenv("PATH", t.TempDir())
	b := DefaultBudget()
	b.MaxFileBytes = 4095
	dst := filepath.Join(d, "out")
	if e = ExtractWithOptions(context.Background(), src, dst, "7z", 0, b); e == nil {
		t.Fatal("oversized native 7z accepted")
	}
	b.MaxFileBytes++
	if e = ExtractWithOptions(context.Background(), src, dst, "7z", 0, b); e != nil {
		t.Fatal(e)
	}
	if got := readFile(t, filepath.Join(dst, "safe.txt")); got != string(data) {
		t.Fatal("wrong real 7z data")
	}
}
