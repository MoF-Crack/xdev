package project

import (
	"os"
	"path/filepath"
	"testing"
)

// makeVersions 在 installRoot/<tool>/ 下造出给定版本目录。
func makeVersions(t *testing.T, installRoot, tool string, versions ...string) {
	t.Helper()
	for _, v := range versions {
		dir := filepath.Join(installRoot, tool, v)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func TestResolveInstalled_Exact(t *testing.T) {
	root := t.TempDir()
	makeVersions(t, root, "python", "3.10.4", "3.11.9", "3.12.1")

	got, ok := ResolveInstalled(root, "python", "3.11.9")
	if !ok || got != "3.11.9" {
		t.Errorf("got %q, %v; want 3.11.9, true", got, ok)
	}
}

func TestResolveInstalled_ExactNotInstalled(t *testing.T) {
	root := t.TempDir()
	makeVersions(t, root, "python", "3.10.4", "3.11.9")

	// 精确值未安装时必须失败（不得退化成范围匹配到别的版本）
	if got, ok := ResolveInstalled(root, "python", "3.12.0"); ok {
		t.Errorf("未安装的精确版本不应解析成功，得到 %q", got)
	}
}

func TestResolveInstalled_Prefix(t *testing.T) {
	root := t.TempDir()
	makeVersions(t, root, "python", "3.10.4", "3.11.9", "3.11.10", "3.12.1")

	got, ok := ResolveInstalled(root, "python", "3.11")
	if !ok {
		t.Fatal("3.11 应解析成功")
	}
	// 应取 3.11.x 里最高的
	if got != "3.11.10" {
		t.Errorf("got %q, want 3.11.10（应取该系列最高）", got)
	}
}

func TestResolveInstalled_Latest(t *testing.T) {
	root := t.TempDir()
	makeVersions(t, root, "node", "18.20.0", "20.11.0", "21.5.0")

	got, ok := ResolveInstalled(root, "node", "latest")
	if !ok {
		t.Fatal("latest 应解析成功")
	}
	if got != "21.5.0" {
		t.Errorf("got %q, want 21.5.0（latest 取最高）", got)
	}
}

func TestResolveInstalled_NoVersions(t *testing.T) {
	root := t.TempDir()
	if _, ok := ResolveInstalled(root, "python", "3.11"); ok {
		t.Error("没有任何已安装版本时应失败")
	}
}

func TestResolveInstalled_IgnoresNonVersionDirs(t *testing.T) {
	root := t.TempDir()
	makeVersions(t, root, "go", "1.27.3")
	// 这些是 install 目录下可能出现的辅助目录，不是版本
	for _, d := range []string{"gopath", "export_templates", "unsafe-version-blocked"} {
		if err := os.MkdirAll(filepath.Join(root, "go", d), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	got, ok := ResolveInstalled(root, "go", "latest")
	if !ok || got != "1.27.3" {
		t.Errorf("got %q, %v; want 1.27.3, true（辅助目录不应被当成版本）", got, ok)
	}
}

func TestResolveInstalled_InvalidSpec(t *testing.T) {
	root := t.TempDir()
	makeVersions(t, root, "python", "3.11.9")

	if _, ok := ResolveInstalled(root, "python", "!!!bad"); ok {
		t.Error("非法规格不应解析成功")
	}
}

func TestResolveInstalled_EmptyArgs(t *testing.T) {
	root := t.TempDir()
	if _, ok := ResolveInstalled(root, "", "3.11"); ok {
		t.Error("空 tool 应失败")
	}
	if _, ok := ResolveInstalled(root, "python", ""); ok {
		t.Error("空 spec 应失败")
	}
}

func TestDirForAndExists(t *testing.T) {
	root := t.TempDir()
	makeVersions(t, root, "go", "1.27.3")

	want := filepath.Join(root, "go", "1.27.3")
	if got := DirFor(root, "go", "1.27.3"); got != want {
		t.Errorf("DirFor = %q, want %q", got, want)
	}
	if !Exists(root, "go", "1.27.3") {
		t.Error("已创建的版本目录应 Exists=true")
	}
	if Exists(root, "go", "9.9.9") {
		t.Error("不存在的版本应 Exists=false")
	}
}
