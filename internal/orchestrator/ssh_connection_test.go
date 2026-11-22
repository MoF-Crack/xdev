package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRemoteRepair_SSHRestoreAllConsumers(t *testing.T) {
	repairFakeCommands(t)
	base := t.TempDir()
	for _, target := range []SSHTarget{
		{Host: "build-host.example", Port: 2222, User: "dev-user", KeyPath: filepath.Join(base, "key-path"), KnownHosts: filepath.Join(base, "strict-hosts")},
		{Host: "2001:db8::1", Port: 22, User: "dev-user"},
	} {
		a, err := NewSSHAdapter(target)
		if err != nil {
			t.Fatal(err)
		}
		inst, err := a.Create(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		home := t.TempDir()
		if err := SaveInstance(home, inst); err != nil {
			t.Fatal(err)
		}
		round, err := LoadSavedInstance(home, inst.ID)
		if err != nil {
			t.Fatal(err)
		}
		fresh, err := NewAdapterForInstance(round, "")
		if err != nil {
			t.Fatal(err)
		}
		if got := fresh.(*SSHAdapter).target; !reflect.DeepEqual(got, target) {
			t.Fatalf("target changed: %+v want %+v", got, target)
		}
		if err := fresh.Start(context.Background(), inst.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := fresh.Exec(context.Background(), inst.ID, []string{"go", "version"}); err != nil {
			t.Fatal(err)
		}
		if err := fresh.(*SSHAdapter).ConnectInteractive(context.Background(), inst.ID, []string{"go", "version"}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRemoteRepair_SSHInvalidRecordsNeverSpawn(t *testing.T) {
	repairFakeCommands(t)
	logPath := filepath.Join(t.TempDir(), "argv")
	t.Setenv("XDEV_REMOTE_FAKE_LOG", logPath)
	valid := SSHConnection{Version: 1, Host: "host-a", Port: 22, User: "user-a", KeyPath: filepath.Join(t.TempDir(), "key"), Auth: "key"}
	for _, change := range []func(*SSHConnection){
		func(c *SSHConnection) { c.Version = 2 }, func(c *SSHConnection) { c.Host = "" }, func(c *SSHConnection) { c.Port = 0 }, func(c *SSHConnection) { c.User = "" }, func(c *SSHConnection) { c.Auth = "" }, func(c *SSHConnection) { c.KeyPath = "" }, func(c *SSHConnection) { c.Auth = "session" },
	} {
		c := valid
		change(&c)
		if _, err := NewAdapterForInstance(&Instance{ID: "not-parsed", SSH: &c, Metadata: map[string]string{"adapter": "ssh"}}, ""); err == nil {
			t.Errorf("bad connection accepted: %+v", c)
		}
	}
	for _, inst := range []*Instance{
		{ID: "ssh-a-b-c-22", Addresses: []string{"host:22"}},
		{ID: "ssh-a-b-c-22", Addresses: []string{"2001:db8::1:22"}, Metadata: map[string]string{"user": "dev"}},
		{ID: "ssh-a-b-c-22", Addresses: []string{"host:22"}, Metadata: map[string]string{"user": "dev"}},
	} {
		if _, err := NewAdapterForInstance(inst, ""); err == nil {
			t.Errorf("legacy record guessed auth: %+v", inst)
		}
	}
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Error("invalid record launched a client")
	}
}

func TestRemoteRepair_SSHSessionIsBoundToConnection(t *testing.T) {
	inst := &Instance{ID: "opaque", SSH: &SSHConnection{Version: 1, Host: "host", Port: 22, User: "dev", Auth: "session"}}
	var session SSHSession
	session.Remember("home", inst, "session-value")
	if session.Password("home", inst) != "session-value" {
		t.Fatal("same connection lost session")
	}
	if session.Password("another-home", inst) != "" {
		t.Fatal("session crossed homes")
	}
	changed := *inst
	connection := *inst.SSH
	connection.Host = "other-host"
	changed.SSH = &connection
	if session.Password("home", &changed) != "" {
		t.Fatal("password reused for changed target")
	}
	var fresh SSHSession
	if fresh.Password("home", inst) != "" {
		t.Fatal("new session inherited credentials")
	}
}

func TestRemoteRepair_SSHStopDestroyRemainNoOp(t *testing.T) {
	repairFakeCommands(t)
	logPath := filepath.Join(t.TempDir(), "argv")
	t.Setenv("XDEV_REMOTE_FAKE_LOG", logPath)
	s := &SSHAdapter{}
	if err := s.Stop(context.Background(), "any-id"); err != nil {
		t.Fatal(err)
	}
	if err := s.Destroy(context.Background(), "any-id"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatal("stop/destroy ran remote command")
	}
}

func TestRemoteRepair_StoreReportsReadErrorsAndKeepsBytes(t *testing.T) {
	home := t.TempDir()
	if err := SaveInstance(home, sshLikeInstance("healthy")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "instances", "broken.json")
	writeFileT(t, path, `{"id":`)
	result := ListSavedInstancesWithDiagnostics(home)
	if len(result.Instances) != 1 || len(result.Diagnostics) != 1 || result.Diagnostics[0].Path != path {
		t.Fatalf("listing=%+v", result)
	}
	got, _ := os.ReadFile(path)
	if string(got) != `{"id":` {
		t.Fatal("damaged bytes changed")
	}
	writeFileT(t, path, "null")
	result = ListSavedInstancesWithDiagnostics(home)
	if len(result.Instances) != 1 || len(result.Diagnostics) != 1 || result.Diagnostics[0].Path != path {
		t.Fatalf("JSON null is not an instance: %+v", result)
	}
	blocked := t.TempDir()
	writeFileT(t, filepath.Join(blocked, "instances"), "not a directory")
	if got := ListSavedInstancesWithDiagnostics(blocked); len(got.Diagnostics) != 1 {
		t.Fatalf("directory failure hidden: %+v", got)
	}
}
