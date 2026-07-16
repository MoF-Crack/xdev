// Package task implements a simple task runner that executes named
// commands defined in .xdev.toml [tasks] sections.
package task

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/lockfile"
	"github.com/xmdemt/xdev/internal/project"
)

// Runner executes tasks defined in a project config.
type Runner struct {
	tasks map[string]project.Task
	dir   string // working directory for the project
	cfg   *config.Config
	lf    *lockfile.Lockfile // for task confirmation tracking (C-7)

	// autoConfirm, when true, skips interactive confirmation for all
	// tasks. Used by tests and non-interactive CI after verifying that
	// the task definitions are trusted.
	autoConfirm bool

	// dryRun, when true, causes Run to print the interpolated command
	// to stdout instead of executing it.
	dryRun bool

	// running tracks the set of tasks currently on the dependency
	// stack, for cycle detection.
	running map[string]bool

	// done tracks tasks that have already completed during the
	// current top-level Run, so that a shared dependency is only
	// executed once.
	done map[string]bool
}

// NewRunner creates a task runner from a project config.
func NewRunner(proj *project.ProjectConfig, projectDir string) *Runner {
	return &Runner{
		tasks: proj.Tasks,
		dir:   projectDir,
	}
}

// SetConfig attaches an xdev configuration so that variable
// interpolation can resolve active tool versions and install paths.
// When nil (the default), version/path placeholders resolve to empty
// strings.
func (r *Runner) SetConfig(cfg *config.Config) *Runner {
	r.cfg = cfg
	return r
}

// SetDryRun toggles dry-run mode: commands are interpolated and
// printed but never executed.
func (r *Runner) SetDryRun(dry bool) *Runner {
	r.dryRun = dry
	return r
}

// SetLockfile attaches a lockfile for task confirmation tracking.
// Security (C-7): When a lockfile is set, the runner checks whether
// the task command has been confirmed by the user before executing.
// If the command has changed since the last confirmation, the user
// is prompted again.
func (r *Runner) SetLockfile(lf *lockfile.Lockfile) *Runner {
	r.lf = lf
	return r
}

// SetAutoConfirm enables automatic confirmation of all tasks, bypassing
// interactive prompts. This is intended for test use and CI environments
// where the task definitions are known to be trusted.
func (r *Runner) SetAutoConfirm(ac bool) *Runner {
	r.autoConfirm = ac
	return r
}

// List returns all task names.
func (r *Runner) List() []string {
	names := make([]string, 0, len(r.tasks))
	for name := range r.tasks {
		names = append(names, name)
	}
	return names
}

// Get returns a task by name.
func (r *Runner) Get(name string) (project.Task, bool) {
	t, ok := r.tasks[name]
	return t, ok
}

// Run executes a named task. If the task declares DependsOn, each
// dependency is run first (recursively). Cycles are detected and
// reported as an error. A task that is a shared dependency of several
// tasks is executed only once per Run invocation.
func (r *Runner) Run(name string) error {
	if r.running == nil {
		r.running = map[string]bool{}
	}
	if r.done == nil {
		r.done = map[string]bool{}
	}
	return r.runWithDeps(name, nil)
}

// runWithDeps runs a task after its dependencies. The chain argument
// holds the task names on the current recursion path, in order, so
// that a cycle can be reported clearly.
func (r *Runner) runWithDeps(name string, chain []string) error {
	// Skip tasks already completed during this Run (dedup).
	if r.done[name] {
		return nil
	}

	t, ok := r.tasks[name]
	if !ok {
		return fmt.Errorf("task %q not found", name)
	}

	// Cycle detection: if this task is already on the active stack,
	// we've hit a circular dependency.
	if r.running[name] {
		cycle := append(append([]string{}, chain...), name)
		return fmt.Errorf("circular task dependency: %s", strings.Join(cycle, " -> "))
	}

	// Run dependencies first.
	if len(t.DependsOn) > 0 {
		r.running[name] = true
		for _, dep := range t.DependsOn {
			if err := r.runWithDeps(dep, append(chain, name)); err != nil {
				r.running[name] = false
				return err
			}
		}
		r.running[name] = false
	}

	// Determine working directory
	workDir := r.dir
	if t.Dir != "" {
		if filepath.IsAbs(t.Dir) {
			workDir = t.Dir
		} else {
			workDir = filepath.Join(r.dir, t.Dir)
		}
	}

		// Interpolate variables in the command string.
		command := r.interpolate(t.Run)

		if r.dryRun {
			fmt.Fprintln(os.Stdout, command)
			r.done[name] = true
			return nil
		}

		// Security (C-7): Confirm task execution before running.
		// Tasks in .xdev.toml execute arbitrary shell commands, which
		// is a supply-chain attack vector (a cloned repo could contain
		// malicious tasks). We require user confirmation the first time
		// a task is run, and again if the command changes.
		if err := r.confirmTask(name, command); err != nil {
			return fmt.Errorf("task %q cancelled: %w", name, err)
		}

		// Execute the command
	cmd := exec.Command("sh", "-c", command)
	if os.PathSeparator == '\\' {
		cmd = exec.Command("cmd", "/c", command)
	}
	cmd.Dir = workDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	// Inherit current environment
	cmd.Env = os.Environ()

	if err := cmd.Run(); err != nil {
		return err
	}
	r.done[name] = true
	return nil
}

// --- Variable interpolation ----------------------------------------------

var (
	// {{xdev:version <tool>}}
	reVersion = regexp.MustCompile(`\{\{\s*xdev:version\s+(\S+?)\s*\}\}`)
	// {{xdev:install_path <tool>}}
	reInstallPath = regexp.MustCompile(`\{\{\s*xdev:install_path\s+(\S+?)\s*\}\}`)
	// $XDEV_<TOOL>_VERSION
	reEnvVersion = regexp.MustCompile(`\$XDEV_([A-Z0-9_]+)_VERSION\b`)
)

// interpolate expands variable placeholders in s using the runner's
// config. Unknown tools resolve to empty strings.
func (r *Runner) interpolate(s string) string {
	if s == "" {
		return s
	}

	// {{xdev:version <tool>}}
	s = reVersion.ReplaceAllStringFunc(s, func(m string) string {
		sub := reVersion.FindStringSubmatch(m)
		if len(sub) < 2 {
			return m
		}
		return r.activeVersion(sub[1])
	})

	// {{xdev:install_path <tool>}}
	s = reInstallPath.ReplaceAllStringFunc(s, func(m string) string {
		sub := reInstallPath.FindStringSubmatch(m)
		if len(sub) < 2 {
			return m
		}
		return r.activeInstallPath(sub[1])
	})

	// $XDEV_<TOOL>_VERSION
	s = reEnvVersion.ReplaceAllStringFunc(s, func(m string) string {
		sub := reEnvVersion.FindStringSubmatch(m)
		if len(sub) < 2 {
			return m
		}
		tool := strings.ToLower(sub[1])
		return r.activeVersion(tool)
	})

	return s
}

// activeVersion returns the currently active version of a tool by
// reading the current/<tool> symlink, or "" if none.
func (r *Runner) activeVersion(tool string) string {
	target := r.activeInstallPath(tool)
	if target == "" {
		return ""
	}
	return filepath.Base(target)
}

// activeInstallPath returns the install path pointed to by the
// current/<tool> symlink, or "" if none.
func (r *Runner) activeInstallPath(tool string) string {
	if r.cfg == nil || r.cfg.Core.XMDevDir == "" {
		return ""
	}
	link := filepath.Join(r.cfg.Core.XMDevDir, "current", tool)
	target, err := os.Readlink(link)
	if err != nil {
		return ""
	}
	return target
}

// FormatTaskList returns a human-readable listing of tasks.
func FormatTaskList(tasks map[string]project.Task) string {
	if len(tasks) == 0 {
		return "(no tasks defined)"
	}
	var b strings.Builder
	for name, t := range tasks {
		desc := t.Desc
		if desc == "" {
			desc = t.Run
		}
		fmt.Fprintf(&b, "  %-20s %s\n", name, desc)
	}
	return b.String()
}

// confirmTask checks whether the user has previously confirmed execution
// of this task with this exact command. If not confirmed, it prompts the
// user and records the confirmation in the lockfile.
//
// Security (C-7): This prevents arbitrary command execution from untrusted
// .xdev.toml files in cloned repositories. The confirmation is tracked per
// task name + command hash, so any change to the task command requires
// re-confirmation.
//
// When running non-interactively (stdin is not a terminal), the confirmation
// is sourced exclusively from the lockfile. If no lockfile is available, the
// task is refused — this is the safe default for CI/CD.
func (r *Runner) confirmTask(name, command string) error {
	// Auto-confirm mode skips the interactive prompt entirely.
	// Used by tests and trusted CI environments.
	if r.autoConfirm {
		return nil
	}

	// Detect non-interactive execution (CI, tests, piped stdin).
	// In this case we cannot prompt the user, so we rely entirely on
	// the lockfile for confirmation.
	isInteractive := isTerminal(os.Stdin)

	hash := taskHash(command)

	// Check if this task+command combination was previously confirmed
	if r.lf != nil && r.lf.IsTaskConfirmed(name, hash) {
		return nil // already confirmed
	}

	// Non-interactive: cannot prompt — refuse the task.
	if !isInteractive {
		return fmt.Errorf("cannot confirm task %q in non-interactive mode — execute from a terminal or pre-confirm via lockfile", name)
	}

	// Display the command and ask for confirmation
	fmt.Fprintf(os.Stderr, "\n⚠ Task %q will execute the following command:\n", name)
	fmt.Fprintf(os.Stderr, "  %s\n\n", command)
	fmt.Fprintf(os.Stderr, "Confirm execution? [y/N] ")

	var response string
	fmt.Scanln(&response)
	response = strings.TrimSpace(strings.ToLower(response))
	if response != "y" && response != "yes" {
		return fmt.Errorf("user declined")
	}

	// Record the confirmation in the lockfile
	if r.lf != nil {
		r.lf.ConfirmTask(name, hash)
		// Save the lockfile so confirmation persists across runs
		lockfile.Save(r.dir, r.lf)
	}

	return nil
}

// isTerminal reports whether f is a terminal (character device).
// Used by confirmTask to decide whether interactive prompting is possible.
func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// taskHash computes a SHA-256 hash of the task command for
// confirmation tracking. This allows detecting when a task's
// command has changed since the last confirmation.
func taskHash(command string) string {
	h := sha256.Sum256([]byte(command))
	return fmt.Sprintf("%x", h[:16]) // first 16 bytes = 32 hex chars
}
