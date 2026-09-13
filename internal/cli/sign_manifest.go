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

			cat, err := manifest.LoadCatalog(manifestsDir)
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
			for _, tool := range cat.Tools() {
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

			cat, err := manifest.LoadCatalog(manifestsDir)
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

			for _, tool := range cat.Tools() {
				if len(filter) > 0 && !filter[tool.Name] {
					filteredOut++
					continue
				}
				toolFilled := 0 // per-tool counter to avoid needless YAML rewrites
				for vi, v := range tool.VersionList {
					for ai, a := range v.Archives {
						// Skip if already has a valid hash
						if a.SHA256 != "" && !isAllZeroHex(a.SHA256) {
							totalSkipped++
							entries = append(entries, fillEntry{
								Tool: tool.Name, Version: v.Version, OSArch: a.OS + "/" + a.Arch,
								URL: a.URL, Status: "skipped", Hash: a.SHA256,
							})
							continue
						}

						url := a.URL
						if a.MirrorURL != "" {
							url = a.MirrorURL
						}
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
						dest := filepath.Join(cacheDir, fmt.Sprintf("%s-%s-%s-%s.%s",
							tool.Name, v.Version, a.OS, a.Arch, a.Format))

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

						// Update the manifest in memory
						tool.VersionList[vi].Archives[ai].SHA256 = hash
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

				// Write updated manifest back to YAML (only if this tool had hashes filled)
				if toolFilled > 0 && !dryRun {
					yamlPath := filepath.Join(manifestsDir, tool.Name+".yaml")
					if err := writeManifestYAML(yamlPath, tool); err != nil {
						fmt.Fprintf(os.Stderr, "❌ %s: write failed: %v\n", tool.Name, err)
						totalFailed++
						entries = append(entries, fillEntry{
							Tool: tool.Name, Status: "failed", Error: "write manifest: " + err.Error(),
						})
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
// 另外对瞬时失败做有限重试（--retries），避免一次抖动导致整条数据线返工。
func downloadAndHash(url, dest string, installArgs []string, format string, noCache bool, retries int) (string, error) {
	// Check cache（校验通过才复用）
	if !noCache {
		if _, err := os.Stat(dest); err == nil {
			if verr := validateArchiveFile(dest, format); verr == nil {
				return fileSHA256(dest)
			} else {
				log.Printf("[fill] cached %s failed validation (%v) — re-downloading", dest, verr)
				_ = os.Remove(dest)
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

func downloadAndHashOnce(url, dest string, installArgs []string, format string) (string, error) {
	client := &http.Client{Timeout: 10 * time.Minute}
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
		// NSIS/Inno 安装器是 PE（MZ）；MSI 是 OLE 复合文档（D0 CF 11 E0）。
		if has("MZ") || has("\xd0\xcf\x11\xe0") {
			return nil
		}
		return fmt.Errorf("validation failed: not a Windows executable/installer (got %q)", snippet(head))
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

// isAllZeroHex reports whether s is a hex string made up entirely of '0'.
// It validates that the string is exactly 64 characters (SHA-256 hex length)
// so that malformed short strings like "0000" are not mistaken for placeholders.
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
