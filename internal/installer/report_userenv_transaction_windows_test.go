//go:build windows

package installer

import (
	"errors"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/env"
)

// Exercise the installer entry point, including its whitelist, with registry-like
// names and distinct SZ/EXPAND_SZ values. A failing write is atomic like RegSetValueEx.
func TestReportD20_InstallerUserEnvTransaction(t *testing.T) {
	names := []string{"DENO_DIR", "JAVA_HOME", "PUB_CACHE", "UV_CACHE_DIR"}
	initial := map[string]env.UserEnvSnapshot{
		"DENO_DIR":     {Name: "Deno_Dir", Exists: true, Raw: `%MY_VAR%\\old`, Type: env.UserEnvString},
		"JAVA_HOME":    {Name: "Java_Home", Exists: true, Raw: "", Type: env.UserEnvExpandString},
		"UV_CACHE_DIR": {Name: "Uv_Cache_Dir", Exists: true, Raw: `%CACHE%\raw`, Type: env.UserEnvExpandString},
	}
	inputs := map[string]string{"Path": "must be skipped", "PUB_CACHE": "new", "UV_CACHE_DIR": "new", "DENO_DIR": "new", "JAVA_HOME": `C:\Managed\new`}
	readErr, writeErr, undoErr := errors.New("injected snapshot failure"), errors.New("injected write failure"), errors.New("injected undo failure")
	for _, tc := range []struct{ name, readFail, writeFail, undoFail string }{
		{name: "success"}, {name: "snapshot", readFail: names[2]}, {name: "first", writeFail: names[0]}, {name: "middle", writeFail: names[1]}, {name: "last", writeFail: names[3]}, {name: "undo failure continues", writeFail: names[3], undoFail: names[1]},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, restorePath := env.UseInMemoryUserPathForTesting(`C:\Windows`)
			t.Cleanup(restorePath)
			t.Cleanup(env.UseInMemoryUserEnvForTesting(nil))
			data := maps.Clone(initial)
			var reads, writes, undo []string
			var failed bool
			t.Cleanup(env.UseUserEnvBackendForTesting(env.UserEnvBackend{
				Read: func(name string) (env.UserEnvSnapshot, error) {
					reads = append(reads, name)
					if name == tc.readFail {
						return env.UserEnvSnapshot{}, readErr
					}
					return data[strings.ToUpper(name)], nil
				},
				Write: func(name, raw string, typ uint32) error {
					key := strings.ToUpper(name)
					if !slices.Equal(reads, names) {
						t.Fatalf("write %s preceded complete snapshot: %v", name, reads)
					}
					if failed {
						undo = append(undo, name)
						if key == tc.undoFail {
							return undoErr
						}
					} else {
						writes = append(writes, name)
						if name == tc.writeFail {
							failed = true
							return writeErr
						}
					}
					data[key] = env.UserEnvSnapshot{Name: name, Raw: raw, Type: typ, Exists: true}
					return nil
				},
				Delete: func(name string) error { undo = append(undo, name); delete(data, strings.ToUpper(name)); return nil },
			}))
			err := new(Installer).setUserEnvVars(inputs)
			if tc.readFail != "" {
				if !errors.Is(err, readErr) || len(writes)+len(undo) != 0 || !maps.Equal(initial, data) {
					t.Fatalf("snapshot error=%v writes=%v undo=%v", err, writes, undo)
				}
				return
			}
			if !reflect.DeepEqual(reads, names) {
				t.Fatalf("snapshots %v want %v (err=%v)", reads, names, err)
			}
			if tc.writeFail == "" {
				if err != nil || !slices.Equal(writes, names) || len(undo) != 0 {
					t.Fatalf("err=%v writes=%v undo=%v", err, writes, undo)
				}
				return
			}
			idx := slices.Index(names, tc.writeFail)
			if !errors.Is(err, writeErr) || !slices.Equal(writes, names[:idx+1]) {
				t.Fatalf("err=%v writes=%v", err, writes)
			}
			var wantUndo []string
			for j := idx - 1; j >= 0; j-- {
				name := names[j]
				if prev := initial[name]; prev.Exists {
					name = prev.Name
				}
				wantUndo = append(wantUndo, name)
			}
			if !slices.Equal(undo, wantUndo) {
				t.Errorf("undo %v want %v", undo, wantUndo)
			}
			if tc.undoFail != "" {
				if !errors.Is(err, undoErr) {
					t.Errorf("missing undo error in %v", err)
				}
				delete(data, tc.undoFail)
				initialRemaining := maps.Clone(initial)
				delete(initialRemaining, tc.undoFail)
				if !maps.Equal(data, initialRemaining) {
					t.Errorf("remaining rows %v", data)
				}
				return
			}
			if !maps.Equal(initial, data) {
				t.Errorf("state %v want %v", data, initial)
			}
		})
	}
}
