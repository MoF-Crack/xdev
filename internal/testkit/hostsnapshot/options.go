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
		ExcludePathPrefixes: excludes,
	}
}

// userOwnedPaths lists user-owned state in the developer's REAL xdev home:
// rewriting any of these from a test is a defect, so they get a full
// size+mtime fingerprint (a same-size rewrite such as overwriting config.yaml
// is caught by the mtime component).
func userOwnedPaths() []string {
	home := config.PeekCachedHomeDir()
	if home == "" {
		return nil
	}
	return []string{
		filepath.Join(home, "config.yaml"),
		filepath.Join(home, "plugins"),
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
