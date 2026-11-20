package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	restartHelperFlag       = "--xdev-internal-restart-helper"
	restartHelperName       = "helper.exe"
	restartPlanName         = "plan.json"
	restartResultName       = "result.json"
	restartBackupName       = "backup.exe"
	restartErrorEnv         = "XDEV_SELF_UPDATE_RESTART_ERROR"
	restartHandshakeTimeout = 10 * time.Second
	restartParentTimeout    = 60 * time.Second
	restartPlanSizeLimit    = 128 << 10
)

// Paths for files other than Target and WorkingDir are derived, not loaded
// from JSON. The helper inherits the environment; it is never written here.
type restartPlan struct {
	Target        string   `json:"target"`
	OldSHA256     string   `json:"old_sha256"`
	PayloadSHA256 string   `json:"payload_sha256"`
	ParentPID     int      `json:"parent_pid"`
	ParentCreated int64    `json:"parent_creation_time"`
	Args          []string `json:"args"`
	WorkingDir    string   `json:"working_dir"`
}

type restartReceipt struct {
	State  string `json:"state"`
	Target string `json:"target,omitempty"`
	Error  string `json:"error,omitempty"`
	Backup string `json:"backup,omitempty"`
	PID    int    `json:"relaunch_pid,omitempty"`
}

// Restart starts the helper and transfers ownership only after it has opened
// the real parent process and acknowledged readiness. The caller must then
// request graceful shutdown. Cancellation before handoff leaves it running.
func (p *PreparedUpdate) Restart(ctx context.Context, opts RestartOptions) error {
	if p == nil {
		return errors.New("no prepared update")
	}
	if runtime.GOOS != "windows" {
		return errRestartUnsupported
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state != restartPrepared || p.stage == "" {
		return errors.New("prepared update is no longer available")
	}
	ctx, cancel := context.WithTimeout(ctx, restartHandshakeTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	plan := p.plan
	actualExe, err := os.Executable()
	if err != nil {
		return err
	}
	actualExe, err = canonicalRestartPath(actualExe)
	if err != nil {
		return err
	}
	if !sameRestartPath(plan.Target, actualExe) {
		return errors.New("prepared update target is not the running executable")
	}
	plan.WorkingDir = opts.WorkingDir
	if plan.WorkingDir == "" {
		plan.WorkingDir, err = os.Getwd()
		if err != nil {
			return err
		}
	}
	plan.WorkingDir, err = canonicalRestartPath(plan.WorkingDir)
	if err != nil {
		return fmt.Errorf("resolve restart working directory: %w", err)
	}
	plan.Args = append([]string(nil), opts.Args...)
	plan.ParentPID = os.Getpid()
	plan.ParentCreated, err = restartCurrentProcessCreated()
	if err != nil {
		return err
	}
	if err := plan.validate(); err != nil {
		return err
	}

	if !sameRestartPath(filepath.Dir(p.stage), filepath.Dir(plan.Target)) {
		return ErrPreparedUpdateInvalid
	}
	root, dir, err := openRestartDirectory(p.stage)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrPreparedUpdateInvalid, err)
	}
	defer root.Close()
	for _, entry := range []struct{ path, digest string }{
		{plan.Target, plan.OldSHA256}, {filepath.Join(p.stage, restartPayloadName), plan.PayloadSHA256},
	} {
		if err := verifyRestartPathHash(ctx, entry.path, entry.digest); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("%w: %v", ErrPreparedUpdateInvalid, err)
		}
	}
	helperRel := filepath.Join(dir, restartHelperName)
	planRel := filepath.Join(dir, restartPlanName)
	var madeHelper, madePlan bool
	defer func() {
		if p.state != restartHandedOff {
			if madePlan {
				_ = root.Remove(planRel)
			}
			if madeHelper {
				_ = root.Remove(helperRel)
			}
		}
	}()
	madeHelper, err = copyRestartExecutable(ctx, root, helperRel, plan.Target, plan.OldSHA256)
	if err != nil {
		return fmt.Errorf("copy restart helper: %w", err)
	}
	madePlan, err = writeRestartJSON(root, planRel, plan)
	if err != nil {
		return fmt.Errorf("write restart plan: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	helperPath := filepath.Join(p.stage, restartHelperName)
	// Prevent an in-place write or rename of the copy before CreateProcess.
	helper, err := openRestartInput(helperPath)
	if err != nil {
		return err
	}
	defer helper.Close()
	got, err := hashRestartInput(ctx, helper)
	if err != nil {
		return err
	}
	if got != plan.OldSHA256 {
		return errors.New("restart helper changed after it was copied")
	}
	if err := startRestartHelper(ctx, helperPath, filepath.Join(p.stage, restartPlanName)); err != nil {
		return fmt.Errorf("start restart helper: %w (diagnostics: %s)", err, filepath.Join(p.stage, restartResultName))
	}
	p.state = restartHandedOff
	return nil
}

func startRestartHelper(ctx context.Context, executable, planPath string) error {
	cmd := exec.Command(executable, restartHelperFlag, planPath)
	// Do not attach the caller's context to the accepted helper. GUI shutdown
	// is expected to cancel that context before the helper replaces anything.
	configureRestartProcess(cmd)
	commit, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	defer commit.Close()
	reader, writer, err := os.Pipe()
	if err != nil {
		return err
	}
	defer reader.Close()
	defer writer.Close()
	cmd.Stdout = writer
	if err := cmd.Start(); err != nil {
		return err
	}
	_ = writer.Close()
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	accepted := false
	defer func() {
		if !accepted {
			_ = cmd.Process.Kill()
			<-done
		}
	}()
	ack := make(chan error, 1)
	go func() { ack <- readRestartByte(reader, 'R') }()
	select {
	case err := <-ack:
		if err != nil {
			return fmt.Errorf("helper did not acknowledge readiness: %w", err)
		}
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := io.WriteString(commit, "C"); err != nil {
		return fmt.Errorf("acknowledge restart handoff: %w", err)
	}
	accepted = true
	return nil
}

// RunRestartHelper must be called before normal application setup/argument
// parsing. When handled is true the caller must exit, even on error.
func RunRestartHelper(args []string) (handled bool, err error) {
	return runRestartHelper(args, restartParentTimeout)
}

func runRestartHelper(args []string, parentTimeout time.Duration) (bool, error) {
	if len(args) == 0 || args[0] != restartHelperFlag {
		return false, nil
	}
	if len(args) != 2 || args[1] == "" {
		return true, errors.New("invalid restart helper invocation")
	}
	if runtime.GOOS != "windows" {
		return true, errRestartUnsupported
	}
	return true, executeRestartPlan(args[1], parentTimeout)
}

func executeRestartPlan(planPath string, parentTimeout time.Duration) (err error) {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if !sameRestartPath(filepath.Base(self), restartHelperName) || !canonicalRestartIdentity(self) {
		return errors.New("restart helper is not running from its staging path")
	}
	stage := filepath.Dir(self)
	if !sameRestartPath(planPath, filepath.Join(stage, restartPlanName)) || !canonicalRestartIdentity(planPath) {
		return errors.New("restart plan is not beside its helper")
	}
	root, dir, err := openRestartDirectory(stage)
	if err != nil {
		return err
	}
	defer root.Close()
	planRel := filepath.Join(dir, restartPlanName)
	reportRel := filepath.Join(dir, restartResultName)
	receipt := restartReceipt{State: "rejected"}
	defer func() {
		if err != nil {
			receipt.Error = err.Error()
		}
		if _, reportErr := writeRestartJSON(root, reportRel, receipt); reportErr != nil {
			err = errors.Join(err, fmt.Errorf("write restart result: %w", reportErr))
		}
		_ = root.Remove(planRel)
		// The Windows loader may hold this executable until process exit.
		_ = root.Remove(filepath.Join(dir, restartHelperName))
	}()
	var plan restartPlan
	if err := readRestartJSON(root, planRel, &plan); err != nil {
		return fmt.Errorf("read restart plan: %w", err)
	}
	if err := plan.validate(); err != nil {
		return err
	}
	receipt.Target = plan.Target
	if !sameRestartPath(filepath.Dir(plan.Target), filepath.Dir(stage)) || !canonicalRestartIdentity(plan.Target) {
		return errors.New("restart target is outside the helper's installation directory")
	}
	parent, err := openRestartParent(plan)
	if err != nil {
		return err
	}
	defer parent.close()
	selfHash, err := restartFileHash(context.Background(), self)
	if err != nil || selfHash != plan.OldSHA256 {
		return fmt.Errorf("restart helper does not match the installed executable: %v", err)
	}
	if _, err := io.WriteString(os.Stdout, "R"); err != nil {
		return fmt.Errorf("signal restart readiness: %w", err)
	}
	commit := make(chan error, 1)
	go func() { commit <- readRestartByte(os.Stdin, 'C') }()
	select {
	case err := <-commit:
		if err != nil {
			return fmt.Errorf("restart handoff was not accepted: %w", err)
		}
	case <-time.After(restartHandshakeTimeout):
		return errors.New("restart handoff was not accepted in time")
	}
	if err := parent.wait(parentTimeout); err != nil {
		receipt.State = "parent_not_exited"
		return err
	}

	receipt.PID, receipt.State, err = applyRestartPlan(root, dir, plan)
	if _, backupErr := root.Lstat(filepath.Join(dir, restartBackupName)); backupErr == nil {
		receipt.Backup = filepath.Join(stage, restartBackupName)
	}
	return err
}

func applyRestartPlan(root *os.Root, dir string, plan restartPlan) (int, string, error) {
	ctx := context.Background() // Parent has exited; complete or roll back.
	if !canonicalRestartIdentity(plan.Target) {
		return 0, "target_conflict", errors.New("installed executable path changed while restarting")
	}
	unlock, err := lockRestartTarget(plan.Target)
	if err != nil {
		return 0, "target_conflict", fmt.Errorf("lock installed executable for restart update: %w", err)
	}
	defer unlock()

	payloadRel := filepath.Join(dir, restartPayloadName)
	backupRel := filepath.Join(dir, restartBackupName)
	payload := filepath.Join(root.Name(), payloadRel)
	backup := filepath.Join(root.Name(), backupRel)
	old, err := openRestartMovable(plan.Target)
	if err != nil {
		cause := fmt.Errorf("open installed executable for replacement: %w", err)
		// An antivirus/indexer or another reader may deny DELETE sharing while
		// still allowing the unchanged executable to be read and launched.
		// The GUI has exited, so recover only from a pinned, authenticated old
		// object; never launch an unverifiable or foreign replacement.
		readOnly, readErr := openRestartInput(plan.Target)
		if readErr != nil {
			return 0, "target_conflict", errors.Join(cause, readErr)
		}
		defer readOnly.Close()
		if checkErr := verifyOpenedRestartInput(ctx, readOnly, plan.OldSHA256); checkErr != nil {
			return 0, "target_conflict", errors.Join(cause, checkErr)
		}
		pid, launchErr := launchCheckedRestartTarget(plan, readOnly, cause.Error())
		if launchErr != nil {
			return 0, "target_locked_relaunch_failed", errors.Join(cause, launchErr)
		}
		return pid, "target_locked", cause
	}
	defer old.Close()
	if err := verifyOpenedRestartInput(ctx, old, plan.OldSHA256); err != nil {
		return 0, "target_conflict", fmt.Errorf("installed executable changed after preparation: %w", err)
	}
	relaunchOld := func(state string, cause error) (int, string, error) {
		pid, launchErr := launchCheckedRestartTarget(plan, old, cause.Error())
		if launchErr != nil {
			return 0, state + "_relaunch_failed", errors.Join(cause, fmt.Errorf("relaunch old executable: %w", launchErr))
		}
		return pid, state, cause
	}
	defer root.Remove(payloadRel)
	if !canonicalRestartIdentity(payload) {
		return relaunchOld("payload_rejected", errors.New("staged executable path changed after preparation"))
	}
	staged, err := openRestartMovable(payload)
	if err != nil {
		return relaunchOld("payload_rejected", fmt.Errorf("open staged executable: %w", err))
	}
	defer staged.Close()
	if err := verifyOpenedRestartInput(ctx, staged, plan.PayloadSHA256); err != nil {
		return relaunchOld("payload_rejected", fmt.Errorf("staged executable hash changed after preparation: %w", err))
	}
	if err := validateRestartPEInput(staged, runtime.GOARCH); err != nil {
		return relaunchOld("payload_rejected", err)
	}

	// Keep both verified objects open with writes and deletes denied. Rename
	// the handles themselves; every destination must still be absent. The
	// verified original remains recoverable in staging if rollback is blocked.
	if err := renameRestartInput(old, backup, false); err != nil {
		return relaunchOld("backup_failed", fmt.Errorf("back up installed executable: %w", err))
	}
	restore := func(state string, cause error) (int, string, error) {
		if err := renameRestartInput(old, plan.Target, false); err != nil {
			return 0, "rollback_failed", errors.Join(cause, fmt.Errorf("restore verified old executable from backup: %w", err))
		}
		return relaunchOld(state, cause)
	}
	if err := renameRestartInput(staged, plan.Target, false); err != nil {
		return restore("replace_failed", fmt.Errorf("replace installed executable: %w", err))
	}
	pid, err := launchCheckedRestartTarget(plan, staged, "")
	if err == nil {
		_ = old.Close()
		_ = root.Remove(backupRel)
		return pid, "updated", nil
	}
	launchErr := fmt.Errorf("launch updated executable: %w", err)
	// Only remove the exact published object, even if it cannot be loaded.
	if err := renameRestartInput(staged, payload, false); err != nil {
		return 0, "rollback_failed", errors.Join(launchErr, fmt.Errorf("move failed executable out of its installed path: %w", err))
	}
	return restore("rolled_back", launchErr)
}

func launchCheckedRestartTarget(plan restartPlan, verified *os.File, oldFailure string) (int, error) {
	if !canonicalRestartIdentity(plan.Target) || !canonicalRestartIdentity(plan.WorkingDir) {
		return 0, errors.New("relaunch paths are no longer canonical")
	}
	installed, err := os.Lstat(plan.Target)
	if err != nil {
		return 0, err
	}
	pinned, err := verified.Stat()
	if err != nil {
		return 0, err
	}
	if !installed.Mode().IsRegular() || !os.SameFile(installed, pinned) {
		return 0, errors.New("relaunch target does not match the verified executable")
	}
	cmd := exec.Command(plan.Target, plan.Args...)
	cmd.Dir = plan.WorkingDir
	configureRestartProcess(cmd)
	// Do not inherit a stale result from a previous failure. os/exec keeps
	// the final value of any duplicate key, case-insensitively on Windows.
	cmd.Env = append(os.Environ(), restartErrorEnv+"="+oldFailure)
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	pid := cmd.Process.Pid
	_ = cmd.Process.Release()
	return pid, nil
}

func (p *restartPlan) validate() error {
	if !filepath.IsAbs(p.Target) || filepath.Clean(p.Target) != p.Target || !strings.EqualFold(filepath.Ext(p.Target), ".exe") ||
		!filepath.IsAbs(p.WorkingDir) || filepath.Clean(p.WorkingDir) != p.WorkingDir ||
		!validRestartDigest(p.OldSHA256) || !validRestartDigest(p.PayloadSHA256) ||
		p.ParentPID <= 0 || uint64(p.ParentPID) > 0xffffffff || p.ParentCreated <= 0 {
		return errors.New("invalid restart plan identity, paths or digests")
	}
	for _, arg := range p.Args {
		if strings.ContainsRune(arg, '\x00') || arg == restartHelperFlag {
			return errors.New("invalid restart argument")
		}
	}
	if fi, err := os.Stat(p.WorkingDir); err != nil || !fi.IsDir() || !canonicalRestartIdentity(p.WorkingDir) {
		return errors.New("restart working directory is not a canonical directory")
	}
	return nil
}

func sameRestartPath(left, right string) bool { return strings.EqualFold(left, right) }

func canonicalRestartIdentity(path string) bool {
	canonical, err := canonicalRestartPath(path)
	return err == nil && sameRestartPath(path, canonical)
}

func openRestartDirectory(stage string) (*os.Root, string, error) {
	dir := filepath.Base(stage)
	if !strings.HasPrefix(dir, restartStagePrefix) || !canonicalRestartIdentity(stage) {
		return nil, "", errors.New("invalid restart staging directory")
	}
	root, err := os.OpenRoot(filepath.Dir(stage))
	if err != nil {
		return nil, "", err
	}
	fi, err := root.Lstat(dir)
	if err != nil || !fi.IsDir() {
		root.Close()
		return nil, "", errors.New("restart staging directory is not a real directory")
	}
	return root, dir, nil
}

func copyRestartExecutable(ctx context.Context, root *os.Root, to, from, expectedHash string) (bool, error) {
	input, err := openRestartInput(from)
	if err != nil {
		return false, err
	}
	defer input.Close()
	out, err := root.OpenFile(to, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
	if err != nil {
		return false, err
	}
	got, err := hashRestartInput(ctx, io.TeeReader(input, out))
	if err == nil && got != expectedHash {
		err = errors.New("installed executable hash changed")
	}
	if err == nil {
		err = out.Sync()
	}
	return true, errors.Join(err, out.Close())
}

func readRestartByte(r io.Reader, want byte) error {
	var b [1]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return err
	}
	if b[0] != want {
		return errors.New("unexpected restart handshake byte")
	}
	return nil
}

func writeRestartJSON(root *os.Root, name string, v any) (bool, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return false, err
	}
	if len(data) > restartPlanSizeLimit {
		return false, errors.New("restart JSON exceeds size limit")
	}
	f, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return false, err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	return true, errors.Join(err, f.Close())
}

func readRestartJSON(root *os.Root, name string, v any) error {
	f, err := root.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > restartPlanSizeLimit {
		return errors.New("invalid restart JSON file")
	}
	d := json.NewDecoder(io.LimitReader(f, restartPlanSizeLimit+1))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("unexpected trailing restart JSON")
	}
	return nil
}
