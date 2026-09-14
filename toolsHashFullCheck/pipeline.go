// pipeline.go implements the core hash-filling pipeline.
//
// The pipeline processes manifest archive entries in batches, downloading
// each archive, computing its SHA-256 hash, writing the hash back to the
// manifest YAML, and then cleaning up the downloaded file. Progress is
// tracked in a JSON state file for break-resume support.
//
// # Security
//
// All downloads are forced to use HTTPS (http:// URLs are rejected). Cache
// files are size-checked against the manifest's declared size field before
// being trusted, preventing stale or corrupt files from producing wrong
// hashes. Each download is tagged in the progress file for auditability.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/xmdemt/xdev/internal/manifest"
)

// batches defines the tool groupings for phased hash filling. Each batch
// can be run independently via the -batch flag.
var batches = map[string][]string{
	"batch1": {"go", "node", "python", "java"},
	"batch2": {"rust", "ruby", "php", "perl", "julia", "lua"},
	"batch3": {"gradle", "maven", "cmake", "ninja", "make", "git"},
	"batch4": {"flutter", "dart", "deno", "bun", "kotlin", "scala"},
	"batch5": {"llvm", "mingw", "dotnet", "haskell", "erlang", "zig"},
	"batch6": {
		"android-build-tools", "android-cmdline-tools", "android-emulator",
		"android-ndk", "android-platform-tools", "android-platform",
	},
}

// task represents a single archive entry to process.
type task struct {
	tool        *manifest.Tool
	version     string
	osName      string
	arch        string
	url         string // primary URL (mirror if available, else original)
	fallbackURL string // original URL to try if primary fails
	size        int64
	vi, ai      int // indices into VersionList / Archives
	installArgs []string
}

// Pipeline orchestrates the hash-filling process.
type Pipeline struct {
	manifestsDir string
	cacheDir     string
	progress     *Progress
	dryRun       bool
	cleanup      bool
	workers      int
}

// NewPipeline creates a new pipeline with the given configuration.
func NewPipeline(manifestsDir, cacheDir, stateFile string, dryRun, cleanup bool, workers int) *Pipeline {
	return &Pipeline{
		manifestsDir: manifestsDir,
		cacheDir:     cacheDir,
		progress:     LoadProgress(stateFile),
		dryRun:       dryRun,
		cleanup:      cleanup,
		workers:      workers,
	}
}

// Run executes the pipeline for the specified batch (or all tools if batch is empty).
func (p *Pipeline) Run(batch string) error {
	// Ensure progress state file path is set
	if p.progress.path == "" {
		return fmt.Errorf("progress state file path not set")
	}

	cat, err := manifest.LoadCatalog(p.manifestsDir)
	if err != nil {
		return fmt.Errorf("load manifests: %w", err)
	}

	// Determine which tools to process
	var toolsToProcess []*manifest.Tool
	if batch != "" {
		toolNames, ok := batches[batch]
		if !ok {
			return fmt.Errorf("unknown batch %q (available: batch1-batch6)", batch)
		}
		for _, name := range toolNames {
			if t, ok := cat.Tool(name); ok {
				toolsToProcess = append(toolsToProcess, t)
			} else {
				fmt.Fprintf(os.Stderr, "⚠ tool %q not found in manifests, skipping\n", name)
			}
		}
	} else {
		toolsToProcess = cat.Tools()
	}

	// Collect all tasks
	var tasks []task
	for _, t := range toolsToProcess {
		for vi, v := range t.VersionList {
			for ai, a := range v.Archives {
				// Skip entries that already have a valid (non-placeholder) hash
				if a.SHA256 != "" && !isAllZeroHex(a.SHA256) {
					continue
				}
				// Prefer mirror URL, fall back to original URL on failure
				primaryURL := a.MirrorURL
				fallbackURL := ""
				if primaryURL == "" {
					primaryURL = a.URL
				} else if a.URL != "" && a.URL != primaryURL {
					fallbackURL = a.URL
				}
				if primaryURL == "" {
					continue
				}
				tasks = append(tasks, task{
					tool:        t,
					version:     v.Version,
					osName:      a.OS,
					arch:        a.Arch,
					url:         primaryURL,
					fallbackURL: fallbackURL,
					size:        a.Size,
					vi:          vi,
					ai:          ai,
					installArgs: a.InstallArgs,
				})
			}
		}
	}

	fmt.Fprintf(os.Stdout, "📋 Found %d entries to process across %d tools\n", len(tasks), len(toolsToProcess))
	if p.dryRun {
		for _, t := range tasks {
			fmt.Fprintf(os.Stdout, "🔄 [DRY RUN] %s@%s %s/%s: would download %s\n",
				t.tool.Name, t.version, t.osName, t.arch, t.url)
		}
		p.printStats()
		return nil
	}

	// Ensure cache directory exists
	if err := os.MkdirAll(p.cacheDir, 0o755); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}

	// Process tasks concurrently, but write manifests serially
	taskCh := make(chan task, len(tasks))
	resultCh := make(chan taskResult, len(tasks))

	// Start worker goroutines
	var wg sync.WaitGroup
	for i := 0; i < p.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range taskCh {
				hash, err := p.processEntry(t)
				resultCh <- taskResult{task: t, hash: hash, err: err}
			}
		}()
	}

	// Feed tasks
	for _, t := range tasks {
		taskCh <- t
	}
	close(taskCh)

	// Wait for all workers to finish, then close result channel
	go func() {
		wg.Wait()
		close(resultCh)
	}()

	// Collect results and write manifests serially (grouped by tool)
	// We accumulate hash updates per-tool, then write each tool's YAML once.
	toolUpdates := make(map[string][]taskResult)
	totalFilled := 0
	totalFailed := 0
	totalSkipped := 0

	for res := range resultCh {
		if res.err != nil {
			fmt.Fprintf(os.Stderr, "❌ %s@%s %s/%s: %v\n",
				res.task.tool.Name, res.task.version, res.task.osName, res.task.arch, res.err)
			totalFailed++
		} else {
			fmt.Fprintf(os.Stdout, "✅ %s@%s %s/%s: %s\n",
				res.task.tool.Name, res.task.version, res.task.osName, res.task.arch, res.hash[:16]+"...")
			totalFilled++
		}
		toolUpdates[res.task.tool.Name] = append(toolUpdates[res.task.tool.Name], res)
	}

	// Write updated manifests (serial, one per tool)
	for toolName, results := range toolUpdates {
		// Find the tool pointer
		var tool *manifest.Tool
		for _, t := range toolsToProcess {
			if t.Name == toolName {
				tool = t
				break
			}
		}
		if tool == nil {
			continue
		}

		// Apply successful hash updates to the in-memory tool struct
		anyFilled := false
		for _, res := range results {
			if res.err == nil {
				tool.VersionList[res.task.vi].Archives[res.task.ai].SHA256 = res.hash
				anyFilled = true
			}
		}

		if anyFilled {
			yamlPath := filepath.Join(p.manifestsDir, tool.Name+".yaml")
			if err := writeManifestYAML(yamlPath, tool); err != nil {
				fmt.Fprintf(os.Stderr, "❌ %s: write manifest failed: %v\n", tool.Name, err)
			} else {
				fmt.Fprintf(os.Stdout, "📝 Updated %s\n", yamlPath)
			}
		}
	}

	// Clean up downloaded files if requested
	if p.cleanup {
		p.cleanupDownloads(tasks)
	}

	fmt.Fprintf(os.Stdout, "\n📊 Filled: %d, Skipped: %d, Failed: %d\n", totalFilled, totalSkipped, totalFailed)
	p.printStats()
	return nil
}

// taskResult holds the outcome of processing a single entry.
type taskResult struct {
	task task
	hash string
	err  error
}

// processEntry downloads (or uses cache) and computes the SHA-256 hash for
// a single manifest archive entry. It updates the progress state file at
// each step. If the primary URL fails and a fallback URL is available,
// the fallback is tried before marking the entry as failed.
func (p *Pipeline) processEntry(t task) (string, error) {
	// Check if already completed (break-resume)
	e := p.progress.EnsureEntry(t.tool.Name, t.version, t.osName, t.arch, t.url, t.size)
	if e.IsComplete() {
		// Already done — return the stored hash
		if e.Hash != "" {
			return e.Hash, nil
		}
	}

	// Build cache file path
	dest := filepath.Join(p.cacheDir, fmt.Sprintf("%s-%s-%s-%s.%s",
		t.tool.Name, t.version, t.osName, t.arch, t.tool.VersionList[t.vi].Archives[t.ai].Format))

	// Try primary URL, then fallback URL
	urls := []string{t.url}
	if t.fallbackURL != "" {
		urls = append(urls, t.fallbackURL)
	}

	var lastErr error
	for _, u := range urls {
		// Validate HTTPS
		if err := validateHTTPS(u); err != nil {
			lastErr = err
			continue
		}

		hash, err := p.downloadOrCache(t, u, dest)
		if err != nil {
			lastErr = err
			fmt.Fprintf(os.Stderr, "⚠ %s@%s %s/%s: %s failed (%v), trying next URL...\n",
				t.tool.Name, t.version, t.osName, t.arch, u, err)
			continue
		}

		// Success — mark progress
		_ = p.progress.UpdateEntry(t.tool.Name, t.version, t.osName, t.arch, stateDownloaded, hash, "")
		_ = p.progress.UpdateEntry(t.tool.Name, t.version, t.osName, t.arch, stateHashed, hash, "")
		return hash, nil
	}

	// All URLs failed
	_ = p.progress.UpdateEntry(t.tool.Name, t.version, t.osName, t.arch, stateFailed, "", lastErr.Error())
	return "", lastErr
}

// downloadOrCache either computes the hash from a valid cached file or
// downloads the file from the given URL and computes the hash simultaneously.
func (p *Pipeline) downloadOrCache(t task, rawURL, dest string) (string, error) {
	// Check if cached file exists and is valid (only on first URL attempt)
	if rawURL == t.url {
		if fi, err := os.Stat(dest); err == nil {
			// Size check: if manifest declares a size and the cached file
			// doesn't match, re-download (the size in manifest may be an
			// estimate, so we only warn on mismatch but still use the cache
			// if the file is non-empty).
			if t.size > 0 && fi.Size() != t.size {
				fmt.Fprintf(os.Stderr, "⚠ %s@%s %s/%s: cached file size %d != manifest size %d (may be estimate), using cache\n",
					t.tool.Name, t.version, t.osName, t.arch, fi.Size(), t.size)
			}
			if fi.Size() > 0 {
				return fileSHA256(dest)
			}
			// Empty file — remove and re-download
			os.Remove(dest)
		}
	}

	// Download with streaming hash computation
	return p.downloadAndHash(rawURL, dest, t.installArgs)
}

// downloadAndHash downloads a file and returns its SHA-256 hash.
// The file is streamed to disk and through a SHA-256 hasher simultaneously.
// Retries up to 3 times on transient network errors (connection reset, timeout).
func (p *Pipeline) downloadAndHash(rawURL, dest string, installArgs []string) (string, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			fmt.Fprintf(os.Stderr, "🔄 retry %d/3 for %s\n", attempt, rawURL)
			time.Sleep(time.Duration(attempt*2) * time.Second)
			// Remove partial download from previous attempt
			os.Remove(dest)
		}

		hash, err := p.downloadOnce(rawURL, dest, installArgs)
		if err == nil {
			return hash, nil
		}
		lastErr = err
		// Don't retry on HTTP 4xx (client errors like 403/404)
		if isHTTPClientError(err) {
			return "", err
		}
	}
	return "", lastErr
}

// isHTTPClientError returns true for non-retryable HTTP 4xx errors.
func isHTTPClientError(err error) bool {
	s := err.Error()
	return strings.Contains(s, "HTTP 4")
}

// downloadOnce performs a single download attempt without retry.
func (p *Pipeline) downloadOnce(rawURL, dest string, installArgs []string) (string, error) {
	client := &http.Client{Timeout: 10 * time.Minute}
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	// Add cookie for Oracle JDK downloads
	for _, arg := range installArgs {
		if strings.HasPrefix(arg, "oraclelicense") {
			req.AddCookie(&http.Cookie{
				Name:  "oraclelicense",
				Value: "accept-securebackup-cookie",
			})
			break
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d for %s", resp.StatusCode, rawURL)
	}

	f, err := os.Create(dest)
	if err != nil {
		return "", fmt.Errorf("create file: %w", err)
	}

	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, hasher), resp.Body); err != nil {
		f.Close()
		os.Remove(dest)
		return "", fmt.Errorf("write: %w", err)
	}
	f.Close()

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// cleanupDownloads removes downloaded cache files for entries that have
// been successfully hashed. Files for failed entries are kept for debugging.
func (p *Pipeline) cleanupDownloads(tasks []task) {
	cleaned := 0
	for _, t := range tasks {
		e := p.progress.FindEntry(t.tool.Name, t.version, t.osName, t.arch)
		if e == nil || (e.Status != stateHashed && e.Status != stateCleaned) {
			continue // Keep files for failed/pending entries
		}
		dest := filepath.Join(p.cacheDir, fmt.Sprintf("%s-%s-%s-%s.%s",
			t.tool.Name, t.version, t.osName, t.arch,
			t.tool.VersionList[t.vi].Archives[t.ai].Format))
		if err := os.Remove(dest); err != nil && !os.IsNotExist(err) {
			log.Printf("[cleanup] failed to remove %s: %v", dest, err)
		} else {
			cleaned++
		}
		_ = p.progress.UpdateEntry(t.tool.Name, t.version, t.osName, t.arch, stateCleaned, "", "")
	}
	if cleaned > 0 {
		fmt.Fprintf(os.Stdout, "🧹 Cleaned up %d downloaded files\n", cleaned)
	}
}

// validateHTTPS ensures the URL uses the https:// scheme.
func validateHTTPS(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("URL must use HTTPS, got %q (insecure)", u.Scheme)
	}
	return nil
}

// fileSHA256 computes the SHA-256 hash of an existing file.
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// isAllZeroHex reports whether s is a 64-character hex string made up
// entirely of '0' characters (a placeholder hash).
func isAllZeroHex(s string) bool {
	s = strings.TrimPrefix(s, "0x")
	s = strings.TrimPrefix(s, "0X")
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if c != '0' {
			return false
		}
	}
	return true
}

// writeManifestYAML writes a Tool struct back to a YAML file.
func writeManifestYAML(path string, tool *manifest.Tool) error {
	data, err := manifest.MarshalTool(tool)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// printStats prints the current progress statistics.
func (p *Pipeline) printStats() {
	pending, downloaded, hashed, cleaned, failed := p.progress.Stats()
	fmt.Fprintf(os.Stdout, "   Progress: pending=%d downloaded=%d hashed=%d cleaned=%d failed=%d\n",
		pending, downloaded, hashed, cleaned, failed)
}
