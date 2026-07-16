package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
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
				// Build SHA-256 map: version → os/arch → sha256
				sha256Map := map[string]map[string]string{}
				hasHash := false
				for _, v := range tool.VersionList {
					verMap := map[string]string{}
					for _, a := range v.Archives {
						key := a.OS + "/" + a.Arch
						if a.SHA256 != "" && !isAllZeroHex(a.SHA256) {
							verMap[key] = a.SHA256
							hasHash = true
						}
					}
					if len(verMap) > 0 {
						sha256Map[v.Version] = verMap
					}
				}

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

func newFillSHA256Cmd() *cobra.Command {
	var (
		manifestsDir string
		cacheDir     string
		dryRun       bool
	)

	cmd := &cobra.Command{
		Use:   "fill-sha256",
		Short: "Download archives and fill SHA-256 hashes in manifests",
		Long: `Download all archives declared in manifests, compute their SHA-256
hashes, and update the manifest YAML files with the real values.

This is a release-time operation: run it before signing manifests
to ensure all SHA-256 fields contain valid hashes.

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

			for _, tool := range cat.Tools() {
				toolFilled := 0 // per-tool counter to avoid needless YAML rewrites
				for vi, v := range tool.VersionList {
					for ai, a := range v.Archives {
						// Skip if already has a valid hash
						if a.SHA256 != "" && !isAllZeroHex(a.SHA256) {
							totalSkipped++
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
							continue
						}

						// Download and compute hash
						dest := filepath.Join(cacheDir, fmt.Sprintf("%s-%s-%s-%s.%s",
							tool.Name, v.Version, a.OS, a.Arch, a.Format))

						hash, err := downloadAndHash(url, dest, a.InstallArgs)
						if err != nil {
							fmt.Fprintf(os.Stderr, "❌ %s@%s %s/%s: %v\n",
								tool.Name, v.Version, a.OS, a.Arch, err)
							totalFailed++
							continue
						}

						// Update the manifest in memory
						tool.VersionList[vi].Archives[ai].SHA256 = hash
						fmt.Fprintf(os.Stdout, "✅ %s@%s %s/%s: %s\n",
							tool.Name, v.Version, a.OS, a.Arch, hash[:16]+"...")
						totalFilled++
						toolFilled++
					}
				}

				// Write updated manifest back to YAML (only if this tool had hashes filled)
				if toolFilled > 0 && !dryRun {
					yamlPath := filepath.Join(manifestsDir, tool.Name+".yaml")
					if err := writeManifestYAML(yamlPath, tool); err != nil {
						fmt.Fprintf(os.Stderr, "❌ %s: write failed: %v\n", tool.Name, err)
					}
				}
			}

			fmt.Fprintf(os.Stdout, "\n📊 Filled: %d, Skipped: %d, Failed: %d\n",
				totalFilled, totalSkipped, totalFailed)
			return nil
		},
	}

	cmd.Flags().StringVar(&manifestsDir, "manifests-dir", "manifests", "path to manifests directory")
	cmd.Flags().StringVar(&cacheDir, "cache-dir", "", "cache directory for downloads (default: temp dir)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be downloaded without modifying files")

	return cmd
}

// downloadAndHash downloads a file and returns its SHA-256 hash.
func downloadAndHash(url, dest string, installArgs []string) (string, error) {
	// Check cache
	if _, err := os.Stat(dest); err == nil {
		return fileSHA256(dest)
	}

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

	return hex.EncodeToString(hasher.Sum(nil)), nil
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
