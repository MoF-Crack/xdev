package toolmeta

import (
	"reflect"
	"testing"
)

// sourceKeys 是 Spec 033 R1+R2 五份来源的工具名键集（实施前盘点产物）。
// 任何来源新增/删除工具名，此清单需同步并更新断言数量。
var sourceKeys = []string{
	// env/scan_local.go scanRules（23）
	"java", "node", "python", "go", "dotnet", "rust", "flutter", "dart",
	"gradle", "maven", "cmake", "llvm", "ruby", "php", "perl", "git",
	"deno", "bun", "zig", "android-platform-tools", "android-ndk", "yarn", "pnpm",
	// gui getBinNamesForTool 独有 / findBinaryInDir 独有（14 新增）
	"typescript", "lua", "make", "ninja", "mingw", "erlang", "haskell",
	"julia", "kotlin", "scala", "sbt", "ant", "adb", "uv",
	// gui checkEnvVars envMap 独有（4）
	"android-build-tools", "android-platform", "android-cmdline-tools", "android-emulator",
}

// versionAliasKeys 是 gui versionFlags 中的纯别名键（不参与扫描，仅查询）。
var versionAliasKeys = []string{
	"python3", "rustc", "cargo", "tsc", "gcc", "clang", "cjc", "ghc", "erl", "mvn",
}

// TC-M1 表键集完整（并集）：五份来源全部工具名 Lookup 命中
func TestTableCoversAllSourceKeys(t *testing.T) {
	for _, name := range sourceKeys {
		if _, ok := Lookup(name); !ok {
			t.Errorf("Lookup(%q) = false, want true (source key missing from table)", name)
		}
	}
	for _, name := range versionAliasKeys {
		if _, ok := Lookup(name); !ok {
			t.Errorf("Lookup(%q) = false, want true (version-alias key missing from table)", name)
		}
	}
}

// TC-M2 BinNames 等价：主工具条目与来源 binNames 一致
func TestBinNamesMatchesSources(t *testing.T) {
	cases := map[string][]string{
		"java":        {"java", "javac"},
		"node":        {"node", "npm"},
		"python":      {"python", "python3"},
		"mingw":       {"gcc", "g++"},
		"maven":       {"mvn"},
		"kotlin":      {"kotlinc"},
		"erlang":      {"erl"},
		"typescript":  {"tsc"},
		"go":          {"go"},
		"zig":         {"zig"},
		"android-ndk": {"ndk-build"},
	}
	for name, want := range cases {
		if got := BinNames(name); !reflect.DeepEqual(got, want) {
			t.Errorf("BinNames(%q) = %v, want %v", name, got, want)
		}
	}
}

// TC-M3 BinNames 未知回退 {name}
func TestBinNamesUnknownFallback(t *testing.T) {
	if got := BinNames("no-such-tool"); !reflect.DeepEqual(got, []string{"no-such-tool"}) {
		t.Errorf("BinNames(unknown) = %v, want [no-such-tool]", got)
	}
}

// TC-M4 VersionArg 特例表
func TestVersionArgSpecialCases(t *testing.T) {
	cases := map[string][]string{
		"go":      {"version"},
		"zig":     {"version"},
		"adb":     {"version"},
		"lua":     {"-v"},
		"gradle":  {"-version"},
		"maven":   {"-version"},
		"mvn":     {"-version"},
		"ant":     {"-version"},
		"erlang":  {"-version"},
		"erl":     {"-version"},
		"java":    {"--version"},
		"cmake":   {"--version"},
		"flutter": {"--version"},
	}
	for name, want := range cases {
		if got := VersionArg(name); !reflect.DeepEqual(got, want) {
			t.Errorf("VersionArg(%q) = %v, want %v", name, got, want)
		}
	}
}

// TC-M5 别名键一致：同工具不同键的 VersionArg 一致
func TestVersionArgAliasConsistent(t *testing.T) {
	if got, want := VersionArg("mvn"), VersionArg("maven"); !reflect.DeepEqual(got, want) {
		t.Errorf("mvn/maven VersionArg differ: %v vs %v", got, want)
	}
	for _, pair := range [][2]string{
		{"rust", "rustc"}, {"rust", "cargo"},
		{"python", "python3"},
		{"typescript", "tsc"},
	} {
		if got, want := VersionArg(pair[0]), VersionArg(pair[1]); !reflect.DeepEqual(got, want) {
			t.Errorf("%s/%s VersionArg differ: %v vs %v", pair[0], pair[1], got, want)
		}
	}
}

// TC-M6 EnvVarKeys 覆盖
func TestEnvVarKeys(t *testing.T) {
	cases := map[string][]string{
		"java":                   {"JAVA_HOME"},
		"go":                     {"GOROOT", "GOPATH"},
		"node":                   {"NODE_PATH", "NVM_HOME"},
		"dotnet":                 {"DOTNET_ROOT"},
		"android-platform-tools": {"ANDROID_HOME", "ANDROID_SDK_ROOT"},
		"android-ndk":            {"ANDROID_NDK_HOME", "NDK_HOME"},
		"android-build-tools":    {"ANDROID_HOME", "ANDROID_SDK_ROOT"},
		"typescript":             {"TYPESCRIPT_HOME"},
		"gradle":                 {"GRADLE_HOME"},
	}
	for name, want := range cases {
		if got := EnvVarKeys(name); !reflect.DeepEqual(got, want) {
			t.Errorf("EnvVarKeys(%q) = %v, want %v", name, got, want)
		}
	}
	if got := EnvVarKeys("ruby"); got != nil && len(got) != 0 {
		t.Errorf("EnvVarKeys(ruby) = %v, want empty", got)
	}
	if got := EnvVarKeys("unknown-key"); got != nil {
		t.Errorf("EnvVarKeys(unknown) = %v, want nil", got)
	}
}

// TC-M7 表不变量：Name 唯一、BinNames/VersionArg 非空、scan 目标无重复
func TestTableInvariants(t *testing.T) {
	seen := map[string]bool{}
	for _, m := range Table() {
		if m.Name == "" {
			t.Fatal("entry with empty Name in table")
		}
		if seen[m.Name] {
			t.Errorf("duplicate table entry: %q", m.Name)
		}
		seen[m.Name] = true
		if len(m.BinNames) == 0 {
			t.Errorf("entry %q has empty BinNames", m.Name)
		}
		if len(m.VersionArg) == 0 {
			t.Errorf("entry %q has empty VersionArg", m.Name)
		}
	}
	// ScanTargets 不允许重复
	scanned := map[string]bool{}
	for _, m := range ScanTargets() {
		if scanned[m.Name] {
			t.Errorf("ScanTargets duplicate: %q", m.Name)
		}
		scanned[m.Name] = true
	}
}

// TC-M8 ScanTargets 覆盖 = 41 主工具（并集 ≥ 原 23，正确性提升）
func TestScanTargetsCoverage(t *testing.T) {
	targets := ScanTargets()
	if len(targets) != 41 {
		t.Errorf("ScanTargets() = %d entries, want 41", len(targets))
	}
	byName := map[string]bool{}
	for _, m := range targets {
		byName[m.Name] = true
	}
	for _, name := range sourceKeys {
		if !byName[name] {
			t.Errorf("ScanTargets missing tool %q", name)
		}
	}
	for _, name := range versionAliasKeys {
		if byName[name] {
			t.Errorf("ScanTargets must NOT include alias key %q", name)
		}
	}
}
