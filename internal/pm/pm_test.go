package pm

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestBuiltinMatchesYAML 守卫「内置回退」与「数据文件」内容一致。
//
// 为什么需要它：builtin.go 是 manifests/package-managers.yaml 的容错副本。
// 两份数据如果悄悄分叉，用户在文件损坏时会得到与正常情况下**不同**的
// 包管理器定义 —— 而且这种差异只在故障时才暴露，极难排查。
//
// 本测试让任何一边的修改都必须同步到另一边。
func TestBuiltinMatchesYAML(t *testing.T) {
	dir := repoManifestsDir(t)
	defs, err := Load(dir)
	if err != nil {
		t.Fatalf("Load(%s): %v", dir, err)
	}
	builtin := Builtin()

	if len(defs) != len(builtin) {
		t.Fatalf("条目数不一致: YAML=%d builtin=%d", len(defs), len(builtin))
	}

	// 按 name 建索引后逐字段比对（YAML 与 builtin 的顺序可能不同）。
	byName := make(map[string]Def, len(builtin))
	for _, d := range builtin {
		byName[d.Name] = d
	}

	for _, want := range defs {
		got, ok := byName[want.Name]
		if !ok {
			t.Errorf("builtin 缺少 %q（YAML 有）", want.Name)
			continue
		}
		if !reflect.DeepEqual(want, got) {
			t.Errorf("%q 定义不一致:\n  YAML   = %+v\n  builtin= %+v", want.Name, want, got)
		}
	}
}

// TestLoad_RealFile 确认仓库里的数据文件能被正确解析。
func TestLoad_RealFile(t *testing.T) {
	dir := repoManifestsDir(t)
	defs, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(defs) != 13 {
		t.Errorf("条目数 = %d, want 13", len(defs))
	}

	// 5 个带可执行命令的条目必须在（这是本次数据化的核心动机）。
	withCmd := map[string]bool{}
	for _, d := range defs {
		if d.InstallCommand != "" || d.EnableCommand != "" {
			withCmd[d.Name] = true
		}
	}
	for _, name := range []string{"brew", "scoop", "choco", "yarn", "pnpm"} {
		if !withCmd[name] {
			t.Errorf("%q 应带可执行命令（install_command 或 enable_command）", name)
		}
	}
}

// TestLoad_MissingFile 确认文件缺失时返回错误（供 LoadOrDefault 回退）。
func TestLoad_MissingFile(t *testing.T) {
	if _, err := Load(t.TempDir()); err == nil {
		t.Error("文件不存在时应返回错误")
	}
}

// TestLoadOrDefault_FallsBack 确认任何加载失败都回退到内置定义。
func TestLoadOrDefault_FallsBack(t *testing.T) {
	// 空目录 → 文件不存在 → 回退
	defs := LoadOrDefault(t.TempDir())
	if len(defs) == 0 {
		t.Fatal("应回退到内置定义，得到空")
	}

	// 损坏的 YAML → 回退
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte("package_managers: [broken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	defs = LoadOrDefault(dir)
	if len(defs) == 0 {
		t.Fatal("YAML 损坏时应回退到内置定义")
	}
}

// TestValidate 覆盖结构性校验。
func TestValidate(t *testing.T) {
	cases := []struct {
		name    string
		defs    []Def
		wantErr bool
	}{
		{"空名", []Def{{Name: ""}}, true},
		{"重复名", []Def{{Name: "a"}, {Name: "a"}}, true},
		{"有命令无执行环境", []Def{{Name: "a", InstallCommand: "x"}}, true},
		{"enable 无父工具", []Def{{Name: "a", EnableCommand: "x"}}, true},
		{"正常", []Def{{Name: "a", InstallCommand: "x", ExecutionEnv: "bash"}}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validate(c.defs)
			if (err != nil) != c.wantErr {
				t.Errorf("validate() err = %v, wantErr = %v", err, c.wantErr)
			}
		})
	}
}

// TestFind 覆盖按名查找（大小写不敏感）。
func TestFind(t *testing.T) {
	defs := Builtin()
	if d, ok := Find(defs, "SCOOP"); !ok || d.Name != "scoop" {
		t.Errorf("Find(SCOOP) = %+v, %v", d, ok)
	}
	if _, ok := Find(defs, "nonexistent"); ok {
		t.Error("未知名字应返回 false")
	}
}

// TestFindByToolName 覆盖按 xdev 工具名反查（pip → python）。
func TestFindByToolName(t *testing.T) {
	defs := Builtin()
	if d, ok := FindByToolName(defs, "python"); !ok || d.Name != "pip" {
		t.Errorf("FindByToolName(python) = %+v, %v; want pip", d, ok)
	}
	if _, ok := FindByToolName(defs, "nonexistent"); ok {
		t.Error("未知工具名应返回 false")
	}
}

// TestBuiltin_ReturnsCopy 确认 Builtin 返回副本，调用方改动不影响下次调用。
func TestBuiltin_ReturnsCopy(t *testing.T) {
	a := Builtin()
	if len(a) == 0 {
		t.Fatal("Builtin 不应为空")
	}
	a[0].Name = "MUTATED"

	b := Builtin()
	if b[0].Name == "MUTATED" {
		t.Error("Builtin 必须返回副本，不能暴露内部切片")
	}
}

// repoManifestsDir 从测试目录回溯到仓库根的 manifests/。
func repoManifestsDir(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		candidate := filepath.Join(dir, "manifests")
		if fi, err := os.Stat(candidate); err == nil && fi.IsDir() {
			return candidate
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("找不到 manifests 目录")
	return ""
}
