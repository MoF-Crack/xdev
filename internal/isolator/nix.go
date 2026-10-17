package isolator

import (
	"context"
	"fmt"
	"log"
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

	// writtenFiles records path → SHA-256 of everything generateFlake wrote
	// (C-07), so the persisted state can prove ownership at Teardown time.
	writtenFiles map[string]string
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
		FlakeDir:     n.flakeDir,
		ProjectDir:   n.projectDir,
		WrittenFiles: n.writtenFiles,
	})
	return nil
}

// Teardown removes the flake files this isolator wrote (C-07 / P1-34).
//
// Ownership rule: a path is deleted only while its content still matches the
// SHA-256 recorded at Setup time. State files written before C-07 carry no
// hashes — ownership cannot be proven, so flake.nix is left alone and the
// degradation is logged (deleting a user's own flake.nix is worse than
// leaving a generated one behind).
//
// Exception: flake.lock is a `nix develop` build product (xdev never writes it
// itself), so it is removed unconditionally whenever a flake dir is known.
// It is regenerable and carries no user content of its own.
func (n *nixIsolator) Teardown(ctx context.Context) error {
	state := loadState(n.xmdeDir, string(ModeNix))
	flakeDir := state.FlakeDir
	if flakeDir == "" {
		flakeDir = n.flakeDir
	}
	if flakeDir != "" {
		flakePath := filepath.Join(flakeDir, "flake.nix")
		if len(state.WrittenFiles) == 0 {
			// Legacy state (pre-C-07): only known-safe action is to skip.
			if _, err := os.Stat(flakePath); err == nil {
				logLegacyTeardownSkip(string(ModeNix), flakePath)
			}
		} else {
			removeIfContentUnchanged(flakePath, writtenHash(state.WrittenFiles, flakePath))
		}
		lockPath := filepath.Join(flakeDir, "flake.lock")
		if err := os.Remove(lockPath); err == nil {
			log.Printf("isolate teardown (nix): removed nix develop artifact %s", lockPath)
		}
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
	//
	// 第七轮审计 F-50（nix 侧，与 container.go 同源）：**不要**把宿主的 PATH 写进
	// flake 的 env。spec.Env["PATH"] 是「宿主 shim 目录 + 宿主 PATH」，那条语义只对
	// `none` 模式成立（就地 os.Setenv）。写进 flake 后，`nix develop` 会用宿主 PATH
	// **覆盖** Nix 自己装配的 profile PATH —— 上面刚列出的 pkgs（bash/coreutils 等）
	// 反而从 PATH 里消失，容器内命令解析全落到宿主路径上（而宿主路径在 nix 沙箱里
	// 大多不存在）。Nix 环境应由 Nix 决定 PATH。
	var envLines []string
	if n.spec != nil {
		for k, v := range n.spec.Env {
			if strings.EqualFold(k, "PATH") {
				log.Printf("isolate: not injecting host PATH into the nix flake (F-50); Nix builds its own PATH from the declared packages")
				continue
			}
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
	// C-07 / P1-34: a pre-existing flake.nix is the user's file — refuse to
	// overwrite it unless --force was passed. The check runs before the write
	// so a refused Setup leaves the project untouched and persists no state.
	if err := ensureOverwritable(flakePath, n.spec != nil && n.spec.Force); err != nil {
		return err
	}
	if err := os.WriteFile(flakePath, []byte(flakeContent), 0o644); err != nil {
		return err
	}
	// Remember exactly what we wrote: Teardown deletes this file only while it
	// still hashes to this value (C-07).
	n.writtenFiles = recordWrittenFile(n.writtenFiles, flakePath, []byte(flakeContent))
	return nil
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
