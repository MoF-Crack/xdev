package toolmeta

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

// TC-P1 CommonInstallPaths 与 env 旧实现（Windows 9 case）逐字节兼容
func TestCommonInstallPaths_EnvCompatible(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only install paths")
	}
	t.Setenv("ProgramFiles", `C:\TestPF`)
	t.Setenv("ProgramFiles(x86)", `C:\TestPF86`)
	t.Setenv("LOCALAPPDATA", `C:\TestLA`)

	cases := map[string][]string{
		"python":                 {`C:\TestLA\Programs\Python`},
		"node":                   {`C:\TestPF\nodejs`, `C:\TestLA\nvm`},
		"go":                     {`C:\TestPF\Go`},
		"java":                   {`C:\TestPF\Java`, `C:\TestPF86\Java`},
		"dotnet":                 {`C:\TestPF\dotnet`},
		"ruby":                   {`C:\TestPF\Ruby`},
		"git":                    {`C:\TestPF\Git`},
		"flutter":                {`C:\TestLA\flutter`},
		"android-platform-tools": {filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local", "Android", "Sdk", "platform-tools")},
	}
	for name, want := range cases {
		got := CommonInstallPaths(name)
		if len(got) < len(want) {
			t.Fatalf("CommonInstallPaths(%q) = %v, want prefix %v", name, got, want)
		}
		prefix := got[:len(want)]
		// go 在 windows 下固定含 %ProgramFiles%\Go，prefix 断言用前 len(want) 项
		if !reflect.DeepEqual(prefix, want) {
			t.Errorf("CommonInstallPaths(%q) prefix = %v, want %v (full: %v)", name, prefix, want, got)
		}
	}
}

// TC-P2 CommonInstallPaths 超集保留（mingw / typescript；darwin / linux 分支）
func TestCommonInstallPaths_Superset(t *testing.T) {
	t.Setenv("LOCALAPPDATA", `C:\TestLA`)
	t.Setenv("ProgramFiles", `C:\TestPF`)

	if runtime.GOOS == "windows" {
		got := CommonInstallPaths("mingw")
		if !containsAll(got, []string{`C:\TestLA\Programs\mingw64`, `C:\TestLA\Programs\mingw32`, `C:\TestPF\mingw64`, `C:\TestPF\mingw32`, filepath.Join("C:", "msys64", "mingw64")}) {
			t.Errorf("CommonInstallPaths(mingw) = %v, want mingw superset", got)
		}
		gotTS := CommonInstallPaths("typescript")
		if !containsAll(gotTS, []string{`C:\TestLA\npm\node_modules\typescript\bin`, `C:\TestPF\nodejs\node_modules\typescript\bin`}) {
			t.Errorf("CommonInstallPaths(typescript) = %v, want npm-global superset", gotTS)
		}
	}
	if runtime.GOOS == "darwin" {
		if got := CommonInstallPaths("go"); !containsAll(got, []string{"/usr/local/go/bin/go", "/opt/homebrew/bin/go"}) {
			t.Errorf("darwin go paths = %v", got)
		}
	}
	if runtime.GOOS == "linux" {
		if got := CommonInstallPaths("java"); !containsAll(got, []string{"/usr/bin/java", "/usr/local/bin/java"}) {
			t.Errorf("linux java paths = %v", got)
		}
	}
}

// TC-P3 CommonInstallPaths 条件探测：~/.nvm / ~/.pyenv / ~/.sdkman 存在才追加
func TestCommonInstallPaths_ConditionalProbe(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	node := CommonInstallPaths("node")
	if _, err := os.Stat(filepath.Join(home, ".nvm")); err == nil {
		if !containsAll(node, []string{filepath.Join(home, ".nvm")}) {
			t.Errorf("node paths should include ~/.nvm when it exists: %v", node)
		}
	} else {
		if containsAll(node, []string{filepath.Join(home, ".nvm")}) {
			t.Errorf("node paths must not include ~/.nvm when missing: %v", node)
		}
	}
	python := CommonInstallPaths("python")
	if _, err := os.Stat(filepath.Join(home, ".pyenv")); err == nil {
		if !containsAll(python, []string{filepath.Join(home, ".pyenv")}) {
			t.Errorf("python paths should include ~/.pyenv when it exists: %v", python)
		}
	}
}

// TC-P4 CheckEnvVars 查表：环境变量指向含 bin 子目录的目录时返回二进制路径
func TestCheckEnvVars(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	goBin := filepath.Join(binDir, "go")
	if err := os.WriteFile(goBin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("GOROOT", dir)
	if got := CheckEnvVars("go"); got != goBin {
		t.Errorf("CheckEnvVars(go) = %q, want %q", got, goBin)
	}

	// 无关联环境变量的工具直接返回空（查表键为空）
	if got := CheckEnvVars("ruby"); got != "" {
		t.Errorf("CheckEnvVars(ruby) = %q, want empty", got)
	}
	// 未知工具返回空
	if got := CheckEnvVars("unknown-tool"); got != "" {
		t.Errorf("CheckEnvVars(unknown) = %q, want empty", got)
	}
}

// TC-P4b CheckEnvVars android-platform-tools 子目录特判（platform-tools/adb）
func TestCheckEnvVars_AndroidPlatformTools(t *testing.T) {
	dir := t.TempDir()
	ptDir := filepath.Join(dir, "platform-tools")
	if err := os.MkdirAll(ptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	adbName := "adb"
	if runtime.GOOS == "windows" {
		adbName = "adb.exe"
	}
	adbPath := filepath.Join(ptDir, adbName)
	if err := os.WriteFile(adbPath, []byte("binary"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("ANDROID_HOME", dir)
	if got := CheckEnvVars("android-platform-tools"); got != adbPath {
		t.Errorf("CheckEnvVars(android-platform-tools) = %q, want %q", got, adbPath)
	}
}

func containsAll(got, want []string) bool {
	set := map[string]bool{}
	for _, p := range got {
		// Windows filepath.Join 输出反斜杠，字面量断言可直比
		set[p] = true
	}
	for _, w := range want {
		if !set[w] {
			return false
		}
	}
	return true
}
