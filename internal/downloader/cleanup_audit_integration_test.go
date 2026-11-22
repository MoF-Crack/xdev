package downloader

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xmdemt/xdev/internal/signer"
	"github.com/xmdemt/xdev/internal/util"
)

func TestRemediationE_LiveWriterSurvivesSweep(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Content-Length", "8")
		_, _ = w.Write([]byte("live"))
		w.(http.Flusher).Flush()
		close(started)
		<-release
		_, _ = w.Write([]byte("body"))
	}))
	defer srv.Close()
	dir := t.TempDir()
	dest := filepath.Join(dir, "demo.zip")
	part := partPathFor(dest, srv.URL)
	done := make(chan error, 1)
	go func() {
		done <- Download(context.Background(), Options{URL: srv.URL, Dest: dest, IsThirdParty: true, Quiet: true, ExpectedSHA: sha256Of([]byte("livebody")), Client: srv.Client(), MaxRetries: 1})
	}()
	select {
	case <-started:
	case e := <-done:
		close(release)
		t.Fatalf("writer stopped: %v", e)
	case <-time.After(8 * time.Second):
		close(release)
		t.Fatal("writer did not start")
	}
	r, e := CleanupAllPartialsInDirResult(dir)
	if !errors.Is(e, ErrPartialBusy) || r.SkippedBusy != 1 || r.Deleted != 0 {
		t.Errorf("sweep during transfer: %+v, %v", r, e)
	}
	if _, e = os.Stat(part); e != nil {
		t.Error(e)
	}
	close(release)
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(dest)
	if e != nil || string(b) != "livebody" {
		t.Fatalf("published %q: %v", b, e)
	}
}

func TestRemediationE_CleanerExcludesNewWriter(t *testing.T) {
	const key = "XDEV_TEST_LOCK_PATH"
	if path := os.Getenv(key); path != "" {
		l, err := util.AcquireFileLock(path, 0)
		if err == nil {
			l.Release()
			t.Fatal("child writer acquired lock during removal")
		}
		if !strings.HasPrefix(err.Error(), "file is locked by another process:") {
			t.Fatal(err)
		}
		return
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "demo.zip.part")
	for _, f := range []string{p, p + ".meta"} {
		if err := os.WriteFile(f, []byte("residue"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	r, e := cleanupGenerationWith(p, func(path string) error {
		cmd := exec.Command(os.Args[0], "-test.run=^TestRemediationE_CleanerExcludesNewWriter$")
		cmd.Env = append(os.Environ(), key+"="+p+".lock")
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("child lock probe: %s: %w", output, err)
		}
		return os.Remove(path)
	})
	if e != nil || r.Deleted != 2 {
		t.Fatalf("cleanup %+v: %v", r, e)
	}
	l, e := util.AcquireFileLock(p+".lock", 0)
	if e != nil {
		t.Fatal(e)
	}
	l.Release()
}

func TestRemediationE_AuditOutcomes(t *testing.T) {
	payload := []byte("checked archive")
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/archive" {
			http.NotFound(w, r)
			return
		}
		http.ServeContent(w, r, "archive", time.Time{}, bytes.NewReader(payload))
	}))
	defer srv.Close()
	for _, tc := range []struct {
		name, class, source string
		fallback            bool
		size                int64
	}{
		{"network", "", "download", false, 15}, {"resume", "", "resume", false, 15}, {"fallback", "", "download", true, 15}, {"checksum", "checksum", "download", false, 15}, {"tofu", "tofu", "download", false, 15}, {"signature", "signature", "download", false, 0}, {"http", "http", "download", false, 0}, {"cancel", "cancelled", "download", false, 0}, {"rename", "rename", "download", false, 15},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			audit := filepath.Join(dir, "audit.jsonl")
			SetAuditLogPath(audit)
			defer SetAuditLogPath("")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			target := srv.URL + "/archive?token=private-query"
			opts := Options{URL: target, Dest: filepath.Join(dir, "demo.zip"), ToolName: "tool", Version: "v1", ExpectedSHA: sha256Of(payload), Client: srv.Client(), MaxRetries: 1, IsThirdParty: true, Quiet: true}
			var e error
			switch tc.name {
			case "resume":
				e = os.WriteFile(partPathFor(opts.Dest, opts.URL), payload[:4], 0600)
			case "fallback":
				opts.URL = srv.URL + "/missing"
				opts.FallbackURLs = []string{target}
			case "checksum":
				opts.ExpectedSHA = strings.Repeat("f", 64)
			case "tofu":
				opts.ExpectedSHA = ""
				e = os.WriteFile(opts.Dest+".sha256", []byte(strings.Repeat("f", 64)), 0600)
			case "signature":
				opts.ManifestSignature = []byte("malformed")
			case "http":
				opts.URL = srv.URL + "/missing"
			case "cancel":
				cancel()
			case "rename":
				e = os.Mkdir(opts.Dest, 0700)
			}
			if e != nil {
				t.Fatal(e)
			}
			e = Download(ctx, opts)
			if (e == nil) != (tc.class == "") {
				t.Fatalf("unexpected download result: %v", e)
			}
			raw, e := os.ReadFile(audit)
			if e != nil {
				t.Fatal(e)
			}
			var event AuditEntry
			if e = json.Unmarshal(raw, &event); e != nil {
				t.Fatalf("expected exactly one JSON event: %v", e)
			}
			if event.Event != "final" || event.Tool != "tool" || event.Version != "v1" || event.ErrorClass != tc.class || event.Source != tc.source || event.Size != tc.size || event.Fallback != tc.fallback || strings.Contains(string(raw), "private-query") {
				t.Fatalf("unexpected event: %s", raw)
			}
			status := "success"
			if tc.class != "" {
				status = "failed"
				if event.Error == "" {
					t.Error("missing failure description")
				}
			}
			if tc.class == "cancelled" {
				status = "cancelled"
			}
			if event.Status != status {
				t.Errorf("status %q want %q", event.Status, status)
			}
		})
	}
}

func TestRemediationE_MalformedRootRejectedBeforeIO(t *testing.T) {
	signer.SetRootPublicKeyForTesting("invalid")
	defer signer.SetRootPublicKeyForTesting("")
	dest := filepath.Join(t.TempDir(), "missing", "archive.zip")
	opts := Options{URL: "https://127.0.0.1", Dest: dest, IsThirdParty: true}
	if err := Download(context.Background(), opts); err == nil || !strings.Contains(err.Error(), "root public key") {
		t.Fatalf("malformed root: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(dest)); !os.IsNotExist(err) {
		t.Fatalf("destination touched: %v", err)
	}
	opts.ManifestSignature = []byte(`{}`)
	if err := verifyManifestSignature(opts); err == nil {
		t.Fatal("standalone verification accepted malformed root")
	}
}

func TestRemediationE_OuterAuditFinishAndRedaction(t *testing.T) {
	dir := t.TempDir()
	audit := filepath.Join(dir, "audit.jsonl")
	SetAuditLogPath(audit)
	defer SetAuditLogPath("")
	req := NewAuditRequest(AuditEntry{Tool: "local", Version: "1", URL: "https://secret-user:secret-pass@example.test/private-path?secret-query#secret-fragment", Source: "cache"})
	req.Finish(errors.New("failed, Cookie: session=secret-cookie\n Authorization: Bearer secret-bearer"))
	req.Finish(nil)
	raw, err := os.ReadFile(audit)
	if err != nil {
		t.Fatal(err)
	}
	var e AuditEntry
	if json.Unmarshal(raw, &e) != nil || e.URL != "https://example.test" || e.Error == "" || e.Status != "failed" || bytes.Contains(raw, []byte("secret")) || bytes.Contains(raw, []byte("private-path")) {
		t.Fatalf("redaction or final-event count failed: %q", raw)
	}
}
