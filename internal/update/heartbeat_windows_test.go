//go:build windows

package update

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHelperHeartbeatProbe(t *testing.T) {
	for _, tc := range []struct {
		name      string
		mode      string
		wantError string
	}{
		{"ack after scheduling delay over 250ms", "delayed", ""},
		{"permanently unresponsive with stale heartbeat", "silent", "timed out waiting"},
		{"process exits without ack", "exit", "helper exited"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target, _, home := copyRunningTestBinary(t)
			path := filepath.Join(filepath.Dir(target), "heartbeat")
			if err := os.WriteFile(path, []byte("stale heartbeat"), 0o600); err != nil {
				t.Fatal(err)
			}
			childStdin, stdin, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer childStdin.Close()
			defer stdin.Close()
			cmd := exec.Command(target, "-test.run=^TestHeartbeatWindowsProbeChild$", "-test.timeout=30s")
			cmd.Stdin = childStdin
			var output bytes.Buffer
			cmd.Stdout, cmd.Stderr = &output, &output
			cmd.Env = replaceRunningBinaryEnv(os.Environ(), map[string]string{
				runningBinaryHelperEnv: "heartbeat-probe",
				runningBinaryModeEnv:   tc.mode,
				runningBinaryBeatEnv:   path,
				"HOME":                 home, "USERPROFILE": home, "XMDEV_HOME": home, "XMDEV_FORCE_HOME": "1",
				"APPDATA": filepath.Join(home, "AppData"), "LOCALAPPDATA": filepath.Join(home, "LocalAppData"),
			})
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			_ = childStdin.Close()
			exited := make(chan struct{})
			var waitErr error
			go func() { waitErr = cmd.Wait(); close(exited) }()
			defer func() {
				_ = stdin.Close()
				_ = cmd.Process.Kill()
				<-exited
			}()
			started := time.Now()
			err = probeHelperHeartbeat(stdin, path, exited, 2*time.Second)
			elapsed := time.Since(started)
			if tc.wantError == "" {
				if err != nil {
					t.Fatalf("delayed helper must acknowledge: %v", err)
				}
				if elapsed < 400*time.Millisecond {
					t.Fatalf("probe returned before delayed acknowledgement: %s", elapsed)
				}
				// A fresh nonce must require a second response, not reuse the first ack.
				if err := probeHelperHeartbeat(stdin, path, exited, 2*time.Second); err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("probe error = %v, want %q", err, tc.wantError)
			}
			t.Logf("mode=%s elapsed=%s probe error=%v", tc.mode, elapsed, err)
			_ = stdin.Close()
			select {
			case <-exited:
				if waitErr != nil {
					t.Fatalf("helper exit: %v\n%s", waitErr, output.String())
				}
			case <-time.After(5 * time.Second):
				t.Fatal("helper did not exit on stdin EOF")
			}
		})
	}
}

func TestHeartbeatWindowsProbeChild(t *testing.T) {
	if os.Getenv(runningBinaryHelperEnv) != "heartbeat-probe" {
		return
	}
	stdin := bufio.NewReader(os.Stdin)
	request, err := stdin.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	switch os.Getenv(runningBinaryModeEnv) {
	case "exit":
		return
	case "silent":
		_, _ = io.Copy(io.Discard, stdin)
		return
	case "delayed":
		time.Sleep(400 * time.Millisecond)
	default:
		t.Fatalf("unknown probe mode %q", os.Getenv(runningBinaryModeEnv))
	}
	if err := serveHelperHeartbeat(bufio.NewReader(io.MultiReader(strings.NewReader(request), stdin)), os.Getenv(runningBinaryBeatEnv)); err != nil {
		t.Fatal(err)
	}
}
