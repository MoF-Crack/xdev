package gui

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/xmdemt/xdev/internal/installer"
)

func TestInstallProgressSerializesTaskID(t *testing.T) {
	b, err := json.Marshal(InstallProgress{TaskID: "unique-session", Tool: "node", Version: "22"})
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(b, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["taskId"] != "unique-session" {
		t.Fatalf("missing taskId: %s", b)
	}
}

type installPayloadRecorder struct {
	desktopRuntime
	events map[string][]map[string]any
	t      *testing.T
}

func (r *installPayloadRecorder) Emit(name string, data any) {
	r.t.Helper()
	b, err := json.Marshal(data)
	if err != nil {
		r.t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(b, &payload); err != nil {
		r.t.Fatal(err)
	}
	r.events[name] = append(r.events[name], payload)
}

// Exercise actual emit helpers and serialization, not Go source formatting.
// Identical tool/version retries must carry different identities on both the
// authoritative stream and the legacy observer streams.
func TestInstallEventPayloadsCarrySessionIdentity(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"completed", nil}, {"error", errors.New("failed")},
		{"cancelled", context.Canceled}, {"needs-repair", installer.ErrIncompleteInstall},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := &installPayloadRecorder{t: t, events: make(map[string][]map[string]any)}
			app := &App{desktop: recorder}
			session, err := app.installSessions.begin(context.Background(), InstallSpec{Tool: "node", Version: "22"}, "node")
			if err != nil {
				t.Fatal(err)
			}
			app.emitInstallSession(session.view)
			app.emitInstallProgress(session.id, "node", "22", "Downloading", 42, "1 MB/s", "2s")
			app.emitInstallError(session.id, "node", "22", "legacy error")
			terminal, ok := app.installSessions.finish(session.id, tc.err)
			if !ok {
				t.Fatal("terminal not produced")
			}
			app.emitInstallSession(terminal)
			for _, name := range []string{"installSession", "installProgress", "installError"} {
				if len(recorder.events[name]) == 0 {
					t.Fatalf("missing %s event", name)
				}
				for _, payload := range recorder.events[name] {
					if payload["taskId"] != session.id || payload["tool"] != "node" || payload["version"] != "22" {
						t.Fatalf("unroutable %s: %#v", name, payload)
					}
					if name == "installSession" && payload["sessionId"] != session.id {
						t.Fatalf("session identity lost: %#v", payload)
					}
				}
			}
			stream := recorder.events["installSession"]
			if len(stream) != 3 || stream[1]["percent"] != float64(42) {
				t.Fatalf("progress stream: %#v", stream)
			}
			last := stream[len(stream)-1]
			if last["status"] != tc.name || last["terminal"] != true || last["canCancel"] != false || last["canPause"] != false {
				t.Fatalf("terminal payload: %#v", last)
			}
			if !(stream[0]["revision"].(float64) < stream[1]["revision"].(float64) && stream[1]["revision"].(float64) < last["revision"].(float64)) {
				t.Fatal("revisions not monotonic")
			}
			retry, err := app.installSessions.begin(context.Background(), InstallSpec{Tool: "node", Version: "22"}, "node")
			if err != nil {
				t.Fatal(err)
			}
			app.emitInstallSession(retry.view)
			if retry.id == session.id || recorder.events["installSession"][3]["taskId"] != retry.id {
				t.Fatal("retry reused prior route")
			}
			app.installSessions.finish(retry.id, nil)
		})
	}
}
