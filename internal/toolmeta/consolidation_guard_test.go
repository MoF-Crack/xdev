package toolmeta

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// guardPatterns 是在 internal/env 与 internal/gui 中必须灭绝的重复实现模式。
// 模式选取避开注释（注释只提函数名或 toolmeta.xxx，不匹配完整签名/字面量）。
var guardPatterns = []struct {
	name string
	re   *regexp.Regexp
}{
	{"versionFlags map literal", regexp.MustCompile(`versionFlags\s*:=`)},                         // R2
	{"scanRules var", regexp.MustCompile(`var\s+scanRules`)},                                      // R1
	{"binaryNames map literal", regexp.MustCompile(`binaryNames\s*:=`)},                           // R1
	{"getBinNamesForTool fn", regexp.MustCompile(`getBinNamesForTool\s*\(`)},                      // R1
	{"isInsideXdevDir fn", regexp.MustCompile(`isInsideXdevDir\s*\(`)},                            // R5
	{"func extractVersionFromOutput", regexp.MustCompile(`func\s+extractVersionFromOutput\s*\(`)}, // R3
	{"func getCommonInstallPaths", regexp.MustCompile(`func\s+getCommonInstallPaths\s*\(`)},       // R4
	{"func extractVersion", regexp.MustCompile(`func\s+extractVersion\s*\(`)},                     // R3
	{"func isVersionLike", regexp.MustCompile(`func\s+isVersionLike\s*\(`)},                       // R3
	{"func stripWindowsSuffix", regexp.MustCompile(`func\s+stripWindowsSuffix\s*\(`)},             // R3
}

// collectGoFiles 递归收集目录下所有 .go 文件（含测试）。
func collectGoFiles(t *testing.T, dir string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".go") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return files
}

// TC-GRD1/2/3/4 静态守卫：env 与 gui 中重复实现灭绝（Spec 033 验收 1-5、10）
func TestConsolidationGuard_EnvAndGui(t *testing.T) {
	for _, dir := range []string{filepath.Join("..", "env"), filepath.Join("..", "gui")} {
		for _, file := range collectGoFiles(t, dir) {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("read %s: %v", file, err)
			}
			src := string(data)
			for _, g := range guardPatterns {
				if g.re.MatchString(src) {
					t.Errorf("%s: pattern %q still present", file, g.name)
				}
			}
		}
	}
}

// TC-GRD3b 扫描侧 isInsideXdevDir 已委托 hostscan.InsideDir（≥3 个调用点）
//
// 2026-10-25：扫描/迁移代码已从 internal/env 搬到 internal/localmigrate
// （env 只保留环境变量与 shell RC 职责）。守卫的**意图**不变：
// 三条扫描路径（scanByEnvVars / scanByPath / scanByCommonDirs）都必须
// 走 hostscan.InsideDir，不得各自手写路径包含判定。只把目录指向新位置。
func TestConsolidationGuard_EnvDelegatesInsideDir(t *testing.T) {
	files := collectGoFiles(t, filepath.Join("..", "localmigrate"))
	re := regexp.MustCompile(`hostscan\.InsideDir\s*\(`)
	count := 0
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		count += len(re.FindAllString(string(data), -1))
	}
	if count < 3 {
		t.Errorf("hostscan.InsideDir call sites in internal/localmigrate = %d, want >= 3 (scanByEnvVars/scanByPath/scanByCommonDirs)", count)
	}
}

// TC-GRD4b toolmeta 内各收口函数定义恰好一次
func TestConsolidationGuard_ToolmetaSingletons(t *testing.T) {
	files := collectGoFiles(t, ".")
	singletons := []*regexp.Regexp{
		regexp.MustCompile(`func\s+ExtractVersion\s*\(`),
		regexp.MustCompile(`func\s+ExtractVersionFromOutput\s*\(`),
		regexp.MustCompile(`func\s+CommonInstallPaths\s*\(`),
		regexp.MustCompile(`func\s+CheckEnvVars\s*\(`),
		regexp.MustCompile(`func\s+IsVersionLike\s*\(`),
		regexp.MustCompile(`func\s+StripWindowsSuffix\s*\(`),
		regexp.MustCompile(`var\s+entries\s*=`),
	}
	for _, re := range singletons {
		count := 0
		for _, file := range files {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			count += len(re.FindAllString(string(data), -1))
		}
		if count != 1 {
			t.Errorf("toolmeta singleton %q defined %d times, want exactly 1", re.String(), count)
		}
	}
}
