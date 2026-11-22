package gui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/pm"
	"github.com/xmdemt/xdev/internal/pm/pmtest"
)

type pmSessionDesktop struct {
	desktopRuntime
	onSession func(InstallSession)
}

func (d *pmSessionDesktop) Emit(name string, data any) {
	if name == "installSession" {
		d.onSession(data.(InstallSession))
	}
}

func TestPMStartInstallDigestSessionIntegration(t *testing.T) {
	for _, mode := range []string{"script", "method", "cancel", "snapshot", "busy"} {
		t.Run(mode, func(t *testing.T) {
			a := newTestApp(t)
			calls := pmtest.Commands(t, "bash", "fixture-method")
			dir := filepath.Join(a.getCfg().Core.ManifestDir, "local")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			filename := filepath.Join(dir, pm.FileName)
			body := `package_managers:
  - name: fixture
    install_command: approved fixture
    execution_env: bash
    parent_tool: node
    install_methods:
      - name: npm
        command: fixture-method install
        detect_cmd: fixture-method
`
			if err := os.WriteFile(filename, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			plan, err := a.PlanPackageManager("local/fixture")
			if err != nil {
				t.Fatal(err)
			}
			// Use the exact public JSON shape supplied by the new frontend.
			raw, _ := json.Marshal(map[string]any{"sessionId": "pm-fixture", "tool": "local/fixture", "kind": "package-manager", "pmDigest": plan.Digest})
			var spec InstallSpec
			if err := json.Unmarshal(raw, &spec); err != nil {
				t.Fatal(err)
			}
			if mode == "method" || mode == "busy" {
				spec.Kind, spec.Method = "sub-tool", "npm"
			}
			missing := spec
			missing.PMDigest = ""
			if _, err := a.StartInstall(missing); err == nil {
				t.Fatal("unapproved public session accepted")
			}
			if mode == "busy" {
				owner, err := a.installSessions.begin(a.rootContext(), InstallSpec{Tool: "node"}, "node")
				if err != nil {
					t.Fatal(err)
				}
				defer a.installSessions.finish(owner.id, nil)
				if _, err := a.StartInstall(spec); err == nil {
					t.Fatal("sub-tool bypassed parent resource owner")
				}
				if calls() != "" {
					t.Fatal(calls())
				}
				return
			}
			committed := false
			a.desktop = &pmSessionDesktop{onSession: func(view InstallSession) {
				if view.Status == "accepted" {
					if mode == "cancel" {
						if err := a.CancelInstall(view.SessionID); err != nil {
							t.Error(err)
						}
					}
					if mode == "snapshot" {
						changed := strings.Replace(body, "approved fixture", "changed fixture", 1)
						if err := os.WriteFile(filename, []byte(changed), 0600); err != nil {
							t.Error(err)
						}
					}
				}
				if view.Status == "committing" {
					committed = true
					if view.CanCancel {
						t.Error("commit retained cancellation")
					}
				}
			}}
			id, err := a.StartInstall(spec)
			if err != nil {
				t.Fatal(err)
			}
			a.installSessions.mu.Lock()
			session := a.installSessions.sessions[id]
			a.installSessions.mu.Unlock()
			awaitInstall(t, session)
			view, err := a.GetInstallSession(id)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "cancel" {
				if view.Status != "cancelled" || committed || calls() != "" {
					t.Fatalf("cancel crossed commit: %+v committed=%v calls=%s", view, committed, calls())
				}
				return
			}
			if view.Status != "completed" || session.err != nil || !committed {
				t.Fatalf("session failed: %+v %v commit=%v", view, session.err, committed)
			}
			expected := "approved fixture"
			if mode == "method" {
				expected = "fixture-method"
				if view.ResourceKey != "node" {
					t.Fatal(view)
				}
			}
			if !strings.Contains(calls(), expected) || strings.Contains(calls(), "changed fixture") {
				t.Fatal(calls())
			}
			if mode == "snapshot" {
				spec.SessionID = "stale-retry"
				if _, err := a.StartInstall(spec); err == nil {
					t.Fatal("stale digest accepted after catalog replacement")
				}
			}
		})
	}
}
