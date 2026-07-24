package hostsnapshot

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/xmdemt/xdev/internal/util"
)

// Options configures which host-persistent surfaces are captured.
type Options struct {
	// EnvKeys are user-persistent environment variable names to track.
	// On Windows these are read from HKCU\Environment via env.GetUserEnvVar.
	EnvKeys []string

	// Paths are absolute filesystem paths whose existence and optional
	// content hash are tracked.
	Paths []string

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
		Paths:               nil,
		ExcludePathPrefixes: excludes,
	}
}
