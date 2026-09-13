// Package hostsnapshot provides suite-level host-state capture and diff
// for proving automated tests do not pollute persistent user environment
// (Constitution principle II / Spec 005).
package hostsnapshot

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/xmdemt/xdev/internal/env"
)

// Snapshot is a point-in-time capture of selected host-persistent state.
type Snapshot struct {
	Env   map[string]string
	Files map[string]FileMeta
}

// FileMeta records whether a path exists and a simple size/mtime/mode
// fingerprint. ModTime is included because a same-size rewrite (e.g. a
// config.yaml overwritten with equally long but different content) would
// otherwise be invisible to the diff.
//
// 目录类条目（Options.DirEntryPaths）走 Entries 字段而**不**看 Size/ModTime：
// 目录的 mtime 会在任何子文件创建/删除时变化——包括与本测试无关的瞬时活动——
// 用它会制造假红（本批第四轮实测到一次：全量并发跑时守卫被目录 mtime 抖动触发，
// 而单独重跑与随后的两次全量重跑都干净）。目录真正要防的是"测试往真实 home 里
// 落了文件"，那是**条目集合**的变化，用 Entries 精确表达。
type FileMeta struct {
	Exists  bool
	Size    int64
	ModTime int64 // UnixNano; 0 when the path is missing
	Mode    os.FileMode
	Entries string // 目录条目集合（排序后以 "\n" 连接）；仅 DirEntryPaths 使用
}

// Change is one difference between two snapshots.
type Change struct {
	Kind   string // env|file
	Key    string
	Before string
	After  string
}

// Capture records the current host-persistent state described by opts.
func Capture(opts Options) (Snapshot, error) {
	s := Snapshot{
		Env:   make(map[string]string, len(opts.EnvKeys)),
		Files: make(map[string]FileMeta, len(opts.Paths)),
	}
	// Batch-read all env vars in a single registry cycle (SC-004 optimization).
	envMap, err := env.GetUserEnvVarsBatch(opts.EnvKeys)
	if err != nil {
		return Snapshot{}, fmt.Errorf("hostsnapshot: batch env read: %w", err)
	}
	for k, v := range envMap {
		s.Env[k] = v
	}
	for _, p := range opts.Paths {
		if p == "" {
			continue
		}
		clean := filepath.Clean(p)
		if excluded(clean, opts.ExcludePathPrefixes) {
			continue
		}
		fi, err := os.Lstat(clean)
		if err != nil {
			if os.IsNotExist(err) {
				s.Files[clean] = FileMeta{Exists: false}
				continue
			}
			return Snapshot{}, fmt.Errorf("hostsnapshot: stat %s: %w", clean, err)
		}
		s.Files[clean] = FileMeta{
			Exists:  true,
			Size:    fi.Size(),
			ModTime: fi.ModTime().UnixNano(),
			Mode:    fi.Mode(),
		}
	}
	// 派生缓存（.home.cache / shim-index.v1）只记存在性：它们本来就会在每次
	// xdev/shim 调用时被合法重写，做 mtime 指纹只会制造假红。见 Options.VolatilePaths。
	for _, p := range opts.VolatilePaths {
		if p == "" {
			continue
		}
		clean := filepath.Clean(p)
		if excluded(clean, opts.ExcludePathPrefixes) {
			continue
		}
		_, err := os.Lstat(clean)
		if err != nil {
			if os.IsNotExist(err) {
				s.Files[clean] = FileMeta{Exists: false}
				continue
			}
			return Snapshot{}, fmt.Errorf("hostsnapshot: stat %s: %w", clean, err)
		}
		s.Files[clean] = FileMeta{Exists: true}
	}
	// 目录类条目（plugins/ 等）只比较**条目集合**，不看目录 mtime/大小：
	// 目录 mtime 会因任何子文件创建/删除而变化（含无关进程的瞬时活动）→ 假红。
	for _, p := range opts.DirEntryPaths {
		if p == "" {
			continue
		}
		clean := filepath.Clean(p)
		if excluded(clean, opts.ExcludePathPrefixes) {
			continue
		}
		entries, err := os.ReadDir(clean)
		if err != nil {
			if os.IsNotExist(err) {
				s.Files[clean] = FileMeta{Exists: false}
				continue
			}
			return Snapshot{}, fmt.Errorf("hostsnapshot: read dir %s: %w", clean, err)
		}
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		sort.Strings(names)
		s.Files[clean] = FileMeta{Exists: true, Entries: strings.Join(names, "\n")}
	}
	return s, nil
}

// Diff returns ordered changes from before to after.
func Diff(before, after Snapshot) []Change {
	var out []Change

	keys := make(map[string]struct{})
	for k := range before.Env {
		keys[k] = struct{}{}
	}
	for k := range after.Env {
		keys[k] = struct{}{}
	}
	var envKeys []string
	for k := range keys {
		envKeys = append(envKeys, k)
	}
	sort.Strings(envKeys)
	for _, k := range envKeys {
		b := before.Env[k]
		a := after.Env[k]
		if b != a {
			out = append(out, Change{
				Kind:   "env",
				Key:    k,
				Before: b,
				After:  a,
			})
		}
	}

	fkeys := make(map[string]struct{})
	for k := range before.Files {
		fkeys[k] = struct{}{}
	}
	for k := range after.Files {
		fkeys[k] = struct{}{}
	}
	var fileKeys []string
	for k := range fkeys {
		fileKeys = append(fileKeys, k)
	}
	sort.Strings(fileKeys)
	for _, k := range fileKeys {
		b := before.Files[k]
		a := after.Files[k]
		if b != a {
			out = append(out, Change{
				Kind:   "file",
				Key:    k,
				Before: formatFile(b),
				After:  formatFile(a),
			})
		}
	}
	return out
}

// 并发全量测试下判定"瞬时 vs 持久"的重采样参数：最多 5 次 × 1.5s（仅在检测到差异时
// 才付出这个代价，正常路径零开销）。取值依据：其它包"写真实注册表再恢复"的窗口是毫秒级，
// 5 次采样全部落在窗口内几乎不可能。
const (
	transientRetries    = 5
	transientRetryDelay = 1500 * time.Millisecond
)

// classifyChanges 判定 after 相对 before 的差异是"瞬时"还是"持久"：
// 反复重采样（最多 retries 次、每次间隔 delay），只要差异全部消失就返回空
// （判为瞬时：另一个包正在写真实注册表且即将恢复）；否则返回仍然存在的差异（真污染）。
//
// 抽成独立函数是为了可测——否则"瞬时/持久"这条判定只能靠并发全量跑碰运气验证。
func classifyChanges(before, after Snapshot, opts Options, retries int, delay time.Duration) []Change {
	persistent := Diff(before, after)
	if len(persistent) == 0 {
		return nil
	}
	for attempt := 0; attempt < retries; attempt++ {
		if delay > 0 {
			time.Sleep(delay)
		}
		now, err := Capture(opts)
		if err != nil {
			break
		}
		remaining := Diff(before, now)
		if len(remaining) == 0 {
			return nil
		}
		persistent = remaining
	}
	return persistent
}

// RunWithHostSnapshot wraps m.Run with before/after host snapshot checks.
// Set XDEV_HOST_SNAPSHOT=0 to skip (CI and default suites must not set this).
func RunWithHostSnapshot(m *testing.M, opts Options) int {
	if os.Getenv("XDEV_HOST_SNAPSHOT") == "0" {
		return m.Run()
	}

	before, err := Capture(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "host state snapshot: capture before failed: %v\n", err)
		return 1
	}

	code := m.Run()

	after, err := Capture(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "host state snapshot: capture after failed: %v\n", err)
		if code == 0 {
			return 1
		}
		return code
	}

	changes := Diff(before, after)
	if len(changes) == 0 {
		fmt.Fprintln(os.Stderr, "host state snapshot: no changes detected")
		return code
	}

	// 持久性判定（本批第四轮复核新增）：并发跑 `go test ./...` 时，**别的包**可能正在
	// "写真实注册表再恢复"——例如票 01 的 GOROOT 守卫（改造前）、internal/env 的真实 PATH
	// RMW、internal/gui 的 InitConfig 追加 PATH、internal/cli 的 JAVA_HOME 用例。它们都在
	// 几百毫秒内恢复，却足以落进本包的 before/after 窗口里，造成**假红**（实测 3 轮全量里
	// 出现 2 次）。真正要拦的是"**留在宿主上的**污染"，所以这里对被判为变化的条目做短窗口
	// 重采样：所有差异都自行消失 → 降级为 WARNING（仍然打印，不静默）；仍存在的差异才阻塞。
	persistent := classifyChanges(before, after, opts, transientRetries, transientRetryDelay)
	if len(persistent) == 0 {
		fmt.Fprintln(os.Stderr, "host state snapshot: transient changes reverted (another package's in-flight test) — not blocking:")
		for _, c := range changes {
			fmt.Fprintf(os.Stderr, "  [%s] %s (before %q → after %q → restored)\n", c.Kind, c.Key, c.Before, c.After)
		}
		return code
	}

	fmt.Fprintln(os.Stderr, "host state snapshot: CHANGES DETECTED (blocking)")
	for _, c := range persistent {
		fmt.Fprintf(os.Stderr, "  [%s] %s\n    before: %q\n    after:  %q\n", c.Kind, c.Key, c.Before, c.After)
	}
	if code == 0 {
		return 1
	}
	return code
}

func formatFile(m FileMeta) string {
	if !m.Exists {
		return "missing"
	}
	if m.Entries != "" || m.Size == 0 && m.ModTime == 0 {
		// 目录条目集合条目（或空目录）：打印条目清单，便于直接看出多了/少了哪个文件。
		return fmt.Sprintf("exists entries=[%s]", strings.ReplaceAll(m.Entries, "\n", ", "))
	}
	return fmt.Sprintf("exists size=%d mtime=%d mode=%v", m.Size, m.ModTime, m.Mode)
}

func excluded(path string, prefixes []string) bool {
	clean := filepath.Clean(path)
	for _, p := range prefixes {
		p = filepath.Clean(p)
		if p == "" {
			continue
		}
		if clean == p || strings.HasPrefix(clean, p+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}
