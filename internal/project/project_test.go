package project

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFind_NoConfig(t *testing.T) {
	dir := t.TempDir()
	_, err := Find(dir)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestFind_InCurrentDir(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ConfigFileName), "tools:\n  python: \"3.11\"\n")

	cfg, err := Find(dir)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if cfg.Root != dir {
		t.Errorf("Root = %q, want %q", cfg.Root, dir)
	}
	if spec, ok := cfg.Spec("python"); !ok || spec != "3.11" {
		t.Errorf("Spec(python) = %q, %v; want \"3.11\", true", spec, ok)
	}
}

func TestFind_InParentDir(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ConfigFileName), "tools:\n  node: \"^20\"\n")

	deep := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}

	cfg, err := Find(deep)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if cfg.Root != root {
		t.Errorf("Root = %q, want %q（应向上找到项目根）", cfg.Root, root)
	}
}

func TestFind_NearestWins(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ConfigFileName), "tools:\n  python: \"3.10\"\n")
	nested := filepath.Join(root, "sub")
	writeFile(t, filepath.Join(nested, ConfigFileName), "tools:\n  python: \"3.12\"\n")

	cfg, err := Find(nested)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if spec, _ := cfg.Spec("python"); spec != "3.12" {
		t.Errorf("应取最近的那份配置，得到 %q", spec)
	}
}

func TestFind_AltExtension(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".xdev.yml"), "tools:\n  go: latest\n")

	cfg, err := Find(dir)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if spec, ok := cfg.Spec("go"); !ok || spec != "latest" {
		t.Errorf("Spec(go) = %q, %v", spec, ok)
	}
}

func TestFind_EmptyTools(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ConfigFileName), "tools: {}\n")

	cfg, err := Find(dir)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(cfg.Tools) != 0 {
		t.Errorf("Tools 应为空，得到 %v", cfg.Tools)
	}
}

func TestParse_NormalizesNames(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ConfigFileName), "tools:\n  Python: \" 3.11 \"\n  NODE: \"20\"\n")

	cfg, err := Find(dir)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if spec, ok := cfg.Spec("python"); !ok || spec != "3.11" {
		t.Errorf("工具名应小写、版本应去空白，得到 %q, %v", spec, ok)
	}
	if spec, ok := cfg.Spec("NODE"); !ok || spec != "20" {
		t.Errorf("Spec 查询也应大小写不敏感，得到 %q, %v", spec, ok)
	}
}

func TestParse_SkipsEmptyEntries(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ConfigFileName), "tools:\n  \"\": \"1.0\"\n  python: \"\"\n  go: \"1.27\"\n")

	cfg, err := Find(dir)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(cfg.Tools) != 1 {
		t.Errorf("空条目应被跳过，得到 %v", cfg.Tools)
	}
	if _, ok := cfg.Spec("go"); !ok {
		t.Error("go 应保留")
	}
}

func TestParse_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ConfigFileName), "tools: [unclosed\n")

	if _, err := Find(dir); err == nil {
		t.Error("非法 YAML 应报错")
	}
}

func TestToolNames_Sorted(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ConfigFileName), "tools:\n  node: \"20\"\n  go: \"1.27\"\n  python: \"3.11\"\n")

	cfg, err := Find(dir)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	names := cfg.ToolNames()
	want := []string{"go", "node", "python"}
	if len(names) != len(want) {
		t.Fatalf("ToolNames = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("ToolNames[%d] = %q, want %q（应排序）", i, names[i], want[i])
		}
	}
}

func TestNilConfig_Safe(t *testing.T) {
	var c *Config
	if _, ok := c.Spec("python"); ok {
		t.Error("nil Config 的 Spec 应返回 false")
	}
	if c.ToolNames() != nil {
		t.Error("nil Config 的 ToolNames 应返回 nil")
	}
}
