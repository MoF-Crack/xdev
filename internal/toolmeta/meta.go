// Package toolmeta 是工具检测元数据的单一权威来源（Spec 033）。
//
// 消除 5 类重复：工具→二进制名映射（R1）、--version 标志表（R2）、
// 版本提取（R3）、常见安装路径（R4）、"在 xdev 目录内"判定（R5）。
// env 与 gui 的本地检测路径统一查这张表。
package toolmeta

// ToolMeta 是单个工具的检测元数据。所有本地检测路径统一查这份表。
type ToolMeta struct {
	Name       string   // 工具名："go", "java", ...
	BinNames   []string // 可执行文件名（不含扩展名）："go", {"java","javac"}
	EnvVarKeys []string // 关联环境变量："GOROOT", {"JAVA_HOME"}
	VersionArg []string // 版本参数：{"version"} for go, {"--version"} for node
}

// entry 是表构造时的中间类型：Meta 为条目本体，scan 标记是否参与 ScanLocalTools。
type entry struct {
	meta ToolMeta
	scan bool
}

// entries 是完整表：主工具条目 + 查询别名条目（mvn/rustc/python3 等）。
// 取三份来源的并集：
//   - env/scan_local.go scanRules（23 主工具）
//   - gui/app_local_detect.go getBinNamesForTool（14 case）
//   - gui/app_local_detect.go findBinaryInDir 内联 binaryNames（33 键）
//   - gui/app_local_detect.go getVersion 内联 versionFlags（36 键，含纯别名键）
//   - gui/app_local_detect.go checkEnvVars 内联 envMap（20 键，含 android-* 子工具）
//
// VersionArg 取 gui 版（更全）；gradle/maven 类统一为 -version（gradle 接受两者，
// gui 已用 -version 且测试通过）。
var entries = []entry{
	// ---- 主工具（参与 ScanLocalTools / 本地探测） ----
	{scan: true, meta: ToolMeta{Name: "java", BinNames: []string{"java", "javac"}, EnvVarKeys: []string{"JAVA_HOME"}, VersionArg: []string{"--version"}}},
	{scan: true, meta: ToolMeta{Name: "node", BinNames: []string{"node", "npm"}, EnvVarKeys: []string{"NODE_PATH", "NVM_HOME"}, VersionArg: []string{"--version"}}},
	{scan: true, meta: ToolMeta{Name: "python", BinNames: []string{"python", "python3"}, EnvVarKeys: []string{"PYTHON_HOME", "PYTHONPATH", "CONDA_PREFIX"}, VersionArg: []string{"--version"}}},
	{scan: true, meta: ToolMeta{Name: "go", BinNames: []string{"go"}, EnvVarKeys: []string{"GOROOT", "GOPATH"}, VersionArg: []string{"version"}}},
	{scan: true, meta: ToolMeta{Name: "dotnet", BinNames: []string{"dotnet"}, EnvVarKeys: []string{"DOTNET_ROOT"}, VersionArg: []string{"--version"}}},
	{scan: true, meta: ToolMeta{Name: "rust", BinNames: []string{"rustc", "cargo"}, EnvVarKeys: []string{"RUSTUP_HOME", "CARGO_HOME"}, VersionArg: []string{"--version"}}},
	{scan: true, meta: ToolMeta{Name: "flutter", BinNames: []string{"flutter"}, EnvVarKeys: []string{"FLUTTER_ROOT"}, VersionArg: []string{"--version"}}},
	{scan: true, meta: ToolMeta{Name: "dart", BinNames: []string{"dart"}, EnvVarKeys: []string{"DART_SDK"}, VersionArg: []string{"--version"}}},
	{scan: true, meta: ToolMeta{Name: "gradle", BinNames: []string{"gradle"}, EnvVarKeys: []string{"GRADLE_HOME"}, VersionArg: []string{"-version"}}},
	{scan: true, meta: ToolMeta{Name: "maven", BinNames: []string{"mvn"}, EnvVarKeys: []string{"MAVEN_HOME", "M2_HOME"}, VersionArg: []string{"-version"}}},
	{scan: true, meta: ToolMeta{Name: "cmake", BinNames: []string{"cmake"}, EnvVarKeys: []string{"CMAKE_ROOT"}, VersionArg: []string{"--version"}}},
	{scan: true, meta: ToolMeta{Name: "llvm", BinNames: []string{"clang"}, EnvVarKeys: []string{"LLVM_HOME"}, VersionArg: []string{"--version"}}},
	{scan: true, meta: ToolMeta{Name: "ruby", BinNames: []string{"ruby"}, EnvVarKeys: []string{}, VersionArg: []string{"--version"}}},
	{scan: true, meta: ToolMeta{Name: "php", BinNames: []string{"php"}, EnvVarKeys: []string{}, VersionArg: []string{"--version"}}},
	{scan: true, meta: ToolMeta{Name: "perl", BinNames: []string{"perl"}, EnvVarKeys: []string{}, VersionArg: []string{"--version"}}},
	{scan: true, meta: ToolMeta{Name: "git", BinNames: []string{"git"}, EnvVarKeys: []string{"GIT_HOME"}, VersionArg: []string{"--version"}}},
	{scan: true, meta: ToolMeta{Name: "deno", BinNames: []string{"deno"}, EnvVarKeys: []string{}, VersionArg: []string{"--version"}}},
	{scan: true, meta: ToolMeta{Name: "bun", BinNames: []string{"bun"}, EnvVarKeys: []string{}, VersionArg: []string{"--version"}}},
	{scan: true, meta: ToolMeta{Name: "zig", BinNames: []string{"zig"}, EnvVarKeys: []string{}, VersionArg: []string{"version"}}},
	{scan: true, meta: ToolMeta{Name: "android-platform-tools", BinNames: []string{"adb"}, EnvVarKeys: []string{"ANDROID_HOME", "ANDROID_SDK_ROOT"}, VersionArg: []string{"version"}}},
	{scan: true, meta: ToolMeta{Name: "android-ndk", BinNames: []string{"ndk-build"}, EnvVarKeys: []string{"ANDROID_NDK_HOME", "NDK_HOME"}, VersionArg: []string{"--version"}}},
	{scan: true, meta: ToolMeta{Name: "yarn", BinNames: []string{"yarn"}, EnvVarKeys: []string{}, VersionArg: []string{"--version"}}},
	{scan: true, meta: ToolMeta{Name: "pnpm", BinNames: []string{"pnpm"}, EnvVarKeys: []string{}, VersionArg: []string{"--version"}}},

	// gui findBinaryInDir binaryNames 独有键（迁移/探测本地路径用）
	{scan: true, meta: ToolMeta{Name: "typescript", BinNames: []string{"tsc"}, EnvVarKeys: []string{"TYPESCRIPT_HOME"}, VersionArg: []string{"--version"}}},
	{scan: true, meta: ToolMeta{Name: "lua", BinNames: []string{"lua"}, EnvVarKeys: []string{}, VersionArg: []string{"-v"}}},
	{scan: true, meta: ToolMeta{Name: "make", BinNames: []string{"make"}, EnvVarKeys: []string{}, VersionArg: []string{"--version"}}},
	{scan: true, meta: ToolMeta{Name: "ninja", BinNames: []string{"ninja"}, EnvVarKeys: []string{}, VersionArg: []string{"--version"}}},
	{scan: true, meta: ToolMeta{Name: "mingw", BinNames: []string{"gcc", "g++"}, EnvVarKeys: []string{}, VersionArg: []string{"--version"}}},
	{scan: true, meta: ToolMeta{Name: "erlang", BinNames: []string{"erl"}, EnvVarKeys: []string{}, VersionArg: []string{"-version"}}},
	{scan: true, meta: ToolMeta{Name: "haskell", BinNames: []string{"ghc"}, EnvVarKeys: []string{}, VersionArg: []string{"--version"}}},
	{scan: true, meta: ToolMeta{Name: "julia", BinNames: []string{"julia"}, EnvVarKeys: []string{}, VersionArg: []string{"--version"}}},
	{scan: true, meta: ToolMeta{Name: "kotlin", BinNames: []string{"kotlinc"}, EnvVarKeys: []string{}, VersionArg: []string{"--version"}}},
	{scan: true, meta: ToolMeta{Name: "scala", BinNames: []string{"scala"}, EnvVarKeys: []string{}, VersionArg: []string{"--version"}}},
	{scan: true, meta: ToolMeta{Name: "sbt", BinNames: []string{"sbt"}, EnvVarKeys: []string{}, VersionArg: []string{"--version"}}},
	{scan: true, meta: ToolMeta{Name: "ant", BinNames: []string{"ant"}, EnvVarKeys: []string{}, VersionArg: []string{"-version"}}},
	{scan: true, meta: ToolMeta{Name: "adb", BinNames: []string{"adb"}, EnvVarKeys: []string{}, VersionArg: []string{"version"}}},
	{scan: true, meta: ToolMeta{Name: "uv", BinNames: []string{"uv"}, EnvVarKeys: []string{}, VersionArg: []string{"--version"}}},

	// gui checkEnvVars envMap 独有键（android-* 子工具）
	{scan: true, meta: ToolMeta{Name: "android-build-tools", BinNames: []string{"android-build-tools"}, EnvVarKeys: []string{"ANDROID_HOME", "ANDROID_SDK_ROOT"}, VersionArg: []string{"--version"}}},
	{scan: true, meta: ToolMeta{Name: "android-platform", BinNames: []string{"android-platform"}, EnvVarKeys: []string{"ANDROID_HOME", "ANDROID_SDK_ROOT"}, VersionArg: []string{"--version"}}},
	{scan: true, meta: ToolMeta{Name: "android-cmdline-tools", BinNames: []string{"android-cmdline-tools"}, EnvVarKeys: []string{"ANDROID_HOME", "ANDROID_SDK_ROOT"}, VersionArg: []string{"--version"}}},
	{scan: true, meta: ToolMeta{Name: "android-emulator", BinNames: []string{"android-emulator"}, EnvVarKeys: []string{"ANDROID_HOME", "ANDROID_SDK_ROOT"}, VersionArg: []string{"--version"}}},

	// ---- 查询别名条目（仅站内查询用，不参与扫描） ----
	// gui getVersion versionFlags 以二进制名/别名作键（env 以 binNames[0] 查、gui 以 name 查）
	{meta: ToolMeta{Name: "python3", BinNames: []string{"python3"}, EnvVarKeys: []string{}, VersionArg: []string{"--version"}}},
	{meta: ToolMeta{Name: "rustc", BinNames: []string{"rustc"}, EnvVarKeys: []string{}, VersionArg: []string{"--version"}}},
	{meta: ToolMeta{Name: "cargo", BinNames: []string{"cargo"}, EnvVarKeys: []string{}, VersionArg: []string{"--version"}}},
	{meta: ToolMeta{Name: "tsc", BinNames: []string{"tsc"}, EnvVarKeys: []string{}, VersionArg: []string{"--version"}}},
	{meta: ToolMeta{Name: "gcc", BinNames: []string{"gcc"}, EnvVarKeys: []string{}, VersionArg: []string{"--version"}}},
	{meta: ToolMeta{Name: "clang", BinNames: []string{"clang"}, EnvVarKeys: []string{}, VersionArg: []string{"--version"}}},
	{meta: ToolMeta{Name: "cjc", BinNames: []string{"cjc"}, EnvVarKeys: []string{}, VersionArg: []string{"--version"}}},
	{meta: ToolMeta{Name: "ghc", BinNames: []string{"ghc"}, EnvVarKeys: []string{}, VersionArg: []string{"--version"}}},
	{meta: ToolMeta{Name: "erl", BinNames: []string{"erl"}, EnvVarKeys: []string{}, VersionArg: []string{"-version"}}},
	{meta: ToolMeta{Name: "mvn", BinNames: []string{"mvn"}, EnvVarKeys: []string{}, VersionArg: []string{"-version"}}},
}

// Lookup 按工具名查元数据。未知工具返回 false。
func Lookup(name string) (ToolMeta, bool) {
	for _, e := range entries {
		if e.meta.Name == name {
			return e.meta, true
		}
	}
	return ToolMeta{}, false
}

// BinNames 返回工具的可执行文件名（不含扩展名）。未知工具回退为 {name}。
func BinNames(name string) []string {
	if m, ok := Lookup(name); ok {
		return m.BinNames
	}
	return []string{name}
}

// EnvVarKeys 返回工具关联的环境变量键。未知工具返回 nil。
func EnvVarKeys(name string) []string {
	if m, ok := Lookup(name); ok {
		return m.EnvVarKeys
	}
	return nil
}

// VersionArg 返回版本探测参数。未知工具默认 {"--version"}。
func VersionArg(name string) []string {
	if m, ok := Lookup(name); ok {
		return m.VersionArg
	}
	return []string{"--version"}
}

// Table 返回全部条目（主工具 + 查询别名），供守卫/完整性核对使用。
func Table() []ToolMeta {
	all := make([]ToolMeta, len(entries))
	for i, e := range entries {
		all[i] = e.meta
	}
	return all
}

// ScanTargets 返回参与本地扫描的主工具条目（不包含查询别名）。
// env.ScanLocalTools 遍历此表；覆盖是原 scanRules 的超集（正确性提升）。
func ScanTargets() []ToolMeta {
	var out []ToolMeta
	for _, e := range entries {
		if e.scan {
			out = append(out, e.meta)
		}
	}
	return out
}