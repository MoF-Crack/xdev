//go:build windows

package update

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	runningBinaryHelperEnv  = "XDEV_RUNNING_BINARY_TEST_HELPER"
	runningBinaryModeEnv    = "XDEV_RUNNING_BINARY_TEST_MODE"
	runningBinaryReportEnv  = "XDEV_RUNNING_BINARY_TEST_REPORT"
	runningBinaryBeatEnv    = "XDEV_RUNNING_BINARY_TEST_HEARTBEAT"
	runningBinaryProxyEnv   = "XDEV_RUNNING_BINARY_TEST_PROXY"
	runningBinaryRootEnv    = "XDEV_RUNNING_BINARY_TEST_ROOT"
	runningBinaryHomeEnv    = "XDEV_RUNNING_BINARY_TEST_HOME"
	runningBinaryVersionEnv = "XDEV_RUNNING_BINARY_TEST_VERSION"

	// The optional external-artifact path is deliberately pinned to the RC1
	// release payload supplied for this regression. Normal CI never needs it.
	runningBinaryRC1SHA256  = "b392d6c41d9a323e11a28d5c19805e2227a3d0dbb34a8db4058c309576c09b47"
	runningBinaryRC1Version = "1.0.0-rc.1"
)

type runningBinaryReport struct {
	State  string   `json:"state"`
	Error  string   `json:"error,omitempty"`
	PID    int      `json:"pid"`
	Target string   `json:"target"`
	Phases []string `json:"phases"`
}

type runningBinaryPayload struct {
	archive      []byte
	version      string
	runArgs      []string
	outputMarker string
}

// TestApplyToWithProgressReplacesRunningWindowsPE uses an actual Go PE as the
// running updater process. It keeps that process alive after Apply returns,
// verifies the target bytes changed before the process exits, and then starts
// the replacement from the same xdev.exe path.
func TestApplyToWithProgressReplacesRunningWindowsPE(t *testing.T) {
	payload := runningBinaryTestPayload(t)
	fixture := newUpdateProxyFixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/archive":
			w.Header().Set("Content-Length", fmt.Sprint(len(payload.archive)))
			_, _ = w.Write(payload.archive)
		case "/sums":
			h := sha256.Sum256(payload.archive)
			if r.URL.Query().Get("case") == "hash-fail" {
				h[0] ^= 0xff
			}
			version := r.URL.Query().Get("version")
			if version == "" {
				version = payload.version
			}
			_, _ = fmt.Fprintf(w, "%x  %s\n", h, assetNameForTarget(version, "windows", runtime.GOARCH))
		default:
			http.NotFound(w, r)
		}
	})

	cert, err := x509.ParseCertificate(fixture.tls.TLS.Certificates[0].Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	rootPath := filepath.Join(t.TempDir(), "fixture-root.pem")
	if err := os.WriteFile(rootPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("successful live replacement and post-exit execution", func(t *testing.T) {
		target, oldBytes, home := copyRunningTestBinary(t)
		report := runRunningBinaryHelper(t, fixture, rootPath, target, home, "success", false, func(report runningBinaryReport) {
			newBytes, err := os.ReadFile(target)
			if err != nil {
				t.Fatalf("read replacement while old process %d is alive: %v", report.PID, err)
			}
			zr, err := zip.NewReader(bytes.NewReader(payload.archive), int64(len(payload.archive)))
			if err != nil {
				t.Fatal(err)
			}
			file, err := zr.Open(binaryNameInArchive())
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			want, err := io.ReadAll(file)
			if err != nil || !bytes.Equal(newBytes, want) || bytes.Equal(oldBytes, newBytes) {
				t.Fatalf("live replacement must equal the verified ZIP's PE and differ from the old process: %v", err)
			}
			t.Logf("old PID %d is alive; target SHA-256=%x matches ZIP executable", report.PID, sha256.Sum256(newBytes))
		})
		if report.State != "ok" {
			t.Fatalf("ApplyToWithProgress failed while the old PE was running: %s", report.Error)
		}
		wantPhases := []string{"downloading", "verifying", "extracting", "installing", "completed"}
		if !equalStrings(report.Phases, wantPhases) {
			t.Fatalf("successful update phases = %v, want %v", report.Phases, wantPhases)
		}
		if filepath.Base(report.Target) != binaryNameInArchive() {
			t.Fatalf("helper updated %q, want basename %q", report.Target, binaryNameInArchive())
		}
		newBytes, err := os.ReadFile(target)
		if err != nil {
			t.Fatalf("read replacement after old process exit: %v", err)
		}
		if bytes.Equal(oldBytes, newBytes) {
			t.Fatal("replacement bytes equal the old running test executable")
		}
		if len(newBytes) == 0 {
			t.Fatal("replacement executable is empty")
		}
		if err := runInstalledFixture(t, target, home, payload.runArgs, payload.outputMarker); err != nil {
			t.Fatalf("replacement did not execute from the original path after old process exit: %v", err)
		}
	})

	t.Run("checksum failure preserves running executable", func(t *testing.T) {
		target, oldBytes, home := copyRunningTestBinary(t)
		report := runRunningBinaryHelper(t, fixture, rootPath, target, home, "hash-fail", false, func(report runningBinaryReport) {
			assertRunningBinaryUnchanged(t, target, oldBytes)
			t.Logf("old PID %d is alive; checksum failure preserved the original executable", report.PID)
		})
		if report.State != "error" || !strings.Contains(report.Error, "SHA256 verification failed") || !strings.Contains(report.Error, "hash mismatch") {
			t.Fatalf("checksum failure report = %+v", report)
		}
		assertRunningBinaryUnchanged(t, target, oldBytes)
		if containsString(report.Phases, "completed") {
			t.Fatalf("checksum failure reported completion: %v", report.Phases)
		}
	})

	t.Run("locked target replacement failure preserves bytes", func(t *testing.T) {
		target, oldBytes, home := copyRunningTestBinary(t)
		report := runRunningBinaryHelper(t, fixture, rootPath, target, home, "locked", true, func(report runningBinaryReport) {
			assertRunningBinaryUnchanged(t, target, oldBytes)
			t.Logf("old PID %d is alive; locked-target failure preserved the original executable", report.PID)
		})
		if report.State != "error" || !strings.Contains(report.Error, "replace binary:") {
			t.Fatalf("expected a concrete locked-target replace error, got %+v", report)
		}
		if !containsString(report.Phases, "installing") || containsString(report.Phases, "completed") {
			t.Fatalf("locked-target failure did not retain truthful install phase: %v", report.Phases)
		}
		assertRunningBinaryUnchanged(t, target, oldBytes)
	})
}

// Optional integration with the caller-provided official RC1 ZIP. Ordinary CI
// uses a tiny locally built PE fixture and has no dependency on this file.
func runningBinaryTestPayload(t *testing.T) runningBinaryPayload {
	t.Helper()
	if zipPath := os.Getenv("XDEV_TEST_UPDATE_ZIP"); zipPath != "" {
		version := os.Getenv("XDEV_TEST_UPDATE_EXPECTED_VERSION")
		if version != runningBinaryRC1Version {
			t.Fatalf("XDEV_TEST_UPDATE_EXPECTED_VERSION must be %q when XDEV_TEST_UPDATE_ZIP is set", runningBinaryRC1Version)
		}
		archive, err := os.ReadFile(zipPath)
		if err != nil {
			t.Fatalf("read optional RC1 ZIP %q: %v", zipPath, err)
		}
		digest := sha256.Sum256(archive)
		if got := hex.EncodeToString(digest[:]); got != runningBinaryRC1SHA256 {
			t.Fatalf("optional RC1 ZIP SHA-256 = %s, want pinned digest %s", got, runningBinaryRC1SHA256)
		}
		zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			t.Fatalf("open optional RC1 ZIP: %v", err)
		}
		found := false
		for _, f := range zr.File {
			if f.Name == binaryNameInArchive() {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("optional RC1 ZIP has no root %s", binaryNameInArchive())
		}
		return runningBinaryPayload{archive: archive, version: version, runArgs: []string{"version"}, outputMarker: "xdev " + version}
	}

	temp := t.TempDir()
	source := filepath.Join(temp, "fixture.go")
	const sourceText = `package main
import "fmt"
func main() { fmt.Println("UPDATED_FIXTURE_VERSION=2.0.0-live-test") }
`
	if err := os.WriteFile(source, []byte(sourceText), 0o600); err != nil {
		t.Fatal(err)
	}
	payloadPath := filepath.Join(temp, binaryNameInArchive())
	goExe := filepath.Join(runtime.GOROOT(), "bin", "go.exe")
	if _, err := os.Stat(goExe); err != nil {
		t.Fatalf("find Go compiler for executable PE fixture at %q: %v", goExe, err)
	}
	cmd := exec.Command(goExe, "build", "-trimpath", "-o", payloadPath, source)
	cmd.Dir = temp
	cmd.Env = replaceRunningBinaryEnv(os.Environ(), map[string]string{
		"GOFLAGS":     "",
		"GOTOOLCHAIN": "local",
	})
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build replacement PE fixture: %v\n%s", err, output)
	}
	newPE, err := os.ReadFile(payloadPath)
	if err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	entry, err := zw.Create(binaryNameInArchive())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write(newPE); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return runningBinaryPayload{archive: archive.Bytes(), version: "1.0.0-beta.2", outputMarker: "UPDATED_FIXTURE_VERSION=2.0.0-live-test"}
}

func copyRunningTestBinary(t *testing.T) (target string, oldBytes []byte, home string) {
	t.Helper()
	work := t.TempDir()
	target = filepath.Join(work, binaryNameInArchive())
	source, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	oldBytes, err = os.ReadFile(source)
	if err != nil {
		t.Fatalf("read Go test executable: %v", err)
	}
	if err := os.WriteFile(target, oldBytes, 0o700); err != nil {
		t.Fatalf("copy test executable to isolated xdev.exe: %v", err)
	}
	home = filepath.Join(work, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	return target, oldBytes, home
}

func runRunningBinaryHelper(t *testing.T, fixture *updateProxyFixture, rootPath, target, home, mode string, lockTarget bool, verifyWhileAlive func(runningBinaryReport)) runningBinaryReport {
	t.Helper()
	reportPath := filepath.Join(filepath.Dir(target), "helper-report.json")
	heartbeatPath := filepath.Join(filepath.Dir(target), "helper-heartbeat")
	childStdin, stdin, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(target, "-test.run=^TestRunningBinaryWindowsHelper$", "-test.timeout=2m")
	cmd.Dir = filepath.Dir(target)
	cmd.Stdin = childStdin
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.Env = replaceRunningBinaryEnv(os.Environ(), map[string]string{
		runningBinaryHelperEnv:  "1",
		runningBinaryModeEnv:    mode,
		runningBinaryReportEnv:  reportPath,
		runningBinaryBeatEnv:    heartbeatPath,
		runningBinaryProxyEnv:   fixture.proxy.URL,
		runningBinaryRootEnv:    rootPath,
		runningBinaryHomeEnv:    home,
		runningBinaryVersionEnv: runningBinaryTestVersionFromMode(mode),
		"XMDEV_HOME":            home,
		"XMDEV_FORCE_HOME":      "1",
		"HOME":                  home,
		"USERPROFILE":           home,
		"LOCALAPPDATA":          filepath.Join(home, "LocalAppData"),
		"APPDATA":               filepath.Join(home, "AppData"),
	})
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		t.Fatalf("start copied running xdev.exe helper: %v", err)
	}
	finished := false
	defer func() {
		_ = stdin.Close()
		_ = childStdin.Close()
		if !finished {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()

	if lockTarget {
		report := waitRunningBinaryReport(t, reportPath, "ready", cmd, &stdout, &stderr)
		if report.PID != cmd.Process.Pid {
			t.Fatalf("ready helper PID = %d, process PID = %d", report.PID, cmd.Process.Pid)
		}
		lock, lockErr := lockRunningTargetForTest(target)
		if lockErr != nil {
			t.Fatalf("hold target without delete sharing to force a Windows replace denial: %v", lockErr)
		}
		if _, err := io.WriteString(stdin, "go\n"); err != nil {
			_ = syscall.CloseHandle(lock)
			t.Fatalf("release helper to attempt locked replacement: %v", err)
		}
		report = waitRunningBinaryReport(t, reportPath, "error", cmd, &stdout, &stderr)
		_ = syscall.CloseHandle(lock)
		assertHelperHeartbeat(t, heartbeatPath)
		verifyWhileAlive(report)
		if err := stdin.Close(); err != nil {
			t.Errorf("close helper stdin: %v", err)
		}
		if err := cmd.Wait(); err != nil {
			t.Fatalf("running helper exit: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
		}
		finished = true
		return report
	}

	report := waitRunningBinaryReport(t, reportPath, "", cmd, &stdout, &stderr)
	assertHelperHeartbeat(t, heartbeatPath)
	if report.PID != cmd.Process.Pid {
		t.Fatalf("helper PID = %d, process PID = %d", report.PID, cmd.Process.Pid)
	}
	verifyWhileAlive(report)
	if err := stdin.Close(); err != nil {
		t.Errorf("close helper stdin: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("running helper exit: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	finished = true
	return report
}

func TestRunningBinaryWindowsHelper(t *testing.T) {
	if os.Getenv(runningBinaryHelperEnv) != "1" {
		return
	}
	mode := os.Getenv(runningBinaryModeEnv)
	reportPath := os.Getenv(runningBinaryReportEnv)
	heartbeatPath := os.Getenv(runningBinaryBeatEnv)
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("helper os.Executable: %v", err)
	}
	if filepath.Base(exe) != binaryNameInArchive() {
		t.Fatalf("helper executable basename = %q, want %q", filepath.Base(exe), binaryNameInArchive())
	}
	if mode == "locked" {
		if err := writeRunningBinaryReport(reportPath, runningBinaryReport{State: "ready", PID: os.Getpid(), Target: exe}); err != nil {
			t.Fatal(err)
		}
		if _, err := bufio.NewReader(os.Stdin).ReadString('\n'); err != nil {
			t.Fatalf("wait for parent to hold target lock: %v", err)
		}
	}
	transport, err := runningBinaryFixtureTransport()
	if err != nil {
		t.Fatalf("configure isolated trusted fixture transport: %v", err)
	}
	http.DefaultTransport = transport
	defer transport.CloseIdleConnections()

	version := os.Getenv(runningBinaryVersionEnv)
	archiveName := assetNameForTarget(version, "windows", runtime.GOARCH)
	caseName := mode
	assetURL := "https://github.com/archive?case=" + url.QueryEscape(caseName) + "&version=" + url.QueryEscape(version)
	sumsURL := "https://github.com/sums?case=" + url.QueryEscape(caseName) + "&version=" + url.QueryEscape(version)
	result := &CheckResult{CurrentVersion: "1.0.0-beta.1", LatestVersion: version, HasUpdate: true, Release: &ReleaseInfo{
		TagName: "v" + version, Prerelease: true, Assets: []Asset{
			{Name: archiveName, BrowserDownloadURL: assetURL, Size: 1, State: "uploaded"},
			{Name: "SHA256SUMS", BrowserDownloadURL: sumsURL, Size: 64 + 2 + int64(len(archiveName)) + 1, State: "uploaded"},
		},
	}}
	var phases []string
	var lastPhase string
	applyErr := ApplyToWithProgress(context.Background(), result, exe, func(p Progress) {
		if p.Phase != lastPhase {
			phases = append(phases, p.Phase)
			lastPhase = p.Phase
		}
	})
	if mode == "success" && applyErr != nil {
		t.Fatalf("ApplyToWithProgress: %v", applyErr)
	}
	if mode != "success" && applyErr == nil {
		t.Fatal("ApplyToWithProgress unexpectedly succeeded")
	}
	// Publish liveness before the report so the parent can check the old helper
	// process has remained alive across replacement without a scheduler race.
	if err := os.WriteFile(heartbeatPath, []byte(time.Now().UTC().Format(time.RFC3339Nano)), 0o600); err != nil {
		t.Fatalf("publish helper liveness: %v", err)
	}
	state := "ok"
	errText := ""
	if applyErr != nil {
		state, errText = "error", applyErr.Error()
	}
	if err := writeRunningBinaryReport(reportPath, runningBinaryReport{State: state, Error: errText, PID: os.Getpid(), Target: exe, Phases: phases}); err != nil {
		t.Fatalf("write update result: %v", err)
	}

	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, os.Stdin)
		close(done)
	}()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case now := <-ticker.C:
			if err := os.WriteFile(heartbeatPath, []byte(now.UTC().Format(time.RFC3339Nano)), 0o600); err != nil {
				t.Fatalf("write liveness heartbeat: %v", err)
			}
		}
	}
}

func runningBinaryFixtureTransport() (*http.Transport, error) {
	rootPEM, err := os.ReadFile(os.Getenv(runningBinaryRootEnv))
	if err != nil {
		return nil, err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(rootPEM) {
		return nil, fmt.Errorf("fixture certificate was not accepted as a root")
	}
	proxyURL, err := url.Parse(os.Getenv(runningBinaryProxyEnv))
	if err != nil || proxyURL.Scheme != "http" || proxyURL.Host == "" {
		return nil, fmt.Errorf("invalid test proxy endpoint")
	}
	return &http.Transport{Proxy: http.ProxyURL(proxyURL), TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}, nil
}

func writeRunningBinaryReport(path string, report runningBinaryReport) error {
	data, err := json.Marshal(report)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func waitRunningBinaryReport(t *testing.T, path, expected string, cmd *exec.Cmd, stdout, stderr *bytes.Buffer) runningBinaryReport {
	t.Helper()
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			var report runningBinaryReport
			if json.Unmarshal(data, &report) == nil {
				if expected == "" && report.State != "ready" || expected != "" && report.State == expected {
					return report
				}
				if report.State == "ready" && expected == "" {
					t.Fatalf("helper paused unexpectedly: %+v", report)
					return report
				}
				if report.State == "error" && expected == "" {
					t.Fatalf("helper update failed: %s\nstdout:\n%s\nstderr:\n%s", report.Error, stdout.String(), stderr.String())
					return report
				}
				if report.State == "ok" && expected != "" {
					t.Fatalf("helper unexpectedly succeeded while failure was expected: %+v", report)
					return report
				}
			}
		}
		if cmd.ProcessState != nil {
			t.Fatalf("helper exited before publishing %q report\nstdout:\n%s\nstderr:\n%s", expected, stdout.String(), stderr.String())
			return runningBinaryReport{}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for helper report %q\nstdout:\n%s\nstderr:\n%s", expected, stdout.String(), stderr.String())
	return runningBinaryReport{}
}

func assertHelperHeartbeat(t *testing.T, path string) {
	t.Helper()
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("helper did not publish liveness heartbeat after update: %v", err)
	}
	time.Sleep(250 * time.Millisecond)
	second, err := os.ReadFile(path)
	if err != nil || bytes.Equal(first, second) {
		t.Fatalf("old updater process was not demonstrably alive after replace: before=%q after=%q err=%v", first, second, err)
	}
}

func lockRunningTargetForTest(path string) (syscall.Handle, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	return syscall.CreateFile(p, syscall.GENERIC_READ, syscall.FILE_SHARE_READ, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
}

func assertRunningBinaryUnchanged(t *testing.T, target string, before []byte) {
	t.Helper()
	after, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("failed update removed original executable: %v", err)
	}
	if !bytes.Equal(before, after) {
		beforeHash, afterHash := sha256.Sum256(before), sha256.Sum256(after)
		t.Fatalf("failed update changed executable bytes: before=%x after=%x", beforeHash, afterHash)
	}
}

func runInstalledFixture(t *testing.T, target, home string, args []string, marker string) error {
	t.Helper()
	cmd := exec.Command(target, args...)
	cmd.Dir = filepath.Dir(target)
	cmd.Env = replaceRunningBinaryEnv(os.Environ(), map[string]string{
		"XMDEV_HOME":       home,
		"XMDEV_FORCE_HOME": "1",
		"HOME":             home,
		"USERPROFILE":      home,
		"LOCALAPPDATA":     filepath.Join(home, "LocalAppData"),
		"APPDATA":          filepath.Join(home, "AppData"),
	})
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("exec %s %v: %w\n%s", target, args, err, output)
	}
	if !strings.Contains(string(output), marker) {
		return fmt.Errorf("exec %s %v output %q does not contain marker %q", target, args, output, marker)
	}
	return nil
}

func replaceRunningBinaryEnv(base []string, overrides map[string]string) []string {
	out := make([]string, 0, len(base)+len(overrides))
	for _, entry := range base {
		key, _, ok := strings.Cut(entry, "=")
		if ok {
			if _, replaced := overrides[key]; replaced {
				continue
			}
		}
		out = append(out, entry)
	}
	for key, value := range overrides {
		out = append(out, key+"="+value)
	}
	return out
}

func runningBinaryTestVersionFromMode(mode string) string {
	if version := os.Getenv("XDEV_TEST_UPDATE_EXPECTED_VERSION"); version != "" {
		return version
	}
	if mode == "hash-fail" || mode == "locked" {
		return "1.0.0-beta.2"
	}
	return "1.0.0-beta.2"
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
