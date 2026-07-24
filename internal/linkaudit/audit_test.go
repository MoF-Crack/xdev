package linkaudit_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xmdemt/xdev/internal/linkaudit"
)

var zipPayload = append([]byte("PK\x03\x04"), bytes.Repeat([]byte{0x42}, 128)...)

func TestAuditHTTPBehaviors(t *testing.T) {
	tests := []struct {
		name       string
		handler    http.HandlerFunc
		wantStatus string
		wantCode   int
		wantRange  bool
	}{
		{name: "200", handler: archiveHandler(http.StatusOK), wantStatus: linkaudit.StatusPass, wantCode: 200},
		{name: "206", handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodHead {
				w.Header().Set("Accept-Ranges", "bytes")
				w.Header().Set("Content-Length", "132")
				w.WriteHeader(http.StatusOK)
				return
			}
			w.Header().Set("Content-Range", "bytes 0-131/132")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(zipPayload)
		}, wantStatus: linkaudit.StatusPass, wantCode: 206, wantRange: true},
		{name: "head 405 falls back", handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodHead {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			if r.Header.Get("Range") != "bytes=0-65535" {
				t.Errorf("Range = %q", r.Header.Get("Range"))
			}
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(zipPayload)
		}, wantStatus: linkaudit.StatusPass, wantCode: 206, wantRange: true},
		{name: "404", handler: statusHandler(404), wantStatus: linkaudit.StatusURLDead, wantCode: 404},
		{name: "410", handler: statusHandler(410), wantStatus: linkaudit.StatusURLDead, wantCode: 410},
		{name: "500", handler: statusHandler(500), wantStatus: linkaudit.StatusTransientError, wantCode: 500},
		{name: "html", handler: func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<!doctype html><title>download</title>"))
		}, wantStatus: linkaudit.StatusHTMLInsteadOfArchive, wantCode: 200},
		{name: "wrong content type valid magic", handler: func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write(zipPayload)
		}, wantStatus: linkaudit.StatusPass, wantCode: 200},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()
			result := auditOne(t, srv.URL+"/tool.zip", linkaudit.ModeProbe, "")
			if result.FailureReason != tt.wantStatus || result.StatusCode != tt.wantCode {
				t.Fatalf("status/code = %s/%d, want %s/%d", result.FailureReason, result.StatusCode, tt.wantStatus, tt.wantCode)
			}
			if result.SupportsRange != tt.wantRange {
				t.Fatalf("SupportsRange = %v", result.SupportsRange)
			}
			if tt.wantStatus == linkaudit.StatusPass && result.ArchiveDetected != "zip" {
				t.Fatalf("ArchiveDetected = %q", result.ArchiveDetected)
			}
		})
	}
}

func TestAuditRedirectAndDowngrade(t *testing.T) {
	final := httptest.NewServer(archiveHandler(http.StatusOK))
	defer final.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, final.URL+"/final.zip", http.StatusFound)
	}))
	defer redirect.Close()
	result := auditOne(t, redirect.URL+"/start.zip", linkaudit.ModeProbe, "")
	if result.FailureReason != linkaudit.StatusPass || !strings.Contains(result.FinalURL, "/final.zip") {
		t.Fatalf("redirect result: %+v", result)
	}

	downgradeClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{"http://insecure.test/file.zip"}}, Body: http.NoBody, Request: r}, nil
	})}
	results, err := linkaudit.Audit(context.Background(), linkaudit.Request{Entries: []linkaudit.Entry{{Tool: "tool", Version: "1", OS: "windows", Arch: "amd64", Role: "official", URL: "https://secure.test/file.zip"}}, Mode: linkaudit.ModeProbe, Client: downgradeClient, Clock: fixedClock})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].FailureReason != linkaudit.StatusURLDead {
		t.Fatalf("downgrade status = %s", results[0].FailureReason)
	}
}

func TestAuditFullSHA(t *testing.T) {
	sum := sha256.Sum256(zipPayload)
	server := httptest.NewServer(archiveHandler(http.StatusOK))
	defer server.Close()
	correct := auditOne(t, server.URL+"/tool.zip", linkaudit.ModeFull, fmt.Sprintf("%x", sum))
	if correct.FailureReason != linkaudit.StatusPass || !correct.SHA256Matches || correct.ActualSHA256 == "" {
		t.Fatalf("correct SHA result: %+v", correct)
	}
	wrong := auditOne(t, server.URL+"/tool.zip", linkaudit.ModeFull, strings.Repeat("0", 64))
	if wrong.FailureReason != linkaudit.StatusHashMismatch || wrong.SHA256Matches {
		t.Fatalf("wrong SHA result: %+v", wrong)
	}
	probe := auditOne(t, server.URL+"/tool.zip", linkaudit.ModeProbe, fmt.Sprintf("%x", sum))
	if probe.SHA256Matches || probe.ActualSHA256 != "" {
		t.Fatalf("probe claimed SHA verification: %+v", probe)
	}
}

func TestAuditTimeoutLoopAndDedupe(t *testing.T) {
	t.Run("timeout", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(100 * time.Millisecond) }))
		defer srv.Close()
		result := auditWith(t, srv.URL, linkaudit.Request{Mode: linkaudit.ModeProbe, Timeout: 10 * time.Millisecond})[0]
		if result.FailureReason != linkaudit.StatusTransientError {
			t.Fatalf("status = %s", result.FailureReason)
		}
	})
	t.Run("redirect loop", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, r.URL.Path, http.StatusFound) }))
		defer srv.Close()
		result := auditOne(t, srv.URL+"/loop", linkaudit.ModeProbe, "")
		if result.FailureReason != linkaudit.StatusTransientError {
			t.Fatalf("status = %s", result.FailureReason)
		}
	})
	t.Run("same URL transfers once but keeps rows", func(t *testing.T) {
		var gets atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet {
				gets.Add(1)
			}
			archiveHandler(http.StatusOK)(w, r)
		}))
		defer srv.Close()
		req := linkaudit.Request{Mode: linkaudit.ModeProbe, Client: srv.Client(), AllowHTTPForTests: true, Clock: fixedClock, Entries: []linkaudit.Entry{{Tool: "a", Version: "1", OS: "windows", Arch: "amd64", Role: "official", URL: srv.URL}, {Tool: "b", Version: "1", OS: "windows", Arch: "amd64", Role: "mirror", URL: srv.URL}}}
		results, err := linkaudit.Audit(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 2 || gets.Load() != 1 {
			t.Fatalf("rows=%d GETs=%d", len(results), gets.Load())
		}
	})
}

func TestAuditLoadsManifestCandidatesAndWritesDeterministically(t *testing.T) {
	srv := httptest.NewServer(archiveHandler(http.StatusOK))
	defer srv.Close()
	dir := t.TempDir()
	yaml := fmt.Sprintf("name: demo\nversions:\n  - version: '1.0'\n    archives:\n      - os: windows\n        arch: amd64\n        format: zip\n        url: %s/official.zip\n        mirror_url: %s/mirror.zip\n        sha256: ''\n", srv.URL, srv.URL)
	if err := os.WriteFile(filepath.Join(dir, "demo.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	results, err := linkaudit.Audit(context.Background(), linkaudit.Request{ManifestDir: dir, Platforms: []string{"windows/amd64"}, Mode: linkaudit.ModeProbe, Client: srv.Client(), AllowHTTPForTests: true, Clock: fixedClock, Concurrency: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].Role != "mirror" || results[1].Role != "official" {
		t.Fatalf("results = %+v", results)
	}
	var jsonA, jsonB, markdown bytes.Buffer
	if err := linkaudit.WriteJSON(&jsonA, results); err != nil {
		t.Fatal(err)
	}
	if err := linkaudit.WriteJSON(&jsonB, append([]linkaudit.Result(nil), results...)); err != nil {
		t.Fatal(err)
	}
	if jsonA.String() != jsonB.String() || !strings.Contains(jsonA.String(), "2026-01-02T03:04:05Z") {
		t.Fatalf("non-deterministic JSON: %s", jsonA.String())
	}
	if err := linkaudit.WriteMarkdown(&markdown, results); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markdown.String(), "| demo | 1.0 | windows | amd64 | mirror | PASS |") {
		t.Fatalf("markdown = %s", markdown.String())
	}
}

func TestAuditFiltersManifestCandidates(t *testing.T) {
	dir := t.TempDir()
	manifest := "name: demo\nversions:\n  - version: '1.0'\n    archives:\n      - os: windows\n        arch: amd64\n        format: zip\n        url: https://example.invalid/demo-1.zip\n        mirror_url: https://mirror.invalid/demo-1.zip\n  - version: '2.0'\n    archives:\n      - os: windows\n        arch: amd64\n        format: zip\n        url: https://example.invalid/demo-2.zip\n"
	if err := os.WriteFile(filepath.Join(dir, "demo.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "other.yaml"), []byte(strings.Replace(manifest, "name: demo", "name: other", 1)), 0o644); err != nil {
		t.Fatal(err)
	}

	results, err := linkaudit.Audit(context.Background(), linkaudit.Request{ManifestDir: dir, Tools: []string{"demo"}, Versions: []string{"1.0"}, Roles: []string{"mirror"}, Mode: linkaudit.ModeProbe, Clock: fixedClock})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Tool != "demo" || results[0].Version != "1.0" || results[0].Role != "mirror" {
		t.Fatalf("results = %+v", results)
	}
}

func TestAuditErrorsWhenManifestFiltersMatchNothing(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "demo.yaml"), []byte("name: demo\nversions:\n  - version: '1.0'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, req := range map[string]linkaudit.Request{
		"tool":    {ManifestDir: dir, Tools: []string{"Demo"}},
		"version": {ManifestDir: dir, Versions: []string{"2.0"}},
		"role":    {ManifestDir: dir, Roles: []string{"mirror"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := linkaudit.Audit(context.Background(), req); err == nil || !strings.Contains(err.Error(), "no manifest entries matched") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestAuditRejectsUnsafeURLsAndRedirects(t *testing.T) {
	for _, rawURL := range []string{"https://127.0.0.1/file.zip", "https://[::1]/file.zip", "https://169.254.1.1/file.zip", "http://192.168.1.2/file.zip"} {
		results, err := linkaudit.Audit(context.Background(), linkaudit.Request{Entries: []linkaudit.Entry{{Tool: "tool", Role: "official", URL: rawURL}}, Mode: linkaudit.ModeProbe, Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("unsafe URL reached transport"); return nil, nil })}, AllowHTTPForTests: true, Clock: fixedClock})
		if err != nil {
			t.Fatal(err)
		}
		if results[0].FailureReason != linkaudit.StatusURLDead {
			t.Fatalf("%s status = %s", rawURL, results[0].FailureReason)
		}
	}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{"https://127.0.0.1/file.zip"}}, Body: http.NoBody, Request: r}, nil
	})}
	results, err := linkaudit.Audit(context.Background(), linkaudit.Request{Entries: []linkaudit.Entry{{Tool: "tool", Role: "official", URL: "https://example.invalid/file.zip"}}, Mode: linkaudit.ModeProbe, Client: client, Clock: fixedClock})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].FailureReason != linkaudit.StatusURLDead {
		t.Fatalf("private redirect status = %s", results[0].FailureReason)
	}
}

func TestAuditRequiresExplicitManifestDirectory(t *testing.T) {
	for _, dir := range []string{filepath.Join(t.TempDir(), "missing"), t.TempDir()} {
		if _, err := linkaudit.Audit(context.Background(), linkaudit.Request{ManifestDir: dir, Mode: linkaudit.ModeProbe}); err == nil {
			t.Fatalf("expected error for manifest dir %s", dir)
		}
	}
}

func TestAuditReportsMirrorBaseWithoutNetwork(t *testing.T) {
	dir := t.TempDir()
	yaml := "name: demo\nversions:\n  - version: '1'\n    archives:\n      - os: windows\n        arch: amd64\n        format: zip\n        url: https://example.invalid/tool.zip\n        mirror_url: https://mirror.invalid/base/\n        sha256: " + strings.Repeat("a", 64) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "demo.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Host, "mirror") {
			t.Fatal("mirror base reached network")
		}
		return nil, context.DeadlineExceeded
	})}
	results, err := linkaudit.Audit(context.Background(), linkaudit.Request{ManifestDir: dir, Platforms: []string{"windows/amd64"}, Mode: linkaudit.ModeProbe, Client: client, Clock: fixedClock})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].Role != "mirror" || results[0].FailureReason != linkaudit.StatusImplementationMissing || !strings.Contains(results[0].Detail, "requires rewrite") {
		t.Fatalf("results = %+v", results)
	}
}

func TestAuditRecognizesConcreteURLsFromRealManifestShapes(t *testing.T) {
	tests := []struct {
		name      string
		format    string
		mirrorURL string
	}{
		{name: "typescript tgz", format: "tar.gz", mirrorURL: "https://registry.npmmirror.com/typescript/-/typescript-5.7.3.tgz"},
		{name: "rust script", format: "sh", mirrorURL: "https://sh.rustup.rs"},
		{name: "sourceforge download suffix", format: "zip", mirrorURL: "https://sourceforge.net/projects/luabinaries/files/5.5.0/lua-5.5.0_Win64_bin.zip/download?use_mirror=netcologne"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			yaml := fmt.Sprintf("name: fixture\nversions:\n  - version: '1'\n    archives:\n      - os: linux\n        arch: x86_64\n        format: %s\n        mirror_url: %s\n", tt.format, tt.mirrorURL)
			if err := os.WriteFile(filepath.Join(dir, "fixture.yaml"), []byte(yaml), 0o644); err != nil {
				t.Fatal(err)
			}
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, context.DeadlineExceeded })}
			results, err := linkaudit.Audit(context.Background(), linkaudit.Request{ManifestDir: dir, Platforms: []string{"linux/x86_64"}, Roles: []string{"mirror"}, Mode: linkaudit.ModeProbe, Client: client, Clock: fixedClock})
			if err != nil {
				t.Fatal(err)
			}
			if len(results) != 1 || results[0].FailureReason == linkaudit.StatusImplementationMissing {
				t.Fatalf("results = %+v", results)
			}
		})
	}
}

func TestAuditStillRejectsMirrorRoot(t *testing.T) {
	dir := t.TempDir()
	yaml := "name: fixture\nversions:\n  - version: '1'\n    archives:\n      - os: linux\n        arch: x86_64\n        format: zip\n        mirror_url: https://mirrors.example.invalid/downloads/\n"
	if err := os.WriteFile(filepath.Join(dir, "fixture.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	results, err := linkaudit.Audit(context.Background(), linkaudit.Request{ManifestDir: dir, Platforms: []string{"linux/x86_64"}, Roles: []string{"mirror"}, Mode: linkaudit.ModeProbe, Clock: fixedClock})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].FailureReason != linkaudit.StatusImplementationMissing {
		t.Fatalf("results = %+v", results)
	}
}

func TestAuditPlatformArchAliasesPreserveManifestValues(t *testing.T) {
	dir := t.TempDir()
	yaml := "name: fixture\nversions:\n  - version: '1'\n    archives:\n      - os: linux\n        arch: x86_64\n        format: zip\n        mirror_url: https://mirror.invalid/linux.zip\n      - os: darwin\n        arch: x86_64\n        format: zip\n        mirror_url: https://mirror.invalid/darwin.zip\n      - os: linux\n        arch: aarch64\n        format: zip\n        mirror_url: https://mirror.invalid/arm.zip\n"
	if err := os.WriteFile(filepath.Join(dir, "fixture.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	for platform, wantOS := range map[string]string{"linux/amd64": "linux", "darwin/x64": "darwin", "linux/arm64": "linux"} {
		t.Run(platform, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, context.DeadlineExceeded })}
			results, err := linkaudit.Audit(context.Background(), linkaudit.Request{ManifestDir: dir, Platforms: []string{platform}, Roles: []string{"mirror"}, Mode: linkaudit.ModeProbe, Client: client, Clock: fixedClock})
			if err != nil {
				t.Fatal(err)
			}
			if len(results) != 1 || results[0].OS != wantOS || (results[0].Arch != "x86_64" && results[0].Arch != "aarch64") {
				t.Fatalf("results = %+v", results)
			}
		})
	}
}

func TestAuditDedupeMapsDeadStatusPerRole(t *testing.T) {
	srv := httptest.NewServer(statusHandler(404))
	defer srv.Close()
	results, err := linkaudit.Audit(context.Background(), linkaudit.Request{Mode: linkaudit.ModeProbe, Client: srv.Client(), AllowHTTPForTests: true, Clock: fixedClock, Entries: []linkaudit.Entry{{Tool: "a", Role: "official", URL: srv.URL + "/file.zip"}, {Tool: "b", Role: "mirror", URL: srv.URL + "/file.zip"}}})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].FailureReason != linkaudit.StatusURLDead || results[1].FailureReason != linkaudit.StatusMirrorDead {
		t.Fatalf("results = %+v", results)
	}
}

func TestAuditFullCacheAndPerRowChecksums(t *testing.T) {
	sum := fmt.Sprintf("%x", sha256.Sum256(zipPayload))
	var gets atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			gets.Add(1)
		}
		archiveHandler(200)(w, r)
	}))
	defer srv.Close()
	cache := t.TempDir()
	req := linkaudit.Request{Mode: linkaudit.ModeFull, CacheDir: cache, Client: srv.Client(), AllowHTTPForTests: true, Clock: fixedClock, Entries: []linkaudit.Entry{{Tool: "a", Role: "official", URL: srv.URL + "/file.zip", ExpectedSHA256: sum}, {Tool: "b", Role: "mirror", URL: srv.URL + "/file.zip", ExpectedSHA256: strings.Repeat("0", 64)}}}
	first, err := linkaudit.Audit(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := linkaudit.Audit(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if gets.Load() != 1 {
		t.Fatalf("GETs = %d, want 1", gets.Load())
	}
	if first[0].FailureReason != linkaudit.StatusPass || first[1].FailureReason != linkaudit.StatusHashMismatch || second[0].FailureReason != linkaudit.StatusPass {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
	for _, sha := range []string{"", "xyz"} {
		result := auditWith(t, srv.URL+"/missing.zip", linkaudit.Request{Mode: linkaudit.ModeFull, CacheDir: t.TempDir(), ExpectedSHA256: sha})[0]
		if result.FailureReason != linkaudit.StatusMissingChecksum {
			t.Fatalf("sha %q status = %s", sha, result.FailureReason)
		}
	}
}

func TestProbeRangeLengthLimitAndHEADFallback(t *testing.T) {
	var written atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", "999999")
			w.WriteHeader(500)
			return
		}
		w.Header().Set("Content-Range", "bytes 0-65535/1234567")
		w.WriteHeader(206)
		_, _ = w.Write(zipPayload)
	}))
	defer srv.Close()
	result := auditOne(t, srv.URL+"/file.zip", linkaudit.ModeProbe, "")
	if result.FailureReason != linkaudit.StatusPass || result.ContentLength != 1234567 {
		t.Fatalf("result = %+v", result)
	}
	large := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(405)
			return
		}
		w.WriteHeader(200)
		for i := 0; i < 200000; i++ {
			if _, err := w.Write([]byte{'P'}); err != nil {
				return
			}
			written.Add(1)
		}
	}))
	defer large.Close()
	_ = auditOne(t, large.URL+"/large.zip", linkaudit.ModeProbe, "")
	// ModeProbe caps client reads at ~64KiB via io.LimitReader, but the server
	// may successfully flush somewhat more (64KiB + transport buffers) before
	// the client close propagates. We assert only that it did NOT download the
	// full 200000 bytes.
	if got := written.Load(); got > 130000 {
		t.Fatalf("probe server wrote %d bytes (mode probe should cap reads)", got)
	}
	for _, headStatus := range []int{403, 500} {
		t.Run(fmt.Sprint(headStatus), func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodHead {
					w.WriteHeader(headStatus)
					return
				}
				archiveHandler(200)(w, r)
			}))
			defer s.Close()
			if got := auditOne(t, s.URL+"/file.zip", linkaudit.ModeProbe, ""); got.FailureReason != linkaudit.StatusPass {
				t.Fatalf("result=%+v", got)
			}
		})
	}
}

func TestMarkdownEscapesLineBreaks(t *testing.T) {
	var out bytes.Buffer
	if err := linkaudit.WriteMarkdown(&out, []linkaudit.Result{{Tool: "a\nb\r|c", FailureReason: linkaudit.StatusPass, CheckedAt: fixedClock()}}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "a\nb") || strings.Contains(out.String(), "b\r") {
		t.Fatalf("markdown contains raw line break: %q", out.String())
	}
}

func auditOne(t *testing.T, rawURL string, mode linkaudit.Mode, sha string) linkaudit.Result {
	t.Helper()
	return auditWith(t, rawURL, linkaudit.Request{Mode: mode, CacheDir: t.TempDir(), ExpectedSHA256: sha})[0]
}

func auditWith(t *testing.T, rawURL string, overrides linkaudit.Request) []linkaudit.Result {
	t.Helper()
	overrides.Entries = []linkaudit.Entry{{Tool: "tool", Version: "1", OS: "windows", Arch: "amd64", Role: "official", URL: rawURL, ExpectedSHA256: overrides.ExpectedSHA256}}
	overrides.AllowHTTPForTests = true
	overrides.Clock = fixedClock
	if overrides.Client == nil {
		overrides.Client = http.DefaultClient
	}
	results, err := linkaudit.Audit(context.Background(), overrides)
	if err != nil {
		t.Fatal(err)
	}
	return results
}

func archiveHandler(status int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", fmt.Sprint(len(zipPayload)))
			w.WriteHeader(status)
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write(zipPayload)
	}
}
func statusHandler(status int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }
}
func fixedClock() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
