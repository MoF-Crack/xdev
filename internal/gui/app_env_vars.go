package gui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/xmdemt/xdev/internal/env"
	"github.com/xmdemt/xdev/internal/util"
)

// ─────────────────────────────────────────────────────────────────────────
// B-08（票 30 / P1-21）：%VAR% 引用展开
//
// 根因：Go 的 os.ExpandEnv 只认 $VAR，Windows 注册表里最常见的 %JAVA_HOME%\bin
// 从不被展开 —— 字面量送 os.Stat 必然失败，于是 CleanEnvVarPath 把**合法条目
// 一律误删**（ValidateEnvVar / buildPathEntries 同病，前端因此把合法 PATH 显示
// 成 invalid）。
//
// 方案（票面指定）：
//   1. 手写正则 %VAR% → 查「合并环境」（user 注册表 + machine 注册表 + 进程
//      env，键大小写不敏感）→ 命中替换；未命中**保留原样**并由调用处判无效。
//   2. 展开次序：先 %VAR%，再 os.ExpandEnv（$VAR 既有语义），最后 os.Stat。
//   3. 批量清理共享一份合并环境快照（否则 N 条目 × 全量枚举 = N² 开销）。
//   4. **不采用**「定制 getenv 回调喂给 os.Expand」的 v1 原稿：那会让所有
//      %-引用都落到保守跳过分支，清理功能退化为 no-op（守卫 30-1 专门防此回归）。
// ─────────────────────────────────────────────────────────────────────────

// percentVarRe 匹配 Windows 风格的 %VAR% 引用（变量名形态与注册表一致）。
var percentVarRe = regexp.MustCompile(`%([A-Za-z_][A-Za-z0-9_]*)%`)

// envLookup 是大小写不敏感的环境变量查询（命中返回值与 true）。
type envLookup func(name string) (string, bool)

// 包级可替换 seam：环境读取必须在测试中可替换。
//
// 理由有二：internal/gui 的测试受主机污染守卫约束（不得写真实注册表/用户
// 环境），而票 30 的缓存断言（30-2「批量 N 条目只枚举一次」）需要在生产路径
// 上计数。默认值全部是生产实现，测试注入内存实现/计数器后恢复。
var (
	// mergedEnvSnapshotFn 一次性枚举合并环境（进程 env + machine/user 注册表）。
	mergedEnvSnapshotFn = defaultMergedEnvSnapshot
	// enumerateEnvVarsFn 列出某个 level 的环境变量（Windows=注册表枚举；
	// 其他平台=进程 env，level 不参与）。
	enumerateEnvVarsFn = defaultEnumerateEnvVars
	// 单个环境的读/写/删操作（票 30 的清理链路：CleanEnvVarPath /
	// CleanAllInvalidEnvVars 只用这三个入口读写注册表；其余 GUI 绑定
	//（SetEnvVar / DeleteEnvVar / DeleteEnvVarEntry）不经此 seam）。
	envGetEnvVarFn    = env.GetEnvVar
	envSetEnvVarFn    = env.SetEnvVar
	envDeleteEnvVarFn = env.DeleteEnvVar
)

// defaultMergedEnvSnapshot 枚举「合并环境」并归一为大写键。
//
// 优先级（后者覆盖前者）：进程 env → machine 注册表 → user 注册表。
// 注册表优先于进程 env 的原因：进程 env 是本进程启动时的快照，xdev 刚写入
// 注册表的值（SetEnvVar / 安装工具写 *_HOME）还不在其中，用陈旧的进程值判定
// 路径存在性会把刚配好的条目判成无效；同名变量在 Windows 上本就以 user 层
// 生效，所以 user 覆盖 machine。
func defaultMergedEnvSnapshot() map[string]string {
	merged := make(map[string]string, 64)
	add := func(name, value string) {
		if name == "" {
			return
		}
		merged[strings.ToUpper(name)] = value
	}
	for _, pair := range os.Environ() {
		if idx := strings.Index(pair, "="); idx > 0 {
			add(pair[:idx], pair[idx+1:])
		}
	}
	if runtime.GOOS == "windows" {
		for _, level := range []string{"machine", "user"} {
			vars, err := env.EnumEnvVars(level)
			if err != nil {
				logDebug("[mergedEnv] enumerating %s env vars failed: %v", level, err)
				continue
			}
			for _, p := range vars {
				add(p.Name, p.Value)
			}
		}
	}
	return merged
}

// defaultEnumerateEnvVars 是 enumerateEnvVarsFn 的生产实现：保持既有行为
// （Windows 列注册表；其他平台列进程 env）。
func defaultEnumerateEnvVars(level string) ([]env.EnvPair, error) {
	if runtime.GOOS != "windows" {
		var out []env.EnvPair
		for _, pair := range os.Environ() {
			idx := strings.Index(pair, "=")
			if idx <= 0 {
				continue
			}
			out = append(out, env.EnvPair{Name: pair[:idx], Value: pair[idx+1:]})
		}
		return out, nil
	}
	return env.EnumEnvVars(level)
}

// percentExpander 在一批条目之间复用同一份合并环境快照（票 30 第 3 条）。
//
// CleanAllInvalidEnvVars 要判定 N 个环境变量，每个变量又可能含 M 个路径条目；
// 若每次 %VAR% 查询都重新枚举注册表，代价是 N×M×全量枚举。percentExpander 把
// 「一次批量 = 一次枚举」变成结构性保证（守卫 30-2 用注入计数器断言 == 1）。
//
// 零值可用（首次查询时惰性加载）。**非并发安全**：每个批量入口各建一个，
// 只在该次调用的单 goroutine 内使用。
type percentExpander struct {
	loaded bool
	merged map[string]string
}

func newPercentExpander() *percentExpander { return &percentExpander{} }

// lookup 以 envLookup 形态提供查询。
func (e *percentExpander) lookup(name string) (string, bool) {
	if !e.loaded {
		e.merged = mergedEnvSnapshotFn()
		e.loaded = true
	}
	v, ok := e.merged[strings.ToUpper(name)]
	return v, ok
}

// expandPercentVars 展开 s 中的 %VAR% 引用（B-08 第 1 条）。
//
// 命中合并环境 → 替换为该值；未命中 → **保留原样**（调用处以
// percentVarsUnresolved 判无效；不要在这里删除引用，否则错误信息会丢失
// 变量名，用户无从排查）。
func expandPercentVars(s string, lookup envLookup) string {
	if lookup == nil || !strings.Contains(s, "%") {
		return s
	}
	return percentVarRe.ReplaceAllStringFunc(s, func(m string) string {
		name := m[1 : len(m)-1]
		if v, ok := lookup(name); ok {
			return v
		}
		return m
	})
}

// percentVarsUnresolved 报告字符串里是否仍残留 %VAR% 形态的引用 —— 即合并
// 环境查不到的引用。这类条目不能再交给 os.Stat（必然失败，且失败原因被
// 误报成「路径不存在」），直接判无效。
func percentVarsUnresolved(s string) bool { return percentVarRe.MatchString(s) }

// looksLikePathValue 报告一个值是否应当按「路径/路径列表」校验。
//
// 既有的启发式（盘符 "X:\"、正斜杠、"\" 或 "/" 开头）在 B-08 场景下会漏判：
// 注册表里写的是 %JAVA_HOME%\bin —— 展开**之前**没有盘符、只有反斜杠，
// 旧代码因此把它当作「非路径值」（前端显示 not-applicable、清理侧靠
// os.Stat 的必然失败把它删掉）。因此这里补一条：残留未解析的 %VAR% 引用
// 且值内含路径分隔符时，同样按路径校验（票 30 第 1 条「调用处标记为无效」）。
func looksLikePathValue(s string) bool {
	if strings.Contains(s, ":\\") || strings.Contains(s, ":/") ||
		strings.HasPrefix(s, "\\") || strings.HasPrefix(s, "/") ||
		strings.Contains(s, "/") {
		return true
	}
	return percentVarsUnresolved(s) && strings.Contains(s, "\\")
}

// expandPathRefs 完成一个路径条目的引用展开：先 %VAR%（B-08），再 $VAR
// （os.ExpandEnv 既有语义）。
func expandPathRefs(s string, lookup envLookup) string {
	return os.ExpandEnv(expandPercentVars(s, lookup))
}

// pathEntryExists 判定单个路径条目是否指向存在的路径（展开 + Stat）。
// 第二个返回值是展开后的路径，供日志/前端展示使用。
func pathEntryExists(p string, lookup envLookup) (bool, string) {
	percentExpanded := expandPercentVars(p, lookup)
	if percentVarsUnresolved(percentExpanded) {
		// 合并环境里没有这个变量：保守判无效（清理不因此退化为 no-op，
		// 也不会因为「字面量 %VAR% 目录恰好存在」而误判为有效）。
		return false, percentExpanded
	}
	expanded := os.ExpandEnv(percentExpanded)
	_, err := os.Stat(expanded)
	return err == nil, expanded
}

type EnvVarInfo struct {
	Name             string     `json:"name"`
	Value            string     `json:"value"`
	Source           string     `json:"source"`
	Level            string     `json:"level"`
	ToolName         string     `json:"toolName"`
	IsValid          bool       `json:"isValid"`
	InvalidReason    string     `json:"invalidReason"`
	ValidationStatus string     `json:"validationStatus"`
	PathEntries      []PathEntry `json:"pathEntries,omitempty"`
}

// PathEntry represents a single semicolon-delimited path within an env var
// value, with its individual validity status. Used by the frontend env var
// editor to show per-path validity without parsing the human-readable
// invalidReason string.
type PathEntry struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
}

func validationStatus(value string, valid bool, reason string) string {
	// B-08：改用共享的 looksLikePathValue —— 含 %VAR% 引用的条目也要给出
	// 校验状态，而不是被当作「非路径值」显示成 not-applicable。
	if !looksLikePathValue(value) {
		return "not-applicable"
	}
	if valid {
		return "valid"
	}
	if strings.Contains(value, ";") && strings.Contains(reason, "/") {
		return "partial"
	}
	return "invalid"
}

func (a *App) GetEnvVars() []EnvVarInfo {
	var result []EnvVarInfo

	// B-08：一次调用共享一份合并环境快照（下面的每个变量都要做 %VAR% 展开，
	// 逐次枚举注册表会让这个只读列表接口变成 N×全量枚举）。
	exp := newPercentExpander()

	devEnvVars := []string{
		"JAVA_HOME", "GOROOT", "GOPATH", "PYTHON_HOME", "PYTHONPATH",
		"NODE_PATH", "DOTNET_ROOT", "RUSTUP_HOME", "CARGO_HOME",
		"FLUTTER_ROOT", "DART_SDK", "GRADLE_HOME", "MAVEN_HOME", "M2_HOME",
		"ANDROID_HOME", "ANDROID_SDK_ROOT", "ANDROID_NDK_HOME",
		"CMAKE_ROOT", "LLVM_HOME", "NVM_HOME", "CONDA_PREFIX",
	}

	envVarToTool := map[string]string{
		"JAVA_HOME": "java", "GOROOT": "go", "GOPATH": "go",
		"PYTHON_HOME": "python", "PYTHONPATH": "python",
		"NODE_PATH": "node", "DOTNET_ROOT": "dotnet",
		"RUSTUP_HOME": "rust", "CARGO_HOME": "rust",
		"FLUTTER_ROOT": "flutter", "DART_SDK": "dart",
		"GRADLE_HOME": "gradle", "MAVEN_HOME": "maven", "M2_HOME": "maven",
		"ANDROID_HOME": "android-platform-tools", "ANDROID_SDK_ROOT": "android-platform-tools", "ANDROID_NDK_HOME": "android-ndk",
		"CMAKE_ROOT": "cmake", "LLVM_HOME": "llvm", "NVM_HOME": "node", "CONDA_PREFIX": "python",
	}

	if runtime.GOOS == "windows" {
		for _, name := range devEnvVars {
			val, err := env.GetEnvVar(name, "user")
			if err != nil {
				logDebug("[GetEnvVars] reading user env var %s failed: %v", name, err)
				continue
			}
			if val != "" {
				toolName := envVarToTool[name]
				isValid, reason := validateEnvVarWith(name, val, exp.lookup)
				result = append(result, EnvVarInfo{
					Name: name, Value: val, Source: "user", Level: "user",
					ToolName: toolName, IsValid: isValid, InvalidReason: reason, ValidationStatus: validationStatus(val, isValid, reason),
					PathEntries: buildPathEntriesWith(val, exp.lookup),
				})
			}
		}
	}

	if runtime.GOOS == "windows" {
		for _, name := range devEnvVars {
			val, err := env.GetEnvVar(name, "machine")
			if err != nil {
				logDebug("[GetEnvVars] reading machine env var %s failed: %v", name, err)
				continue
			}
			if val != "" {
				toolName := envVarToTool[name]
				isValid, reason := validateEnvVarWith(name, val, exp.lookup)
				result = append(result, EnvVarInfo{
					Name: name, Value: val, Source: "system", Level: "machine",
					ToolName: toolName, IsValid: isValid, InvalidReason: reason, ValidationStatus: validationStatus(val, isValid, reason),
					PathEntries: buildPathEntriesWith(val, exp.lookup),
				})
			}
		}
	}

	if runtime.GOOS != "windows" {
		for _, name := range devEnvVars {
			val := os.Getenv(name)
			if val != "" {
				toolName := envVarToTool[name]
				isValid, reason := validateEnvVarWith(name, val, exp.lookup)
				result = append(result, EnvVarInfo{
					Name: name, Value: val, Source: "system", Level: "process",
					ToolName: toolName, IsValid: isValid, InvalidReason: reason, ValidationStatus: validationStatus(val, isValid, reason),
					PathEntries: buildPathEntriesWith(val, exp.lookup),
				})
			}
		}
	}

	return result
}

// buildPathEntries splits a semicolon-delimited env var value into individual
// path entries with per-entry existence checks. Non-path values (no path-like
// characters) return nil so the frontend knows not to show a path list.
func buildPathEntries(value string) []PathEntry {
	return buildPathEntriesWith(value, newPercentExpander().lookup)
}

// buildPathEntriesWith 是 buildPathEntries 的实现体，接收共享的合并环境查询
//（B-08：批量场景下复用同一份快照；单次调用传入一次性 lookup）。
func buildPathEntriesWith(value string, lookup envLookup) []PathEntry {
	if value == "" {
		return nil
	}
	expanded := expandPathRefs(value, lookup)
	looksLikePath := looksLikePathValue(expanded)
	if !looksLikePath {
		return nil
	}
	// Split on the RAW (unexpanded) value so the frontend sees the original
	// string the user typed, not the expanded one. Existence is checked on
	// the expanded form.
	parts := strings.Split(value, ";")
	var entries []PathEntry
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// B-08：这里同样要先展开 %VAR%（旧代码只做 os.ExpandEnv，含
		// %JAVA_HOME% 的合法条目在前端被显示为「不存在」）。
		exists, _ := pathEntryExists(p, lookup)
		entries = append(entries, PathEntry{Path: p, Exists: exists})
	}
	return entries
}

// ValidateEnvVar 报告环境变量值是否为「存在的路径/路径列表」。
//
// 保留原签名（GUI 绑定与既有调用者不变）；B-08 后内部走
// validateEnvVarWith，%VAR% 与 $VAR 两种引用都会先展开再判定。
func ValidateEnvVar(name, value string) (bool, string) {
	return validateEnvVarWith(name, value, newPercentExpander().lookup)
}

// validateEnvVarWith 是 ValidateEnvVar 的实现体，接收共享的合并环境查询
//（B-08 缓存：批量判定共用一份快照）。
func validateEnvVarWith(name, value string, lookup envLookup) (bool, string) {
	if value == "" {
		return true, ""
	}

	expanded := expandPathRefs(value, lookup)

	if !looksLikePathValue(expanded) {
		return true, ""
	}

	if strings.Contains(expanded, ";") {
		parts := strings.Split(expanded, ";")
		var invalidParts []string
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			// B-08：未命中合并环境的 %VAR% 引用判为无效路径（不再靠
			// os.Stat 对字面量 %VAR% 的必然失败来「顺便」判定）。
			if exists, _ := pathEntryExists(p, lookup); !exists {
				invalidParts = append(invalidParts, p)
			}
		}
		if len(invalidParts) > 0 {
			if len(invalidParts) == len(parts)-countEmpty(parts) {
				return false, fmt.Sprintf("all %d paths do not exist", len(invalidParts))
			}
			return false, fmt.Sprintf("%d/%d paths do not exist: %s",
				len(invalidParts), len(parts)-countEmpty(parts),
				truncatePaths(invalidParts, 80))
		}
		return true, ""
	}

	if exists, _ := pathEntryExists(expanded, lookup); !exists {
		return false, fmt.Sprintf("path does not exist: %s", expanded)
	}
	return true, ""
}

func countEmpty(parts []string) int {
	count := 0
	for _, p := range parts {
		if strings.TrimSpace(p) == "" {
			count++
		}
	}
	return count
}

func truncatePaths(paths []string, maxLen int) string {
	var b strings.Builder
	for i, p := range paths {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(p)
		if b.Len() > maxLen {
			return b.String()[:maxLen] + "..."
		}
	}
	return b.String()
}

func (a *App) GetAllEnvVars(level string) []EnvVarInfo {
	return a.getAllEnvVarsWith(level, newPercentExpander())
}

// getAllEnvVarsWith 是 GetAllEnvVars 的实现体，额外接收本次批量共享的
// percentExpander（B-08 缓存：票 30 第 3 条）。
func (a *App) getAllEnvVarsWith(level string, exp *percentExpander) []EnvVarInfo {
	if runtime.GOOS != "windows" {
		// 非 Windows：level 不参与，列进程 env（保持既有行为）。
		pairs, err := enumerateEnvVarsFn("process")
		if err != nil {
			logDebug("[GetAllEnvVars] enumerating process env vars failed: %v", err)
			return nil
		}
		return buildEnvVarInfos(pairs, "system", "process", exp)
	}

	vars, err := enumerateEnvVarsFn(level)
	if err != nil {
		logDebug("[GetAllEnvVars] enumerating %s env vars failed: %v", level, err)
		return nil
	}
	return buildEnvVarInfos(vars, level, level, exp)
}

// buildEnvVarInfos 为一批 (name,value) 构造 EnvVarInfo（含有效性判定与逐条
// 路径条目）——整批复用同一个 percentExpander，所以合并环境只枚举一次。
func buildEnvVarInfos(pairs []env.EnvPair, source, level string, exp *percentExpander) []EnvVarInfo {
	var result []EnvVarInfo
	for _, pair := range pairs {
		isValid, reason := validateEnvVarWith(pair.Name, pair.Value, exp.lookup)
		result = append(result, EnvVarInfo{
			Name: pair.Name, Value: pair.Value,
			Source: source, Level: level,
			IsValid: isValid, InvalidReason: reason, ValidationStatus: validationStatus(pair.Value, isValid, reason),
			PathEntries: buildPathEntriesWith(pair.Value, exp.lookup),
		})
	}
	return result
}

func (a *App) DeleteEnvVarEntry(name, level string) error {
	if util.IsProtectedSystemEnvVar(name) {
		return fmt.Errorf("cannot delete protected system env var: %s", name)
	}
	switch level {
	case "user", "machine":
		return env.DeleteEnvVar(name, level)
	default:
		return fmt.Errorf("unsupported level: %q, only user or machine", level)
	}
}

func (a *App) CleanEnvVarPath(name, level string) error {
	return a.cleanEnvVarPath(name, level, newPercentExpander())
}

// cleanEnvVarPath 是 CleanEnvVarPath 的实现体，接收批量共享的 percentExpander
//（B-08：CleanAllInvalidEnvVars 整批只枚举一次合并环境）。
func (a *App) cleanEnvVarPath(name, level string, exp *percentExpander) error {
	if util.IsProtectedSystemEnvVar(name) && !strings.EqualFold(name, "path") {
		return fmt.Errorf("cannot modify protected system env var: %s", name)
	}
	logInfo("[CleanEnvVarPath] cleaning env var: %s", name)

	var value string
	var err error
	switch level {
	case "user", "machine":
		value, err = envGetEnvVarFn(name, level)
		if err != nil {
			return fmt.Errorf("failed to read env var: %v", err)
		}
	default:
		return fmt.Errorf("unsupported level: %q", level)
	}

	if value == "" {
		return nil
	}

	// B-08：先 %VAR%（合并环境），再 $VAR（os.ExpandEnv）。旧代码只做
	// os.ExpandEnv，%JAVA_HOME%\bin 这类 Windows 常见条目送 os.Stat 必然失败
	// → 合法条目被删。
	expanded := expandPathRefs(value, exp.lookup)

	if strings.Contains(expanded, ";") {
		parts := strings.Split(value, ";")
		var validParts []string
		removed := 0
		for _, p := range parts {
			pTrimmed := strings.TrimSpace(p)
			if pTrimmed == "" {
				continue
			}
			exists, expandedP := pathEntryExists(pTrimmed, exp.lookup)
			if !exists {
				removed++
				logInfo("[CleanEnvVarPath] removing invalid path: %s (expanded: %s)", pTrimmed, expandedP)
				continue
			}
			validParts = append(validParts, p)
		}

		if removed == 0 {
			return nil
		}

		newValue := strings.Join(validParts, ";")
		if newValue == "" {
			return envDeleteEnvVarFn(name, level)
		}

		if err := envSetEnvVarFn(name, newValue, level); err != nil {
			return fmt.Errorf("failed to update env var: %v", err)
		}
		logInfo("[CleanEnvVarPath] cleaned %d invalid paths from %s", removed, name)
		return nil
	}

	if exists, _ := pathEntryExists(value, exp.lookup); !exists {
		return envDeleteEnvVarFn(name, level)
	}

	return nil
}

// CleanAllInvalidEnvVars iterates all env vars at the given level and cleans
// every one that is invalid or partially valid (contains non-existent paths).
// Returns a summary of what was cleaned. Protected system vars are skipped.
type CleanResult struct {
	Name   string `json:"name"`
	Action string `json:"action"` // "cleaned" | "deleted" | "skipped"
	Detail string `json:"detail"`
}

func (a *App) CleanAllInvalidEnvVars(level string) []CleanResult {
	logInfo("[CleanAllInvalidEnvVars] level=%s", level)
	// B-08（票 30 第 3 条）：整批共享同一个 percentExpander —— 合并环境
	//（user+machine 注册表+进程 env）只枚举一次。若每个变量的每次 %VAR%
	// 查询都各自枚举，N 个变量就是 N×全量枚举（N² 级开销），在注册表项
	// 多、PATH 长的机器上直接表现为清理卡顿。守卫 30-2 断言枚举次数 == 1。
	exp := newPercentExpander()
	vars := a.getAllEnvVarsWith(level, exp)
	if len(vars) == 0 {
		return nil
	}
	var results []CleanResult
	for _, v := range vars {
		if util.IsProtectedSystemEnvVar(v.Name) && !strings.EqualFold(v.Name, "path") {
			results = append(results, CleanResult{Name: v.Name, Action: "skipped", Detail: "protected"})
			continue
		}
		if v.IsValid {
			continue // valid �?nothing to do
		}
		// invalid or partial �?try cleaning
		err := a.cleanEnvVarPath(v.Name, level, exp)
		if err != nil {
			if strings.Contains(err.Error(), "admin") || strings.Contains(err.Error(), "ACCESS_DENIED") || strings.Contains(err.Error(), "error 5") {
				results = append(results, CleanResult{Name: v.Name, Action: "skipped", Detail: "admin required"})
			} else {
				results = append(results, CleanResult{Name: v.Name, Action: "skipped", Detail: err.Error()})
			}
			continue
		}
		// Check if var was deleted (all paths invalid) or just cleaned (some paths removed)
		remaining, readErr := envGetEnvVarFn(v.Name, level)
		if readErr != nil || remaining == "" {
			results = append(results, CleanResult{Name: v.Name, Action: "deleted", Detail: "all paths were invalid"})
		} else {
			results = append(results, CleanResult{Name: v.Name, Action: "cleaned", Detail: "removed invalid paths"})
		}
	}
	return results
}

func (a *App) SetEnvVar(toolName, name, value, level string) error {
	if err := util.ValidateEnvVarName(name); err != nil && !strings.EqualFold(name, "path") {
		return err
	}
	if util.IsProtectedSystemEnvVar(name) && !strings.EqualFold(name, "path") {
		return fmt.Errorf("cannot modify protected system env var: %s", name)
	}
	switch level {
	case "user", "machine":
		if err := env.SetEnvVar(name, value, level); err != nil {
			return err
		}
		// Ticket 38 (P2-05): the former autoAppendBinToPath call was removed
		// together with the function. It was dead code (it looked the name up
		// in a table and returned without doing anything), and implementing it
		// would mean silently rewriting the user's PATH whenever they edit a
		// *_HOME variable — an implicit side effect the editor must not have.
		// PATH management belongs to the shim/CleanupUserPath machinery, which
		// is the single owner of those entries.
		return nil
	default:
		return fmt.Errorf("unsupported level: %q, only user or machine", level)
	}
}

func (a *App) CheckEnvVarExists(name, level string) bool {
	switch level {
	case "user", "machine":
		val, err := env.GetEnvVar(name, level)
		if err != nil {
			return false
		}
		return val != ""
	}
	return false
}

// DeleteEnvVar removes a user/machine environment variable.
func (a *App) DeleteEnvVar(name, level string) error {
	if util.IsProtectedSystemEnvVar(name) {
		return fmt.Errorf("cannot modify protected system env var: %s", name)
	}
	switch level {
	case "user", "machine":
		return env.DeleteEnvVar(name, level)
	default:
		return fmt.Errorf("unsupported level: %q, only user or machine", level)
	}
}

func (a *App) GetOSEnvVars(level string) []EnvVarInfo {
	if runtime.GOOS != "windows" {
		return nil
	}

	vars, err := env.EnumEnvVars(level)
	if err != nil {
		logDebug("[GetOSEnvVars] enumerating %s env vars failed: %v", level, err)
		return nil
	}

	devEnvVarSet := map[string]bool{
		"JAVA_HOME": true, "GOROOT": true, "GOPATH": true, "PYTHON_HOME": true, "PYTHONPATH": true,
		"NODE_PATH": true, "DOTNET_ROOT": true, "RUSTUP_HOME": true, "CARGO_HOME": true,
		"FLUTTER_ROOT": true, "DART_SDK": true, "GRADLE_HOME": true, "MAVEN_HOME": true, "M2_HOME": true,
		"ANDROID_HOME": true, "ANDROID_SDK_ROOT": true, "ANDROID_NDK_HOME": true,
		"CMAKE_ROOT": true, "LLVM_HOME": true, "NVM_HOME": true, "CONDA_PREFIX": true,
		"Path": true, "PATH": true,
	}

	var result []EnvVarInfo
	for _, v := range vars {
		if devEnvVarSet[v.Name] {
			result = append(result, EnvVarInfo{
				Name: v.Name, Value: v.Value,
				Source: level, Level: level,
			})
		}
	}
	return result
}

func (a *App) SwitchToLocalVersion(toolName, localPath string) error {
	logInfo("[SwitchToLocalVersion] switching %s to local version", toolName)

	// Ticket 06 (audit-fixes / P0-08): validate everything BEFORE touching
	// the filesystem. Previously toolName was used to build
	// current/<toolName> and the link was removed/re-created before the
	// plugin lookup ran — a traversal name (e.g. "..\\current\\node")
	// attacked the real link, and an unregistered tool name still left the
	// link dangling after the late failure.
	if err := util.ValidateShimName(toolName); err != nil {
		return fmt.Errorf("invalid tool name %q: %w", toolName, err)
	}
	// Reject unknown tools up front: the plugin also drives the shim
	// refresh below, so a "switch" for an unknown tool can never be valid.
	p, err := a.mustGetPlugin(toolName)
	if err != nil {
		return err
	}

	cfg := a.getCfg()

	path := strings.TrimSpace(localPath)
	if path == "" {
		_, cached, _ := a.getLocalCached(toolName)
		path = cached
	}
	if path == "" {
		return fmt.Errorf("no local installation found for %s", toolName)
	}
	// Security: localPath arrives from the WebView and becomes the
	// current/<tool> junction target — require an existing, real (non-
	// symlink) directory so the binding cannot point a tool at an
	// arbitrary location. The cached path was already scan-verified, but
	// an explicit path argument must be re-checked here.
	if !filepath.IsAbs(path) {
		return fmt.Errorf("local path must be absolute: %q", path)
	}
	if fi, err := os.Stat(path); err != nil || !fi.IsDir() {
		return fmt.Errorf("local path is not an existing directory: %q", path)
	}

	currentLink := a.currentLink(toolName)
	// Ticket 06: a failed link removal must fail the switch — the old code
	// only logged, leaving CreateJunction to run against a live link.
	if err := os.Remove(currentLink); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing current link %s: %w", currentLink, err)
	}
	// Ticket 06: a failed junction creation must fail the switch — the old
	// code logged "falling back" (there was no fallback) and returned nil,
	// reporting success with the tool's link now missing entirely.
	if err := util.CreateJunction(currentLink, path); err != nil {
		return fmt.Errorf("creating junction %s -> %s: %w", currentLink, path, err)
	}

	for _, shimName := range p.Shims() {
		cmdPath := filepath.Join(cfg.Core.ShimDir, shimName+".cmd")
		os.Remove(cmdPath)
	}

	xdevExe, _ := os.Executable()
	for _, shimName := range p.Shims() {
		shimPathExe := filepath.Join(cfg.Core.ShimDir, shimName+".exe")
		if _, err := os.Stat(shimPathExe); err != nil {
			if err := util.CopyFile(xdevExe, shimPathExe); err != nil {
				logWarn("[SwitchToLocalVersion] copying shim %s failed: %v", shimName, err)
			}
		}
	}

	a.refreshAndSaveLocalCache()
	logInfo("[SwitchToLocalVersion] %s switched to local version at %s", toolName, path)
	return nil
}

func (a *App) SwitchToXdevVersion(toolName, version string) error {
	logInfo("[SwitchToXdevVersion] switching %s to xdev version %s", toolName, version)

	p, err := a.mustGetPlugin(toolName)
	if err != nil {
		return err
	}

	cfg := a.getCfg()
	installPath := p.InstallDir(cfg.Core.InstallDir, version)

	if _, err := os.Stat(installPath); err != nil {
		return fmt.Errorf("version %s of %s is not installed", version, toolName)
	}

	for _, shimName := range p.Shims() {
		cmdPath := filepath.Join(cfg.Core.ShimDir, shimName+".cmd")
		os.Remove(cmdPath)
	}

	ins := a.newInstaller(cfg)
	if err := ins.Activate(p, installPath, version); err != nil {
		return fmt.Errorf("activation failed: %v", err)
	}

	a.refreshAndSaveLocalCache()
	logInfo("[SwitchToXdevVersion] %s switched to xdev version %s", toolName, version)
	return nil
}
