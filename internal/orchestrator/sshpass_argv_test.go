package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Ticket 02 (audit-fixes / P0-01 + P1-32 + N-05) guard tests.
//
// sshPassArgv is the single source for the sshpass argv prefix. The historical
// bug passed {"-e", "SSHPASS", sshBin, ...} — the bare "SSHPASS" word is
// parsed by sshpass as the command to run (sshpass 1.06–1.10), making every
// password login fail with "Failed to run command".

// S1: exact argv order, and never a bare "SSHPASS" element.
func TestSSHPassArgv_ExactOrderNoBareSSHPASS(t *testing.T) {
	got := sshPassArgv("ssh", []string{"-p", "22", "u@h"})
	want := []string{"-e", "ssh", "-p", "22", "u@h"}
	if len(got) != len(want) {
		t.Fatalf("sshPassArgv = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("sshPassArgv[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
	for _, a := range got {
		if a == "SSHPASS" {
			t.Errorf(`argv must never contain a bare "SSHPASS" element (got %v)`, got)
		}
	}
}

// S2: no ssh args at all (ConnectInteractive-style single trailing command).
func TestSSHPassArgv_EmptyArgs(t *testing.T) {
	got := sshPassArgv("ssh", nil)
	want := []string{"-e", "ssh"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("sshPassArgv(ssh, nil) = %v, want %v", got, want)
	}
}

// S3: Exec password path — ExitCode and stderr must reflect the real
// process outcome. A fake sshpass that exits 7 with "boom" on stderr is
// injected via PATH (no real network access happens: the fake replaces
// the outer command entirely).
func TestExec_PasswordPath_ExitCodeAndStderr(t *testing.T) {
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		script := "@echo off\r\necho boom 1>&2\r\nexit /b 7\r\n"
		if err := os.WriteFile(filepath.Join(dir, "sshpass.cmd"), []byte(script), 0o644); err != nil {
			t.Fatal(err)
		}
	} else {
		script := "#!/bin/sh\necho boom >&2\nexit 7\n"
		p := filepath.Join(dir, "sshpass")
		if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	s := &SSHAdapter{target: SSHTarget{
		Host:     "test-host",
		Port:     22,
		User:     "u",
		Password: "secret",
	}}
	res, err := s.Exec(context.Background(), "ssh-id", []string{"echo", "hi"})
	if err == nil {
		t.Fatal("expected error from exit-7 mock sshpass")
	}
	if res == nil {
		t.Fatal("expected non-nil ExecResult alongside the error")
	}
	if res.ExitCode != 7 {
		t.Errorf("ExitCode = %d, want 7 (previously hard-coded to 0)", res.ExitCode)
	}
	if !strings.Contains(res.Stderr, "boom") {
		t.Errorf("Stderr = %q, want contains %q (previously discarded)", res.Stderr, "boom")
	}
}

// S4: Exec key (no-password) path — the RunCommand error must propagate
// (N-05 regression guard: it used to be discarded, so a failed remote
// command looked like "success with empty output"). A pre-cancelled
// context makes the failure deterministic without touching the network.
func TestExec_KeyPath_ErrorPropagates(t *testing.T) {
	s := &SSHAdapter{target: SSHTarget{
		Host: "test-host",
		Port: 22,
		User: "u",
	}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := s.Exec(ctx, "ssh-id", []string{"echo", "hi"})
	if err == nil {
		t.Fatal("expected propagated error from key path (was swallowed before the fix)")
	}
	if res == nil {
		t.Fatal("expected non-nil ExecResult alongside the error")
	}
}
