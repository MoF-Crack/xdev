package gui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xmdemt/xdev/internal/downloader"
	"github.com/xmdemt/xdev/internal/installer"
	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/plugin"
)

type blockedArchiveTransport struct{ started chan struct{} }

func (transport blockedArchiveTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	close(transport.started)
	<-req.Context().Done()
	return nil, req.Context().Err()
}

func TestRemediationE_GUIInstallCancellationAudit(t *testing.T) {
	for _, mode := range []string{"button", "caller", "shutdown", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			app, root := retentionApp(t)
			app.dlMgr = downloader.NewManager()
			appRoot, shutdown := context.WithCancel(context.Background())
			defer shutdown()
			app.rootCtx = appRoot
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			reg := plugin.NewRegistry()
			ins := installer.New(app.cfg, reg)
			ins.SetNoEnvWrite(true)
			defer downloader.SetAuditLogPath("")
			archive := manifest.Archive{OS: plugin.HostOS(), Arch: plugin.HostArch(), Format: "zip", URL: "https://127.0.0.1/demo.zip", SHA256: strings.Repeat("a", 64)}
			tool := plugin.NewGeneric(&manifest.Tool{Name: "demo", VersionList: []manifest.Version{{Version: "1.0.0", Stable: true, Archives: []manifest.Archive{archive}}}})
			reg.Register(tool)
			started := make(chan struct{})
			done := make(chan error, 1)
			delegate := app.guiDownloadFunc("fixture", "demo", "1.0.0")
			go func() {
				_, err := ins.InstallWithProgress(ctx, tool, "1.0.0", nil, installer.InstallOptions{NoActivate: true, DownloadFunc: func(parent context.Context, opts downloader.Options) error {
					opts.Client = &http.Client{Transport: blockedArchiveTransport{started}}
					opts.MaxRetries = 1
					if mode == "deadline" {
						limited, stop := context.WithTimeout(parent, 500*time.Millisecond)
						defer stop()
						parent = limited
					}
					return delegate(parent, opts)
				}})
				done <- err
			}()
			select {
			case <-started:
			case err := <-done:
				t.Fatalf("before transfer began: %v", err)
			case <-time.After(5 * time.Second):
				t.Fatal("no transfer")
			}
			want := error(context.Canceled)
			class, status := "cancelled", "cancelled"
			switch mode {
			case "button":
				if err := app.CancelDownload("fixture"); err != nil {
					t.Fatal(err)
				}
			case "caller":
				cancel()
			case "shutdown":
				shutdown()
			case "deadline":
				want = context.DeadlineExceeded
				class, status = "timeout", "failed"
			}
			select {
			case err := <-done:
				if !errors.Is(err, want) {
					t.Fatalf("install error: %v, want %v", err, want)
				}
			case <-time.After(7 * time.Second):
				t.Fatal("transfer did not exit")
			}
			if tasks := app.GetDownloadTasks(); len(tasks) != 0 {
				t.Fatalf("tasks left after cancellation: %v", tasks)
			}
			data, err := os.ReadFile(filepath.Join(root, "logs", "download_audit.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			var event downloader.AuditEntry
			if err := json.Unmarshal(data, &event); err != nil {
				t.Fatalf("expected exactly one record: %s (%v)", data, err)
			}
			if event.Event != "final" || event.ErrorClass != class || event.Status != status || event.Tool != "demo" || event.Version != "1.0.0" {
				t.Fatalf("unexpected final event: %s", data)
			}
		})
	}
}
