package installer

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 20261021 审计 P3-15：`removeLinkEntry` 过去用
// `filepath.Dir(filepath.Dir(path))` 反推 xmdeHome，只对
// `<home>/current/<tool>` 这一种形状成立。现在 home 是显式参数。

func TestRemoveLinkEntry_ArchivesUnderPassedHome(t *testing.T) {
	// 让归档后的删除失败，保留证据条目。
	orig := removeAllResidue
	removeAllResidue = func(string) error { return errors.New("forced: keep archive") }
	t.Cleanup(func() { removeAllResidue = orig })

	home := t.TempDir()
	other := t.TempDir()

	// path 故意放在另一个目录下，证明归档位置只由传入的 home 决定。
	link := filepath.Join(other, "current", "faketool")
	if err := os.MkdirAll(link, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(link, "marker"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	removeLinkEntry(link, home)

	entries, err := os.ReadDir(filepath.Join(home, ".stale-residue"))
	if err != nil {
		t.Fatalf("archive dir under passed home missing: %v", err)
	}
	if len(entries) == 0 {
		t.Error("residue must be archived under the explicit xmdeHome")
	}
	if _, err := os.Stat(filepath.Join(other, ".stale-residue")); err == nil {
		t.Error("residue must NOT be archived under a home derived from the path")
	}
}

func TestRemoveLinkEntry_SourceUsesExplicitHome(t *testing.T) {
	data, err := os.ReadFile("installer.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	start := strings.Index(src, "func removeLinkEntry(")
	if start < 0 {
		t.Fatal("removeLinkEntry not found")
	}
	body := src[start:]
	if end := strings.Index(body[1:], "\nfunc "); end > 0 {
		body = body[:end+1]
	}

	var code strings.Builder
	for _, line := range strings.Split(body, "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		code.WriteString(line)
		code.WriteString("\n")
	}
	exec := code.String()

	if strings.Contains(exec, "filepath.Dir(filepath.Dir(") {
		t.Error("removeLinkEntry must not derive xmdeHome from the path; take it as an explicit parameter")
	}
	if !strings.Contains(exec, "xmdeHome") {
		t.Error("removeLinkEntry must use the explicit xmdeHome parameter")
	}
}
