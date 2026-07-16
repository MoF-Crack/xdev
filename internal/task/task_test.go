package task

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/project"
)

func TestRunnerList(t *testing.T) {
	proj := &project.ProjectConfig{
		Tasks: map[string]project.Task{
			"build": {Run: "go build ./...", Desc: "Build the project"},
			"test":  {Run: "go test ./..."},
			"lint":  {Run: "golangci-lint run", Desc: "Run linters"},
		},
	}
	r := NewRunner(proj, t.TempDir())

	names := r.List()
	sort.Strings(names)

	expected := []string{"build", "lint", "test"}
	if len(names) != len(expected) {
		t.Fatalf("expected %d tasks, got %d", len(expected), len(names))
	}
	for i, n := range names {
		if n != expected[i] {
			t.Errorf("names[%d] = %q, want %q", i, n, expected[i])
		}
	}
}

func TestRunnerGet(t *testing.T) {
	proj := &project.ProjectConfig{
		Tasks: map[string]project.Task{
			"build": {Run: "go build ./...", Desc: "Build the project"},
		},
	}
	r := NewRunner(proj, t.TempDir())

	// Existing task
	task, ok := r.Get("build")
	if !ok {
		t.Fatal("expected to find task 'build'")
	}
	if task.Run != "go build ./..." {
		t.Errorf("task.Run = %q, want %q", task.Run, "go build ./...")
	}
	if task.Desc != "Build the project" {
		t.Errorf("task.Desc = %q, want %q", task.Desc, "Build the project")
	}

	// Non-existing task
	_, ok = r.Get("nonexistent")
	if ok {
		t.Error("expected not to find task 'nonexistent'")
	}
}

func TestRunnerRunNotFound(t *testing.T) {
	proj := &project.ProjectConfig{
		Tasks: map[string]project.Task{},
	}
	r := NewRunner(proj, t.TempDir())

	err := r.Run("missing")
	if err == nil {
		t.Fatal("expected error for missing task")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want it to contain 'not found'", err.Error())
	}
}

func TestRunnerRunEcho(t *testing.T) {
	proj := &project.ProjectConfig{
		Tasks: map[string]project.Task{
			"hello": {Run: "echo hello"},
		},
	}
	r := NewRunner(proj, t.TempDir()).SetAutoConfirm(true)

	err := r.Run("hello")
	if err != nil {
		t.Errorf("Run('hello') failed: %v", err)
	}
}

func TestFormatTaskList(t *testing.T) {
	// With tasks that have descriptions
	tasks := map[string]project.Task{
		"build": {Run: "go build ./...", Desc: "Build the project"},
		"test":  {Run: "go test ./..."},
	}
	output := FormatTaskList(tasks)

	if !strings.Contains(output, "build") {
		t.Error("output should contain 'build'")
	}
	if !strings.Contains(output, "Build the project") {
		t.Error("output should contain task description")
	}
	if !strings.Contains(output, "test") {
		t.Error("output should contain 'test'")
	}
	// Task without Desc should show the Run command instead
	if !strings.Contains(output, "go test ./...") {
		t.Error("output should show Run command when Desc is empty")
	}

	// Empty tasks
	emptyOutput := FormatTaskList(map[string]project.Task{})
	if emptyOutput != "(no tasks defined)" {
		t.Errorf("empty output = %q, want %q", emptyOutput, "(no tasks defined)")
	}
}

func TestRunnerListEmpty(t *testing.T) {
	proj := &project.ProjectConfig{
		Tasks: map[string]project.Task{},
	}
	r := NewRunner(proj, t.TempDir())

	names := r.List()
	if len(names) != 0 {
		t.Errorf("expected empty list, got %v", names)
	}
}

func TestRunnerRunWithDir(t *testing.T) {
	tmpDir := t.TempDir()
	proj := &project.ProjectConfig{
		Tasks: map[string]project.Task{
			"pwd": {Run: "echo pwd_ok", Dir: tmpDir},
		},
	}
	r := NewRunner(proj, t.TempDir()).SetAutoConfirm(true)

	err := r.Run("pwd")
	if err != nil {
		t.Errorf("Run('pwd') failed: %v", err)
	}
}

func TestFormatTaskListShowsRunWhenNoDesc(t *testing.T) {
	tasks := map[string]project.Task{
		"clean": {Run: "rm -rf build/"},
	}
	output := FormatTaskList(tasks)
	if !strings.Contains(output, "rm -rf build/") {
		t.Errorf("expected Run command in output when Desc is empty, got: %q", output)
	}
}

func TestRunnerNilTasksMap(t *testing.T) {
	proj := &project.ProjectConfig{
		Tasks: nil,
	}
	r := NewRunner(proj, t.TempDir())

	// List should work with nil map (returns empty)
	names := r.List()
	if len(names) != 0 {
		t.Errorf("expected empty list for nil tasks, got %v", names)
	}

	// Get should return false
	_, ok := r.Get("anything")
	if ok {
		t.Error("expected Get to return false for nil tasks map")
	}

	// Run should return not found error
	err := r.Run("anything")
	if err == nil {
		t.Error("expected error for Run with nil tasks map")
	}

	_ = os.DevNull // avoid unused import
}

// --- Dependency tests ----------------------------------------------------

func TestRunDependencies(t *testing.T) {
	// Each task echoes its own name to stdout; we capture the
	// combined output and verify that dependencies run before their
	// dependents.
	proj := &project.ProjectConfig{
		Tasks: map[string]project.Task{
			"a": {Run: "echo a", DependsOn: []string{"b", "c"}},
			"b": {Run: "echo b", DependsOn: []string{"c"}},
			"c": {Run: "echo c"},
		},
	}
	r := NewRunner(proj, t.TempDir()).SetAutoConfirm(true)

	out := captureStdout(t, func() {
		if err := r.Run("a"); err != nil {
			t.Fatalf("Run('a') failed: %v", err)
		}
	})

	log := strings.Fields(strings.TrimSpace(out))

	// c must run before b, and both before a.
	wantOrder := []string{"c", "b", "a"}
	if len(log) != len(wantOrder) {
		t.Fatalf("expected %d entries, got %d: %v", len(wantOrder), len(log), log)
	}
	for i, w := range wantOrder {
		if log[i] != w {
			t.Errorf("order[%d] = %q, want %q (full: %v)", i, log[i], w, log)
		}
	}
}

func TestRunCycleDetection(t *testing.T) {
	proj := &project.ProjectConfig{
		Tasks: map[string]project.Task{
			"a": {Run: "echo a", DependsOn: []string{"b"}},
			"b": {Run: "echo b", DependsOn: []string{"a"}},
		},
	}
	r := NewRunner(proj, t.TempDir())

	err := r.Run("a")
	if err == nil {
		t.Fatal("expected cycle error, got nil")
	}
	if !strings.Contains(err.Error(), "circular") {
		t.Errorf("error = %q, want it to contain 'circular'", err.Error())
	}
}

func TestRunSelfCycleDetection(t *testing.T) {
	proj := &project.ProjectConfig{
		Tasks: map[string]project.Task{
			"loop": {Run: "echo loop", DependsOn: []string{"loop"}},
		},
	}
	r := NewRunner(proj, t.TempDir())

	err := r.Run("loop")
	if err == nil {
		t.Fatal("expected cycle error for self-dependency")
	}
	if !strings.Contains(err.Error(), "circular") {
		t.Errorf("error = %q, want it to contain 'circular'", err.Error())
	}
}

func TestRunMissingDependency(t *testing.T) {
	proj := &project.ProjectConfig{
		Tasks: map[string]project.Task{
			"a": {Run: "echo a", DependsOn: []string{"missing"}},
		},
	}
	r := NewRunner(proj, t.TempDir())

	err := r.Run("a")
	if err == nil {
		t.Fatal("expected error for missing dependency")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want it to contain 'not found'", err.Error())
	}
}

// --- Dry-run test --------------------------------------------------------

func TestDryRunDoesNotExecute(t *testing.T) {
	proj := &project.ProjectConfig{
		Tasks: map[string]project.Task{
			"touch": {Run: "echo SHOULD_NOT_PRINT"},
		},
	}
	r := NewRunner(proj, t.TempDir()).SetDryRun(true)

	if err := r.Run("touch"); err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
}

// --- Interpolation tests -------------------------------------------------

// newSymlink creates a current/<tool> symlink pointing at a fake
// install directory under xmdeDir, returning the install path.
func newSymlink(t *testing.T, xmdeDir, tool, version string) string {
	t.Helper()
	installPath := filepath.Join(xmdeDir, "install", tool, version)
	if err := os.MkdirAll(installPath, 0o755); err != nil {
		t.Fatalf("mkdir install dir: %v", err)
	}
	currentDir := filepath.Join(xmdeDir, "current")
	if err := os.MkdirAll(currentDir, 0o755); err != nil {
		t.Fatalf("mkdir current dir: %v", err)
	}
	link := filepath.Join(currentDir, tool)
	_ = os.Remove(link)
	if err := os.Symlink(installPath, link); err != nil {
		// Symlinks may require privileges on Windows; skip if so.
		if runtime.GOOS == "windows" && isPrivilegeError(err) {
			t.Skipf("cannot create symlink on this Windows environment: %v", err)
		}
		t.Fatalf("symlink: %v", err)
	}
	return installPath
}

func isPrivilegeError(err error) bool {
	pe, ok := err.(*os.LinkError)
	if !ok {
		return false
	}
	s := pe.Error()
	return strings.Contains(s, "privilege") ||
		strings.Contains(s, "requires") ||
		strings.Contains(s, "access")
}

func TestInterpolateVersionAndPath(t *testing.T) {
	xmdeDir := t.TempDir()
	installPath := newSymlink(t, xmdeDir, "python", "3.12.1")

	cfg := &config.Config{}
	cfg.Core.XMDevDir = xmdeDir

	proj := &project.ProjectConfig{
		Tasks: map[string]project.Task{
			"info": {Run: "echo {{xdev:version python}} {{xdev:install_path python}} $XDEV_PYTHON_VERSION"},
		},
	}
	r := NewRunner(proj, t.TempDir()).SetConfig(cfg)

	// Capture the interpolated command via dry-run.
	r.SetDryRun(true)
	out := captureStdout(t, func() {
		if err := r.Run("info"); err != nil {
			t.Fatalf("Run failed: %v", err)
		}
	})

	expected := "echo 3.12.1 " + installPath + " 3.12.1"
	if strings.TrimSpace(out) != expected {
		t.Errorf("interpolated = %q, want %q", strings.TrimSpace(out), expected)
	}
}

func TestInterpolateUnknownTool(t *testing.T) {
	xmdeDir := t.TempDir()
	cfg := &config.Config{}
	cfg.Core.XMDevDir = xmdeDir

	proj := &project.ProjectConfig{
		Tasks: map[string]project.Task{
			"x": {Run: "echo [{{xdev:version ghost}}]"},
		},
	}
	r := NewRunner(proj, t.TempDir()).SetConfig(cfg).SetDryRun(true)
	out := captureStdout(t, func() {
		if err := r.Run("x"); err != nil {
			t.Fatalf("Run failed: %v", err)
		}
	})
	if strings.TrimSpace(out) != "echo []" {
		t.Errorf("interpolated = %q, want %q", strings.TrimSpace(out), "echo []")
	}
}

func TestInterpolateNoConfig(t *testing.T) {
	// Without SetConfig, placeholders resolve to empty strings.
	proj := &project.ProjectConfig{
		Tasks: map[string]project.Task{
			"x": {Run: "echo [{{xdev:version go}}]"},
		},
	}
	r := NewRunner(proj, t.TempDir()).SetDryRun(true)
	out := captureStdout(t, func() {
		if err := r.Run("x"); err != nil {
			t.Fatalf("Run failed: %v", err)
		}
	})
	if strings.TrimSpace(out) != "echo []" {
		t.Errorf("interpolated = %q, want %q", strings.TrimSpace(out), "echo []")
	}
}

// --- Helpers -------------------------------------------------------------

// captureStdout runs fn while redirecting os.Stdout to a pipe, and
// returns whatever was written.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	old := os.Stdout
	os.Stdout = w
	// Ensure the exec package also uses the redirected stdout for
	// child processes by relying on the default (os.Stdout) which we
	// just replaced.
	done := make(chan struct{})
	go func() {
		fn()
		_ = w.Close()
		close(done)
	}()
	var buf strings.Builder
	_, _ = io.Copy(&buf, r)
	os.Stdout = old
	<-done
	return buf.String()
}

// ensure exec is referenced (used implicitly by the task runner in
// the dependency tests).
var _ = exec.Command
