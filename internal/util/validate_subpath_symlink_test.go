package util

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// 20261022 CI（macOS）：current/<tool> 在 Activate 校验时已经是符号链接，
// 旧实现会把整条 child 解析到 install 树，导致所有合法激活被判为越界。
// 正确语义：校验**链接路径本身**是否在 parent 内，不跟随最后一段。
func TestValidateSubPath_DoesNotFollowChildLeafSymlink(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "current")
	outside := filepath.Join(root, "install", "tool", "1.0.0")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(parent, "tool")
	if err := os.Symlink(outside, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink needs privileges on Windows: %v", err)
		}
		t.Fatalf("symlink: %v", err)
	}

	if err := ValidateSubPath(parent, link); err != nil {
		t.Errorf("link path itself is inside parent, must pass (macOS regression): %v", err)
	}
}

// 中间目录的符号链接仍然必须被识别为越界（修复不能放松安全检查）。
func TestValidateSubPath_RejectsIntermediateSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "current")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}

	// parent/link -> outside（中间段），再拼 leaf。
	escape := filepath.Join(parent, "link")
	if err := os.Symlink(outside, escape); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink needs privileges on Windows: %v", err)
		}
		t.Fatalf("symlink: %v", err)
	}

	if err := ValidateSubPath(parent, filepath.Join(escape, "leaf")); err == nil {
		t.Error("intermediate symlink pointing outside parent must be rejected")
	}
}
