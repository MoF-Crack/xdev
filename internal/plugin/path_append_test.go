package plugin

import (
	"path/filepath"
	"testing"

	"github.com/xmdemt/xdev/internal/manifest"
)

// P1 回归守卫（2026-10-25）：PathAppend 必须正确插值三种模板，
// 且过滤掉非绝对路径与重复项。
func TestGeneric_PathAppend(t *testing.T) {
	// 注意：install 必须是**本平台意义上的**绝对路径。
	// 直接用 "/x/install/go/1.27.3" 在 Windows 上 filepath.IsAbs 返回 false
	// （缺盘符），会让 PathAppend 把每条都当非绝对路径过滤掉 —— 这不是 bug，
	// 而是测试用例没考虑平台差异。t.TempDir() 天然是绝对路径。
	install := filepath.Join(t.TempDir(), "install", "go", "1.27.3")

	t.Run("无声明返回 nil", func(t *testing.T) {
		g := &Generic{M: &manifest.Tool{Name: "x"}}
		if got := g.PathAppend(install); got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})

	t.Run("{{install}} 插值", func(t *testing.T) {
		g := &Generic{M: &manifest.Tool{
			Name:       "go",
			PathAppend: []string{"{{install}}/bin"},
		}}
		got := g.PathAppend(install)
		want := filepath.Clean(filepath.Join(install, "bin"))
		if len(got) != 1 || got[0] != want {
			t.Errorf("got %v, want [%s]", got, want)
		}
	})

	t.Run("{{ENVNAME}} 引用同 manifest 变量", func(t *testing.T) {
		g := &Generic{M: &manifest.Tool{
			Name:    "go",
			EnvVars: map[string]string{"GOPATH": "{{install}}\\..\\gopath"},
			// 用 {{GOPATH}} 引用上面定义的变量
			PathAppend: []string{"{{GOPATH}}\\bin"},
		}}
		got := g.PathAppend(install)
		if len(got) != 1 {
			t.Fatalf("got %v, want 1 entry", got)
		}
		if !filepath.IsAbs(got[0]) {
			t.Errorf("entry %q 不是绝对路径", got[0])
		}
		if filepath.Base(got[0]) != "bin" {
			t.Errorf("entry %q 应以 bin 结尾", got[0])
		}
		// 必须真的解析掉了 {{GOPATH}}，不能留下模板残留
		if filepath.Base(filepath.Dir(got[0])) != "gopath" {
			t.Errorf("entry %q 的父目录应为 gopath（说明 {{GOPATH}} 未被替换）", got[0])
		}
	})

	t.Run("{{tool}} 插值", func(t *testing.T) {
		g := &Generic{M: &manifest.Tool{
			Name:       "mytool",
			PathAppend: []string{"{{install}}/{{tool}}-bin"},
		}}
		got := g.PathAppend(install)
		if len(got) != 1 || filepath.Base(got[0]) != "mytool-bin" {
			t.Errorf("got %v, want entry ending with mytool-bin", got)
		}
	})

	t.Run("过滤非绝对路径", func(t *testing.T) {
		g := &Generic{M: &manifest.Tool{
			Name:       "x",
			PathAppend: []string{"relative/bin", "{{install}}/bin"},
		}}
		got := g.PathAppend(install)
		if len(got) != 1 {
			t.Errorf("got %v, want only the absolute entry", got)
		}
	})

	t.Run("去重", func(t *testing.T) {
		g := &Generic{M: &manifest.Tool{
			Name:       "x",
			PathAppend: []string{"{{install}}/bin", "{{install}}/bin"},
		}}
		if got := g.PathAppend(install); len(got) != 1 {
			t.Errorf("got %v, want 1 entry after dedup", got)
		}
	})

	t.Run("空条目被跳过", func(t *testing.T) {
		g := &Generic{M: &manifest.Tool{
			Name:       "x",
			PathAppend: []string{"", "   ", "{{install}}/bin"},
		}}
		if got := g.PathAppend(install); len(got) != 1 {
			t.Errorf("got %v, want 1 entry", got)
		}
	})
}
