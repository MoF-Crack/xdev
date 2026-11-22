//go:build windows

package env

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestReportD01_PathModesKeepConfiguredShim(t *testing.T) {
	t.Cleanup(UseEnvironmentBroadcastForTesting(func() {}))
	const home = `D:\Managed`
	for _, shimDir := range []string{home + `\shims`, `E:\Custom Dispatch\bin`} {
		for _, mode := range []string{"shim", "auto", "direct"} {
			for _, placement := range []string{"missing", "last", "first", "duplicate"} {
				t.Run(mode+"/"+placement+"/"+shimDir, func(t *testing.T) {
					parts := []string{`C:\Windows\System32`, home + `\current\java\bin`, home + `\current\xdev`, home + `\install\java\1\bin`}
					switch placement {
					case "last":
						parts = append(parts, shimDir)
					case "first":
						parts = append([]string{shimDir}, parts...)
					case "duplicate":
						parts = append([]string{shimDir}, append(parts, strings.ToLower(shimDir))...)
					}
					get, restore := UseInMemoryUserPathForTesting(strings.Join(parts, ";"))
					defer restore()
					if _, err := CleanupUserPath(home, shimDir, mode); err != nil {
						t.Fatal(err)
					}
					want := []string{`C:\Windows\System32`, home + `\current\xdev`}
					if mode == "direct" {
						want = []string{`C:\Windows\System32`, home + `\current\java\bin`, home + `\current\xdev`}
					} else {
						want = append([]string{shimDir}, want...)
					}
					if got := strings.Split(get(), ";"); !reflect.DeepEqual(got, want) {
						t.Errorf("PATH entries = %q; want %q", got, want)
					}
					if changed, err := CleanupUserPath(home, shimDir, mode); err != nil || changed {
						t.Errorf("second cleanup = %v, %v; want unchanged", changed, err)
					}
				})
			}
		}
	}
}

func TestReportD02_UserPathPreservesOpaqueEntries(t *testing.T) {
	t.Cleanup(UseEnvironmentBroadcastForTesting(func() {}))
	parts := []string{
		`\\server\share\`, `\\server\share\a\\b`,
		`\\?\C:\long\..\bin`, `\\?\UNC\server\share\a\\b`,
		`\\.\PIPE\xdev`, `\??\C:\device\bin`,
		`C:\a\\b`, `C:/Mixed//目录/`, `relative\\path`, `\rooted\\path`, `%TOOLS%\\bin`,
	}
	initial := strings.Join(parts, ";")
	get, restore := UseInMemoryUserPathForTesting(initial)
	defer restore()
	if _, err := CleanupUserPath(`D:\Managed`, `E:\Custom\shims`, "direct"); err != nil {
		t.Fatal(err)
	}
	want := append([]string(nil), parts...)
	want[6] = `C:\a\b`
	want[7] = `C:/Mixed/目录/`
	if got := strings.Split(get(), ";"); !reflect.DeepEqual(got, want) {
		t.Errorf("PATH entries = %q; want %q", got, want)
	}
}

func TestReportD02_MachinePathWriteAndBroadcast(t *testing.T) {
	const opaque = `\\server\share\;\\?\C:\long\\part ;\\.\PIPE\工具;"\\?\UNC\server\share ";relative\\bin; C:\padded ;C:\%TOOLS%\\bin`
	const fixed = opaque + `;D:\bin\tools`
	current := opaque + `;D:\bin\\tools`
	writes, broadcasts := 0, 0
	var injected error
	prevRead, prevWrite := systemPathReadFn, systemPathWriteFn
	t.Cleanup(func() { systemPathReadFn, systemPathWriteFn = prevRead, prevWrite })
	systemPathReadFn = func() (string, error) { return current, nil }
	systemPathWriteFn = func(next string) error {
		writes++
		if injected != nil {
			return injected
		}
		current = next
		return nil
	}
	t.Cleanup(UseEnvironmentBroadcastForTesting(func() { broadcasts++ }))
	injected = errors.New("mock denied")
	if changed, err := CleanupSystemPath(); changed || !errors.Is(err, injected) || writes != 1 || broadcasts != 0 {
		t.Fatalf("failed write: changed=%v err=%v writes=%d broadcasts=%d", changed, err, writes, broadcasts)
	}
	injected = nil
	if changed, err := CleanupSystemPath(); !changed || err != nil || current != fixed || writes != 2 || broadcasts != 1 {
		t.Fatalf("successful write: changed=%v err=%v value=%q writes=%d broadcasts=%d", changed, err, current, writes, broadcasts)
	}
	if changed, err := CleanupSystemPath(); changed || err != nil || writes != 2 || broadcasts != 1 {
		t.Fatalf("repeat: changed=%v err=%v writes=%d broadcasts=%d", changed, err, writes, broadcasts)
	}
}

func TestReportD20_TransactionFailsClosedWithoutLock(t *testing.T) {
	lockErr := errors.New("cannot open lock")
	prev := userPathLockPathFn
	userPathLockPathFn = func() (string, error) { return "", lockErr }
	t.Cleanup(func() { userPathLockPathFn = prev })
	t.Cleanup(UseUserEnvBackendForTesting(UserEnvBackend{Read: func(string) (UserEnvSnapshot, error) {
		t.Fatal("snapshot ran without lock")
		return UserEnvSnapshot{}, nil
	}}))
	vars := map[string]string{"TEST_VAR": "new"}
	if err := ApplyUserEnvVars(vars); !errors.Is(err, lockErr) {
		t.Fatalf("transaction without lock returned %v", err)
	}
	if err := WithUserPathLock(func() error { return ApplyUserEnvVars(vars) }); err == nil {
		t.Fatal("nested transaction accepted degraded lock")
	}
}

func TestReportD20_MixedCaseGoRootCleanup(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "go", "pkg", "tool"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(UseInMemoryUserEnvForTesting(map[string]string{"GoRoot": dir}))
	notices := 0
	t.Cleanup(UseEnvironmentBroadcastForTesting(func() { notices++ }))
	if err := CleanupStaleGOROOT(); err != nil {
		t.Fatal(err)
	}
	if _, ok := GetAllUserEnvVars()["GoRoot"]; ok || notices != 1 {
		t.Fatalf("stale GoRoot still present=%v, notifications=%d", ok, notices)
	}
}

func TestReportD20_SingleAndBatchQueriesFoldWindowsNames(t *testing.T) {
	restore := UseInMemoryUserEnvForTesting(map[string]string{"Java_Home": `C:\Java`, "GoRoot": "", "CUSTOM_NAME": "raw"})
	defer restore()
	if got, err := GetUserEnvVarTrusted("JAVA_HOME"); err != nil || got != `C:\Java` {
		t.Errorf("single case-fold query = %q, %v", got, err)
	}
	if got, err := GetUserEnvVarTrusted("GOROOT"); err != nil || got != "" {
		t.Errorf("existing empty case-fold query = %q, %v", got, err)
	}
	got, err := GetUserEnvVarsBatch([]string{"JAVA_HOME", "GOROOT", "custom_name", "ABSENT"})
	want := map[string]string{"JAVA_HOME": `C:\Java`, "GOROOT": "", "custom_name": "raw", "ABSENT": ""}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("batch = %#v, %v; want %#v", got, err, want)
	}
}
