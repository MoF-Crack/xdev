package cli

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/util"
)

func newManifestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "manifest",
		Short: "Manage tool manifests",
		Long: `Manage tool manifests that define how xdev discovers, downloads
and installs development tools.

Manifests are YAML files that describe tool metadata, available
versions, download URLs and environment variables. Built-in manifests
are embedded in the binary; third-party manifests can be added from
Git repositories.`,
	}

	cmd.AddCommand(
		newManifestListCmd(),
		newManifestAddCmd(),
		newManifestRemoveCmd(),
		newManifestUpdateCmd(),
		// 票 18 / P2-15：index.yaml 重新生成（守卫要求索引与工具清单同步）。
		newManifestIndexCmd(),
		// A 方案风险 R4/R9：归档 URL 存活探测（上游改名检测，HEAD-only 可定期跑）。
		newManifestCheckURLsCmd(),
	)

	return cmd
}

// newManifestListCmd implements `xdev manifest list`.
func newManifestListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List loaded manifests",
		RunE: func(cmd *cobra.Command, args []string) error {
			cat := loadCatalog()
			tools := cat.Tools()
			if len(tools) == 0 {
				fmt.Println("(no manifests loaded)")
				return nil
			}

			headers := []string{"Name", "Display Name", "Category", "Latest"}
			var rows [][]string
			for _, t := range tools {
				latest := t.Latest
				if latest == "" && len(t.VersionList) > 0 {
					latest = t.VersionList[0].Version
				}
				rows = append(rows, []string{
					t.Name,
					t.DisplayName,
					t.Category,
					latest,
				})
			}
			renderTable(headers, rows)

			// Also show third-party manifests
			thirdPartyDir := thirdPartyManifestDir()
			if entries, err := os.ReadDir(thirdPartyDir); err == nil {
				var tpFiles []string
				for _, e := range entries {
					if !e.IsDir() && strings.HasSuffix(e.Name(), ".yaml") {
						tpFiles = append(tpFiles, e.Name())
					}
				}
				if len(tpFiles) > 0 {
					fmt.Printf("\nThird-party manifests (%s):\n", thirdPartyDir)
					for _, f := range tpFiles {
						fmt.Printf("  %s\n", strings.TrimSuffix(f, ".yaml"))
					}
				}
			}

			return nil
		},
	}
}

// newManifestAddCmd implements `xdev manifest add <repo-url>`.
func newManifestAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add <repo-url>",
		Short: "Add third-party manifests from a Git repository",
		Long: `Clone or update a Git repository containing manifest YAML files
and copy them into $XMDEV_HOME/manifests/third-party/.

The repository should contain .yaml files in its root or a manifests/ subdirectory.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repoURL := args[0]
			destDir := thirdPartyManifestDir()
			if err := os.MkdirAll(destDir, 0o755); err != nil {
				return fmt.Errorf("create third-party manifest dir: %w", err)
			}

			// Clone into a temp directory, then copy YAML files
			tmpDir, err := os.MkdirTemp("", "xdev-manifest-*")
			if err != nil {
				return fmt.Errorf("create temp dir: %w", err)
			}
			defer os.RemoveAll(tmpDir)

			fmt.Printf("Cloning %s ...\n", repoURL)
			// Security (M-13): Validate git clone URL to prevent ext:: protocol attacks
			if err := validateGitCloneURL(repoURL); err != nil {
				return fmt.Errorf("invalid manifest URL: %w", err)
			}
			cloneCmd := exec.Command("git", "clone", "--depth", "1", repoURL, tmpDir)
			if out, err := cloneCmd.CombinedOutput(); err != nil {
				return fmt.Errorf("git clone failed: %w\n%s", err, string(out))
			}

			// Find YAML files in the cloned repo
			var yamlFiles []string
			// Check root directory
			if entries, err := os.ReadDir(tmpDir); err == nil {
				for _, e := range entries {
					if !e.IsDir() && strings.HasSuffix(e.Name(), ".yaml") {
						yamlFiles = append(yamlFiles, filepath.Join(tmpDir, e.Name()))
					}
				}
			}
			// Check manifests/ subdirectory
			manifestsSubdir := filepath.Join(tmpDir, "manifests")
			if entries, err := os.ReadDir(manifestsSubdir); err == nil {
				for _, e := range entries {
					if !e.IsDir() && strings.HasSuffix(e.Name(), ".yaml") {
						yamlFiles = append(yamlFiles, filepath.Join(manifestsSubdir, e.Name()))
					}
				}
			}

			if len(yamlFiles) == 0 {
				fmt.Println("No .yaml manifest files found in the repository")
				return nil
			}

			// Validate and copy each YAML file
			copied := 0
			for _, src := range yamlFiles {
				// Validate it's a valid manifest
				data, err := os.ReadFile(src)
				if err != nil {
					fmt.Fprintf(stderr(), "⚠ failed to read %s: %v\n", filepath.Base(src), err)
					continue
				}
				var t manifest.Tool
				if err := parseYAML(data, &t); err != nil {
					fmt.Fprintf(stderr(), "⚠ invalid manifest %s: %v\n", filepath.Base(src), err)
					continue
				}
				if t.Name == "" {
					t.Name = strings.TrimSuffix(filepath.Base(src), ".yaml")
				}

				dest := filepath.Join(destDir, filepath.Base(src))
				if err := os.WriteFile(dest, data, 0o644); err != nil {
					fmt.Fprintf(stderr(), "⚠ failed to write %s: %v\n", filepath.Base(src), err)
					continue
				}
				fmt.Printf("✔ added manifest: %s (%s)\n", t.Name, filepath.Base(src))
				copied++
			}

			// Save repo URL for future updates
			saveThirdPartyRepo(repoURL)
			fmt.Printf("\n%d manifest(s) added from %s\n", copied, repoURL)
			return nil
		},
	}
}

// newManifestRemoveCmd implements `xdev manifest remove <name>`.
func newManifestRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a third-party manifest",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			// Security (M-9): Validate name is a safe path component to
			// prevent path traversal when constructing file paths.
			if _, err := util.SafeName(name); err != nil {
				return fmt.Errorf("manifest name %q is not valid: %w", name, err)
			}
			destDir := thirdPartyManifestDir()

			// Try exact filename first, then with .yaml suffix
			candidates := []string{
				filepath.Join(destDir, name),
				filepath.Join(destDir, name+".yaml"),
			}

			removed := false
			for _, c := range candidates {
				if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
					if err := os.Remove(c); err != nil {
						return fmt.Errorf("remove manifest %s: %w", name, err)
					}
					fmt.Printf("✔ removed manifest: %s\n", name)
					removed = true
					break
				}
			}

			if !removed {
				return fmt.Errorf("third-party manifest %q not found in %s", name, destDir)
			}
			return nil
		},
	}
}

// newManifestUpdateCmd implements `xdev manifest update`.
func newManifestUpdateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "Update all third-party manifests",
		RunE: func(cmd *cobra.Command, args []string) error {
			repos := loadThirdPartyRepos()
			if len(repos) == 0 {
				fmt.Println("No third-party manifest repositories configured")
				return nil
			}

			// 统计用于结尾汇总：修复前无论成败都打印「Update complete」，
			// 且写失败时也只打印「✔ updated」。现在失败会计入并返回错误。
			updated, failed := 0, 0

			for _, repoURL := range repos {
				fmt.Printf("Updating manifests from %s ...\n", repoURL)
				// Security (M-13): Validate git clone URL to prevent ext:: protocol attacks
				if err := validateGitCloneURL(repoURL); err != nil {
					fmt.Fprintf(stderr(), "⚠ invalid manifest URL %s: %v\n", repoURL, err)
					continue
				}
				// Re-run the add logic (clone + copy)
				tmpDir, err := os.MkdirTemp("", "xdev-manifest-update-*")
				if err != nil {
					fmt.Fprintf(stderr(), "⚠ failed to create temp dir: %v\n", err)
					continue
				}
				defer os.RemoveAll(tmpDir)

				cloneCmd := exec.Command("git", "clone", "--depth", "1", repoURL, tmpDir)
				if _, err := cloneCmd.CombinedOutput(); err != nil {
					fmt.Fprintf(stderr(), "⚠ git clone failed for %s: %v\n", repoURL, err)
					continue
				}

				destDir := thirdPartyManifestDir()
				if err := os.MkdirAll(destDir, 0o755); err != nil {
					fmt.Fprintf(stderr(), "⚠ create %s: %v\n", destDir, err)
					continue
				}
				// Copy YAML files from root and manifests/ subdirectory
				for _, searchDir := range []string{tmpDir, filepath.Join(tmpDir, "manifests")} {
					entries, err := os.ReadDir(searchDir)
					if err != nil {
						continue
					}
					for _, e := range entries {
						if !e.IsDir() && strings.HasSuffix(e.Name(), ".yaml") {
							src := filepath.Join(searchDir, e.Name())
							data, err := os.ReadFile(src)
							if err != nil {
								fmt.Fprintf(stderr(), "  ⚠ read %s: %v\n", e.Name(), err)
								failed++
								continue
							}
							// 2026-10-26（架构评估修复）：修复前这里丢弃了
							// os.WriteFile 的返回值，然后无条件打印「✔ updated」。
							// 磁盘满 / 权限不足 / 路径被占用时，用户看到成功
							// 而实际什么都没写。现在失败必报且计入统计。
							dest := filepath.Join(destDir, e.Name())
							if werr := os.WriteFile(dest, data, 0o644); werr != nil {
								fmt.Fprintf(stderr(), "  ⚠ write %s: %v\n", e.Name(), werr)
								failed++
								continue
							}
							fmt.Printf("  ✔ updated %s\n", e.Name())
							updated++
						}
					}
				}
			}
			fmt.Printf("Update complete: %d updated", updated)
			if failed > 0 {
				fmt.Printf(", %d failed", failed)
			}
			fmt.Println()
			if failed > 0 {
				return fmt.Errorf("%d manifest file(s) failed to update", failed)
			}
			return nil
		},
	}
}

// thirdPartyManifestDir returns the directory for third-party manifests.
func thirdPartyManifestDir() string {
	return filepath.Join(Cfg().Core.ManifestDir, "third-party")
}

// thirdPartyReposFile returns the path to the repo registry file.
func thirdPartyReposFile() string {
	return filepath.Join(Cfg().Core.ManifestDir, "third-party-repos.json")
}

// saveThirdPartyRepo appends a repo URL to the third-party repos file.
func saveThirdPartyRepo(repoURL string) {
	repos := loadThirdPartyRepos()
	for _, r := range repos {
		if r == repoURL {
			return // already saved
		}
	}
	repos = append(repos, repoURL)
	data, _ := json.MarshalIndent(repos, "", "  ")
	os.MkdirAll(filepath.Dir(thirdPartyReposFile()), 0o755)
	os.WriteFile(thirdPartyReposFile(), data, 0o644)
}

// loadThirdPartyRepos reads the list of third-party repo URLs.
func loadThirdPartyRepos() []string {
	data, err := os.ReadFile(thirdPartyReposFile())
	if err != nil {
		return nil
	}
	var repos []string
	if json.Unmarshal(data, &repos) != nil {
		return nil
	}
	return repos
}

// parseYAML unmarshals YAML data into a manifest.Tool.
func parseYAML(data []byte, out *manifest.Tool) error {
	return yaml.Unmarshal(data, out)
}

// validateGitCloneURL ensures the URL is safe to pass to git clone.
// Only https:// and http:// schemes are allowed to prevent ext:: and
// other dangerous git transport protocols.
func validateGitCloneURL(rawURL string) error {
	if strings.HasPrefix(rawURL, "ext::") {
		return fmt.Errorf("git ext:: protocol is not allowed (security risk)")
	}
	if strings.Contains(rawURL, "::") {
		return fmt.Errorf("git remote helper protocol is not allowed: %q", rawURL)
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	switch u.Scheme {
	case "https":
		return util.IsSafeHTTPSExternalURL(rawURL)
	case "http", "git":
		// Security (N-10): http:// and git:// are unencrypted and
		// vulnerable to MITM. Reject them rather than trusting a
		// warning that users may never see.
		return fmt.Errorf("%s:// URLs are not allowed for manifest add (use https://)", u.Scheme)
	case "ssh":
		return fmt.Errorf("ssh:// URLs are not allowed for manifest add (use https://)")
	case "file":
		return fmt.Errorf("file:// URLs are not allowed for manifest add (use https://)")
	default:
		return fmt.Errorf("unsupported URL scheme %q (use https://)", u.Scheme)
	}
}
