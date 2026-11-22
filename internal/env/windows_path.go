package env

import (
	"path"
	"strings"

	"github.com/xmdemt/xdev/internal/pathmode"
)

// NormalizeWindowsPathEntry repairs separators only in ordinary absolute drive
// paths. UNC, extended/device namespaces, environment references and unknown
// formats retain their literal spelling, including meaningful trailing spaces.
func NormalizeWindowsPathEntry(entry string) string {
	if !isLocalDrivePath(entry) {
		return entry
	}
	var out strings.Builder
	for j := 0; j < len(entry); j++ {
		c := entry[j]
		if isWindowsSeparator(c) && j > 0 && isWindowsSeparator(entry[j-1]) {
			continue
		}
		out.WriteByte(c)
	}
	return out.String()
}

func isWindowsSeparator(c byte) bool { return c == '\\' || c == '/' }

func isLocalDrivePath(s string) bool {
	return len(s) >= 3 && ((s[0] >= 'A' && s[0] <= 'Z') || (s[0] >= 'a' && s[0] <= 'z')) && s[1] == ':' && isWindowsSeparator(s[2]) && !strings.ContainsAny(s, `%"`)
}

// WindowsPathComparisonKey is deliberately separate from the written spelling.
// Only ordinary drive paths have known case/separator/lexical normalization;
// opaque paths compare literally rather than guessing namespace semantics.
func WindowsPathComparisonKey(entry string) string {
	entry = NormalizeWindowsPathEntry(entry)
	if !isLocalDrivePath(entry) {
		return entry
	}
	// Clean the rooted suffix separately so .. can never consume the drive.
	suffix := path.Clean(strings.ReplaceAll(entry[2:], `\`, "/"))
	return strings.ToLower(entry[:2] + strings.ReplaceAll(suffix, "/", `\`))
}

func cleanWindowsSystemPath(current string) string {
	var kept []string
	for _, entry := range strings.Split(current, ";") {
		if strings.TrimSpace(entry) != "" {
			kept = append(kept, NormalizeWindowsPathEntry(entry))
		}
	}
	return strings.Join(kept, ";")
}

func cleanWindowsUserPath(current, home, shimDir, mode string) string {
	if strings.TrimSpace(home) == "" || strings.TrimSpace(shimDir) == "" {
		return current
	}
	shimKey := WindowsPathComparisonKey(shimDir)
	homeKey := WindowsPathComparisonKey(home)
	installPrefix := strings.TrimRight(homeKey, `\`) + `\install\`
	currentPrefix := strings.TrimRight(homeKey, `\`) + `\current\`
	selfKey := currentPrefix + "xdev"
	useShims := pathmode.UsesShims(mode)
	var kept []string
	shimEntry := ""
	for _, entry := range strings.Split(current, ";") {
		if strings.TrimSpace(entry) == "" {
			continue
		}
		written := NormalizeWindowsPathEntry(entry)
		key := WindowsPathComparisonKey(written)
		// Explicit configuration wins even if ShimDir is inside a managed subtree.
		if key == shimKey {
			if shimEntry == "" {
				shimEntry = written
			}
			continue
		}
		if key == homeKey || strings.HasPrefix(key, installPrefix) {
			continue
		}
		if useShims && strings.HasPrefix(key, currentPrefix) && key != selfKey {
			continue
		}
		// Retain the established legacy cleanup only for ordinary drive paths.
		if isLocalDrivePath(written) && (strings.Contains(key, `\ximulocaldev\xmdevenvmt\shims`) || strings.Contains(key, `\reverseeng\xmdevenvmt`) || strings.Contains(key, `\testinitconfig_nodeadlock`)) {
			continue
		}
		kept = append(kept, written)
	}
	if useShims {
		if shimEntry == "" {
			shimEntry = NormalizeWindowsPathEntry(shimDir)
		}
		kept = append([]string{shimEntry}, kept...)
	}
	return strings.Join(kept, ";")
}
