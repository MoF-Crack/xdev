package update

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/xmdemt/xdev/internal/util"
)

// Only a metadata-provided API URL for this exact repository and asset ID is
// an alternate entrypoint. Do not manufacture API URLs or trust arbitrary URLs.
func validAssetAPI(asset Asset) bool {
	u, err := url.Parse(asset.URL)
	return err == nil && asset.ID > 0 && u.Scheme == "https" && u.Host == "api.github.com" &&
		u.User == nil && u.Opaque == "" && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && u.RawPath == "" &&
		u.Path == "/repos/"+githubOwner+"/"+githubRepo+"/releases/assets/"+strconv.FormatInt(asset.ID, 10)
}

func assetHTTPClient(timeout time.Duration) *http.Client {
	secure := util.SecureRedirectCheck(10)
	return &http.Client{Timeout: timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		// Explicitly strip even for future redirect behavior changes. Bearer
		// credentials are for the exact GitHub API origin, never its asset CDN.
		if req.URL.Scheme != "https" || req.URL.Host != "api.github.com" {
			req.Header.Del("Authorization")
		}
		return secure(req, via)
	}}
}

// One attempt per entrypoint under one original-context deadline. A checksum,
// TLS, redirect-policy, cancellation or filesystem failure is never retried.
func downloadReleaseArchive(ctx context.Context, asset Asset, file *os.File, progress *progressReporter) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	err := primaryAssetAttempt(ctx, asset.BrowserDownloadURL, file, asset.Size, progress, 45*time.Second)
	if !canRetryAsset(ctx, asset, err) {
		return err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind update download: %w", err)
	}
	if err := file.Truncate(0); err != nil {
		return fmt.Errorf("reset update download: %w", err)
	}
	return downloadAssetAttempt(ctx, asset.URL, file, asset.Size, progress, true)
}

func downloadReleaseChecksums(ctx context.Context, asset Asset) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var buf bytes.Buffer
	limited := &checksumWriter{w: &buf, remaining: 1 << 20}
	err := primaryAssetAttempt(ctx, asset.BrowserDownloadURL, limited, 0, nil, 15*time.Second)
	if !canRetryAsset(ctx, asset, err) {
		if err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	}
	return checksumAttempt(ctx, asset.URL, true)
}

// Reserve at least half of a shorter caller budget for the alternate API.
// A per-entry deadline is retriable only while the original context is live.
func primaryAssetAttempt(ctx context.Context, endpoint string, w io.Writer, size int64, progress *progressReporter, budget time.Duration) error {
	if deadline, ok := ctx.Deadline(); ok {
		budget = min(budget, time.Until(deadline)/2)
	}
	attempt, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	err := downloadAssetAttempt(attempt, endpoint, w, size, progress, false)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &assetDownloadError{host: assetHost(endpoint), cause: context.DeadlineExceeded}
	}
	return err
}

func canRetryAsset(ctx context.Context, asset Asset, err error) bool {
	if err == nil || ctx.Err() != nil || !validAssetAPI(asset) || asset.URL == asset.BrowserDownloadURL {
		return false
	}
	var failure *assetDownloadError
	if !errors.As(err, &failure) {
		return false
	}
	if failure.status != 0 {
		return failure.status == 403 || failure.status == 408 || failure.status == 429 ||
			(failure.status >= 500 && failure.status <= 599)
	}
	var timeout net.Error
	return errors.As(failure.cause, &timeout) && timeout.Timeout()
}

func checksumAttempt(ctx context.Context, endpoint string, assetAPI bool) ([]byte, error) {
	var buf bytes.Buffer
	limited := &checksumWriter{w: &buf, remaining: 1 << 20}
	if err := downloadAssetAttempt(ctx, endpoint, limited, 0, nil, assetAPI); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type checksumWriter struct {
	w         io.Writer
	remaining int
}

func (w *checksumWriter) Write(p []byte) (int, error) {
	if len(p) > w.remaining {
		return 0, fmt.Errorf("SHA256SUMS exceeds 1 MiB")
	}
	n, err := w.w.Write(p)
	w.remaining -= n
	return n, err
}

// Error() intentionally omits signed URLs/userinfo/query strings while
// Unwrap preserves cancellation and timeout identity for callers and retry.
type assetDownloadError struct {
	host   string
	cause  error
	status int
}

func (e *assetDownloadError) Error() string {
	if e.status != 0 {
		return fmt.Sprintf("download from %s returned HTTP %d", e.host, e.status)
	}
	cause := e.cause
	var requestError *url.Error
	if errors.As(cause, &requestError) {
		cause = requestError.Err
	}
	return fmt.Sprintf("download from %s: %s", e.host, redactDownloadURLs(cause.Error()))
}

func (e *assetDownloadError) Unwrap() error { return e.cause }

func newAssetDownloadError(ctx context.Context, rawURL string, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	host := assetHost(rawURL)
	var requestError *url.Error
	if errors.As(err, &requestError) {
		if final, parseErr := url.Parse(requestError.URL); parseErr == nil && final.Host != "" {
			host = final.Host
		}
	}
	return &assetDownloadError{host: host, cause: err}
}

func assetHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err == nil && u.Host != "" {
		return u.Host
	}
	return "release asset"
}

var downloadURLPattern = regexp.MustCompile(`https?://[^\s"'<>]+`)

func redactDownloadURLs(message string) string {
	return downloadURLPattern.ReplaceAllStringFunc(message, func(raw string) string {
		u, err := url.Parse(strings.TrimRight(raw, "),;"))
		if err != nil || u.Host == "" {
			return "[release URL]"
		}
		return u.Scheme + "://" + u.Host
	})
}
