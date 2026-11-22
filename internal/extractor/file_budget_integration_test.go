package extractor

import (
	"os"
	"path/filepath"
	"testing"
)

// This regression exercises production Extract rather than the lower-level
// writer. The same test also runs under the previous implementation via an
// overlay that scales its original hardcoded limits down to 4 and 5 bytes.
func TestExtractExternalRouteNeverPublishesOverBudget(t *testing.T) {
	d := t.TempDir()
	src := filepath.Join(d, "archive.zip")
	dst := filepath.Join(d, "destination")
	createTestZip(t, src, []string{"first|abc", "second|def"})
	t.Setenv("XDEV_EXTRACT_MAX_FILE_BYTES", "4")
	t.Setenv("XDEV_EXTRACT_MAX_TOTAL_BYTES", "5")
	if err := Extract(src, dst, "7z", 0); err == nil {
		t.Error("accepted 6 expanded bytes with a 5-byte total budget")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Errorf("published failed extraction: %v", err)
	}
}
