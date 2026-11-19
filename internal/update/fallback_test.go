package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/xmdemt/xdev/internal/remote"
)

type fixtureTimeout struct{}

func (fixtureTimeout) Error() string   { return "fixture network timeout" }
func (fixtureTimeout) Timeout() bool   { return true }
func (fixtureTimeout) Temporary() bool { return true }

func withFallbackAssetMetadata(t *testing.T, result *CheckResult) {
	t.Helper()
	for i := range result.Release.Assets {
		data, err := json.Marshal(result.Release.Assets[i])
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]any
		if err := json.Unmarshal(data, &fields); err != nil {
			t.Fatal(err)
		}
		fields["id"] = 101 + i
		fields["url"] = fmt.Sprintf("https://api.github.com/repos/MoF-Crack/xdev/releases/assets/%d", 101+i)
		data, _ = json.Marshal(fields)
		if err := json.Unmarshal(data, &result.Release.Assets[i]); err != nil {
			t.Fatal(err)
		}
	}
}

func TestApplyAssetAPIFallbackTimeoutAndChecksum(t *testing.T) {
	exe := updateTargetFixture(t)
	archive := updateArchiveFixture(t, []byte("API verified binary"), binaryNameInArchive())
	apiCalls := 0
	server := useUpdateTLSFixture(t, func(w http.ResponseWriter, r *http.Request) {
		apiCalls++
		if r.Header.Get("Accept") != "application/octet-stream" {
			t.Error("asset API did not request binary media")
		}
		switch r.URL.Path {
		case "/repos/MoF-Crack/xdev/releases/assets/101":
			_, _ = w.Write(archive)
		case "/repos/MoF-Crack/xdev/releases/assets/102":
			_, _ = fmt.Fprintf(w, "%x  %s\n", sha256.Sum256(archive), assetNameForPlatform("1.0.0-beta.2"))
		default:
			t.Errorf("unexpected API path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	})
	base := http.DefaultTransport
	http.DefaultTransport = updateRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "api.github.com" {
			return nil, fixtureTimeout{}
		}
		return base.RoundTrip(req)
	})
	t.Cleanup(func() { http.DefaultTransport = base })
	result := updateResultFixture(server.URL, archive)
	withFallbackAssetMetadata(t, result)
	var events []Progress
	if err := ApplyToWithProgress(context.Background(), result, exe, func(p Progress) { events = append(events, p) }); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(exe)
	if err != nil || string(got) != "API verified binary" || apiCalls != 2 {
		t.Fatalf("fallback did not install verified archive: err=%v apiCalls=%d", err, apiCalls)
	}
	last := events[len(events)-1]
	if last.Phase != "completed" || last.DownloadedBytes != int64(len(archive)) {
		t.Fatalf("fallback lost completion byte contract: %+v", last)
	}
}

func TestApplyAssetAPIFallbackHashStillMandatory(t *testing.T) {
	exe := updateTargetFixture(t)
	archive := updateArchiveFixture(t, []byte("untrusted binary"), binaryNameInArchive())
	server := useUpdateTLSFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/101") {
			_, _ = w.Write(archive)
		} else {
			_, _ = fmt.Fprintf(w, "%x  %s\n", sha256.Sum256([]byte("wrong")), assetNameForPlatform("1.0.0-beta.2"))
		}
	})
	base := http.DefaultTransport
	http.DefaultTransport = updateRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "api.github.com" {
			return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader("unavailable")), Header: make(http.Header)}, nil
		}
		return base.RoundTrip(req)
	})
	t.Cleanup(func() { http.DefaultTransport = base })
	result := updateResultFixture(server.URL, archive)
	withFallbackAssetMetadata(t, result)
	var events []Progress
	err := ApplyToWithProgress(context.Background(), result, exe, func(p Progress) { events = append(events, p) })
	if err == nil || !strings.Contains(err.Error(), "hash mismatch") {
		t.Fatalf("API fallback bypassed checksum: %v", err)
	}
	assertOldUpdateTarget(t, exe)
	assertNoUpdateCompletion(t, events)
}

// Return an actual partial body followed by a network timeout; retry must
// rewind AND truncate, including when the replacement archive is shorter.
type partialTimeoutBody struct {
	*bytes.Reader
}

func (r partialTimeoutBody) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if err == io.EOF {
		return 0, fixtureTimeout{}
	}
	return n, err
}
func (partialTimeoutBody) Close() error { return nil }

func TestApplyAssetAPIFallbackPartialBytesReset(t *testing.T) {
	exe := updateTargetFixture(t)
	archive := updateArchiveFixture(t, []byte("short valid binary"), binaryNameInArchive())
	server := useUpdateTLSFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/101") {
			_, _ = w.Write(archive)
		} else {
			_, _ = fmt.Fprintf(w, "%x  %s\n", sha256.Sum256(archive), assetNameForPlatform("1.0.0-beta.2"))
		}
	})
	base := http.DefaultTransport
	http.DefaultTransport = updateRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/archive" {
			return &http.Response{StatusCode: 200, Body: partialTimeoutBody{bytes.NewReader(bytes.Repeat([]byte("stale partial"), 8192))}, Header: make(http.Header), ContentLength: -1}, nil
		}
		if req.URL.Path == "/sums" {
			return nil, fixtureTimeout{}
		}
		return base.RoundTrip(req)
	})
	t.Cleanup(func() { http.DefaultTransport = base })
	result := updateResultFixture(server.URL, archive)
	withFallbackAssetMetadata(t, result)
	var events []Progress
	if err := ApplyToWithProgress(context.Background(), result, exe, func(p Progress) { events = append(events, p) }); err != nil {
		t.Fatal(err)
	}
	reset := false
	for i := 1; i < len(events); i++ {
		if events[i].Phase == "downloading" && events[i].DownloadedBytes == 0 && events[i-1].DownloadedBytes > 0 {
			reset = true
		}
	}
	if !reset || events[len(events)-1].DownloadedBytes != int64(len(archive)) {
		t.Fatalf("retry progress was not reset to actual new bytes: %+v", events)
	}
}

func TestApplyAssetFallbackCancellationNeverRetries(t *testing.T) {
	exe := updateTargetFixture(t)
	archive := updateArchiveFixture(t, []byte("new binary"), binaryNameInArchive())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	server := useUpdateTLSFixture(t, func(w http.ResponseWriter, r *http.Request) { t.Error("cancelled request reached API") })
	base := http.DefaultTransport
	http.DefaultTransport = updateRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		cancel()
		return nil, fixtureTimeout{}
	})
	t.Cleanup(func() { http.DefaultTransport = base })
	result := updateResultFixture(server.URL, archive)
	withFallbackAssetMetadata(t, result)
	err := ApplyToWithProgress(ctx, result, exe, nil)
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("cancelled update retried: err=%v calls=%d", err, calls)
	}
	assertOldUpdateTarget(t, exe)
}

func TestAssetFallbackSafetyGates(t *testing.T) {
	for _, mode := range []string{"no API", "foreign API", "wrong ID", "query API", "HTTP 404", "certificate", "private redirect"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			server := useUpdateTLSFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if mode == "private redirect" {
					http.Redirect(w, r, "https://10.0.0.1/archive", http.StatusFound)
					return
				}
				t.Error("unexpected API fallback")
			})
			asset := Asset{ID: 101, URL: "https://api.github.com/repos/MoF-Crack/xdev/releases/assets/101", BrowserDownloadURL: server.URL + "/archive"}
			switch mode {
			case "no API":
				asset.URL = ""
			case "foreign API":
				asset.URL = "https://api.github.com/repos/other/xdev/releases/assets/101"
			case "wrong ID":
				asset.ID = 102
			case "query API":
				asset.URL += "?unexpected=1"
			}
			base := http.DefaultTransport
			http.DefaultTransport = updateRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if mode == "private redirect" {
					return base.RoundTrip(req)
				}
				if mode == "certificate" {
					return nil, x509.UnknownAuthorityError{}
				}
				status := 503
				if mode == "HTTP 404" {
					status = 404
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("fixture"))}, nil
			})
			t.Cleanup(func() { http.DefaultTransport = base })
			file, err := os.CreateTemp(t.TempDir(), "archive")
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			if err := downloadReleaseArchive(context.Background(), asset, file, nil); err == nil || calls != 1 {
				t.Fatalf("security/non-transient failure retried: calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestAssetFallbackAuthAndDeadline(t *testing.T) {
	remote.ConfigureGitHubToken("fixture-only-credential")
	t.Cleanup(func() { remote.ConfigureGitHubToken("") })
	server := useUpdateTLSFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/releases/assets/") {
			http.Redirect(w, r, "https://93.184.216.34/redirected", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte("API payload"))
	})
	base := http.DefaultTransport
	var primaryDeadline, apiDeadline time.Time
	calls := 0
	http.DefaultTransport = updateRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		switch req.URL.Host {
		case "api.github.com":
			apiDeadline, _ = req.Context().Deadline()
			if req.Header.Get("Authorization") != "Bearer fixture-only-credential" {
				t.Error("exact API origin missing credential")
			}
			return base.RoundTrip(req)
		case "93.184.216.34":
			if req.Header.Get("Authorization") != "" {
				t.Error("API credential leaked on external redirect")
			}
			clone := req.Clone(req.Context())
			u, _ := url.Parse(server.URL + "/redirected")
			clone.URL = u
			return base.RoundTrip(clone)
		default:
			primaryDeadline, _ = req.Context().Deadline()
			if req.Header.Get("Authorization") != "" {
				t.Error("browser download received API credential")
			}
			return nil, fixtureTimeout{}
		}
	})
	t.Cleanup(func() { http.DefaultTransport = base })
	file, err := os.CreateTemp(t.TempDir(), "archive")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	asset := Asset{ID: 101, URL: "https://api.github.com/repos/MoF-Crack/xdev/releases/assets/101", BrowserDownloadURL: server.URL + "/archive"}
	if err := downloadReleaseArchive(ctx, asset, file, nil); err != nil {
		t.Fatal(err)
	}
	if calls != 3 || !apiDeadline.After(primaryDeadline) || time.Until(primaryDeadline) > 2*time.Second {
		t.Fatalf("entrypoint budgets not bounded/reserved: calls=%d", calls)
	}
}

func TestAssetFallbackReturnsAPIErrorAndRedactsURLs(t *testing.T) {
	asset := Asset{ID: 101, URL: "https://api.github.com/repos/MoF-Crack/xdev/releases/assets/101", BrowserDownloadURL: "https://github.com/archive?jwt=fixture-private-query"}
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	calls := 0
	http.DefaultTransport = updateRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.URL.Host == "api.github.com" {
			return &http.Response{StatusCode: 403, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("fixture"))}, nil
		}
		return nil, fixtureTimeout{}
	})
	file, err := os.CreateTemp(t.TempDir(), "archive")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	err = downloadReleaseArchive(context.Background(), asset, file, nil)
	if err == nil || !strings.Contains(err.Error(), "api.github.com") || !strings.Contains(err.Error(), "403") || calls != 2 {
		t.Fatalf("final API error not returned or too many retries: %v calls=%d", err, calls)
	}
	http.DefaultTransport = updateRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, &url.Error{Op: "Get", URL: asset.BrowserDownloadURL, Err: fmt.Errorf("dial %s: %w", asset.BrowserDownloadURL, fixtureTimeout{})}
	})
	err = downloadAsset(context.Background(), asset.BrowserDownloadURL, io.Discard)
	if err == nil || strings.Contains(err.Error(), "fixture-private-query") || strings.Contains(err.Error(), "jwt=") || !strings.Contains(err.Error(), "github.com") || !strings.Contains(err.Error(), "timeout") {
		t.Fatal("signed URL error lost host/reason or leaked query")
	}
}
