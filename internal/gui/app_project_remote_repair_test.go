package gui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/orchestrator"
)

// Fake clients execute this binary's init rather than any client/network code.
func init() {
	if os.Getenv("XDEV_REMOTE_FAKE") != "1" {
		return
	}
	name := strings.TrimSuffix(strings.ToLower(filepath.Base(os.Args[0])), ".exe")
	if name != "docker" && name != "ssh" && name != "sshpass" {
		return
	}
	if p := os.Getenv("XDEV_REMOTE_FAKE_LOG"); p != "" {
		b, _ := json.Marshal(os.Args[1:])
		_ = os.WriteFile(p, b, 0o600)
	}
	if name == "docker" && len(os.Args) > 1 && os.Args[1] != "exec" {
		os.Exit(1)
	}
	fmt.Println("ok")
	if os.Getenv("XDEV_REMOTE_FAKE_MODE") == "fail" {
		fmt.Fprintln(os.Stderr, "fixture stderr")
		os.Exit(23)
	}
	os.Exit(0)
}

func remoteRepairApp(t *testing.T) (*App, string, string) {
	t.Helper()
	dir := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"docker", "ssh", "sshpass"} {
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		p := filepath.Join(dir, name)
		if err := os.Link(exe, p); err != nil {
			b, rerr := os.ReadFile(exe)
			if rerr != nil {
				t.Fatal(rerr)
			}
			if err := os.WriteFile(p, b, 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Setenv("PATH", dir)
	t.Setenv("XDEV_REMOTE_FAKE", "1")
	t.Setenv("XDEV_REMOTE_FAKE_MODE", "")
	logPath := filepath.Join(t.TempDir(), "argv.json")
	t.Setenv("XDEV_REMOTE_FAKE_LOG", logPath)
	home := t.TempDir()
	return &App{cfg: &config.Config{Core: config.CoreConfig{XMDevDir: home}}}, home, logPath
}

func TestRemoteRepair_GUIDockerFailureKeepsOutput(t *testing.T) {
	a, _, _ := remoteRepairApp(t)
	t.Setenv("XDEV_REMOTE_FAKE_MODE", "fail")
	out, err := a.ExecRemoteInstance("docker", "fixture-container", []string{"go", "version"})
	if err == nil {
		t.Fatalf("GUI reported nonzero docker success: output=%q", out)
	}
	for _, want := range []string{"exit 23", "stdout:\nok", "stderr:\nfixture stderr"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("GUI error lost %q: %v", want, err)
		}
	}
	if !strings.Contains(out, "ok") || !strings.Contains(out, "fixture stderr") {
		t.Errorf("returned output lost: %q", out)
	}
	t.Setenv("XDEV_REMOTE_FAKE_MODE", "")
	if out, err := a.ExecRemoteInstance("docker", "fixture-container", []string{"go", "version"}); err != nil || !strings.Contains(out, "ok") {
		t.Fatalf("success control = %q, %v", out, err)
	}
}

func TestRemoteRepair_GUISSHTargetSurvivesRestart(t *testing.T) {
	a, home, logPath := remoteRepairApp(t)
	keyPath, err := filepath.Abs("keys/id_ed25519")
	if err != nil {
		t.Fatal(err)
	}
	created, err := a.CreateRemoteInstance("ssh", "", "build-host.example", 2222, "dev-user", "keys/id_ed25519", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, app := range []*App{a, {cfg: a.cfg}} {
		if _, err := app.ExecRemoteInstance("ssh", created.ID, []string{"go", "version"}); err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(logPath)
		if err != nil {
			t.Fatal(err)
		}
		var args []string
		if err := json.Unmarshal(b, &args); err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"2222", "dev-user@build-host.example", keyPath} {
			if !containsRemoteArg(args, want) {
				t.Errorf("restart lost %q: %v", want, args)
			}
		}
	}
	b, err := os.ReadFile(filepath.Join(home, "instances", created.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"version": 1`) {
		t.Errorf("missing versioned SSH record: %s", b)
	}
}

func TestRemoteRepair_GUIPasswordStaysInSession(t *testing.T) {
	a, home, logPath := remoteRepairApp(t)
	const password = "test-session-only-value"
	created, err := a.CreateRemoteInstance("ssh", "", "password-host", 0, "dev-user", "", password)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.ExecRemoteInstance("ssh", created.ID, []string{"go", "version"}); err != nil {
		t.Fatalf("same-session password: %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(home, "instances", created.ID+".json"))
	if strings.Contains(string(b), password) || strings.Contains(created.ID, password) {
		t.Fatal("password leaked to instance record/id")
	}
	args, _ := os.ReadFile(logPath)
	if strings.Contains(string(args), password) {
		t.Fatal("password leaked on argv")
	}
	_ = os.Remove(logPath)
	fresh := &App{cfg: a.cfg}
	if _, err := fresh.ExecRemoteInstance("ssh", created.ID, []string{"go", "version"}); err == nil {
		t.Error("fresh App must require password reconfiguration")
	}
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Error("missing session credentials spawned SSH")
	}
}

func TestRemoteRepair_GUILegacyUsesAddressAndUser(t *testing.T) {
	a, home, logPath := remoteRepairApp(t)
	keyPath := filepath.Join(home, "legacy-key")
	knownHosts := filepath.Join(home, "strict-hosts")
	inst := &orchestrator.Instance{ID: "cannot-split-this-id", Status: "running", Addresses: []string{"[2001:db8::1]:2222"}, Metadata: map[string]string{"adapter": "ssh", "user": "dev-user", "keyPath": keyPath, "knownHosts": knownHosts}}
	if err := orchestrator.SaveInstance(home, inst); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ExecRemoteInstance("ssh", inst.ID, []string{"go", "version"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(logPath)
	var args []string
	_ = json.Unmarshal(b, &args)
	for _, want := range []string{"dev-user@2001:db8::1", "2222", keyPath, "UserKnownHostsFile=" + knownHosts, "StrictHostKeyChecking=yes"} {
		if !containsRemoteArg(args, want) {
			t.Errorf("legacy lost %q: %v", want, args)
		}
	}
	inst.Metadata = map[string]string{"adapter": "ssh", "user": "dev-user"}
	if err := orchestrator.SaveInstance(home, inst); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(logPath)
	if _, err := a.ExecRemoteInstance("ssh", inst.ID, []string{"go", "version"}); err == nil {
		t.Error("legacy missing authentication must require reconfiguration")
	}
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Error("legacy missing authentication spawned SSH")
	}
}

func containsRemoteArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func TestRemoteRepair_GUIHealthyListAndDiagnostics(t *testing.T) {
	a, home, _ := remoteRepairApp(t)
	good := &orchestrator.Instance{ID: "healthy", Status: "running", Metadata: map[string]string{"adapter": "ssh"}}
	if err := orchestrator.SaveInstance(home, good); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "instances", "broken.json")
	bad := []byte(`{"id":`)
	if err := os.WriteFile(path, bad, 0o600); err != nil {
		t.Fatal(err)
	}
	// Reflection keeps this a behavioral red test before the new bridge exists.
	method := reflect.ValueOf(a).MethodByName("ListRemoteInstancesWithDiagnostics")
	if !method.IsValid() {
		t.Fatal("GUI does not expose healthy list plus per-file diagnostics")
	}
	values := method.Call(nil)
	b, err := json.Marshal(values[0].Interface())
	if err != nil {
		t.Fatal(err)
	}
	var snapshot struct {
		Instances   []RemoteInstanceInfo `json:"instances"`
		Diagnostics []struct {
			Path    string `json:"path"`
			Message string `json:"message"`
		} `json:"diagnostics"`
	}
	if err := json.Unmarshal(b, &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Instances) != 1 || snapshot.Instances[0].ID != "healthy" {
		t.Errorf("healthy instances = %+v", snapshot.Instances)
	}
	found := false
	for _, d := range snapshot.Diagnostics {
		if d.Path == path && d.Message != "" {
			found = true
		}
	}
	if !found {
		t.Errorf("missing broken-file diagnosis: %s", b)
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(bad) {
		t.Error("listing changed damaged original bytes")
	}
}
