package util

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLooksLikeVersionDirName(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		// 真实版本语料
		{"1.2.2-alpha", true},
		{"2026.2.0.1", true},
		{"go1.22.10", true}, // 数字开头判据会误杀，必须接受
		{"17", true},
		{"v2.0.0", true},

		// 本机实测的辅助目录（20261021 审计 P1-4）
		{"export_templates", false},
		{"gopath", false},
		{"unsafe-version-blocked", false},
		{"unsafe-name-blocked", false},
		{"unsafe-path-blocked", false},
		{"lld_is_a_generic_driver._", false},

		// xdev 自己的残留
		{".stale-abc", false},
		{"node.stale-abc", false},
		{".tmp-123", false},

		// 路径安全 / 空值
		{"", false},
		{".", false},
		{"..", false},
		{"../evil", false},
		{`..\evil`, false},
		{`a/b`, false},
		{`a\b`, false},
	}
	for _, tc := range cases {
		if got := LooksLikeVersionDirName(tc.name); got != tc.want {
			t.Errorf("LooksLikeVersionDirName(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestListVersionDirNames_FiltersAuxiliaryDirs(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"1.2.2-alpha", "go1.22.10", "17",
		"export_templates", "gopath", "unsafe-version-blocked",
		"lld_is_a_generic_driver._", ".stale-x", ".tmp-y",
	} {
		if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// 普通文件（不是目录）不得入列。
	if err := os.WriteFile(filepath.Join(dir, "20.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ListVersionDirNames(dir)
	if err != nil {
		t.Fatalf("ListVersionDirNames: %v", err)
	}
	want := []string{"1.2.2-alpha", "17", "go1.22.10"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ListVersionDirNames = %v, want %v", got, want)
	}
}

func TestListVersionDirNames_MissingDirIsError(t *testing.T) {
	if _, err := ListVersionDirNames(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("missing dir must return an error so callers can distinguish empty from unreadable")
	}
}

func TestIsResidueName_SharedWithShim(t *testing.T) {
	// 单一事实来源：util 是定义，shim.IsStaleResidueName 只委托。
	for _, name := range []string{"node.stale-1", "go.tmp-2", ".STALE-x"} {
		if !IsResidueName(name) {
			t.Errorf("IsResidueName(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"node", "go1.22.10", "1.2.2-alpha"} {
		if IsResidueName(name) {
			t.Errorf("IsResidueName(%q) = true, want false", name)
		}
	}
}
