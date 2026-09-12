// app_env_vars_percent_test.go 守卫票 30（B-08 / P1-21）：
//
//   - 30-1 %JAVA_HOME%\bin（注册表定义 JAVA_HOME、进程 env 未定义）
//     不再被误删；真正失效的条目仍被清理（防「清理退化为 no-op」回归）；
//   - 30-2 CleanAllInvalidEnvVars 批量 N 条目只枚举一次合并环境（缓存断言）。
//
// 主机污染守卫：本文件的用例一律把票 30 的包级 seam（合并环境快照 / 变量
// 枚举 / 读 / 写 / 删）替换为内存实现，不触碰真实注册表与用户环境。
package gui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/xmdemt/xdev/internal/env"
)

// b5EnvStore 是票 30 测试用的内存环境存储 + 枚举计数器。
type b5EnvStore struct {
	mu        sync.Mutex
	vars      map[string]string // key: level|NAME（大写）
	enumerate int               // mergedEnvSnapshotFn 被调用次数
}

func b5EnvKey(level, name string) string { return level + "|" + strings.ToUpper(name) }

func (s *b5EnvStore) get(level, name string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.vars[b5EnvKey(level, name)]
}

func (s *b5EnvStore) has(level, name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.vars[b5EnvKey(level, name)]
	return ok
}

func (s *b5EnvStore) enumerateCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.enumerate
}

// b5InstallEnvSeams 用内存实现替换票 30 的 5 个包级 seam，并在测试结束时
// 恢复（seam 默认值 = 生产实现）。
func b5InstallEnvSeams(t *testing.T, store *b5EnvStore, merged map[string]string, pairs []env.EnvPair) {
	t.Helper()
	oldSnapshot, oldEnum := mergedEnvSnapshotFn, enumerateEnvVarsFn
	oldGet, oldSet, oldDelete := envGetEnvVarFn, envSetEnvVarFn, envDeleteEnvVarFn

	mergedEnvSnapshotFn = func() map[string]string {
		store.mu.Lock()
		store.enumerate++
		store.mu.Unlock()
		return merged
	}
	enumerateEnvVarsFn = func(string) ([]env.EnvPair, error) { return pairs, nil }
	envGetEnvVarFn = func(name, level string) (string, error) { return store.get(level, name), nil }
	envSetEnvVarFn = func(name, value, level string) error {
		store.mu.Lock()
		defer store.mu.Unlock()
		store.vars[b5EnvKey(level, name)] = value
		return nil
	}
	envDeleteEnvVarFn = func(name, level string) error {
		store.mu.Lock()
		defer store.mu.Unlock()
		delete(store.vars, b5EnvKey(level, name))
		return nil
	}

	t.Cleanup(func() {
		mergedEnvSnapshotFn, enumerateEnvVarsFn = oldSnapshot, oldEnum
		envGetEnvVarFn, envSetEnvVarFn, envDeleteEnvVarFn = oldGet, oldSet, oldDelete
	})
}

// b5JavaHomeWithBin 造一个「存在 JAVA_HOME\bin」的真实目录。
func b5JavaHomeWithBin(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	// 同时把 JAVA_HOME 本身也当作一个存在的条目来源
	return home
}

// ─────────────────────────────────────────────────────────────────────
// 30-1 %VAR% 合法条目不再被误删 + 真失效仍被清理
// ─────────────────────────────────────────────────────────────────────

// TestCleanEnvVarPath_PercentVarEntrySurvives（修复前红）：
// PATH = "%JAVA_HOME%\bin;<不存在目录>"，JAVA_HOME 只由注册表定义
//（进程 env 里没有）。B-08 前 os.ExpandEnv 不认 %VAR% → 字面量送 os.Stat
// 必失败 → 合法条目被删；修复后保留合法条目、清理失效条目。
func TestCleanEnvVarPath_PercentVarEntrySurvives(t *testing.T) {
	javaHome := b5JavaHomeWithBin(t)
	missing := filepath.Join(t.TempDir(), "gone")

	store := &b5EnvStore{vars: map[string]string{}}
	store.vars[b5EnvKey("user", "Path")] = `%JAVA_HOME%\bin;` + missing
	b5InstallEnvSeams(t, store, map[string]string{"JAVA_HOME": javaHome}, nil)

	app := newTestApp(t)
	if err := app.CleanEnvVarPath("Path", "user"); err != nil {
		t.Fatalf("CleanEnvVarPath: %v", err)
	}

	got := store.get("user", "Path")
	if !strings.Contains(got, `%JAVA_HOME%\bin`) {
		t.Fatalf("合法条目 %%%%JAVA_HOME%%%%\\bin 被误删（B-08 未修复）：清理后 = %q", got)
	}
	if strings.Contains(got, missing) {
		t.Errorf("失效条目必须仍被清理（防 no-op 回归）：清理后 = %q", got)
	}
	if !strings.EqualFold(strings.TrimSpace(got), `%JAVA_HOME%\bin`) {
		t.Errorf("清理结果 = %q, want 原样保留的 `%%JAVA_HOME%%\\bin`", got)
	}
}

// TestValidateEnvVar_PercentVarEntryValid：同病的展示侧（ValidateEnvVar /
// buildPathEntries）——含 %VAR% 的合法条目必须判为有效，前端不再把它显示成
// invalid；未定义引用仍判无效。
func TestValidateEnvVar_PercentVarEntryValid(t *testing.T) {
	javaHome := b5JavaHomeWithBin(t)
	store := &b5EnvStore{vars: map[string]string{}}
	b5InstallEnvSeams(t, store, map[string]string{"JAVA_HOME": javaHome}, nil)

	valid, reason := ValidateEnvVar("Path", `%JAVA_HOME%\bin`)
	if !valid {
		t.Fatalf("含 %%JAVA_HOME%% 的合法条目被判无效: %s（B-08）", reason)
	}

	entries := buildPathEntries(`%JAVA_HOME%\bin`)
	if len(entries) != 1 || !entries[0].Exists {
		t.Fatalf("buildPathEntries 未把 %%JAVA_HOME%%\\\\bin 标为存在: %+v", entries)
	}
	// 条目路径必须是原始（未展开）形式，前端显示用户输入的字符串。
	if entries[0].Path != `%JAVA_HOME%\bin` {
		t.Errorf("PathEntry.Path = %q, want 原始形式", entries[0].Path)
	}

	undefined, _ := ValidateEnvVar("Path", `%NO_SUCH_VAR_B5%\bin`)
	if undefined {
		t.Error("未命中合并环境的 %VAR% 引用必须判无效")
	}
}

// TestCleanEnvVarPath_UndefinedPercentVarStillRemoved：未定义引用与真缺失
// 路径都必须被清理（防「所有 %-引用落到保守跳过」的 no-op 退化）；全部条目
// 失效时整个变量被删除（单条目分支同样生效）。
func TestCleanEnvVarPath_UndefinedPercentVarStillRemoved(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "gone")
	store := &b5EnvStore{vars: map[string]string{}}
	store.vars[b5EnvKey("user", "Path")] = `%NO_SUCH_VAR_B5%\bin;` + missing
	b5InstallEnvSeams(t, store, map[string]string{}, nil)

	app := newTestApp(t)
	if err := app.CleanEnvVarPath("Path", "user"); err != nil {
		t.Fatalf("CleanEnvVarPath: %v", err)
	}
	if store.has("user", "Path") {
		t.Errorf("全部条目失效时变量必须被删除，实际残留: %q", store.get("user", "Path"))
	}

	// 单条目分支：未定义引用 → 视为失效 → 删除变量（不退化为静默保留）。
	store2 := &b5EnvStore{vars: map[string]string{}}
	store2.vars[b5EnvKey("user", "MYONLYPATH")] = `%NO_SUCH_VAR_B5%\bin`
	b5InstallEnvSeams(t, store2, map[string]string{}, nil)
	if err := app.CleanEnvVarPath("MYONLYPATH", "user"); err != nil {
		t.Fatalf("CleanEnvVarPath（单条目）: %v", err)
	}
	if store2.has("user", "MYONLYPATH") {
		t.Error("单条目分支：未定义 %VAR% 引用必须判为失效并删除变量")
	}
}

// TestExpandPercentVars_CaseInsensitiveAndPreservingUnresolved：helper 语义
// ——键大小写不敏感命中、未命中保留原样（由调用处标记无效）。
func TestExpandPercentVars_CaseInsensitiveAndPreservingUnresolved(t *testing.T) {
	store := &b5EnvStore{vars: map[string]string{}}
	b5InstallEnvSeams(t, store, map[string]string{"JAVA_HOME": `C:\jdk`}, nil)

	exp := newPercentExpander()
	if got := expandPercentVars(`%java_home%\bin`, exp.lookup); got != `C:\jdk\bin` {
		t.Errorf("大小写不敏感展开失败: got %q, want %q", got, `C:\jdk\bin`)
	}
	if got := expandPercentVars(`%NoPe%\bin`, exp.lookup); got != `%NoPe%\bin` {
		t.Errorf("未命中引用必须保留原样: got %q", got)
	}
	if !percentVarsUnresolved(`%NoPe%\bin`) {
		t.Error("percentVarsUnresolved 应报告残留引用")
	}
	if percentVarsUnresolved(`C:\jdk\bin`) {
		t.Error("percentVarsUnresolved 对无引用的路径应为 false")
	}
	if got := expandPathRefs(`%JAVA_HOME%\bin`, exp.lookup); got != `C:\jdk\bin` {
		t.Errorf("expandPathRefs 展开 %%%%VAR%%%% 失败: %q", got)
	}
}

// ─────────────────────────────────────────────────────────────────────
// 30-2 批量清理只枚举一次合并环境
// ─────────────────────────────────────────────────────────────────────

// TestCleanAllInvalidEnvVars_EnumeratesMergedEnvOnce：N 个条目（每个都含
// %JAVA_HOME% 引用）走一次批量清理，合并环境必须只枚举一次（票 30 第 3 条）；
// 同时断言合法条目全保留、失效条目全清理（双向防回归）。
func TestCleanAllInvalidEnvVars_EnumeratesMergedEnvOnce(t *testing.T) {
	javaHome := b5JavaHomeWithBin(t)
	missingRoot := t.TempDir()

	const n = 12
	store := &b5EnvStore{vars: map[string]string{}}
	var pairs []env.EnvPair

	pathValue := `%JAVA_HOME%\bin;` + filepath.Join(missingRoot, "gone-path")
	store.vars[b5EnvKey("user", "PATH")] = pathValue
	pairs = append(pairs, env.EnvPair{Name: "PATH", Value: pathValue})

	for i := 0; i < n; i++ {
		name := fmt.Sprintf("B5VAR%02d", i)
		value := `%JAVA_HOME%\bin;` + filepath.Join(missingRoot, fmt.Sprintf("gone-%d", i))
		store.vars[b5EnvKey("user", name)] = value
		pairs = append(pairs, env.EnvPair{Name: name, Value: value})
	}
	b5InstallEnvSeams(t, store, map[string]string{"JAVA_HOME": javaHome}, pairs)

	app := newTestApp(t)
	results := app.CleanAllInvalidEnvVars("user")
	if len(results) == 0 {
		t.Fatal("批量清理应报告结果")
	}

	if got := store.enumerateCount(); got != 1 {
		t.Fatalf("合并环境枚举次数 = %d, want 1（30-2 缓存断言：批量必须复用同一份快照）", got)
	}

	// 合法条目（%JAVA_HOME%\bin）全部保留……
	if got := store.get("user", "PATH"); got != `%JAVA_HOME%\bin` {
		t.Errorf("PATH 清理结果 = %q, want %%JAVA_HOME%%\\bin（合法条目被误删）", got)
	}
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("B5VAR%02d", i)
		if got := store.get("user", name); got != `%JAVA_HOME%\bin` {
			t.Errorf("%s 清理结果 = %q, want %%JAVA_HOME%%\\bin", name, got)
		}
	}
	// ……失效条目全部清理（防 no-op 回归）。
	for _, r := range results {
		if r.Action == "skipped" {
			t.Errorf("条目 %s 被跳过（清理退化为 no-op？）: %s", r.Name, r.Detail)
		}
	}
}
