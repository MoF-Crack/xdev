package hostsnapshot

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/util"
)

// Options configures which host-persistent surfaces are captured.
type Options struct {
	// EnvKeys are user-persistent environment variable names to track.
	// On Windows these are read from HKCU\Environment via env.GetUserEnvVar.
	EnvKeys []string

	// Paths are absolute filesystem paths whose existence and
	// size/mtime/mode fingerprint are tracked.
	Paths []string

	// VolatilePaths are absolute paths tracked for existence only. Use them
	// for derived caches that legitimate host activity rewrites freely
	// (<home>/.home.cache, <home>/cache/shim-index.v1), so that a full
	// fingerprint does not report ordinary toolchain churn as pollution.
	VolatilePaths []string

	// DirEntryPaths are directories tracked by their ENTRY SET only (no
	// directory mtime/size). A directory's mtime changes on any child
	// create/delete — including transient activity from unrelated processes —
	// which made the guard flaky（本批第四轮实测：一次全量并发运行被目录 mtime 抖动误报，
	// 单独重跑即恢复）。目录真正要防的是"测试往真实 home 落了文件"，那是条目集合的变化。
	DirEntryPaths []string

	// ExcludePathPrefixes are path prefixes ignored during Diff for file entries
	// (self-interference and system temp).
	ExcludePathPrefixes []string
}

// DefaultOptions returns the default xdev pollution surface whitelist.
func DefaultOptions() Options {
	keys := make([]string, 0, len(util.AllowedEnvVarNames))
	for k := range util.AllowedEnvVarNames {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	temp := os.TempDir()
	excludes := []string{
		filepath.Clean(temp),
	}
	if home, err := os.UserHomeDir(); err == nil {
		// Snapshot helpers must never recurse into secrets.
		excludes = append(excludes,
			filepath.Join(home, ".ssh"),
			filepath.Join(home, ".gnupg"),
		)
	}

	return Options{
		EnvKeys:             keys,
		Paths:               userOwnedPaths(),
		VolatilePaths:       derivedCachePaths(),
		DirEntryPaths:       userOwnedDirs(),
		ExcludePathPrefixes: excludes,
	}
}

// userOwnedDirs lists user-owned DIRECTORIES in the real xdev home, tracked by
// entry set (not mtime). plugins/ 的内容由用户/插件安装决定——测试往里落文件
// （例如历史事件里被写进 plugins/e2eplug.lua）必须在条目集合上暴露出来。
func userOwnedDirs() []string {
	home := config.PeekCachedHomeDir()
	if home == "" {
		return nil
	}
	return []string{filepath.Join(home, "plugins")}
}

// userOwnedPaths lists user-owned FILES in the developer's REAL xdev home:
// rewriting any of these from a test is a defect, so they get a full
// size+mtime fingerprint (a same-size rewrite such as overwriting config.yaml
// is caught by the mtime component)。目录不在此列——plugins/ 走
// DirEntryPaths（条目集合），因为目录 mtime 会因无关进程的瞬时子文件活动抖动。
func userOwnedPaths() []string {
	home := config.PeekCachedHomeDir()
	if home == "" {
		return nil
	}
	return []string{
		filepath.Join(home, "config.yaml"),
	}
}

// derivedCachePaths lists the real home's *derived* caches. They are tracked
// for existence only: every legitimate xdev/shim/CLI/GUI invocation may rewrite
// them (see 票 43 — with the old one-line cache format every invocation did), so
// fingerprinting their mtime would flag normal toolchain activity as pollution.
// A test creating or deleting them is still reported.
func derivedCachePaths() []string {
	home := config.PeekCachedHomeDir()
	if home == "" {
		return nil
	}
	return []string{
		filepath.Join(home, ".home.cache"),
		filepath.Join(home, "cache", "shim-index.v1"),
	}
}
