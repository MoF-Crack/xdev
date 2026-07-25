package linkaudit

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/util"
	"gopkg.in/yaml.v3"
)

type evidence struct {
	finalURL, contentType, actualSHA, archive, failure, detail string
	status                                                     int
	length                                                     int64
	ranges, available                                          bool
}

func Audit(ctx context.Context, req Request) ([]Result, error) {
	entries := req.Entries
	if len(entries) == 0 {
		var err error
		entries, err = manifestEntries(req.ManifestDir, req.Platforms, req.Tools, req.Versions, req.Roles)
		if err != nil {
			return nil, err
		}
	}
	if req.Mode == "" {
		req.Mode = ModeProbe
	}
	if req.Mode != ModeProbe && req.Mode != ModeFull {
		return nil, fmt.Errorf("invalid mode %q", req.Mode)
	}
	if req.Timeout <= 0 {
		req.Timeout = 30 * time.Second
	}
	if req.Concurrency <= 0 {
		req.Concurrency = 4
	}
	if req.Clock == nil {
		req.Clock = time.Now
	}
	client := secureClient(req.Client, req.Timeout, req.AllowHTTPForTests)

	type call struct {
		done chan struct{}
		ev   evidence
	}
	calls := map[string]*call{}
	var mu sync.Mutex
	sem := make(chan struct{}, req.Concurrency)
	results := make([]Result, len(entries))
	var wg sync.WaitGroup
	for i, entry := range entries {
		if entry.PreStatus != "" {
			results[i] = resultFrom(entry, evidence{failure: entry.PreStatus, detail: entry.Detail}, req.Mode, req.Clock())
			continue
		}
		wg.Add(1)
		go func(index int, item Entry) {
			defer wg.Done()
			mu.Lock()
			c, exists := calls[item.URL]
			if !exists {
				c = &call{done: make(chan struct{})}
				calls[item.URL] = c
			}
			mu.Unlock()
			if !exists {
				sem <- struct{}{}
				c.ev = inspect(ctx, req, client, item)
				<-sem
				close(c.done)
			} else {
				<-c.done
			}
			results[index] = resultFrom(item, c.ev, req.Mode, req.Clock())
		}(i, entry)
	}
	wg.Wait()

	// Retry TRANSIENT_ERROR results up to 3 times with short backoff
	// (Spec 007 #4: transient network errors should not permanently block).
	// Only retries if there are TRANSIENT errors; backoff is 2s to keep tests fast.
	maxRetries := 3
	for retry := 0; retry < maxRetries; retry++ {
		transientIdx := []int{}
		for i, r := range results {
			if r.FailureReason == StatusTransientError {
				transientIdx = append(transientIdx, i)
			}
		}
		if len(transientIdx) == 0 {
			break
		}
		if retry > 0 {
			select {
			case <-time.After(2 * time.Second):
			case <-ctx.Done():
				break
			}
		}
		for _, i := range transientIdx {
			ev := inspect(ctx, req, client, entries[i])
			results[i] = resultFrom(entries[i], ev, req.Mode, req.Clock())
		}
	}

	sortResults(results)
	return results, nil
}

func manifestEntries(dir string, platforms, tools, versions, roles []string) ([]Entry, error) {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("manifest directory %q is unavailable", dir)
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("manifest directory %q contains no YAML files", dir)
	}
	wanted := map[string]bool{}
	for _, p := range platforms {
		wanted[normalizePlatform(p)] = true
	}
	wantedTools := stringSet(tools)
	wantedVersions := stringSet(versions)
	wantedRoles := stringSet(roles)
	for role := range wantedRoles {
		if role != "official" && role != "mirror" {
			return nil, fmt.Errorf("invalid role %q: expected official or mirror", role)
		}
	}
	var entries []Entry
	for _, path := range files {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, readErr
		}
		var tool manifest.Tool
		if err := yaml.Unmarshal(data, &tool); err != nil {
			return nil, fmt.Errorf("parse %s: %w", filepath.Base(path), err)
		}
		if tool.Name == "" {
			tool.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		}
		if len(wantedTools) > 0 && !wantedTools[tool.Name] {
			continue
		}
		for _, version := range tool.VersionList {
			if len(wantedVersions) > 0 && !wantedVersions[version.Version] {
				continue
			}
			for _, archive := range version.Archives {
				if len(wanted) > 0 && !wanted[normalizePlatform(archive.OS+"/"+archive.Arch)] {
					continue
				}
				if archive.URL != "" && (len(wantedRoles) == 0 || wantedRoles["official"]) {
					entries = append(entries, Entry{Tool: tool.Name, Version: version.Version, OS: archive.OS, Arch: archive.Arch, Role: "official", URL: archive.URL, ExpectedSHA256: archive.SHA256, Format: archive.Format})
				}
				if archive.MirrorURL != "" && (len(wantedRoles) == 0 || wantedRoles["mirror"]) {
					entry := Entry{Tool: tool.Name, Version: version.Version, OS: archive.OS, Arch: archive.Arch, Role: "mirror", URL: archive.MirrorURL, ExpectedSHA256: archive.SHA256, Format: archive.Format}
					if !concreteArtifactURL(archive.MirrorURL, archive.Format) {
						entry.PreStatus, entry.Detail = StatusImplementationMissing, "mirror template/base requires rewrite to a concrete artifact URL"
					}
					entries = append(entries, entry)
				}
			}
		}
	}
	if len(entries) == 0 && (len(wantedTools) > 0 || len(wantedVersions) > 0 || len(wantedRoles) > 0) {
		return nil, fmt.Errorf("no manifest entries matched the requested filters")
	}
	return entries, nil
}

func stringSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[value] = true
	}
	return set
}

func normalizePlatform(platform string) string {
	osName, arch, found := strings.Cut(platform, "/")
	if !found {
		return platform
	}
	switch arch {
	case "amd64", "x86_64", "x64":
		arch = "amd64"
	case "arm64", "aarch64":
		arch = "arm64"
	}
	return osName + "/" + arch
}

func secureClient(base *http.Client, timeout time.Duration, allowHTTPForTests bool) *http.Client {
	client := &http.Client{Timeout: timeout}
	if base != nil {
		*client = *base
		client.Timeout = timeout
	}
	previous := client.CheckRedirect
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("too many redirects")
		}
		if len(via) > 0 && via[len(via)-1].URL.Scheme == "https" && next.URL.Scheme != "https" {
			return fmt.Errorf("HTTPS downgrade blocked")
		}
		if err := validateTarget(next.URL.String(), allowHTTPForTests); err != nil {
			return fmt.Errorf("unsafe redirect: %w", err)
		}
		if previous != nil {
			return previous(next, via)
		}
		return nil
	}
	return client
}

func inspect(ctx context.Context, req Request, client *http.Client, entry Entry) evidence {
	if err := validateTarget(entry.URL, req.AllowHTTPForTests); err != nil {
		return evidence{failure: "dead"}
	}
	cache := filepath.Join(req.CacheDir, fmt.Sprintf("%x.archive", sha256.Sum256([]byte(entry.URL))))
	if req.Mode == ModeFull {
		if ev, ok := cachedEvidence(cache); ok {
			return ev
		}
	}
	requestCtx, cancel := context.WithTimeout(ctx, req.Timeout)
	defer cancel()
	head, err := do(requestCtx, client, http.MethodHead, entry.URL, "")
	if err != nil {
		if isSecurityRejection(err) {
			return evidence{failure: "dead"}
		}
		return evidence{failure: StatusTransientError}
	}
	head.Body.Close()
	if head.StatusCode == http.StatusUnauthorized {
		return evidence{status: head.StatusCode, finalURL: head.Request.URL.String(), failure: StatusAuthOrLicenseRequired}
	}
	if head.StatusCode == 404 || head.StatusCode == 410 {
		return evidence{status: head.StatusCode, finalURL: head.Request.URL.String(), failure: "dead"}
	}
	headLength := head.ContentLength
	needGet := true
	if needGet {
		rangeHeader := "bytes=0-65535"
		if req.Mode == ModeFull {
			rangeHeader = ""
		}
		resp, getErr := do(requestCtx, client, http.MethodGet, entry.URL, rangeHeader)
		if getErr != nil {
			if isSecurityRejection(getErr) {
				return evidence{failure: "dead"}
			}
			return evidence{failure: StatusTransientError}
		}
		defer resp.Body.Close()
		ev := evidence{status: resp.StatusCode, finalURL: resp.Request.URL.String(), contentType: resp.Header.Get("Content-Type"), length: responseLength(resp, headLength), ranges: resp.StatusCode == 206 || resp.Header.Get("Accept-Ranges") != ""}
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			ev.failure = StatusAuthOrLicenseRequired
			return ev
		}
		if resp.StatusCode == 404 || resp.StatusCode == 410 {
			ev.failure = "dead"
			return ev
		}
		if resp.StatusCode >= 500 {
			ev.failure = StatusTransientError
			return ev
		}
		if resp.StatusCode/100 != 2 {
			ev.failure = "dead"
			return ev
		}
		body := io.Reader(resp.Body)
		if req.Mode == ModeProbe {
			body = io.LimitReader(resp.Body, 65537)
		}
		reader := bufio.NewReader(body)
		magic, _ := reader.Peek(512)
		ev.archive = detectArchive(magic)
		if ev.archive == "html" {
			ev.failure = StatusHTMLInsteadOfArchive
			return ev
		}
		if ev.archive == "" {
			ev.failure = StatusUnsupportedLayout
			return ev
		}
		ev.available, ev.failure = true, StatusPass
		if req.Mode == ModeFull {
			if err := os.MkdirAll(req.CacheDir, 0o755); err != nil {
				ev.available = false
				ev.failure = StatusImplementationMissing
				return ev
			}
			file, err := os.CreateTemp(req.CacheDir, filepath.Base(cache)+".*.part")
			if err != nil {
				ev.available = false
				ev.failure = StatusImplementationMissing
				return ev
			}
			hasher := sha256.New()
			_, copyErr := io.Copy(io.MultiWriter(file, hasher), reader)
			closeErr := file.Close()
			if copyErr != nil || closeErr != nil {
				_ = os.Remove(file.Name())
				ev.available = false
				ev.failure = StatusTransientError
				return ev
			}
			ev.actualSHA = hex.EncodeToString(hasher.Sum(nil))
			if err := os.Rename(file.Name(), cache); err != nil {
				_ = os.Remove(file.Name())
				ev.available = false
				ev.failure = StatusImplementationMissing
				return ev
			}
		}
		return ev
	}
	return evidence{failure: StatusImplementationMissing}
}

func do(ctx context.Context, client *http.Client, method, rawURL, byteRange string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "xdev-linkaudit/1")
	if byteRange != "" {
		req.Header.Set("Range", byteRange)
	}
	return client.Do(req)
}

func validateTarget(rawURL string, allowHTTPForTests bool) error {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return fmt.Errorf("invalid URL")
	}
	if allowHTTPForTests && u.Scheme == "http" && util.IsLocalhost(u.Hostname()) {
		return nil
	}
	return util.IsSafeHTTPSExternalURL(rawURL)
}

func concreteArtifactURL(rawURL, format string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || strings.HasSuffix(u.Path, "/") {
		return false
	}
	name := strings.ToLower(filepath.Base(u.Path))
	if name == "download" {
		name = strings.ToLower(filepath.Base(filepath.Dir(u.Path)))
	}
	for _, suffix := range []string{".zip", ".tar.gz", ".tgz", ".tar.xz", ".txz", ".7z", ".exe"} {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return strings.EqualFold(format, "sh")
}

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func cachedEvidence(path string) (evidence, bool) {
	file, err := os.Open(path)
	if err != nil {
		return evidence{}, false
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return evidence{}, false
	}
	hasher := sha256.New()
	reader := bufio.NewReader(io.TeeReader(file, hasher))
	magic, _ := reader.Peek(512)
	archive := detectArchive(magic)
	if archive == "" || archive == "html" {
		return evidence{}, false
	}
	if _, err := io.Copy(io.Discard, reader); err != nil {
		return evidence{}, false
	}
	return evidence{length: info.Size(), archive: archive, actualSHA: hex.EncodeToString(hasher.Sum(nil)), available: true, failure: StatusPass}, true
}

func responseLength(resp *http.Response, headLength int64) int64 {
	if contentRange := resp.Header.Get("Content-Range"); contentRange != "" {
		if slash := strings.LastIndex(contentRange, "/"); slash >= 0 {
			var total int64
			if _, err := fmt.Sscan(contentRange[slash+1:], &total); err == nil {
				return total
			}
		}
	}
	if resp.ContentLength >= 0 && resp.StatusCode != http.StatusPartialContent {
		return resp.ContentLength
	}
	return headLength
}

func detectArchive(b []byte) string {
	text := strings.ToLower(strings.TrimSpace(string(b)))
	switch {
	case len(b) >= 4 && string(b[:4]) == "PK\x03\x04":
		return "zip"
	case len(b) >= 2 && b[0] == 0x1f && b[1] == 0x8b:
		return "gzip"
	case len(b) >= 6 && string(b[:6]) == "\xfd7zXZ\x00":
		return "xz"
	case len(b) >= 6 && string(b[:6]) == "7z\xbc\xaf'\x1c":
		return "7z"
	case len(b) >= 2 && string(b[:2]) == "MZ":
		return "exe"
	case strings.HasPrefix(text, "<!doctype html") || strings.HasPrefix(text, "<html"):
		return "html"
	case strings.HasPrefix(text, "#!"):
		return "script"
	default:
		return ""
	}
}

func deadStatus(role string) string {
	if role == "mirror" {
		return StatusMirrorDead
	}
	return StatusURLDead
}
func isSecurityRejection(err error) bool {
	return strings.Contains(err.Error(), "HTTPS downgrade blocked") || strings.Contains(err.Error(), "unsafe redirect")
}
func resultFrom(e Entry, v evidence, mode Mode, checked time.Time) Result {
	failure := v.failure
	if failure == "dead" {
		failure = deadStatus(e.Role)
	}
	result := Result{Tool: e.Tool, Version: e.Version, OS: e.OS, Arch: e.Arch, Role: e.Role, SourceURL: e.URL, FinalURL: v.finalURL, StatusCode: v.status, ContentType: v.contentType, ContentLength: v.length, SupportsRange: v.ranges, ExpectedSHA256: e.ExpectedSHA256, ActualSHA256: v.actualSHA, ArchiveDetected: v.archive, Available: v.available, FailureReason: failure, Detail: v.detail, CheckedAt: checked.UTC()}
	if mode == ModeFull && !validSHA256(e.ExpectedSHA256) {
		result.Available = false
		result.FailureReason = StatusMissingChecksum
		result.Detail = "full verification requires a 64-character hexadecimal SHA-256"
		return result
	}
	if v.actualSHA != "" && e.ExpectedSHA256 != "" {
		result.SHA256Matches = strings.EqualFold(v.actualSHA, e.ExpectedSHA256)
		if !result.SHA256Matches {
			result.Available = false
			result.FailureReason = StatusHashMismatch
		}
	}
	return result
}
func sortResults(r []Result) {
	sort.SliceStable(r, func(i, j int) bool {
		a, b := r[i], r[j]
		return strings.Join([]string{a.Tool, a.Version, a.OS, a.Arch, a.Role, a.SourceURL}, "\x00") < strings.Join([]string{b.Tool, b.Version, b.OS, b.Arch, b.Role, b.SourceURL}, "\x00")
	})
}
