package manifest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xmdemt/xdev/internal/util"
)

// 回归（E-01）：所有 Android SDK 组件的 ANDROID_HOME 必须指向同一个
// SDK 根，即 `{{install}}\..\..`（= install/android-sdk）。
//
// 背景：六个组件曾各写各的，其中 android-ndk / android-platform-tools /
// android-platform 写成 `{{install}}\..`，实际解析到
// install/android-sdk/<tool>（组件自己的目录），而不是 SDK 根，
// 导致 Gradle Android 插件与 sdkmanager 找不到同级组件。
func TestAndroidComponentsShareSDKRoot(t *testing.T) {
	const wantExpr = `{{install}}\..\..`

	components := []string{
		"android-ndk",
		"android-platform-tools",
		"android-platform",
		"android-cmdline-tools",
		"android-emulator",
		"android-build-tools",
	}

	cat := loadRepoCatalog(t)
	for _, name := range components {
		tool, ok := cat.Tool(name)
		if !ok {
			t.Errorf("catalog 中缺少 %s", name)
			continue
		}
		if tool.InstallRoot != "android-sdk" {
			t.Errorf("%s: install_root = %q, want \"android-sdk\"", name, tool.InstallRoot)
		}
		got, ok := tool.EnvVars["ANDROID_HOME"]
		if !ok {
			t.Errorf("%s: 缺少 ANDROID_HOME", name)
			continue
		}
		if got != wantExpr {
			t.Errorf("%s: ANDROID_HOME = %q, want %q（必须是 SDK 根，不是组件目录）",
				name, got, wantExpr)
		}
	}
}

// 回归（E-04）：Java 必须同时声明 JAVA_HOME 与 JDK_HOME。
func TestJavaDeclaresJDKHome(t *testing.T) {
	cat := loadRepoCatalog(t)
	tool, ok := cat.Tool("java")
	if !ok {
		t.Fatal("catalog 中缺少 java")
	}
	for _, name := range []string{"JAVA_HOME", "JDK_HOME"} {
		if _, ok := tool.EnvVars[name]; !ok {
			t.Errorf("java 缺少 %s", name)
		}
	}
}

// 回归（E-07）：Flutter 必须声明官方变量 FLUTTER_ROOT。
func TestFlutterDeclaresFlutterRoot(t *testing.T) {
	cat := loadRepoCatalog(t)
	tool, ok := cat.Tool("flutter")
	if !ok {
		t.Fatal("catalog 中缺少 flutter")
	}
	if _, ok := tool.EnvVars["FLUTTER_ROOT"]; !ok {
		t.Error("flutter 缺少官方变量 FLUTTER_ROOT")
	}
}

// 回归（E-06）：白名单中的变量都应有 manifest 实际使用，避免授权面漂移。
// 保留清单见 allowedUnused：这些是标准变量，GUI 允许用户手动设置。
//
// 注：本断言在 manifest 包内，白名单在 util 包，因此需要显式 import。
func TestNoOrphanWhitelistEntries(t *testing.T) {
	// 标准变量：xdev 不自动写，但允许用户在 machine 层手动设置。
	allowedUnused := map[string]bool{
		"GOROOT":           true, // Go 有意不设（见 go.yaml 注释），但保留手动能力
		"PYTHONHOME":       true, // python-build-standalone 的 ._pth 会忽略它
		"ANDROID_SDK_ROOT": true, // ANDROID_HOME 的官方同义变量
	}

	cat := loadRepoCatalog(t)
	used := map[string]bool{}
	for _, tool := range cat.Tools() {
		for k := range tool.EnvVars {
			used[k] = true
		}
	}

	for name := range util.AllowedEnvVarNames {
		if used[name] || allowedUnused[name] {
			continue
		}
		t.Errorf("白名单变量 %q 无任何 manifest 使用，也不在 allowedUnused 中 —— "+
			"授权面大于实际需要，请删除或加入 allowedUnused 并说明理由", name)
	}
}

// loadRepoCatalog 从测试目录回溯到仓库根的 manifests/ 并加载 catalog。
func loadRepoCatalog(t *testing.T) *Catalog {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		candidate := filepath.Join(dir, "manifests")
		if fi, err := os.Stat(candidate); err == nil && fi.IsDir() {
			cat, err := LoadCatalog(candidate)
			if err != nil {
				t.Fatalf("LoadCatalog(%s): %v", candidate, err)
			}
			return cat
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("找不到 manifests 目录")
	return nil
}
