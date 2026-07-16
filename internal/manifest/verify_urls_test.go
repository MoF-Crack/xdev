//go:build manual

// This test verifies download URLs by making network requests.
// Run with: go test -tags manual ./internal/manifest/...
// It is excluded from normal test runs because network access is unreliable.

package manifest

import (
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"
)

// TestVerifyDownloadURLs checks that all archive URLs in manifests
// return HTTP 200 (or 3xx redirect to 200) within a reasonable timeout.
func TestVerifyDownloadURLs(t *testing.T) {
	dir := "../manifests"
	cat, err := LoadCatalog(dir)
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}

	type result struct {
		tool    string
		version string
		osArch  string
		url     string
		status  int
		err     error
	}

	var (
		results []result
		mu      sync.Mutex
		wg      sync.WaitGroup
		sem     = make(chan struct{}, 5) // 5 concurrent requests
	)

	client := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Follow up to 5 redirects
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	for _, tool := range cat.Tools() {
		for _, v := range tool.VersionList {
			for _, a := range v.Archives {
				url := a.URL
				if url == "" {
					continue
				}

				wg.Add(1)
				go func(toolName, ver, osArch, u string) {
					defer wg.Done()
					sem <- struct{}{}
					defer func() { <-sem }()

					req, err := http.NewRequest("HEAD", u, nil)
					if err != nil {
						mu.Lock()
						results = append(results, result{toolName, ver, osArch, u, 0, err})
						mu.Unlock()
						return
					}

					// Add cookie for Oracle JDK
					if len(a.InstallArgs) > 0 {
						for _, arg := range a.InstallArgs {
							if arg == "oraclelicense=accept-securebackup-cookie" {
								req.AddCookie(&http.Cookie{
									Name:  "oraclelicense",
									Value: "accept-securebackup-cookie",
								})
							}
						}
					}

					resp, err := client.Do(req)
					if err != nil {
						mu.Lock()
						results = append(results, result{toolName, ver, osArch, u, 0, err})
						mu.Unlock()
						return
					}
					resp.Body.Close()

					mu.Lock()
					results = append(results, result{toolName, ver, osArch, u, resp.StatusCode, nil})
					mu.Unlock()
				}(tool.Name, v.Version, a.OS+"/"+a.Arch, url)
			}
		}
	}

	wg.Wait()

	// Report results
	ok := 0
	failed := 0
	redirect := 0
	for _, r := range results {
		switch {
		case r.err != nil:
			failed++
			t.Errorf("❌ %s@%s %s: %v\n   URL: %s", r.tool, r.version, r.osArch, r.err, r.url)
		case r.status >= 200 && r.status < 300:
			ok++
		case r.status >= 300 && r.status < 400:
			redirect++
			t.Logf("🔄 %s@%s %s: HTTP %d (redirect)\n   URL: %s", r.tool, r.version, r.osArch, r.status, r.url)
		case r.status == 403:
			failed++
			t.Errorf("🔒 %s@%s %s: HTTP 403 (Forbidden — may need cookie/auth)\n   URL: %s", r.tool, r.version, r.osArch, r.url)
		default:
			failed++
			t.Errorf("❌ %s@%s %s: HTTP %d\n   URL: %s", r.tool, r.version, r.osArch, r.status, r.url)
		}
	}

	t.Logf("\n📊 Download URL Verification Summary")
	t.Logf("   ✅ OK: %d", ok)
	t.Logf("   🔄 Redirect: %d", redirect)
	t.Logf("   ❌ Failed: %d", failed)
	t.Logf("   📁 Total: %d", len(results))

	if failed > 0 {
		t.Errorf("%d download URL(s) are not accessible", failed)
	}
}
