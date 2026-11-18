package gui

import (
	"context"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Business modules must not acquire a framework runtime dependency. The adapter
// owns UI effects, and a missing desktop must be safe in tests/maintenance mode.
func TestBusinessModulesUseDesktopAdapter(t *testing.T) {
	files, err := filepath.Glob("app*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		node, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range node.Imports {
			name, _ := strconv.Unquote(imp.Path.Value)
			if strings.Contains(name, "github.com/wailsapp/wails/") {
				t.Errorf("%s imports framework runtime %s", file, name)
			}
		}
	}
	app := &App{}
	app.emitEvent("test", map[string]string{"state": "safe"})
	if _, err := app.chooseDirectory("Select"); err == nil {
		t.Fatal("missing desktop must report dialog unavailable")
	}
	if _, err := app.confirmInstall(context.Background(), "fixture", "1.0"); err == nil {
		t.Fatal("missing desktop must not confirm installation")
	}
}

func TestRuntimeVersionIsCurrent(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	info := (&App{}).GetAboutInfo()
	if !strings.Contains(string(data), "github.com/wailsapp/wails/v3 "+info.WailsVersion) {
		t.Fatalf("About Wails version %s differs from dependency", info.WailsVersion)
	}
}
