package update

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// Use a real TLS server without changing production URL/redirect policy. Only
// the test transport maps api.github.com to the fixture's trusted TLS endpoint.
type fixtureTransport struct {
	target *url.URL
	base   http.RoundTripper
}

func (f fixtureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host != "api.github.com" {
		return f.base.RoundTrip(req)
	}
	clone := req.Clone(req.Context())
	u := *req.URL
	u.Scheme, u.Host = f.target.Scheme, f.target.Host
	clone.URL = &u
	return f.base.RoundTrip(clone)
}

func useUpdateTLSFixture(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	original := http.DefaultTransport
	http.DefaultTransport = fixtureTransport{target: target, base: server.Client().Transport}
	t.Cleanup(func() { http.DefaultTransport = original })
	return server
}

func releaseFixture(tag string, prerelease bool) map[string]any {
	return map[string]any{
		"tag_name": tag, "prerelease": prerelease, "draft": false,
		"assets": []Asset{
			{Name: assetNameForPlatform(tag), BrowserDownloadURL: "https://github.com/MoF-Crack/xdev/releases/download/" + tag + "/archive", Size: 100},
			{Name: "SHA256SUMS", BrowserDownloadURL: "https://github.com/MoF-Crack/xdev/releases/download/" + tag + "/SHA256SUMS", Size: 100},
		},
	}
}

func TestCheckPrereleaseDiscovery(t *testing.T) {
	cases := []struct {
		name, current, latest string
		update                bool
	}{
		{"alpha3 selects beta2", "1.0.0-alpha.3", "1.0.0-beta.2", true},
		{"beta1 selects beta2", "v1.0.0-beta.1", "1.0.0-beta.2", true},
		{"beta2 equal", "1.0.0-beta.2", "1.0.0-beta.2", false},
		{"rc does not downgrade to beta", "1.0.0-rc.1", "1.0.0-beta.2", false},
		{"stable excludes beta and mislabeled alpha", "0.9.0", "0.9.1", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			releases := []map[string]any{
				releaseFixture("v1.0.0-alpha.3", false), // /latest's non-prerelease alpha is not stable semver.
				releaseFixture("v0.9.1", false),
				releaseFixture("v1.0.0-beta.2", true),
				releaseFixture("v1.0.0-beta.1", true),
			}
			useUpdateTLSFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasPrefix(r.URL.Path, "/repos/MoF-Crack/xdev/releases") {
					t.Errorf("wrong release repo: %s", r.URL.Path)
				}
				if r.URL.Path == "/repos/MoF-Crack/xdev/releases/latest" {
					_ = json.NewEncoder(w).Encode(releases[0]) // Reproduce the old implementation, not a 404.
					return
				}
				_ = json.NewEncoder(w).Encode(releases)
			})
			got, err := Check(context.Background(), tc.current)
			if err != nil {
				t.Fatal(err)
			}
			if got.LatestVersion != tc.latest || got.HasUpdate != tc.update {
				t.Fatalf("Check(%s) = latest %s / update %v; want %s / %v", tc.current, got.LatestVersion, got.HasUpdate, tc.latest, tc.update)
			}
		})
	}
}

func TestCheckReleaseEligibilityAndSemanticOrder(t *testing.T) {
	draft := releaseFixture("v9.0.0", false)
	draft["draft"] = true
	missingSums := releaseFixture("v8.0.0", false)
	missingSums["assets"] = missingSums["assets"].([]Asset)[:1]
	guiOnly := releaseFixture("v7.0.0", false)
	guiOnly["assets"].([]Asset)[0].Name = "xdev-gui-windows-amd64-v7.0.0.zip"
	badURL := releaseFixture("v6.0.0", false)
	badURL["assets"].([]Asset)[0].BrowserDownloadURL = "http://example.test/archive"
	emptyURL := releaseFixture("v5.0.0", false)
	emptyURL["assets"].([]Asset)[1].BrowserDownloadURL = ""
	invalid := releaseFixture("v4.0.0junk", false)
	leadingZero := releaseFixture("v03.0.0", false)
	invalidPre := releaseFixture("v3.0.0-beta.01", true)
	emptyArchive := releaseFixture("v4.2.0", false)
	emptyArchive["assets"].([]Asset)[0].Size = 0
	pendingAsset := releaseFixture("v4.1.0", false)
	pendingAsset["assets"].([]Asset)[0].State = "starter"
	duplicateArchive := releaseFixture("v4.0.1", false)
	duplicateArchive["assets"] = append(duplicateArchive["assets"].([]Asset), duplicateArchive["assets"].([]Asset)[0])
	old := releaseFixture("v1.0.1", false)
	old["published_at"] = "2026-11-19T00:00:00Z"
	high := releaseFixture("v2.0.0", false)
	high["published_at"] = "2025-01-01T00:00:00Z"
	releases := []map[string]any{old, draft, missingSums, guiOnly, badURL, invalid, high, leadingZero, emptyURL, invalidPre, emptyArchive, pendingAsset, duplicateArchive}
	useUpdateTLSFixture(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(releases)
	})
	got, err := Check(context.Background(), "1.0.0-alpha.3")
	if err != nil {
		t.Fatal(err)
	}
	if got.LatestVersion != "2.0.0" || !got.HasUpdate {
		t.Fatalf("got %+v, want installable semantic maximum 2.0.0", got)
	}
}

func TestCheckEmptyOrInvalidReleases(t *testing.T) {
	cases := []struct {
		name     string
		releases []map[string]any
	}{
		{"empty list", []map[string]any{}},
		{"invalid tag only", []map[string]any{releaseFixture("not-a-version", false)}},
		{"beta only on stable", []map[string]any{releaseFixture("v1.0.0-beta.2", true)}},
		{"stable tag marked prerelease", []map[string]any{releaseFixture("v2.0.0", true)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			useUpdateTLSFixture(t, func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(tc.releases)
			})
			got, err := Check(context.Background(), "1.0.0")
			if err != nil {
				t.Fatal(err)
			}
			if got.HasUpdate || got.Release != nil || got.LatestVersion != "1.0.0" {
				t.Fatalf("no eligible release must be a no-update result: %+v", got)
			}
		})
	}
}

func TestCheckHTTPFailureAndCancellation(t *testing.T) {
	t.Run("HTTP failure", func(t *testing.T) {
		useUpdateTLSFixture(t, func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "fixture failure", http.StatusServiceUnavailable)
		})
		if _, err := Check(context.Background(), "1.0.0-beta.1"); err == nil || !strings.Contains(err.Error(), "503") {
			t.Fatalf("want HTTP error, got %v", err)
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		useUpdateTLSFixture(t, func(w http.ResponseWriter, r *http.Request) {
			cancel()
			<-r.Context().Done()
		})
		if _, err := Check(ctx, "1.0.0-beta.1"); !errors.Is(err, context.Canceled) {
			t.Fatalf("want context cancellation, got %v", err)
		}
	})
	for _, body := range []string{"{bad", "null", `{\"tag_name\":\"v1.0.0\"}`} {
		t.Run("invalid JSON "+body, func(t *testing.T) {
			useUpdateTLSFixture(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) })
			if _, err := Check(context.Background(), "1.0.0"); err == nil {
				t.Fatal("malformed/non-array release list must fail")
			}
		})
	}
}

func TestCheckPaginationIsBounded(t *testing.T) {
	requests := 0
	useUpdateTLSFixture(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
		if page < 1 || perPage < 1 || perPage > 100 {
			t.Errorf("bounded list query missing: %s", r.URL.String())
			perPage = 100
		}
		releases := make([]map[string]any, perPage)
		for i := range releases {
			releases[i] = releaseFixture("invalid", false)
		}
		if page == 2 {
			releases[0] = releaseFixture("v1.0.0-beta.2", true)
		}
		_ = json.NewEncoder(w).Encode(releases)
	})
	got, err := Check(context.Background(), "1.0.0-alpha.3")
	if err != nil {
		t.Fatal(err)
	}
	if requests < 2 || requests > 5 || got.LatestVersion != "1.0.0-beta.2" {
		t.Fatalf("bounded pagination: requests=%d result=%+v", requests, got)
	}
}
