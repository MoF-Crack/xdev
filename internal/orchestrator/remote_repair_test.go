package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Copies of this test executable act as docker/ssh/sshpass. No client, daemon,
// shell or network is used. init runs before the test harness/host snapshots.
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
	if mode := os.Getenv("XDEV_REMOTE_FAKE_MODE"); mode == "wait" {
		fmt.Fprintln(os.Stderr, "before cancellation")
		for {
			time.Sleep(time.Second)
		}
	} else if mode == "fail" {
		fmt.Fprintln(os.Stderr, "fixture stderr")
		os.Exit(23)
	}
	os.Exit(0)
}

func repairFakeCommands(t *testing.T) string {
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
	return dir
}

func TestRemoteRepair_DockerExecOutcome(t *testing.T) {
	repairFakeCommands(t)
	for _, mode := range []string{"", "fail", "wait", "missing"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("XDEV_REMOTE_FAKE_MODE", mode)
			ctx := context.Background()
			if mode == "wait" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 800*time.Millisecond)
				defer cancel()
			}
			if mode == "missing" {
				t.Setenv("PATH", t.TempDir())
			}
			res, err := (&DockerAdapter{}).Exec(ctx, "fixture-container", []string{"go", "version"})
			if res == nil {
				t.Fatal("missing execution result")
			}
			if mode == "" {
				if err != nil || res.ExitCode != 0 || res.Stdout != "ok\n" {
					t.Fatalf("success = %+v, %v", res, err)
				}
				return
			}
			if err == nil || res.ExitCode == 0 {
				t.Errorf("failure reported success: result=%+v err=%v", res, err)
			}
			if mode == "fail" && (res.ExitCode != 23 || !strings.Contains(res.Stderr, "fixture stderr") || !strings.Contains(res.Stdout, "ok")) {
				t.Errorf("lost failure output/exit: %+v", res)
			}
			if mode == "wait" && (!strings.Contains(res.Stdout, "ok") || !strings.Contains(res.Stderr, "before cancellation")) {
				t.Errorf("lost pre-cancellation output: %+v", res)
			}
		})
	}
}

func TestRemoteRepair_SSHCreatePersistsConnection(t *testing.T) {
	repairFakeCommands(t)
	target := SSHTarget{Host: "build-host.example", User: "dev-user", Port: 2222, KeyPath: "keys/id_ed25519", KnownHosts: "keys/known_hosts"}
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
	b, err := os.ReadFile(filepath.Join(home, "instances", inst.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	connection, _ := doc["ssh"].(map[string]any)
	target.KeyPath, err = filepath.Abs(target.KeyPath)
	if err != nil {
		t.Fatal(err)
	}
	target.KnownHosts, err = filepath.Abs(target.KnownHosts)
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]any{"version": float64(1), "host": target.Host, "port": float64(2222), "user": target.User, "keyPath": target.KeyPath, "knownHosts": target.KnownHosts, "auth": "key"} {
		if connection[key] != want {
			t.Errorf("SSH record %s=%v want %v; JSON=%s", key, connection[key], want, b)
		}
	}
	if strings.Contains(inst.ID, target.Host) || strings.Contains(inst.ID, target.User) {
		t.Errorf("new SSH id should be opaque: %s", inst.ID)
	}
}

func TestRemoteRepair_EmptySSHTargetFailsBeforeSpawn(t *testing.T) {
	repairFakeCommands(t)
	logPath := filepath.Join(t.TempDir(), "argv.json")
	t.Setenv("XDEV_REMOTE_FAKE_LOG", logPath)
	a, _ := NewAdapter(AdapterSSH)
	if _, err := a.Exec(context.Background(), "ambiguous-user-host-id", []string{"go", "version"}); err == nil {
		t.Error("empty target executed")
	}
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Errorf("SSH launched without target; stat=%v", err)
	}
}

func TestRemoteRepair_SaveInstanceConcurrentReaders(t *testing.T) {
	home := t.TempDir()
	inst := sshLikeInstance("atomic-fixture")
	inst.Metadata["payload"] = strings.Repeat(inst.Status+";", 8192)
	if err := SaveInstance(home, inst); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	stop := make(chan struct{})
	observed := make(chan error, 1)
	go func() {
		defer close(observed)
		close(started)
		for {
			select {
			case <-stop:
				return
			default:
			}
			v, err := LoadSavedInstance(home, inst.ID)
			if err != nil {
				observed <- err
				return
			}
			if v.ID != inst.ID || v.Metadata["payload"] != strings.Repeat(v.Status+";", 8192) {
				observed <- fmt.Errorf("reader saw fields from different versions: status %q", v.Status)
				return
			}
		}
	}()
	<-started
	var wg sync.WaitGroup
	for w := 0; w < 2; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				next := *inst
				next.Status = strconv.Itoa(w*20 + i)
				next.Metadata = map[string]string{"adapter": "ssh", "payload": strings.Repeat(next.Status+";", 8192)}
				if err := SaveInstance(home, &next); err != nil {
					t.Errorf("writer: %v", err)
					return
				}
			}
		}(w)
	}
	wg.Wait()
	close(stop)
	if err := <-observed; err != nil {
		t.Error(err)
	}
}

// Run with a util.AtomicWriteFile overlay that returns an injected failure at
// each real write/sync/close/rename boundary. This is fault injection, not a crash.
func TestRemoteRepair_SaveInjectedAtomicFailure(t *testing.T) {
	stage := os.Getenv("XDEV_REMOTE_ATOMIC_FAIL")
	if stage == "" {
		t.Skip("stage injection is run by the evidence command")
	}
	home := t.TempDir()
	inst := sshLikeInstance("atomic-failure")
	path := filepath.Join(home, "instances", inst.ID+".json")
	writeFileT(t, path, `{"id":"atomic-failure","status":"old"}`)
	old, _ := os.ReadFile(path)
	err := SaveInstance(home, inst)
	if err == nil || !strings.Contains(err.Error(), "injected "+stage) {
		t.Errorf("stage %s was not propagated: %v", stage, err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(old) {
		t.Errorf("failed publication changed old bytes: %s", got)
	}
	files, _ := os.ReadDir(filepath.Dir(path))
	for _, e := range files {
		if e.Name() != filepath.Base(path) {
			t.Errorf("left uncommitted temporary file: %s", e.Name())
		}
	}
}
