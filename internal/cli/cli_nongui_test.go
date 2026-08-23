package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCLISourcesDoNotImportGUI(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(thisFile)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), `github.com/xmdemt/xdev/internal/gui`) {
			t.Errorf("%s imports internal/gui — CLI/shim must not link Wails (Spec 030)", name)
		}
	}
}

// TestGUILauncherHook 单二进制模型：xdev gui 子命令通过 main 注入的
// 启动器在进程内启动 GUI；未注入时给出明确错误。
func TestGUILauncherHook(t *testing.T) {
	orig := guiLauncher
	defer func() { guiLauncher = orig }()

	// nil launcher → 明确报错
	guiLauncher = nil
	if err := newGuiCmd().RunE(nil, nil); err == nil {
		t.Error("expected error when GUI launcher not injected")
	}

	// 注入后 → launcher 被调用
	called := 0
	guiLauncher = func() { called++ }
	if err := newGuiCmd().RunE(nil, nil); err != nil {
		t.Fatalf("RunE with launcher: %v", err)
	}
	if called != 1 {
		t.Errorf("launcher called %d times, want 1", called)
	}
}

// TestSetGUILauncher 验证注入入口。
func TestSetGUILauncher(t *testing.T) {
	orig := guiLauncher
	defer func() { guiLauncher = orig }()
	called := 0
	SetGUILauncher(func() { called++ })
	if guiLauncher == nil {
		t.Fatal("launcher not registered")
	}
	guiLauncher()
	if called != 1 {
		t.Errorf("launcher called %d times, want 1", called)
	}
}
