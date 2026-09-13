package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/xmdemt/xdev/internal/manifest"
)

// urlProbeResult 是单条 URL 的探测结果（--json 输出）。
type urlProbeResult struct {
	Tool       string `json:"tool"`
	Version    string `json:"version"`
	OSArch     string `json:"os_arch"`
	URL        string `json:"url"`
	Mirror     bool   `json:"mirror,omitempty"`
	Status     string `json:"status"` // ok | dead | error | allowlisted
	HTTPStatus int    `json:"http_status,omitempty"`
	Error      string `json:"error,omitempty"`
}

// newManifestCheckURLsCmd 实现 `xdev manifest check-urls`（A 方案风险 R4/R9 的检测手段）。
//
// 为什么需要它：本次实测发现上游会**改名资产**（llvm 22.x 的 Linux/macOS 从
// `clang+llvm-*` 改为 `LLVM-*`；zig 0.14+ 从 `zig-<os>-<arch>` 改为 `zig-<arch>-<os>`），
// 而 manifest 里写死的 URL 不会自己更新——结果是"有版本、装不了"。这类漂移只有主动探测
// 才能发现，故提供：
//   - 只发 HEAD（必要时 Range: bytes=0-0 的极小 GET），**不下载归档**，可定期跑；
//   - 4xx/5xx = dead，连接失败/超时 = error（区分"上游下线"与"我方网络受限"）；
//   - 支持白名单文件（允许已知不可达条目带原因豁免）与非零退出码，便于 CI 门禁。
func newManifestCheckURLsCmd() *cobra.Command {
	var (
		manifestsDir  string
		concurrency   int
		timeout       time.Duration
		jsonOut       string
		allowFile     string
		withMirrors   bool
		quiet         bool
		failOnErrorNet bool
	)
	cmd := &cobra.Command{
		Use:   "check-urls",
		Short: "Probe every archive URL for liveness (HEAD only, no downloads)",
		Long: `Probe all archive URLs declared in the manifests and report dead links.

Upstream projects rename or retire release assets; a hard-coded URL then only
fails at install time. This command detects that drift cheaply (HEAD requests,
no archive downloads) so it can run on a schedule.

Exit code is non-zero when any URL is dead (4xx/5xx). Network errors are
reported separately: pass --fail-on-network-error to treat them as failures too
(a dead proxy should not fail a CI job by default).

Use --allow-file to exempt known-unreachable URLs (one URL per line, '#' for
comments).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if manifestsDir == "" {
				manifestsDir = "manifests"
			}
			if concurrency <= 0 {
				concurrency = 8
			}
			if timeout <= 0 {
				timeout = 30 * time.Second
			}

			allow, err := readURLAllowlist(allowFile)
			if err != nil {
				return err
			}

			tools, err := manifest.LoadToolManifests(manifestsDir)
			if err != nil {
				return fmt.Errorf("load manifests: %w", err)
			}

			type target struct {
				result urlProbeResult
			}
			var targets []target
			seen := map[string]bool{}
			add := func(tool, version, osArch, u string, mirror bool) {
				if u == "" {
					return
				}
				key := u + fmt.Sprint(mirror)
				if seen[key] {
					return
				}
				seen[key] = true
				targets = append(targets, target{urlProbeResult{
					Tool: tool, Version: version, OSArch: osArch, URL: u, Mirror: mirror,
				}})
			}
			for _, t := range tools {
				for _, v := range t.VersionList {
					for _, a := range v.Archives {
						osArch := a.OS + "/" + a.Arch
						add(t.Name, v.Version, osArch, a.URL, false)
						if withMirrors {
							for _, mu := range a.EffectiveMirrorURLs() {
								add(t.Name, v.Version, osArch, mu, true)
							}
						}
					}
				}
			}

			client := &http.Client{Timeout: timeout}
			sem := make(chan struct{}, concurrency)
			var wg sync.WaitGroup
			for i := range targets {
				wg.Add(1)
				sem <- struct{}{}
				go func(i int) {
					defer wg.Done()
					defer func() { <-sem }()
					res := &targets[i].result
					if allow[res.URL] {
						res.Status = "allowlisted"
						return
					}
					code, netErr := probeURL(client, res.URL)
					res.HTTPStatus = code
					switch {
					case netErr != "":
						res.Status = "error"
						res.Error = netErr
					case code >= 200 && code < 400:
						res.Status = "ok"
					default:
						res.Status = "dead"
					}
				}(i)
			}
			wg.Wait()

			var results []urlProbeResult
			counts := map[string]int{}
			for _, t := range targets {
				results = append(results, t.result)
				counts[t.result.Status]++
			}
			sort.Slice(results, func(i, j int) bool {
				if results[i].Status != results[j].Status {
					return results[i].Status < results[j].Status
				}
				if results[i].Tool != results[j].Tool {
					return results[i].Tool < results[j].Tool
				}
				return results[i].Version < results[j].Version
			})

			if !quiet {
				for _, r := range results {
					if r.Status == "ok" || r.Status == "allowlisted" {
						continue
					}
					detail := r.Error
					if detail == "" {
						detail = fmt.Sprintf("HTTP %d", r.HTTPStatus)
					}
					fmt.Printf("%-10s %s@%s %-14s %s\n  %s\n",
						strings.ToUpper(r.Status), r.Tool, r.Version, r.OSArch, r.URL, detail)
				}
			}
			fmt.Printf("\n📊 URLs: %d (ok=%d dead=%d error=%d allowlisted=%d)\n",
				len(results), counts["ok"], counts["dead"], counts["error"], counts["allowlisted"])

			if jsonOut != "" {
				payload := struct {
					Total, OK, Dead, Error, Allowlisted int
					Results                             []urlProbeResult
				}{len(results), counts["ok"], counts["dead"], counts["error"], counts["allowlisted"], results}
				data, jerr := json.MarshalIndent(payload, "", "  ")
				if jerr != nil {
					return jerr
				}
				if werr := os.WriteFile(jsonOut, append(data, '\n'), 0o644); werr != nil {
					return fmt.Errorf("write %s: %w", jsonOut, werr)
				}
				fmt.Printf("📄 report: %s\n", jsonOut)
			}

			if counts["dead"] > 0 {
				return fmt.Errorf("%d archive URL(s) are dead — update the manifests (upstream asset renames) ", counts["dead"])
			}
			if failOnErrorNet && counts["error"] > 0 {
				return fmt.Errorf("%d archive URL(s) could not be reached (network) — use --allow-file for known-blocked hosts", counts["error"])
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&manifestsDir, "manifests-dir", "manifests", "path to manifests directory")
	cmd.Flags().IntVar(&concurrency, "concurrency", 8, "parallel probes")
	cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Second, "per-request timeout")
	cmd.Flags().StringVar(&jsonOut, "json", "", "write a machine-readable JSON report")
	cmd.Flags().StringVar(&allowFile, "allow-file", "", "file listing URLs to exempt (one per line, # comments)")
	cmd.Flags().BoolVar(&withMirrors, "mirrors", false, "also probe mirror URLs")
	cmd.Flags().BoolVar(&quiet, "quiet", false, "only print the summary")
	cmd.Flags().BoolVar(&failOnErrorNet, "fail-on-network-error", false, "treat unreachable (network) URLs as failures")
	return cmd
}

// readURLAllowlist 读取豁免清单（空行与 # 注释忽略）。
func readURLAllowlist(path string) (map[string]bool, error) {
	allow := map[string]bool{}
	if path == "" {
		return allow, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open allowlist %s: %w", path, err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if i := strings.Index(line, "#"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		if line != "" {
			allow[line] = true
		}
	}
	return allow, sc.Err()
}

// probeURL 探测单个 URL：先 HEAD；405/403/501 或 HEAD 失败时退回 Range GET（只取 1 字节）。
// 返回值：HTTP 状态码（拿不到时为 0）与网络错误描述（成功时为空串）。
func probeURL(c *http.Client, rawURL string) (int, string) {
	req, err := http.NewRequest("HEAD", rawURL, nil)
	if err == nil {
		if resp, derr := c.Do(req); derr == nil {
			resp.Body.Close()
			if resp.StatusCode < 400 {
				return resp.StatusCode, ""
			}
			if resp.StatusCode == http.StatusMethodNotAllowed ||
				resp.StatusCode == http.StatusForbidden ||
				resp.StatusCode == http.StatusNotImplemented {
				return rangeProbe(c, rawURL)
			}
			return resp.StatusCode, ""
		}
	}
	code, netErr := rangeProbe(c, rawURL)
	if code == 0 && netErr == "" {
		netErr = "request failed"
	}
	return code, netErr
}

func rangeProbe(c *http.Client, rawURL string) (int, string) {
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return 0, err.Error()
	}
	req.Header.Set("Range", "bytes=0-0")
	resp, err := c.Do(req)
	if err != nil {
		if ue, ok := err.(*url.Error); ok && ue.Timeout() {
			return 0, "timeout: " + err.Error()
		}
		return 0, err.Error()
	}
	defer resp.Body.Close()
	return resp.StatusCode, ""
}
