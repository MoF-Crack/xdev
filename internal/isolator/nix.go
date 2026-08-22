package isolator

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/xmdemt/xdev/internal/util"
)

// nixIsolator creates an isolated environment using Nix flakes.
type nixIsolator struct {
	flakeDir   string
	spec       *EnvironmentSpec
	projectDir string
	xmdeDir    string
}

func newNixIsolator() (*nixIsolator, error) {
	if _, err := exec.LookPath("nix"); err != nil {
		return nil, fmt.Errorf("nix not found: install Nix (https://nixos.org/download) to use nix isolation")
	}
	return &nixIsolator{}, nil
}

func (n *nixIsolator) loadSaved() {
	s := loadState(n.xmdeDir, string(ModeNix))
	if s.FlakeDir != "" {
		n.flakeDir = s.FlakeDir
		n.projectDir = s.ProjectDir
	}
}

func (n *nixIsolator) Mode() IsolationMode { return ModeNix }

func (n *nixIsolator) Setup(ctx context.Context, spec *EnvironmentSpec) error {
	n.spec = spec

	if spec.WorkingDir != "" {
		n.projectDir = spec.WorkingDir
	} else {
		dir, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("get working dir: %w", err)
		}
		n.projectDir = dir
	}

	// Generate flake.nix
	if err := n.generateFlake(); err != nil {
		return fmt.Errorf("generate flake.nix: %w", err)
	}

	saveState(n.xmdeDir, string(ModeNix), persistedState{
		FlakeDir:   n.flakeDir,
		ProjectDir: n.projectDir,
	})
	return nil
}

func (n *nixIsolator) Teardown(ctx context.Context) error {
	// Remove generated flake files if they exist
	if n.flakeDir != "" {
		flakePath := filepath.Join(n.flakeDir, "flake.nix")
		os.Remove(flakePath)
		lockPath := filepath.Join(n.flakeDir, "flake.lock")
		os.Remove(lockPath)
	}
	clearState(n.xmdeDir, string(ModeNix))
	return nil
}

func (n *nixIsolator) Exec(ctx context.Context, cmd []string) (*ExecResult, error) {
	if len(cmd) == 0 {
		return nil, fmt.Errorf("empty command")
	}

	flakeDir := n.flakeDir
	if flakeDir == "" {
		if n.projectDir != "" {
			flakeDir = n.projectDir
		} else {
			return nil, fmt.Errorf("nix environment not set up; run 'xdev isolate setup' first")
		}
	}

	// Use `nix develop` to enter the flake shell and run the command
	nixArgs := []string{"develop", flakeDir, "--command"}
	nixArgs = append(nixArgs, cmd...)

	c := exec.CommandContext(ctx, "nix", nixArgs...)
	util.HideWindow(c)

	var stdout, stderr strings.Builder
	c.Stdout = &stdout
	c.Stderr = &stderr
	c.Dir = n.projectDir

	err := c.Run()
	result := &ExecResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: util.ExitCode(err),
	}
	return result, nil
}

// generateFlake writes a flake.nix to the project directory.
func (n *nixIsolator) generateFlake() error {
	if n.projectDir == "" {
		return fmt.Errorf("project directory not set")
	}

	n.flakeDir = n.projectDir

	// Build packages list from spec tools
	var pkgs []string
	if n.spec != nil {
		for tool := range n.spec.Tools {
			// Security (N-8): sanitize tool names before inserting into
			// the Nix flake to prevent code injection via a malicious
			// .xdev.toml that supplies a tool name containing Nix syntax.
			pkgs = append(pkgs, n.sanitizeNixIdent(tool))
		}
	}
	if len(pkgs) == 0 {
		pkgs = []string{"bash", "coreutils"}
	}

	// Build env vars as Nix env attributes
	var envLines []string
	if n.spec != nil {
		for k, v := range n.spec.Env {
			envLines = append(envLines, fmt.Sprintf("    %s = \"%s\";", n.sanitizeNixIdent(k), escapeNixString(v)))
		}
	}
	envBlock := ""
	if len(envLines) > 0 {
		envBlock = "\n  env = {\n" + strings.Join(envLines, "\n") + "\n  };"
	}

	pkgsStr := strings.Join(pkgs, "\n      ")

	flakeContent := fmt.Sprintf(`{
  description = "xdev isolated environment";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  };

  outputs = { self, nixpkgs }:
    let
      system = builtins.currentSystem;
      pkgs = nixpkgs.legacyPackages.${system};
    in
    {
      devShells.${system}.default = pkgs.mkShell {
        packages = [
          %s
        ];%s
      };
    };
}
`, pkgsStr, envBlock)

	flakePath := filepath.Join(n.flakeDir, "flake.nix")
	return os.WriteFile(flakePath, []byte(flakeContent), 0o644)
}

// escapeNixString escapes a string for safe use inside a Nix double-quoted
// string literal. Nix string interpolation (${...}) and escape sequences
// (\n, \t, \\, \") must be escaped to prevent code injection.
func escapeNixString(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '$':
			// Escape $ to prevent ${...} interpolation
			b.WriteString(`\$`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// sanitizeNixIdent makes a string safe for use as a Nix attribute name.
func (n *nixIsolator) sanitizeNixIdent(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}
