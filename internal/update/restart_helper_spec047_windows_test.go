//go:build windows

package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	spec047Parent   = "--spec047-test-parent"
	spec047Relaunch = "--spec047-test-relaunch"
	spec047Fixture  = "XDEV_SPEC047_RESTART_FIXTURE"
	spec047Cert     = "XDEV_SPEC047_RESTART_CERT"
	spec047Version  = "47.0.0"
)

type spec047ParentReport struct {
	Stage, Error, Asset string
	Phases              []string
}

type spec047LaunchReport struct {
	Executable, WorkDir, Hash, Error string
	Argv                             []string
}

// This wiring mirrors main: dispatch the two private args before parsing any
// normal options. All three roles are real Windows processes, not stubs.
func TestMain(m *testing.M) {
	if os.Getenv(spec047Fixture) != "" && len(os.Args) > 1 {
		var err error
		switch os.Args[1] {
		case restartHelperFlag:
			switch os.Getenv(spec047Fixture) {
			case "no-ready":
				os.Exit(8)
			case "parent-timeout":
				_, err = runRestartHelper(os.Args[1:], 350*time.Millisecond)
			default:
				_, err = RunRestartHelper(os.Args[1:])
			}
		case spec047Parent:
			err = spec047RunParent(os.Args[2:])
		case spec047Relaunch:
			err = spec047RunRelaunch(os.Args[2:])
		default:
			os.Exit(m.Run())
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func spec047RunParent(args []string) error {
	if len(args) != 5 {
		return fmt.Errorf("need five fixture parent arguments")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM([]byte(os.Getenv(spec047Cert))) {
		return fmt.Errorf("load fixture root certificate")
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}}
	defer transport.CloseIdleConnections()
	ctx := context.WithValue(context.Background(), networkContextKey{}, updateNetwork{transport: transport})
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	var report spec047ParentReport
	prepared, err := PrepareForRestart(ctx, spec047CheckResult(args[0], args[1], false), exe, func(p Progress) {
		if n := len(report.Phases); n == 0 || report.Phases[n-1] != p.Phase {
			report.Phases = append(report.Phases, p.Phase)
		}
	})
	if err != nil {
		return err
	}
	report.Stage, report.Asset = prepared.stage, prepared.AssetName
	opts := RestartOptions{WorkingDir: args[3], Args: []string{spec047Relaunch, args[4], `space and "quote"`, "", `trailing\`}}
	if mode := os.Getenv(spec047Fixture); mode == "invalid-prepared" {
		if err := os.Remove(filepath.Join(prepared.stage, restartPayloadName)); err != nil {
			return err
		}
	}
	if err := prepared.Restart(ctx, opts); err != nil {
		report.Error = err.Error()
		if os.Getenv(spec047Fixture) == "invalid-prepared" {
			if !strings.Contains(err.Error(), ErrPreparedUpdateInvalid.Error()) {
				return fmt.Errorf("missing invalid-prepared error: %w", err)
			}
			if err := prepared.Discard(); err != nil {
				return err
			}
		}
	} else if err := prepared.Discard(); err == nil {
		return fmt.Errorf("discard accepted after helper handoff")
	}
	if err := spec047WriteJSON(args[2], report); err != nil {
		return err
	}
	_, err = io.Copy(io.Discard, os.Stdin)
	return err
}

func spec047RunRelaunch(args []string) error {
	if len(args) != 4 {
		return fmt.Errorf("wrong fixture relaunch args: %q", args)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	workDir, err := os.Getwd()
	if err != nil {
		return err
	}
	hash, err := restartFileHash(context.Background(), exe)
	if err != nil {
		return err
	}
	return spec047WriteJSON(args[0], spec047LaunchReport{exe, workDir, hash, os.Getenv(restartErrorEnv), args[1:]})
}

func spec047WriteJSON(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func spec047AssetName(archive bool) string {
	if archive {
		return "xdev-windows-" + runtime.GOARCH + "-v" + spec047Version + ".zip"
	}
	return "xdev-windows-" + runtime.GOARCH + "-v" + spec047Version + ".exe"
}

func spec047CheckResult(baseURL, mode string, archive bool) *CheckResult {
	return &CheckResult{CurrentVersion: "46.0.0", LatestVersion: spec047Version, HasUpdate: true, Release: &ReleaseInfo{
		TagName: "v" + spec047Version,
		Assets: []Asset{
			{Name: spec047AssetName(archive), BrowserDownloadURL: baseURL + "/" + mode + "/payload", Size: 1},
			{Name: "SHA256SUMS", BrowserDownloadURL: baseURL + "/" + mode + "/sums", Size: 1},
		},
	}}
}

func spec047FixtureTLS(t *testing.T, payloads map[string][]byte, archive bool) (*httptest.Server, []byte) {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mode, suffix, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
		payload, ok := payloads[mode]
		if !ok {
			http.NotFound(w, r)
			return
		}
		switch suffix {
		case "payload":
			w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
			_, _ = w.Write(payload)
		case "sums":
			h := sha256.Sum256(payload)
			if mode == "bad-sha" {
				h[0] ^= 0xff
			}
			fmt.Fprintf(w, "%x  %s\n", h, spec047AssetName(archive))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
}

func spec047WaitJSON(t *testing.T, path string, out any) {
	t.Helper()
	deadline := time.Now().Add(12 * time.Second)
	var err error
	for time.Now().Before(deadline) {
		var data []byte
		data, err = os.ReadFile(path)
		if err == nil {
			err = json.Unmarshal(data, out)
			if err == nil {
				return
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("wait for %s: %v", path, err)
}

func spec047OwnExecutable(t *testing.T) []byte {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestSpec047PreparedRestartRealWindowsProcesses(t *testing.T) {
	oldBytes := spec047OwnExecutable(t)
	newBytes := append(bytes.Clone(oldBytes), []byte("spec047 updated fixture overlay")...)
	badExe := bytes.Clone(newBytes)
	peOffset := binary.LittleEndian.Uint32(badExe[0x3c:])
	optionalHeader := peOffset + 4 + 20
	binary.LittleEndian.PutUint32(badExe[optionalHeader+56:], 0) // Loader-invalid SizeOfImage, still a PE of this architecture.
	payloads := map[string][]byte{"success": newBytes, "corrupt": newBytes, "parent-timeout": newBytes,
		"launch-failure": badExe, "no-ready": newBytes, "invalid-prepared": newBytes, "locked-target": newBytes}
	server, cert := spec047FixtureTLS(t, payloads, false)
	for _, tc := range []struct {
		mode, helperState string
		relaunch          bool
	}{
		{"success", "updated", true},
		{"corrupt", "payload_rejected", true},
		{"parent-timeout", "parent_not_exited", false},
		{"launch-failure", "rolled_back", true},
		{"locked-target", "target_locked", true},
		{"no-ready", "", false},
		{"invalid-prepared", "", false},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "running spec047.exe")
			wd := filepath.Join(dir, "relaunch workdir")
			if err := os.Mkdir(wd, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(target, oldBytes, 0o700); err != nil {
				t.Fatal(err)
			}
			parentReport, launchedReport := filepath.Join(dir, "parent.json"), filepath.Join(dir, "relaunched.json")
			cmd := exec.Command(target, spec047Parent, server.URL, tc.mode, parentReport, wd, launchedReport)
			cmd.Dir = dir
			logPath := filepath.Join(dir, "fixture.log")
			logFile, err := os.Create(logPath)
			if err != nil {
				t.Fatal(err)
			}
			defer logFile.Close()
			cmd.Stdout, cmd.Stderr = logFile, logFile
			cmd.Env = replaceRunningBinaryEnv(os.Environ(), map[string]string{
				spec047Fixture: tc.mode, spec047Cert: string(cert), "HOME": dir, "USERPROFILE": dir,
				"XMDEV_HOME": dir, "XMDEV_FORCE_HOME": "1", "LOCALAPPDATA": dir, "APPDATA": dir,
			})
			stdin, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			defer stdin.Close()
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			parentDone := make(chan error, 1)
			go func() { parentDone <- cmd.Wait() }()
			defer func() {
				_ = stdin.Close()
				select {
				case err := <-parentDone:
					if err != nil && !t.Failed() {
						t.Error(err)
					}
				case <-time.After(3 * time.Second):
					_ = cmd.Process.Kill()
					<-parentDone
					t.Error("parent fixture did not exit")
				}
				if t.Failed() {
					logData, _ := os.ReadFile(logPath)
					t.Logf("parent fixture log: %s", logData)
				}
			}()
			var parent spec047ParentReport
			spec047WaitJSON(t, parentReport, &parent)
			if !equalStrings(parent.Phases, []string{"downloading", "verifying", "ready"}) || parent.Asset != spec047AssetName(false) {
				t.Fatalf("bad prepare phases or asset: %+v", parent)
			}
			assertRunningBinaryUnchanged(t, target, oldBytes)
			if tc.helperState == "" {
				if parent.Error == "" {
					t.Fatal("restart unexpectedly accepted")
				}
				if _, err := os.Lstat(launchedReport); !os.IsNotExist(err) {
					t.Fatalf("unexpected launch after readiness failure: %v", err)
				}
				_ = stdin.Close()
				return
			}
			if parent.Error != "" {
				t.Fatalf("restart not accepted: %s", parent.Error)
			}
			result := filepath.Join(parent.Stage, restartResultName)
			if tc.mode == "locked-target" {
				lock, err := lockRunningTargetForTest(target)
				if err != nil {
					t.Fatal(err)
				}
				defer syscall.CloseHandle(lock)
			}
			if tc.mode == "corrupt" {
				if err := os.WriteFile(filepath.Join(parent.Stage, restartPayloadName), []byte("corrupt after ready"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.mode != "parent-timeout" {
				time.Sleep(175 * time.Millisecond)
				if _, err := os.Lstat(result); !os.IsNotExist(err) {
					t.Fatalf("helper wrote a result before the parent exited: %v", err)
				}
				assertRunningBinaryUnchanged(t, target, oldBytes)
				_ = stdin.Close()
			}
			var receipt restartReceipt
			spec047WaitJSON(t, result, &receipt)
			if receipt.State != tc.helperState || (receipt.Error == "") != (tc.mode == "success") {
				t.Fatalf("unexpected helper receipt: %+v", receipt)
			}
			if tc.mode == "parent-timeout" {
				select {
				case err := <-parentDone:
					parentDone <- err
					t.Fatal("helper timeout stopped the parent")
				default:
				}
				_ = stdin.Close()
			}
			wantBytes := oldBytes
			if tc.mode == "success" {
				wantBytes = newBytes
			}
			assertRunningBinaryUnchanged(t, target, wantBytes)
			if tc.relaunch {
				var launch spec047LaunchReport
				spec047WaitJSON(t, launchedReport, &launch)
				if receipt.PID <= 0 || !sameRestartPath(launch.Executable, target) || !sameRestartPath(launch.WorkDir, wd) ||
					launch.Hash != fmt.Sprintf("%x", sha256.Sum256(wantBytes)) || !equalStrings(launch.Argv, []string{`space and "quote"`, "", `trailing\`}) ||
					(launch.Error == "") != (tc.mode == "success") {
					t.Fatalf("wrong relaunched executable, options, or error: %+v, receipt: %+v", launch, receipt)
				}
			} else if _, err := os.Lstat(launchedReport); !os.IsNotExist(err) {
				t.Fatalf("unexpected relaunch: %v", err)
			}
			t.Logf("state=%s relaunch=%t target SHA-256=%x", receipt.State, tc.relaunch, sha256.Sum256(wantBytes))
		})
	}
}
