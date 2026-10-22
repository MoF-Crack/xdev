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

// loadSaved 恢复上次持久化的 nix 会话（第九轮 A-02 与 container.go 同源）：
// 只填充句柄；状态损坏**不缓存**成字段，交给真正消费状态的路径在当下重新
// loadState 判定（第十轮 P2-3 让 container 侧也改成同样的形态，两侧对称）。
func (n *nixIsolator) loadSaved() {
	s, err := loadState(n.xmdeDir, string(ModeNix))
	if err != nil {
		return
	}
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

	// 第九轮 A-02（与 container.go 的 Setup 同源）：新会话即将覆盖旧的持久化
	// 状态，若旧状态损坏，其 writtenFiles 归属记录会永久消失，先把损坏文件改名留证。
	// 第十轮 P2-3：在**当下**重新 loadState 判定（不再用构造时的缓存字段）。
	if _, stateErr := loadState(n.xmdeDir, string(ModeNix)); stateErr != nil {
		log.Printf("isolate setup (nix): previous state was corrupt (%v); %s",
			stateErr, quarantineState(n.xmdeDir, string(ModeNix)))
	}
	// 第十轮 P2-2（与 container.go 的 Setup 同源）：state 落盘失败必须**报错并
	// 回滚**。修复前 saveState 无返回值，这里静默通过：flake.nix 已经写进项目，
	// 归属记录（WrittenFiles）却没落盘 → Teardown 读不到会话，只会静默成功 →
	// flake.nix 永久残留，用户重跑 setup 又被 ensureOverwritable 拒绝（环境卡死）。
	// 审计现场：`nix setup err=<nil> state_exists=false flake_written=true`。
	if err := saveState(n.xmdeDir, string(ModeNix), persistedState{
		FlakeDir:     n.flakeDir,
		ProjectDir:   n.projectDir,
		WrittenFiles: n.writtenFiles,
	}); err != nil {
		return n.rollbackFailedSetup(err)
	}
	return nil
}

// rollbackFailedSetup 在 state 无法落盘时删除本次 Setup 刚写的 flake.nix，
// 并返回说明「两件事」的错误：state 没落盘 + 回滚是否成功。
//
// 回滚复用 C-07 的内容 hash 归属规则（removeIfContentUnchanged）：只删
// **内容仍与本次写入一致**的文件。这同时保证了 --force 场景下的正确性——若用户
// 在 Setup 与回滚之间改写了 flake.nix，文件不再属于 xdev，必须原样留下。
func (n *nixIsolator) rollbackFailedSetup(cause error) error {
	if n.flakeDir == "" {
		return fmt.Errorf("nix isolation setup could not persist its state: %w", cause)
	}
	flakePath := filepath.Join(n.flakeDir, "flake.nix")
	removed := removeIfContentUnchanged(flakePath, writtenHash(n.writtenFiles, flakePath))
	if !removed {
		return fmt.Errorf("nix isolation setup could not persist its state: %w; "+
			"the generated %s could not be rolled back (it may have been changed or removed) — "+
			"delete it manually before re-running setup", cause, flakePath)
	}
	return fmt.Errorf("nix isolation setup could not persist its state, so the generated flake was rolled back "+
		"(removed) to avoid leaving an unowned file behind: %w", cause)
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
	state, err := loadState(n.xmdeDir, string(ModeNix))
	if err != nil {
		// 第九轮 A-02（与 container.go 同源）：损坏状态返回零值，而零值
		// WrittenFiles 会走「legacy state，跳过删除」分支——用户被告知
		// teardown 成功，xdev 生成的 flake.nix 却原样残留，重跑 setup 又被
		// ensureOverwritable 拒绝。这里必须明确报错，而不是伪装成 legacy 降级。
		hint := quarantineState(n.xmdeDir, string(ModeNix))
		return fmt.Errorf("cannot tear down nix isolation: %w; %s",
			err, corruptStateHint(ModeNix, hint))
	}
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
	// 第十轮 P2-3：与 container.go 的 Teardown 对齐——只清除**仍然属于本次
	// 处理的那个会话**的 state。上面刚刚重新 loadState 过，这里的 guard 再读一次
	// 磁盘，挡住「loadState 与 clearState 之间另一个进程 setup 了新会话」的窗口，
	// 否则新会话的归属记录会被连带删除。
	clearState(n.xmdeDir, string(ModeNix), func(s persistedState) bool {
		return s.FlakeDir == state.FlakeDir && s.ProjectDir == state.ProjectDir
	})
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
