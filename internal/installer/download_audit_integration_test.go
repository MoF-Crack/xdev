package installer

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/xmdemt/xdev/internal/downloader"
	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/signer"
)

func TestInstallerAuditScopeReachesActualDownloadAdapter(t *testing.T) {
	for _, failure := range []error{nil, context.Canceled, errors.New("HTTP 404 for credential-bearing URL")} {
		t.Run(fmt.Sprint(failure), func(t *testing.T) {
			ins, cfg, _ := newTestInstaller(t)
			auditPath := filepath.Join(t.TempDir(), "audit.jsonl")
			downloader.SetAuditLogPath(auditPath)
			t.Cleanup(func() { downloader.SetAuditLogPath("") })
			a := &manifest.Archive{URL: "https://example.com/a.zip", Format: "zip"}
			err := ins.downloadWithProgress(context.Background(), "auditnet", "1.0.0", a, nil, InstallOptions{DownloadFunc: func(ctx context.Context, opts downloader.Options) error {
				if opts.Audit == nil || opts.ToolName != "auditnet" || opts.Version != "1.0.0" {
					t.Fatal("logical request missing from real adapter options")
				}
				if failure == nil {
					if err := os.MkdirAll(cfg.Core.CacheDir, 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(opts.Dest, []byte("archive"), 0600); err != nil {
						t.Fatal(err)
					}
					opts.Audit.Update(downloader.AuditEntry{URL: opts.URL, Source: "download", Size: 7})
				}
				return failure
			}})
			if !errors.Is(err, failure) {
				t.Fatalf("adapter error not preserved: %v, want %v", err, failure)
			}
			data, err := os.ReadFile(auditPath)
			if err != nil {
				t.Fatal(err)
			}
			lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
			if len(lines) != 1 {
				t.Fatalf("%d final events", len(lines))
			}
			var event downloader.AuditEntry
			if err := json.Unmarshal(lines[0], &event); err != nil {
				t.Fatal(err)
			}
			want := "success"
			if failure != nil {
				want = "failed"
			}
			if errors.Is(failure, context.Canceled) {
				want = "cancelled"
			}
			if event.Status != want || event.Event != "final" || event.Tool != "auditnet" || event.Version != "1.0.0" {
				t.Fatalf("final event = %+v", event)
			}
		})
	}
}

func TestInstallerFinalAuditCoversCacheAndEarlyFailures(t *testing.T) {
	for _, tc := range []struct {
		name           string
		malformedRoot  bool
		invalidRequest bool
		expectedSHA    bool
	}{
		{name: "verified_cache", expectedSHA: true}, {name: "tofu_cache"}, {name: "malformed_root", malformedRoot: true, expectedSHA: true}, {name: "invalid_request", invalidRequest: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ins, cfg, _ := newTestInstaller(t)
			ins.SetNoEnvWrite(true)
			auditPath := filepath.Join(t.TempDir(), "audit.jsonl")
			downloader.SetAuditLogPath(auditPath)
			t.Cleanup(func() { downloader.SetAuditLogPath("") })
			tool, ver := "audittool", "1.0.0"
			if tc.invalidRequest {
				tool = "../bad"
			}
			a := &manifest.Archive{URL: "https://example.com/a.zip", Format: "zip"}
			if !tc.invalidRequest {
				if err := os.MkdirAll(cfg.Core.CacheDir, 0700); err != nil {
					t.Fatal(err)
				}
				cache := filepath.Join(cfg.Core.CacheDir, "audittool-1.0.0.zip")
				if err := os.WriteFile(cache, []byte("cached"), 0600); err != nil {
					t.Fatal(err)
				}
				if tc.expectedSHA {
					a.SHA256 = fileSHA(t, cache)
				}
			}
			oldKey := base64.StdEncoding.EncodeToString(signer.RootPublicKey())
			signer.SetRootPublicKeyForTesting("")
			if tc.malformedRoot {
				signer.SetRootPublicKeyForTesting("malformed")
			}
			t.Cleanup(func() { signer.SetRootPublicKeyForTesting(oldKey) })
			err := ins.downloadWithProgress(context.Background(), tool, ver, a, nil, InstallOptions{DownloadFunc: func(context.Context, downloader.Options) error { t.Fatal("cache must not download"); return nil }})
			if wantFail := tc.invalidRequest || tc.malformedRoot; (err != nil) != wantFail {
				t.Fatalf("err=%v want failed=%v", err, wantFail)
			}
			data, readErr := os.ReadFile(auditPath)
			if readErr != nil {
				t.Fatal(readErr)
			}
			lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
			if len(lines) != 1 {
				t.Fatalf("events=%d want 1: %s", len(lines), data)
			}
			var event downloader.AuditEntry
			if err := json.Unmarshal(lines[0], &event); err != nil {
				t.Fatal(err)
			}
			if event.Tool != tool || event.Version != ver || event.Event != "final" {
				t.Fatalf("missing logical request identity: %+v", event)
			}
			if tc.invalidRequest || tc.malformedRoot {
				if event.Status != "failed" {
					t.Fatalf("failure audit=%+v", event)
				}
			} else if event.Source != "cache" || event.Status != "success" || event.Size != 6 {
				t.Fatalf("cache audit=%+v", event)
			}
		})
	}
}
