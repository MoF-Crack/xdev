package update

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

type updateRoundTripFunc func(*http.Request) (*http.Response, error)

func (f updateRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestUpdateTransportsPreserveSecureRedirectPolicy(t *testing.T) {
	for _, tc := range []struct{ name, location, want string }{
		{"HTTPS downgrade", "http://example.test/archive", "HTTPS downgrade blocked"},
		{"private host", "https://10.0.0.1/archive", "redirect target blocked"},
	} {
		for _, operation := range []string{"release metadata", "archive", "checksums"} {
			t.Run(tc.name+"/"+operation, func(t *testing.T) {
				server := useUpdateTLSFixture(t, func(w http.ResponseWriter, r *http.Request) {
					http.Redirect(w, r, tc.location, http.StatusFound)
				})
				fixtureURL, _ := url.Parse(server.URL)
				base := http.DefaultTransport
				// A failing security guard still cannot dial any external target
				// in this regression: unexpected outbound requests fail locally.
				http.DefaultTransport = updateRoundTripFunc(func(req *http.Request) (*http.Response, error) {
					if req.URL.Host != "api.github.com" && req.URL.Host != fixtureURL.Host {
						return nil, errors.New("unexpected outbound redirect escaped guard")
					}
					return base.RoundTrip(req)
				})
				t.Cleanup(func() { http.DefaultTransport = base })
				var err error
				switch operation {
				case "release metadata":
					_, err = Check(context.Background(), "1.0.0-beta.1")
				case "archive":
					err = downloadAsset(context.Background(), server.URL, io.Discard)
				case "checksums":
					_, err = downloadToMemory(context.Background(), server.URL)
				}
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("%s lost secure redirect policy: %v", operation, err)
				}
			})
		}
	}
}

func TestUpdateDownloadsRequireHTTPS(t *testing.T) {
	if err := downloadAsset(context.Background(), "http://example.test/archive", io.Discard); err == nil {
		t.Fatal("archive download accepted HTTP")
	}
	if _, err := downloadToMemory(context.Background(), "http://example.test/sums"); err == nil {
		t.Fatal("checksum download accepted HTTP")
	}
}

func TestUpdateDownloadsDoNotDisableTLSVerification(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("untrusted TLS fixture"))
	}))
	t.Cleanup(server.Close)
	if err := downloadAsset(context.Background(), server.URL, io.Discard); err == nil || !strings.Contains(err.Error(), "x509") {
		t.Fatalf("untrusted TLS certificate must fail: %v", err)
	}
}
