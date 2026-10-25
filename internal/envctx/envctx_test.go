package envctx

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/plugin"
)

// 测试统一用真实的 plugin.Generic —— 它完整实现了 plugin.Plugin，
// 且 EnvVars / PathAppend 的插值逻辑正是生产路径用的那套。
// 自建 fake 需要复刻全部接口方法（ArchiveFor/InstallKind 等），
// 一旦接口新增方法就会静默失配，得不偿失。
func newGeneric(name string, vars map[string]string, dirs []string) *plugin.Generic {
	return &plugin.Generic{M: &manifest.Tool{
		Name:       name,
		EnvVars:    vars,
		PathAppend: dirs,
	}}
}

func TestResolve_NilPlugin(t *testing.T) {
	ctx := Resolve(nil, "/x")
	if !ctx.Empty() {
		t.Errorf("nil 插件应返回空上下文，得到 %+v", ctx)
	}
}

func TestResolve_EmptyInstallPath(t *testing.T) {
	p := newGeneric("x", map[string]string{"A": "1"}, nil)
	ctx := Resolve(p, "")
	if !ctx.Empty() {
		t.Errorf("空 installPath 应返回空上下文")
	}
}

func TestResolve_FiltersPathVar(t *testing.T) {
	// PATH 必须被排除：它走 PathAppend 通道，整串覆盖会毁掉用户 PATH。
	p := newGeneric("x", map[string]string{"PATH": "/evil", "GOOD": "1"}, nil)
	ctx := Resolve(p, "/x")
	if _, has := ctx.Vars["PATH"]; has {
		t.Error("PATH 不应进入 Vars")
	}
	if ctx.Vars["GOOD"] != "1" {
		t.Errorf("GOOD 应保留，得到 %q", ctx.Vars["GOOD"])
	}
}

func TestResolve_FiltersRelativePathAppend(t *testing.T) {
	abs := "/abs/bin"
	rel := "relative/bin"
	if runtime.GOOS == "windows" {
		abs = `C:\abs\bin`
		rel = `relative\bin`
	}
	p := newGeneric("x", nil, []string{rel, abs})
	ctx := Resolve(p, "/x")
	if len(ctx.PathAppend) != 1 {
		t.Errorf("相对路径应被过滤，得到 %v", ctx.PathAppend)
	}
}

func TestApply_OverridesVars(t *testing.T) {
	base := []string{"A=old", "B=keep"}
	ctx := &Context{Vars: map[string]string{"A": "new"}}
	out := ctx.Apply(base)

	if got := lookupEnv(out, "A"); got != "new" {
		t.Errorf("A = %q, want new", got)
	}
	if got := lookupEnv(out, "B"); got != "keep" {
		t.Errorf("B = %q, want keep（未被覆盖的应原样保留）", got)
	}
}

func TestApply_PrependsPath(t *testing.T) {
	sep := string(os.PathListSeparator)
	base := []string{"PATH=" + strings.Join([]string{"/usr/bin", "/bin"}, sep)}
	ctx := &Context{PathAppend: []string{"/opt/tool/bin"}}
	out := ctx.Apply(base)

	got := lookupEnv(out, findPathKey(out))
	parts := strings.Split(got, sep)
	if parts[0] != "/opt/tool/bin" {
		t.Errorf("新目录应在最前，得到 %q", got)
	}
	if len(parts) != 3 {
		t.Errorf("PATH 项数应为 3，得到 %d: %q", len(parts), got)
	}
}

func TestApply_PathIdempotent(t *testing.T) {
	sep := string(os.PathListSeparator)
	base := []string{"PATH=" + "/opt/tool/bin" + sep + "/usr/bin"}
	ctx := &Context{PathAppend: []string{"/opt/tool/bin"}}

	out := ctx.Apply(base)
	got := lookupEnv(out, findPathKey(out))
	if strings.Count(got, "/opt/tool/bin") != 1 {
		t.Errorf("已存在的目录不应重复添加，得到 %q", got)
	}
}

func TestApply_EmptyContextReturnsBase(t *testing.T) {
	base := []string{"A=1"}
	ctx := &Context{}
	out := ctx.Apply(base)
	if len(out) != 1 || out[0] != "A=1" {
		t.Errorf("空上下文应原样返回，得到 %v", out)
	}
}

func TestApply_NoPathInBase(t *testing.T) {
	ctx := &Context{PathAppend: []string{"/opt/bin"}}
	out := ctx.Apply([]string{"A=1"})
	if got := lookupEnv(out, findPathKey(out)); got != "/opt/bin" {
		t.Errorf("base 无 PATH 时应新建，得到 %q", got)
	}
}

// TestApply_OnlyOnePathEntry 确认结果里不会出现两条 PATH
// （Windows 上 base 常写作 "Path"，注入后必须仍是单条）。
func TestApply_OnlyOnePathEntry(t *testing.T) {
	key := "PATH"
	if runtime.GOOS == "windows" {
		key = "Path"
	}
	base := []string{key + "=/usr/bin", "A=1"}
	ctx := &Context{PathAppend: []string{"/opt/bin"}}
	out := ctx.Apply(base)

	n := 0
	for _, kv := range out {
		k, _, ok := strings.Cut(kv, "=")
		if ok && strings.EqualFold(k, "PATH") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("结果中 PATH 条目数应为 1，得到 %d: %v", n, out)
	}
}

// TestResolve_GenericManifest 用真实 Generic 插件走一遍端到端解析，
// 确认 {{install}} / {{ENVNAME}} 插值在 envctx 里同样生效。
func TestResolve_GenericManifest(t *testing.T) {
	install := filepath.Join(t.TempDir(), "install", "go", "1.27.3")
	g := newGeneric("go",
		map[string]string{"GOPATH": "{{install}}\\..\\gopath"},
		[]string{"{{GOPATH}}\\bin"},
	)

	ctx := Resolve(g, install)
	if len(ctx.Vars) != 1 || ctx.Vars["GOPATH"] == "" {
		t.Fatalf("GOPATH 未解析，得到 %+v", ctx.Vars)
	}
	if len(ctx.PathAppend) != 1 {
		t.Fatalf("path_append 未解析，得到 %v", ctx.PathAppend)
	}
	if filepath.Base(ctx.PathAppend[0]) != "bin" {
		t.Errorf("path_append 应以 bin 结尾，得到 %q", ctx.PathAppend[0])
	}
	if !filepath.IsAbs(ctx.PathAppend[0]) {
		t.Errorf("path_append 必须是绝对路径，得到 %q", ctx.PathAppend[0])
	}
}

// TestResolve_CleansPathVars 确认 `{{install}}\..\gopath` 这类带 `..` 的
// 声明会被规范成干净路径 —— 否则 `go env GOPATH` 会输出 `.../1.27.3/../gopath`。
func TestResolve_CleansPathVars(t *testing.T) {
	install := filepath.Join(t.TempDir(), "install", "go", "1.27.3")
	g := newGeneric("go",
		map[string]string{"GOPATH": "{{install}}\\..\\gopath"}, nil)

	ctx := Resolve(g, install)
	got := ctx.Vars["GOPATH"]
	if strings.Contains(got, "..") {
		t.Errorf("GOPATH 应已 Clean（不含 ..），得到 %q", got)
	}
	if filepath.Base(got) != "gopath" {
		t.Errorf("GOPATH 应以 gopath 结尾，得到 %q", got)
	}
}

// TestResolve_DoesNotCleanNonPathVars 确认非路径值不被误改。
func TestResolve_DoesNotCleanNonPathVars(t *testing.T) {
	g := newGeneric("x", map[string]string{
		"SOME_FLAG": "true",
		"SOME_NUM":  "1.0",
	}, nil)
	ctx := Resolve(g, "/x")
	if ctx.Vars["SOME_FLAG"] != "true" {
		t.Errorf("非路径值不应被改写，得到 %q", ctx.Vars["SOME_FLAG"])
	}
	if ctx.Vars["SOME_NUM"] != "1.0" {
		t.Errorf("非路径值不应被改写，得到 %q", ctx.Vars["SOME_NUM"])
	}
}
