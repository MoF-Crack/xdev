package orchestrator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An executable copy reads the selected files locally; no SSH client runs.
func init() {
	if os.Getenv("XDEV_REMOTE_PATH_FAKE") != "1" || strings.TrimSuffix(strings.ToLower(filepath.Base(os.Args[0])), ".exe") != "ssh" {
		return
	}
	var paths []string
	for i, arg := range os.Args[1:] {
		if arg == "-i" && i+2 < len(os.Args) {
			paths = append(paths, os.Args[i+2])
		}
		if strings.HasPrefix(arg, "UserKnownHostsFile=") {
			paths = append(paths, strings.TrimPrefix(arg, "UserKnownHostsFile="))
		}
	}
	var contents []string
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(92)
		}
		contents = append(contents, string(data))
	}
	f, err := os.OpenFile(os.Getenv("XDEV_REMOTE_PATH_LOG"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		os.Exit(93)
	}
	fmt.Fprintln(f, strings.Join(contents, "|"))
	f.Close()
	fmt.Println("ok")
	os.Exit(0)
}

func TestRemoteRepair_SSHPathsStableAcrossCWD(t *testing.T) {
	for _, factory := range []bool{false, true} {
		t.Run(fmt.Sprint(factory), func(t *testing.T) {
			repairFakeCommands(t)
			t.Setenv("XDEV_REMOTE_FAKE", "")
			t.Setenv("XDEV_REMOTE_PATH_FAKE", "1")
			base := t.TempDir()
			first := filepath.Join(base, "first")
			second := filepath.Join(base, "second")
			home := filepath.Join(base, "home")
			for _, dir := range []string{first, second} {
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				for _, file := range []string{"key", "hosts"} {
					if err := os.WriteFile(filepath.Join(dir, file), []byte(filepath.Base(dir)+"-"+file), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			journal := filepath.Join(base, "calls")
			t.Setenv("XDEV_REMOTE_PATH_LOG", journal)
			t.Chdir(first)
			target := SSHTarget{Host: "build-host", User: "dev-user", Port: 22, KeyPath: "key", KnownHosts: "hosts"}
			var a Adapter
			var err error
			var spec *EnvironmentSpec
			if factory {
				a, err = NewAdapter(AdapterSSH)
				spec = &EnvironmentSpec{SSHTarget: target}
			} else {
				a, err = NewSSHAdapter(target)
			}
			if err != nil {
				t.Fatal(err)
			}
			inst, err := a.Create(context.Background(), spec)
			if err != nil {
				t.Fatal(err)
			}
			if err = SaveInstance(home, inst); err != nil {
				t.Fatal(err)
			}
			t.Chdir(second)
			saved, err := LoadSavedInstance(home, inst.ID)
			if err != nil {
				t.Fatal(err)
			}
			restored, err := NewAdapterForInstance(saved, "")
			if err != nil {
				t.Fatal(err)
			}
			if err = restored.Start(context.Background(), inst.ID); err != nil {
				t.Fatal(err)
			}
			if _, err = restored.Exec(context.Background(), inst.ID, []string{"go", "version"}); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(journal)
			if err != nil {
				t.Fatal(err)
			}
			want := strings.Repeat("first-key|first-hosts\n", 3)
			if string(data) != want {
				t.Fatalf("restored credential files changed: %q want %q", data, want)
			}
			if saved.SSH.KeyPath != filepath.Join(first, "key") || saved.SSH.KnownHosts != filepath.Join(first, "hosts") {
				t.Fatalf("persisted paths not anchored: %+v", saved.SSH)
			}
		})
	}
}

func TestRemoteRepair_SSHStoredRelativePathsRequireReconfiguration(t *testing.T) {
	repairFakeCommands(t)
	absolute := filepath.Join(t.TempDir(), "key")
	for _, legacy := range []bool{false, true} {
		for _, field := range []string{"key", "hosts"} {
			t.Run(fmt.Sprintf("legacy=%v/%s", legacy, field), func(t *testing.T) {
				c := SSHConnection{Version: 1, Host: "host", Port: 22, User: "dev", Auth: "key", KeyPath: absolute}
				if field == "key" {
					c.KeyPath = "relative-key"
				} else {
					c.KnownHosts = "relative-hosts"
				}
				inst := &Instance{ID: "opaque", SSH: &c, Addresses: []string{"host:22"}, Metadata: map[string]string{"adapter": "ssh", "user": "dev", "keyPath": c.KeyPath, "knownHosts": c.KnownHosts}}
				if legacy {
					inst.SSH = nil
				}
				_, err := NewAdapterForInstance(inst, "")
				if err == nil || !strings.Contains(err.Error(), "reconfigure") {
					t.Fatalf("unstable saved path must require reconfiguration: %v", err)
				}
			})
		}
	}
	// Empty paths still select the existing agent/client-config contract.
	c := &SSHConnection{Version: 1, Host: "host", Port: 22, User: "dev", Auth: "agent"}
	a, err := NewAdapterForInstance(&Instance{SSH: c}, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := a.(*SSHAdapter).target; got.KeyPath != "" || got.KnownHosts != "" {
		t.Fatalf("changed implicit agent paths: %+v", got)
	}
}
