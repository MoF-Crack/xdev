// Package pmtest supplies no-op executable fixtures for PM integration tests.
package pmtest

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func init() {
	dir := os.Getenv("XDEV_PM_TEST_COMMAND_DIR")
	if dir == "" {
		return
	}
	exe, err := os.Executable()
	if err != nil || filepath.Dir(exe) != dir {
		return
	}
	journal := filepath.Join(dir, "calls")
	f, err := os.OpenFile(journal, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		os.Exit(1)
	}
	fmt.Fprintln(f, filepath.Base(exe), strings.Join(os.Args[1:], " "))
	f.Close()
	if strings.TrimSuffix(filepath.Base(exe), ".exe") == "net" {
		os.Exit(5)
	}
	fmt.Println("1.0")
	os.Exit(0)
}

// Each child only records argv and exits. In particular, a shell never interprets its arguments.
func Commands(t *testing.T, names ...string) func() string {
	t.Helper()
	dir := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		dst := filepath.Join(dir, name)
		if err := os.Link(exe, dst); err != nil {
			src, err := os.Open(exe)
			if err != nil {
				t.Fatal(err)
			}
			out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
			if err != nil {
				t.Fatal(err)
			}
			_, err = io.Copy(out, src)
			src.Close()
			if e := out.Close(); e != nil {
				t.Fatal(e)
			}
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Setenv("PATH", dir)
	t.Setenv("XDEV_PM_TEST_COMMAND_DIR", dir)
	return func() string {
		data, err := os.ReadFile(filepath.Join(dir, "calls"))
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		return string(data)
	}
}
