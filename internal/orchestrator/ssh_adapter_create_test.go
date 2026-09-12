package orchestrator

import (
	"context"
	"os"
	"strings"
	"testing"
)

// Ticket 07 (audit-fixes / P0-10) guard tests: SSHAdapter.Create must consume
// EnvironmentSpec.SSHTarget when the adapter was created zero-valued via
// NewAdapter(AdapterSSH). Previously the spec target was silently dropped:
// the GUI could pass "test connection" but "create" always failed against
// the empty adapter target.
//
// The tests use pre-cancelled contexts so Create's TestConnection leg fails
// immediately without any network access — the assertion target is the
// adapter's internal target state, which is set before the connection test.

// A1: NewAdapter(AdapterSSH) + Create with a spec → the spec target is
// adopted (host, user and port all consumed).
func TestSSHAdapterCreate_ConsumesSpecTarget(t *testing.T) {
	a, err := NewAdapter(AdapterSSH)
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	ssh := a.(*SSHAdapter)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _ = ssh.Create(ctx, &EnvironmentSpec{SSHTarget: SSHTarget{
		Host: "host.example", User: "alice", Port: 2222,
	}})

	if ssh.target.Host != "host.example" || ssh.target.User != "alice" || ssh.target.Port != 2222 {
		t.Errorf("Create did not consume spec target: got %+v, want host=host.example user=alice port=2222", ssh.target)
	}
}

// A2: an explicitly constructed target (NewSSHAdapter / CLI / GUI prefill)
// must never be overwritten by a later spec — the Host guard holds.
func TestSSHAdapterCreate_KeepsExistingTarget(t *testing.T) {
	s := &SSHAdapter{target: SSHTarget{
		Host: "cli.example", User: "bob", Port: 22,
	}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _ = s.Create(ctx, &EnvironmentSpec{SSHTarget: SSHTarget{
		Host: "other.example", User: "eve", Port: 2222,
	}})

	if s.target.Host != "cli.example" || s.target.User != "bob" || s.target.Port != 22 {
		t.Errorf("existing adapter target was overwritten by spec: %+v", s.target)
	}
}

// A3: a zero-user spec gets the normalizeTarget defaults — the user
// three-step chain ($USER → $USERNAME → root) and port 22 — so it can
// never render as "@host".
func TestSSHAdapterCreate_DefaultsUserAndPort(t *testing.T) {
	wantUser := os.Getenv("USER")
	if wantUser == "" {
		wantUser = os.Getenv("USERNAME")
	}
	if wantUser == "" {
		wantUser = "root"
	}

	s := &SSHAdapter{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _ = s.Create(ctx, &EnvironmentSpec{SSHTarget: SSHTarget{Host: "host.example"}})

	if s.target.User != wantUser {
		t.Errorf("normalized user = %q, want %q (chain result)", s.target.User, wantUser)
	}
	if s.target.Port != 22 {
		t.Errorf("normalized port = %d, want 22", s.target.Port)
	}
}

// A4: an unsafe spec target is rejected and never adopted.
func TestSSHAdapterCreate_InvalidTargetRejected(t *testing.T) {
	s := &SSHAdapter{}
	_, err := s.Create(context.Background(), &EnvironmentSpec{SSHTarget: SSHTarget{
		Host: "..", User: "u",
	}})
	if err == nil {
		t.Fatal("expected validation error for '..' host")
	}
	if !strings.Contains(err.Error(), "unsafe") {
		t.Errorf("error should mention unsafe characters, got: %v", err)
	}
	if s.target.Host == ".." {
		t.Error("invalid target must not be adopted into the adapter state")
	}
}

// normalizeTarget unit guard: the defaults only fill empties and never
// override explicit values.
func TestNormalizeTarget_FillsOnlyEmpties(t *testing.T) {
	wantDefaultUser := os.Getenv("USER")
	if wantDefaultUser == "" {
		wantDefaultUser = os.Getenv("USERNAME")
	}
	if wantDefaultUser == "" {
		wantDefaultUser = "root"
	}

	got := normalizeTarget(SSHTarget{Host: "h"})
	if got.User != wantDefaultUser {
		t.Errorf("normalizeTarget user = %q, want %q", got.User, wantDefaultUser)
	}
	if got.Port != 22 {
		t.Errorf("normalizeTarget port = %d, want 22", got.Port)
	}

	kept := normalizeTarget(SSHTarget{Host: "h", User: "carol", Port: 2200})
	if kept.User != "carol" || kept.Port != 2200 {
		t.Errorf("normalizeTarget must keep explicit values, got %+v", kept)
	}
}
