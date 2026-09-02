package update

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xmdemt/xdev/internal/util"
	"github.com/xmdemt/xdev/internal/version"
)

// stubProbe 返回固定版本的探测桩（默认"旧版本"）。
func stubProbe(ver string) probeVersionFunc {
	return func(path string) string { return ver }
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TC-S5-1 旧副本被替换（替换后 hash == currentExe hash）
func TestReplaceStaleCLICopy_OldReplaced(t *testing.T) {
	home := t.TempDir()
	current := filepath.Join(t.TempDir(), "xdev.exe")
	copyPath := filepath.Join(home, "xdev.exe")
	writeFile(t, current, "NEW-BINARY-CONTENT")
	writeFile(t, copyPath, "OLD-BINARY-CONTENT")

	if err := ReplaceStaleCLICopyWithProbe(current, home, stubProbe("0.2.0")); err != nil {
		t.Fatalf("ReplaceStaleCLICopyWithProbe: %v", err)
	}
	got := readFile(t, copyPath)
	if got != "NEW-BINARY-CONTENT" {
		t.Errorf("copy not replaced: %q", got)
	}
	h1, _ := util.FileSHA256(current)
	h2, _ := util.FileSHA256(copyPath)
	if h1 != h2 {
		t.Errorf("hash mismatch after replace: %s vs %s", h1, h2)
	}
}

// TC-S5-2 同版本跳过（已是当前版本镜像，文件不变）
func TestReplaceStaleCLICopy_SameVersion(t *testing.T) {
	home := t.TempDir()
	current := filepath.Join(t.TempDir(), "xdev.exe")
	copyPath := filepath.Join(home, "xdev.exe")
	writeFile(t, current, "NEW")
	writeFile(t, copyPath, "OLD")

	if err := ReplaceStaleCLICopyWithProbe(current, home, stubProbe(version.Version)); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, copyPath); got != "OLD" {
		t.Errorf("copy changed on same version: %q", got)
	}
}

// TC-S5-3 版本号断层场景（冒烟反馈 #5 核心）：副本探测版本「高于」当前
// （旧分支 1.2.2-alpha vs 主线 0.3.3——CompareSemver 判 1.2.2 更新导致
// 永不替换的实测症状）。镜像一致性语义下：版本不同即替换。
func TestReplaceStaleCLICopy_HigherVersionStillReplaced(t *testing.T) {
	home := t.TempDir()
	current := filepath.Join(t.TempDir(), "xdev.exe")
	copyPath := filepath.Join(home, "xdev.exe")
	writeFile(t, current, "NEW-BINARY-CONTENT")
	writeFile(t, copyPath, "OLD-LEGACY-CONTENT")

	if err := ReplaceStaleCLICopyWithProbe(current, home, stubProbe("1.2.2-alpha")); err != nil {
		t.Fatalf("ReplaceStaleCLICopyWithProbe: %v", err)
	}
	if got := readFile(t, copyPath); got != "NEW-BINARY-CONTENT" {
		t.Errorf("legacy copy (higher semver, version discontinuity) not replaced: %q", got)
	}
}

// TC-S5-4 探测失败（空版本）跳过
func TestReplaceStaleCLICopy_ProbeFailure(t *testing.T) {
	home := t.TempDir()
	current := filepath.Join(t.TempDir(), "xdev.exe")
	copyPath := filepath.Join(home, "xdev.exe")
	writeFile(t, current, "NEW")
	writeFile(t, copyPath, "OLD")

	if err := ReplaceStaleCLICopyWithProbe(current, home, stubProbe("")); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, copyPath); got != "OLD" {
		t.Errorf("copy changed on probe failure: %q", got)
	}
}

// TC-S5-5 自指跳过（副本 == currentExe）
func TestReplaceStaleCLICopy_SelfReference(t *testing.T) {
	home := t.TempDir()
	copyPath := filepath.Join(home, "xdev.exe")
	writeFile(t, copyPath, "MYSELF")

	if err := ReplaceStaleCLICopyWithProbe(copyPath, home, stubProbe("0.1.0")); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, copyPath); got != "MYSELF" {
		t.Errorf("self copy was modified: %q", got)
	}
}

// TC-S5-6a 无副本（缺失场景）→ nil 不动
func TestReplaceStaleCLICopy_NoCopy(t *testing.T) {
	home := t.TempDir()
	current := filepath.Join(t.TempDir(), "xdev.exe")
	writeFile(t, current, "NEW")

	if err := ReplaceStaleCLICopyWithProbe(current, home, stubProbe("0.1.0")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, "xdev.exe")); !os.IsNotExist(err) {
		t.Errorf("missing copy should stay missing, stat err=%v", err)
	}
}

// TC-S5-6b 副本为目录 → nil 跳过
func TestReplaceStaleCLICopy_CopyIsDir(t *testing.T) {
	home := t.TempDir()
	current := filepath.Join(t.TempDir(), "xdev.exe")
	writeFile(t, current, "NEW")
	if err := os.MkdirAll(filepath.Join(home, "xdev.exe"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := ReplaceStaleCLICopyWithProbe(current, home, stubProbe("0.1.0")); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(home, "xdev.exe"))
	if err != nil || !fi.IsDir() {
		t.Errorf("dir copy should be untouched, err=%v", err)
	}
}