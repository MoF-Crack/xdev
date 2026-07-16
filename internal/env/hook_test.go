package env

import (
	"strings"
	"testing"
)

func TestHookScript(t *testing.T) {
	tests := []struct {
		name            string
		shell           string
		wantContains    string
		wantNotContains string
	}{
		{
			name:         "bash",
			shell:        "bash",
			wantContains: "_xdev_cd_hook",
		},
		{
			name:         "zsh",
			shell:        "zsh",
			wantContains: "_xdev_chpwd",
		},
		{
			name:         "fish",
			shell:        "fish",
			wantContains: "_xdev_chpwd",
		},
		{
			name:         "pwsh",
			shell:        "pwsh",
			wantContains: "prompt",
		},
		{
			name:         "powershell",
			shell:        "powershell",
			wantContains: "prompt",
		},
		{
			name:         "unknown defaults to bash",
			shell:        "unknown-shell",
			wantContains: "_xdev_cd_hook",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HookScript(tt.shell, "/fake/home")
			if !strings.Contains(got, tt.wantContains) {
				t.Errorf("HookScript(%q) missing expected pattern %q\nGot:\n%s", tt.shell, tt.wantContains, got)
			}
			if tt.wantNotContains != "" && strings.Contains(got, tt.wantNotContains) {
				t.Errorf("HookScript(%q) contains unexpected pattern %q", tt.shell, tt.wantNotContains)
			}
		})
	}
}

func TestHookOutput(t *testing.T) {
	t.Run("bash with paths and env", func(t *testing.T) {
		got := HookOutput("bash", []string{"/usr/local/bin", "/opt/bin"}, map[string]string{"FOO": "bar"})
		if !strings.Contains(got, `export PATH='/usr/local/bin:/opt/bin:$PATH'`) {
			t.Errorf("missing PATH export, got:\n%s", got)
		}
		if !strings.Contains(got, `export FOO='bar'`) {
			t.Errorf("missing FOO export, got:\n%s", got)
		}
	})

	t.Run("zsh with paths and env", func(t *testing.T) {
		got := HookOutput("zsh", []string{"/usr/local/bin"}, map[string]string{"BAZ": "qux"})
		if !strings.Contains(got, `export PATH='/usr/local/bin:$PATH'`) {
			t.Errorf("missing PATH export, got:\n%s", got)
		}
		if !strings.Contains(got, `export BAZ='qux'`) {
			t.Errorf("missing BAZ export, got:\n%s", got)
		}
	})

	t.Run("fish with paths and env", func(t *testing.T) {
		got := HookOutput("fish", []string{"/usr/local/bin", "/opt/bin"}, map[string]string{"FOO": "bar"})
		// H-5: Fish PATH entries are now single-quoted for safety
		if !strings.Contains(got, "set -gx PATH '/usr/local/bin' '/opt/bin' $PATH") {
			t.Errorf("missing fish PATH set, got:\n%s", got)
		}
		if !strings.Contains(got, "set -gx FOO 'bar'") {
			t.Errorf("missing fish FOO set, got:\n%s", got)
		}
	})

	t.Run("pwsh with paths and env", func(t *testing.T) {
		got := HookOutput("pwsh", []string{`C:\tools\bin`, `C:\opt\bin`}, map[string]string{"FOO": "bar"})
		if !strings.Contains(got, `$env:Path = "C:\tools\bin;C:\opt\bin;" + $env:Path`) {
			t.Errorf("missing pwsh Path set, got:\n%s", got)
		}
		if !strings.Contains(got, `$env:FOO = "bar"`) {
			t.Errorf("missing pwsh FOO set, got:\n%s", got)
		}
	})

	t.Run("empty inputs return empty string", func(t *testing.T) {
		got := HookOutput("bash", nil, nil)
		if got != "" {
			t.Errorf("got %q, want empty string", got)
		}
	})

	t.Run("empty paths with env vars", func(t *testing.T) {
		got := HookOutput("bash", nil, map[string]string{"FOO": "bar"})
		if strings.Contains(got, "PATH") {
			t.Errorf("should not contain PATH when no binPaths, got:\n%s", got)
		}
		if !strings.Contains(got, `export FOO='bar'`) {
			t.Errorf("missing FOO export, got:\n%s", got)
		}
	})
}
