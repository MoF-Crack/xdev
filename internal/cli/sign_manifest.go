package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/signer"
	"github.com/xmdemt/xdev/internal/util"
)

func newSignManifestsCmd() *cobra.Command {
	var (
		manifestsDir string
		privKeyPath  string
		outputDir    string
	)

	cmd := &cobra.Command{
		Use:   "sign-manifests",
		Short: "Sign manifest files with Ed25519 private key",
		Long: `Sign all manifest YAML files in the given directory.

For each <tool>.yaml, this command:
1. Extracts all SHA-256 hashes from the archives
2. Signs the hash map with the Ed25519 private key
3. Writes <tool>.yaml.sig next to the manifest file`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if privKeyPath == "" {
				return fmt.Errorf("--priv-key is required")
			}
			if manifestsDir == "" {
				manifestsDir = "manifests"
			}

			privKey, err := signer.LoadPrivateKey(privKeyPath)
			if err != nil {
				return fmt.Errorf("load private key: %w", err)
			}

			// 第六轮审计 F1（P0）：签名的必须是**磁盘上的当前状态**。
			//
			// 旧代码走 manifest.LoadCatalog——在已构建的二进制里它把嵌入清单
			// （构建时刻的快照，163 个空哈希）作为合并基座，磁盘上同版本的归档
			// 字段被整体丢弃（manifest.go loadMergedCatalog 的 embed-first 语义）。
			// 签名数据线的工序是「build → fill（写盘）→ sign（同一二进制）」，
			// 于是 sign 读到的永远是 fill 之前的嵌入状态：填好的哈希永远签不进去，
			// 产出的 .sig 与同一 artifact 里的填充后 manifest 互不一致——合并后
			// keyed 构建会把本数据线要修的每一个条目全部拒装（"possible hash
			// tampering"）。xdev manifest index 与 check-urls 已用磁盘专用加载器，
			// 签名端与之对齐：签磁盘上实际存在的内容。
			tools, err := manifest.LoadToolManifests(manifestsDir)
			if err != nil {
				return fmt.Errorf("load manifests: %w", err)
			}

			outDir := outputDir
			if outDir == "" {
				outDir = manifestsDir
			}
			if err := os.MkdirAll(outDir, 0o755); err != nil {
				return fmt.Errorf("create output dir: %w", err)
			}

			signed := 0
			skipped := 0
			for _, tool := range tools {
				// F7：工具名来自磁盘 manifest（不可信数据），进入 .sig 输出路径前
				// 必须通过路径安全校验（拒绝分隔符/盘符冒号/".."）。
				if err := validateManifestPathName(tool.Name); err != nil {
					fmt.Fprintf(os.Stderr, "❌ %v\n", err)
					continue
				}
				sha256Map, hasHash := manifest.BuildSignatureHashMap(tool)

				if !hasHash {
					fmt.Fprintf(os.Stdout, "⏭️  %s: no SHA-256 hashes, skipping\n", tool.Name)
					skipped++
					continue
				}

				sha256MapJSON := signer.BuildSHA256Map(sha256Map)
				sig, err := signer.SignManifest(privKey, tool.Name, sha256MapJSON)
				if err != nil {
					fmt.Fprintf(os.Stderr, "❌ %s: sign failed: %v\n", tool.Name, err)
					continue
				}

				sigPath := filepath.Join(outDir, tool.Name+".yaml.sig")
				if err := signer.WriteSignatureFile(sigPath, sig); err != nil {
					fmt.Fprintf(os.Stderr, "❌ %s: write failed: %v\n", tool.Name, err)
					continue
				}
				fmt.Fprintf(os.Stdout, "✅ %s: signed (%d versions)\n", tool.Name, len(sha256Map))
				signed++
			}

			fmt.Fprintf(os.Stdout, "\n📊 Signed: %d, Skipped: %d\n", signed, skipped)
			return nil
		},
	}

	cmd.Flags().StringVar(&manifestsDir, "manifests-dir", "manifests", "path to manifests directory")
	cmd.Flags().StringVar(&privKeyPath, "priv-key", "", "path to Ed25519 private key")
	cmd.Flags().StringVar(&outputDir, "output-dir", "", "output directory for .sig files (default: same as manifests-dir)")
	_ = cmd.MarkFlagRequired("priv-key")

	return cmd
}

// buildManifestSHA256Map 已迁移为 manifest.BuildSignatureHashMap（票 17 第 3 项）：
// 该规则（含 `os/arch#mirror` 键）同时被「重签后覆盖完整性」守卫复用，
// 放在 manifest 域可避免守卫侧复制一份规则而与签名侧漂移。

// fillEntry 是 fill-sha256 的单条结果（用于 --report 机器可读输出）。
//
// 为什么需要它（A 方案风险评估 R5）：原实现只把失败计数打印到 stderr 并**始终返回 nil**，
// CI 无法据此判定成败，容易把"部分填充"当成成功提交。现在默认失败即非零退出，
// 并把逐条结果写成 JSON 供流水线门禁与 PR 正文使用。
type fillEntry struct {
	Tool    string `json:"tool"`
	Version string `json:"version"`
	OSArch  string `json:"os_arch"`
	URL     string `json:"url"`
	Status  string `json:"status"` // filled | skipped | failed
	Hash    string `json:"hash,omitempty"`
	Error   string `json:"error,omitempty"`
}

func newFillSHA256Cmd() *cobra.Command {
	var (
		manifestsDir string
		cacheDir     string
		dryRun       bool
		reportPath   string
		allowPartial bool
		noCache      bool
		retries      int
		toolFilter   string
	)

	cmd := &cobra.Command{
		Use:   "fill-sha256",
		Short: "Download archives and fill SHA-256 hashes in manifests",
		Long: `Download all archives declared in manifests, compute their SHA-256
hashes, and update the manifest YAML files with the real values.

This is a release-time operation: run it before signing manifests
to ensure all SHA-256 fields contain valid hashes.

Safeguards (A-plan risk review):
  - downloaded content is validated against the declared archive format
    (magic bytes), so a 200 OK error page / login page can never be hashed
    into a manifest that then gets signed;
  - any download failure makes the command exit non-zero (--allow-partial
    opts out) so CI cannot mistake a partial fill for success;
  - --report writes a machine-readable JSON result for pipeline gating.

Use --dry-run to see what would be downloaded without actually
modifying any files.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if manifestsDir == "" {
				manifestsDir = "manifests"
			}
			if cacheDir == "" {
				cacheDir = filepath.Join(os.TempDir(), "xdev-fill-sha256")
			}

			// 第六轮审计 F1：与 sign-manifests 同因——fill 必须针对**磁盘状态**
			// 决策（LoadCatalog 的 embed-first 合并会把构建时刻的空哈希快照当成
			// 基座），否则 CI 工序「build → fill → sign」里 fill 看到的也是
			// 构建前状态，重跑时会整批重新下载已填好的哈希（依赖缓存兜底），
			// 且与磁盘加载的签名/巡检命令语义不一致。
			tools, err := manifest.LoadToolManifests(manifestsDir)
			if err != nil {
				return fmt.Errorf("load manifests: %w", err)
			}

			if err := os.MkdirAll(cacheDir, 0o755); err != nil {
				return fmt.Errorf("create cache dir: %w", err)
			}

			totalFilled := 0
			totalSkipped := 0
			totalFailed := 0
			var entries []fillEntry
			filter := map[string]bool{}
			for _, name := range strings.Split(toolFilter, ",") {
				if name = strings.TrimSpace(name); name != "" {
					filter[name] = true
				}
			}
			filteredOut := 0

			for _, tool := range tools {
				if len(filter) > 0 && !filter[tool.Name] {
					filteredOut++
					continue
				}
				toolFilled := 0 // per-tool counter to avoid needless YAML rewrites
				var pending []manifest.ArchiveSHAUpdate
				// 第六轮审计 F10：记录本工具在 entries 里的起始位置——写回失败时
				// 须把本轮已登记为 "filled" 的逐条目降级为 "failed"，否则 JSON 报告
				// 会把「哈希算出来了但没落盘」也记为成功，流水线与 PR 机器消费面
				// 会把不存在的哈希当成已填充（--allow-partial 下尤甚）。
				entryStart := len(entries)
				for vi, v := range tool.VersionList {
					for ai, a := range v.Archives {
						// Skip if already has a valid hash
						if a.SHA256 != "" && !util.IsAllZeroHash(a.SHA256) {
							totalSkipped++
							entries = append(entries, fillEntry{
								Tool: tool.Name, Version: v.Version, OSArch: a.OS + "/" + a.Arch,
								URL: a.URL, Status: "skipped", Hash: a.SHA256,
							})
							continue
						}

						// H-4（第三轮复核实测新增）：**必须用官方 URL 计算 `sha256`**。
						//
						// 原实现是 `if a.MirrorURL != "" { url = a.MirrorURL }` —— 有镜像时
						// 优先下载镜像，却把结果写进表示**官方**产物哈希的 `sha256` 字段。
						// 两个后果：
						//  ① 语义错误：`mirror_sha256` 的存在前提就是"镜像与官方可能不同字节"，
						//     把镜像哈希写进 `sha256`，会让从官方 URL 下载的校验失败；
						//  ② 可用性耦断：镜像挂掉/403 会直接让填充失败（实测清华 github-release
						//     镜像对 ninja 返回 403），把"官方可达"的条目也拖成缺口。
						// 因此这里只认官方 URL；镜像哈希若有需要，由 `mirror_sha256` 单独承载
						// （java.yaml 就是人工这样填的），不借用 `sha256`。
						url := a.URL
						if url == "" {
							continue
						}

						// Skip non-Windows platforms in dry run for speed
						if dryRun {
							fmt.Fprintf(os.Stdout, "🔄 [DRY RUN] %s@%s %s/%s: would download %s\n",
								tool.Name, v.Version, a.OS, a.Arch, url)
							totalFilled++
							toolFilled++
							entries = append(entries, fillEntry{
								Tool: tool.Name, Version: v.Version, OSArch: a.OS + "/" + a.Arch,
								URL: url, Status: "filled",
							})
							continue
						}

						// Download and compute hash
						//
						// 第六轮审计 F7：manifest 的 name/version/os/arch/format 是
						// **不可信数据**（第三方/手工放置的 manifest 可随意声明），
						// 直接拼进路径可把下载字节写到缓存目录之外（os: "../../.."）。
						// 先逐分量做路径安全校验，Join 后再断言仍在缓存目录内。
						fname, ferr := fillCacheFilename(tool.Name, v.Version, a.OS, a.Arch, a.Format)
						if ferr != nil {
							fmt.Fprintf(os.Stderr, "❌ %s@%s %s/%s: %v\n",
								tool.Name, v.Version, a.OS, a.Arch, ferr)
							totalFailed++
							entries = append(entries, fillEntry{
								Tool: tool.Name, Version: v.Version, OSArch: a.OS + "/" + a.Arch,
								URL: url, Status: "failed", Error: ferr.Error(),
							})
							continue
						}
						dest := filepath.Join(cacheDir, fname)
						if uerr := ensureUnderDir(dest, cacheDir); uerr != nil {
							fmt.Fprintf(os.Stderr, "❌ %s@%s %s/%s: %v\n",
								tool.Name, v.Version, a.OS, a.Arch, uerr)
							totalFailed++
							entries = append(entries, fillEntry{
								Tool: tool.Name, Version: v.Version, OSArch: a.OS + "/" + a.Arch,
								URL: url, Status: "failed", Error: uerr.Error(),
							})
							continue
						}

						hash, err := downloadAndHash(url, dest, a.InstallArgs, a.Format, noCache, retries)
						if err != nil {
							fmt.Fprintf(os.Stderr, "❌ %s@%s %s/%s: %v\n",
								tool.Name, v.Version, a.OS, a.Arch, err)
							totalFailed++
							entries = append(entries, fillEntry{
								Tool: tool.Name, Version: v.Version, OSArch: a.OS + "/" + a.Arch,
								URL: url, Status: "failed", Error: err.Error(),
							})
							continue
						}

						// Update the manifest in memory + 记录待写回项
						tool.VersionList[vi].Archives[ai].SHA256 = hash
						pending = append(pending, manifest.ArchiveSHAUpdate{
							Version: v.Version, OS: a.OS, Arch: a.Arch, SHA256: hash,
						})
						fmt.Fprintf(os.Stdout, "✅ %s@%s %s/%s: %s\n",
							tool.Name, v.Version, a.OS, a.Arch, hash[:16]+"...")
						totalFilled++
						toolFilled++
						entries = append(entries, fillEntry{
							Tool: tool.Name, Version: v.Version, OSArch: a.OS + "/" + a.Arch,
							URL: url, Status: "filled", Hash: hash,
						})
					}
				}

				// 写回：**文本级、只改 sha256 那一行**（加锁 + 原子写）。
				//
				// 为什么不再整文件重写（本批复核的实证）：MarshalTool 往返会丢掉全部注释并
				// 重排格式——实测 52 个 manifest 全走一遍是 5990→6989 行、220 行注释归零、
				// 最坏文件仅 5% 行保留。那会让"重填 163 个哈希"的 PR 完全不可评审，并把
				// manifest 里的说明性注释（上游改名依据/A-03 依赖评估/安装策略）一并抹掉。
				if toolFilled > 0 && !dryRun {
					// F7：工具名进入 yamlPath 拼接前做路径安全校验（manifest 不可信）。
					if nerr := validateManifestPathName(tool.Name); nerr != nil {
						fmt.Fprintf(os.Stderr, "❌ %s: %v\n", tool.Name, nerr)
						totalFailed++
						downgradeFilledEntries(entries[entryStart:], nerr.Error())
						entries = append(entries, fillEntry{Tool: tool.Name, Status: "failed", Error: nerr.Error()})
						continue
					}
					yamlPath := filepath.Join(manifestsDir, tool.Name+".yaml")
					changed, missed, err := manifest.UpdateArchiveSHA256InFile(yamlPath, pending)
					if err != nil {
						fmt.Fprintf(os.Stderr, "❌ %s: write failed: %v\n", tool.Name, err)
						totalFailed++
						// F10：写回失败 → 本工具已登记的 "filled" 逐条目降级为 "failed"，
						// 报告不得把未落盘的哈希记为成功。
						downgradeFilledEntries(entries[entryStart:], "write manifest: "+err.Error())
						entries = append(entries, fillEntry{
							Tool: tool.Name, Status: "failed", Error: "write manifest: " + err.Error(),
						})
					} else if len(missed) > 0 {
						// 定位不到条目 = 静默缺口，必须报出来（而不是假装成功）。
						// 注意用 missed 判定而非 changed!=len(pending)：后者会把"值本来就一样"
						// 的幂等情形误报为失败。
						detail := fmt.Sprintf("wrote %d hash(es); could not locate: %s",
							changed, strings.Join(missed, "; "))
						fmt.Fprintf(os.Stderr, "❌ %s: %s\n", tool.Name, detail)
						totalFailed++
						downgradeFilledEntries(entries[entryStart:], "write manifest: "+detail)
						entries = append(entries, fillEntry{Tool: tool.Name, Status: "failed", Error: detail})
					}
				}
			}

			fmt.Fprintf(os.Stdout, "\n📊 Filled: %d, Skipped: %d, Failed: %d\n",
				totalFilled, totalSkipped, totalFailed)

			if reportPath != "" {
				if err := writeFillReport(reportPath, entries, totalFilled, totalSkipped, totalFailed); err != nil {
					return fmt.Errorf("write report %s: %w", reportPath, err)
				}
				fmt.Fprintf(os.Stdout, "📄 report: %s\n", reportPath)
			}

			// R5：失败必须可见且默认失败即非零退出，避免 CI 把"部分填充"当成功。
			if totalFailed > 0 && !allowPartial {
				return fmt.Errorf("fill-sha256 failed for %d archive(s); re-run with --allow-partial only if the gaps are explicitly accepted (see --report)", totalFailed)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&manifestsDir, "manifests-dir", "manifests", "path to manifests directory")
	cmd.Flags().StringVar(&cacheDir, "cache-dir", "", "cache directory for downloads (default: temp dir)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be downloaded without modifying files")
	cmd.Flags().StringVar(&reportPath, "report", "", "write a machine-readable JSON report of every entry")
	cmd.Flags().BoolVar(&allowPartial, "allow-partial", false, "exit 0 even when some downloads failed (default: fail)")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "ignore cached downloads (recommended in CI for determinism)")
	cmd.Flags().IntVar(&retries, "retries", 2, "retry count per URL for transient failures")
	cmd.Flags().StringVar(&toolFilter, "tool", "", "comma-separated tool names to limit the run (incremental fills)")

	return cmd
}

// writeFillReport 写出逐条结果与汇总（供 CI 门禁 / PR 正文使用）。
func writeFillReport(path string, entries []fillEntry, filled, skipped, failed int) error {
	payload := struct {
		Filled  int         `json:"filled"`
		Skipped int         `json:"skipped"`
		Failed  int         `json:"failed"`
		Entries []fillEntry `json:"entries"`
	}{filled, skipped, failed, entries}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// downloadAndHash downloads a file and returns its SHA-256 hash.
//
// A 方案风险评估 R6：哈希是整条信任链的锚点，"填错＝把自己签死"。原实现只校验
// HTTP 200，因此 200 的登录页/错误页会被当成资产哈希（cangjie/Google/Cursor 这类
// 非标准下载端点尤其容易）。现在两处加固：
//  1. 下载后按声明的 format 做**魔数校验**（validateArchiveFile）；
//  2. 缓存命中也要通过同一校验，否则视为损坏并重新下载（--no-cache 直接忽略缓存）。
//
// 另外对瞬时失败做有限重试（--retries），避免一次抖动导致整条数据线返工。
func downloadAndHash(url, dest string, installArgs []string, format string, noCache bool, retries int) (string, error) {
	// Check cache（校验通过才复用）
	if !noCache {
		if _, err := os.Stat(dest); err == nil {
			// 第六轮审计 F6：缓存命中必须同时证明「这字节就是从这个 URL 下载的」。
			// 缓存键只含 tool/version/os/arch/format——同一元组换 URL（上游 yank 重传、
			// 拼写修正 PR）或跨运行投毒时，旧字节会被静默哈希进**要被签名**的 manifest。
			// sidecar <dest>.url 记录来源 URL；缺失或不匹配一律视为 miss 重新下载。
			urlSidecar := dest + ".url"
			sidecarData, serr := os.ReadFile(urlSidecar)
			if serr != nil || strings.TrimSpace(string(sidecarData)) != url {
				log.Printf("[fill] cached %s is from a different URL — re-downloading (cache key lacks URL; F6)", dest)
				_ = os.Remove(dest)
				_ = os.Remove(urlSidecar)
			} else if verr := validateArchiveFile(dest, format); verr == nil {
				return fileSHA256(dest)
			} else {
				log.Printf("[fill] cached %s failed validation (%v) — re-downloading", dest, verr)
				_ = os.Remove(dest)
				_ = os.Remove(urlSidecar)
			}
		}
	}

	var lastErr error
	attempts := retries + 1
	if attempts < 1 {
		attempts = 1
	}
	for attempt := 1; attempt <= attempts; attempt++ {
		hash, err := downloadAndHashOnce(url, dest, installArgs, format)
		if err == nil {
			return hash, nil
		}
		lastErr = err
		if attempt < attempts {
			wait := time.Duration(attempt) * 2 * time.Second
			log.Printf("[fill] attempt %d/%d failed for %s (%v) — retrying in %v", attempt, attempts, url, err, wait)
			time.Sleep(wait)
		}
	}
	return "", lastErr
}

// fillHTTPTransportHook 是测试接缝：让测试注入自定义 Transport（例如信任 httptest
// 的自签证书），而**不影响**生产客户端的重定向策略（SecureRedirectCheck 始终生效）。
// 与 internal/signer 的 testHookDirectTransport 同一模式。
var fillHTTPTransportHook func() http.RoundTripper

// fillHTTPClient 构造 fill 下载用的 HTTP 客户端：安全基线（H-1）在这里集中声明。
func fillHTTPClient() *http.Client {
	c := &http.Client{
		Timeout:       10 * time.Minute,
		CheckRedirect: util.SecureRedirectCheck(10),
	}
	if fillHTTPTransportHook != nil {
		c.Transport = fillHTTPTransportHook()
	}
	return c
}

func downloadAndHashOnce(url, dest string, installArgs []string, format string) (string, error) {
	// H-1（本批复核新增）：这个下载产出的是**要被签名的哈希**（信任锚），必须与仓库其它
	// 下载路径（downloader / update）同一条安全基线：限制重定向次数、拒绝 HTTPS→HTTP 降级、
	// 每一跳都过 IsSafeExternalURL（开放重定向 SSRF）。此前是裸 http.Client，默认会跟随
	// 最多 10 跳到任意协议/主机——与既有审计项 H-8/N-6/N-9 的要求不一致。
	//
	// 第六轮审计 F3：CheckRedirect 只作用于**重定向目标**（第 2..N 跳），初始 URL 从不经过
	// 任何校验——http:// 明文起点绕过 H-1 的降级防御，http://169.254.169.254/内网主机把
	// CI 变成 SSRF 探针（失败信息里的响应前缀还会进 fill-report/step summary）。
	// 采用产品统一基线 util.IsSafeExternalURL（与 AddCustomTool/TestMirrorConnectivity
	// 一致）：https 强制（仅 localhost 例外的本地服务语义）、拒绝私网/保留地址、
	// DNS 失败即 fail-closed。
	if err := util.IsSafeExternalURL(url); err != nil {
		return "", fmt.Errorf("fill-sha256 URL is not safe: %v", err)
	}
	client := fillHTTPClient()
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	// Add cookie for Oracle JDK downloads
	if len(installArgs) > 0 {
		for _, arg := range installArgs {
			if strings.HasPrefix(arg, "oraclelicense") {
				req.AddCookie(&http.Cookie{
					Name:  "oraclelicense",
					Value: "accept-securebackup-cookie",
				})
			}
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	f, err := os.Create(dest)
	if err != nil {
		return "", fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, hasher), resp.Body); err != nil {
		os.Remove(dest)
		return "", fmt.Errorf("write: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(dest)
		return "", fmt.Errorf("close: %w", err)
	}

	// R6：内容必须真的是声明的归档格式，否则整条信任锚会被污染。
	if err := validateArchiveFile(dest, format); err != nil {
		os.Remove(dest)
		return "", err
	}

	// F6：记录来源 URL sidecar，供缓存复用判定「字节与 URL 对应」。
	_ = os.WriteFile(dest+".url", []byte(url), 0o644)

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// validateArchiveFile 按声明的 format 校验文件魔数（头，dmg 另看尾部）。
//
// 目的不是做完整解析，而是**挡住"端点返回了非归档内容"**这一类错误：登录页、
// 错误页、同意条款页、空响应等。声明为空或未知 format 时不做判断（返回 nil），
// 以免误伤历史上没写 format 的条目。
func validateArchiveFile(path, format string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open for validation: %w", err)
	}
	defer f.Close()

	head := make([]byte, 512)
	n, _ := io.ReadFull(f, head)
	head = head[:n]
	if n == 0 {
		return fmt.Errorf("validation failed: empty download")
	}

	has := func(prefix string) bool { return bytes.HasPrefix(head, []byte(prefix)) }
	hasAt := func(off int, magic string) bool {
		return len(head) >= off+len(magic) && string(head[off:off+len(magic)]) == magic
	}

	switch strings.ToLower(strings.TrimSpace(format)) {
	case "zip":
		if has("PK\x03\x04") || has("PK\x05\x06") || has("PK\x07\x08") {
			return nil
		}
		return fmt.Errorf("validation failed: not a ZIP archive (got %q)", snippet(head))
	case "tar.gz", "tgz":
		if has("\x1f\x8b") {
			return nil
		}
		return fmt.Errorf("validation failed: not a gzip archive (got %q)", snippet(head))
	case "tar.xz", "txz":
		if has("\xfd7zXZ\x00") {
			return nil
		}
		return fmt.Errorf("validation failed: not an xz archive (got %q)", snippet(head))
	case "tar.zst", "tzst":
		if has("\x28\xb5\x2f\xfd") {
			return nil
		}
		return fmt.Errorf("validation failed: not a zstd archive (got %q)", snippet(head))
	case "tar":
		if hasAt(257, "ustar") {
			return nil
		}
		return fmt.Errorf("validation failed: not a tar archive (got %q)", snippet(head))
	case "7z":
		if has("7z\xbc\xaf\x27\x1c") {
			return nil
		}
		return fmt.Errorf("validation failed: not a 7z archive (got %q)", snippet(head))
	case "exe", "msi":
		// `format: exe` 在本项目里表示"可执行产物"，实际至少有四种载体（本批复核时被
		// Cursor 的 Linux 条目抓到：它声明 exe，下发的却是 AppImage）：
		//   - Windows PE（NSIS/Inno 安装器、绿色版 exe、部分自解压包）：MZ
		//   - MSI（OLE 复合文档）：D0 CF 11 E0
		//   - Linux AppImage / 打包二进制：ELF（\x7fELF）
		//   - macOS Mach-O：小端/大端的 32/64 位四种 magic + fat/universal（Cafebabe）
		//     （common 的 x86-64/arm64 是**小端**：CF FA ED FE；只认大端会把
		//     绝大多数 macOS 二进制误判——本批的单测就抓到过这个笔误）
		// 其余情况（HTML 下载页、JSON 错误体等）仍然拒收——这正是本校验要挡的东西。
		if has("MZ") || has("\xd0\xcf\x11\xe0") || has("\x7fELF") ||
			has("\xce\xfa\xed\xfe") || has("\xfe\xed\xfa\xce") ||
			has("\xcf\xfa\xed\xfe") || has("\xfe\xed\xfa\xcf") ||
			has("\xca\xfe\xba\xbe") {
			return nil
		}
		return fmt.Errorf("validation failed: not a Windows executable/installer, ELF or Mach-O binary (got %q)", snippet(head))
	case "dmg":
		// UDIF 的 magic 在**尾部** 512 字节的 "koly"；少数是 xar。
		if has("xar!") {
			return nil
		}
		if len(head) >= 4 {
			if fi, err := f.Stat(); err == nil && fi.Size() >= 512 {
				tail := make([]byte, 512)
				if _, err := f.ReadAt(tail, fi.Size()-512); err == nil && bytes.HasPrefix(tail, []byte("koly")) {
					return nil
				}
			}
		}
		return fmt.Errorf("validation failed: not a DMG/UDIF image (got %q)", snippet(head))
	case "sh":
		if has("#!") || has("#") {
			return nil
		}
		return fmt.Errorf("validation failed: not a shell script (got %q)", snippet(head))
	case "":
		return nil // 未声明格式：不判断（保持历史行为）
	default:
		return nil // 未知格式：不判断，交由后续流程
	}
}

// snippet 给校验失败信息截取可读前缀（同时暴露 HTML/JSON 这类"页面"特征）。
func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 40 {
		s = s[:40]
	}
	return strings.ReplaceAll(s, "\n", " ")
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

// downgradeFilledEntries（第六轮审计 F10）：写回失败时把本工具已登记为
// "filled" 的逐条目降级为 "failed"。entries 直接以 slice 引用传入（调用方随后
// append 的工具级失败条目不受影响——那是新元素）。
func downgradeFilledEntries(entries []fillEntry, reason string) {
	for i := range entries {
		if entries[i].Status == "filled" {
			entries[i].Status = "failed"
			entries[i].Error = reason
			// Hash 保留：报告消费面仍可知道"算出来了但没落盘"的值。
		}
	}
}

// fillCacheFilename（第六轮审计 F7）：把 manifest 供给的五个分量拼成缓存文件名，
// 任何分量含路径分隔符、盘符冒号、".." 或为空都拒绝——manifest 是不可信数据，
// 未校验的拼接可把下载字节写到缓存目录之外。
func fillCacheFilename(tool, version, osName, arch, format string) (string, error) {
	components := []struct{ kind, value string }{
		{"tool name", tool},
		{"version", version},
		{"os", osName},
		{"arch", arch},
		{"format", format},
	}
	for _, c := range components {
		if strings.TrimSpace(c.value) == "" {
			return "", fmt.Errorf("manifest %s is empty", c.kind)
		}
		if strings.ContainsAny(c.value, `/\:`) {
			return "", fmt.Errorf("manifest %s %q contains a path separator or drive colon", c.kind, c.value)
		}
		if c.value == "." || c.value == ".." || strings.Contains(c.value, "..") {
			return "", fmt.Errorf("manifest %s %q is not path-safe", c.kind, c.value)
		}
	}
	return fmt.Sprintf("%s-%s-%s-%s.%s", tool, version, osName, arch, format), nil
}

// ensureUnderDir asserts that path (already Joined) still sits directly under dir —
// the belt-and-braces containment check behind fillCacheFilename.
func ensureUnderDir(path, dir string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", path, err)
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", dir, err)
	}
	wantPrefix := absDir + string(filepath.Separator)
	if absPath != absDir && !strings.HasPrefix(absPath, wantPrefix) {
		return fmt.Errorf("resolved cache path %s escapes cache dir %s", absPath, absDir)
	}
	return nil
}

// validateManifestPathName（第六轮审计 F7）：manifest 供给的工具名进入输出/写回
// 路径（<name>.yaml.sig、<name>.yaml）前必须通过路径安全校验——LoadToolManifests
// 对磁盘上手工放置的 yaml 不做任何名字约束，"../x" 一类名字可把 .sig 写到
// manifests 目录之外。
func validateManifestPathName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("manifest tool name is empty")
	}
	if strings.ContainsAny(name, `/\:`) {
		return fmt.Errorf("manifest tool name %q contains a path separator or drive colon", name)
	}
	if name == "." || name == ".." || strings.Contains(name, "..") {
		return fmt.Errorf("manifest tool name %q is not path-safe", name)
	}
	return nil
}

// writeManifestYAML 已不再用于 fill 的回写路径（见 UpdateArchiveSHA256InFile 的说明：
// 整文件 MarshalTool 会丢注释并重排格式）。保留本函数供未来需要"整文件重排"的显式场景，
// 但它必须走原子写（H-3）。
func writeManifestYAML(path string, tool *manifest.Tool) error {
	data, err := manifest.MarshalTool(tool)
	if err != nil {
		return err
	}
	return util.AtomicWriteFile(path, data, 0o644)
}
