package extractor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Optional pre-generated native 7z fixture. The CI maintainer can prepare
// names unavailable on the runtime filesystem on another host.
func TestNative7zRawNameAndControl(t *testing.T) {
	base := os.Getenv("XDEV_TEST_RAW7Z")
	if base == "" {
		t.Skip("provide directory with normal.7z and newline.7z generated on Unix")
	}
	for _, name := range []string{"normal", "newline"} {
		d := t.TempDir()
		dst := filepath.Join(d, "out")
		e := ExtractWithOptions(context.Background(), filepath.Join(base, name+".7z"), dst, "7z", 0, DefaultBudget())
		if name == "normal" {
			if e != nil {
				t.Fatal(e)
			}
			if got := readFile(t, filepath.Join(dst, "ok.txt")); got != "sample" {
				t.Fatalf("wrong fixture output: %q", got)
			}
		} else {
			if e == nil {
				t.Fatal("lossy decoding accepted raw newline")
			}
			t.Logf("rejected raw name: %q", e)
			if _, err := os.Stat(dst); !os.IsNotExist(err) {
				t.Fatalf("published rejection: %v", err)
			}
		}
	}
}
