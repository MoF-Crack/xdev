// profile.go defines the `xdev profile export` and `xdev profile import`
// commands for environment configuration portability.
//
// Export captures the full xdev environment (installed tools, active versions,
// PATH mode, proxy, mirrors, env vars, favorites, custom tools) as a JSON file.
// Import applies a previously exported profile, installing missing tools and
// restoring all settings. This enables team-wide environment synchronization.
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/env"
	"github.com/xmdemt/xdev/internal/installer"
	"github.com/xmdemt/xdev/internal/util"
	"github.com/xmdemt/xdev/internal/version"
)

// collectExportEnvVars reads the HKCU user environment variables and keeps
// only names in the xdev whitelist (util.AllowedEnvVarNames), returning the
// entries ready to be serialized as the profile "envVars" array plus the
// number of variables that were skipped.
//
// 票 14（P0-12 / 审计 E-03）: the exported profile is a shareable/backup
// artifact, so it must only carry variables xdev itself manages. Before this
// filter the CLI dumped every user variable (GITHUB_TOKEN, MY_SECRET, …) into
// the file. The GUI export uses the fixed GetEnvVars enumeration and needs no
// change.
func collectExportEnvVars() ([]map[string]string, int) {
	userEnv := env.GetAllUserEnvVars()
	envVars := make([]map[string]string, 0, len(userEnv))
	skipped := 0
	for name, value := range userEnv {
		if !util.IsAllowedEnvVarName(name) {
			skipped++
			continue
		}
		envVars = append(envVars, map[string]string{
			"name":  name,
			"value": value,
			"level": "user",
		})
	}
	return envVars, skipped
}

// profileExportSummary builds the human-readable export summary line.
// 票 14: the summary must report how many non-xdev variables were left out,
// so a user can tell that filtering happened.
func profileExportSummary(path string, skipped int) string {
	return fmt.Sprintf("Profile exported to %s (skipped %d non-xdev variables)", path, skipped)
}

func newProfileCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Export and import environment profiles",
	}
	cmd.AddCommand(newExportCmd(), newImportCmd())
	return cmd
}

func newExportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "export <file>",
		Short: "Export current environment profile to a JSON file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := Cfg()
			_, reg := loadCatalogAndRegistry()

			profile := map[string]interface{}{
				"version":  version.Version,
				"exported": time.Now().Format(time.RFC3339),
				"pathMode": cfg.GetPathMode(),
				"proxy":    cfg.Proxy,
				"mirrors":  cfg.Mirror,
				"tools":    []map[string]string{},
				"envVars":  []map[string]string{},
			}

			// Collect installed tools and active versions
			tools := profile["tools"].([]map[string]string)
			for _, p := range reg.All() {
				activeVer, _ := activeVersion(p.Name())

				// List installed versions from install/<tool>/ directory
				// 20261021 审计 P1-4：只导出真实版本目录，避免 profile
				// 把 gopath / export_templates 当版本带给导入方。
				installDir := filepath.Join(cfg.Core.InstallDir, p.Name())
				versionNames, verr := util.ListVersionDirNames(installDir)
				if verr != nil {
					continue
				}
				for _, ver := range versionNames {
					entry := map[string]string{
						"tool":    p.Name(),
						"version": ver,
						"active":  "",
					}
					if ver == activeVer {
						entry["active"] = "true"
					}
					tools = append(tools, entry)
				}
			}
			profile["tools"] = tools

			// Collect user environment variables set by xdev.
			// 票 14（P0-12 / 审计 E-03）：只导出白名单内的变量名，
			// 其余（与 xdev 无关的敏感变量）一律跳过并计入摘要。
			envVars, skipped := collectExportEnvVars()
			profile["envVars"] = envVars

			// Collect GUI favorites and custom tools from the cache dir.
			// These are stored as raw JSON so the CLI does not need to
			// depend on the gui package's struct definitions; the data is
			// passed through verbatim.
			if favRaw, err := os.ReadFile(filepath.Join(cfg.Core.CacheDir, "favorites.json")); err == nil {
				var favs json.RawMessage
				if json.Unmarshal(favRaw, &favs) == nil {
					profile["favorites"] = favs
				}
			}
			if ctRaw, err := os.ReadFile(filepath.Join(cfg.Core.CacheDir, "custom_tools.json")); err == nil {
				var cts json.RawMessage
				if json.Unmarshal(ctRaw, &cts) == nil {
					profile["customTools"] = cts
				}
			}

			data, err := json.MarshalIndent(profile, "", "  ")
			if err != nil {
				return fmt.Errorf("marshal profile: %w", err)
			}

			if err := os.WriteFile(args[0], data, 0o644); err != nil {
				return fmt.Errorf("write profile: %w", err)
			}

			// Printed on the command's stdout (same stream as before; cobra's
			// default output is os.Stdout) so the summary stays testable.
			fmt.Fprintln(cmd.OutOrStdout(), profileExportSummary(args[0], skipped))
			return nil
		},
	}
}

// profileImportMaxSize 是 `xdev profile import` 允许的导入文件上限。
//
// 票 40（P2-33 / N-18）：GUI 侧的 ImportProfile 用 profileMaxSize（10 MB，
// internal/gui/app_profile.go）限制导入大小，CLI 侧原先直接 os.ReadFile
// 任意大小文件——同一个产品两条路径不对称，且一个被误选/恶意构造的超大
// JSON 会直接把 CLI 读爆内存。此处对齐同一个 10 MB 上限。
const profileImportMaxSize = 10 << 20

// readProfileForImport 读取导入文件，并强制 10 MB 上限。
//
// 先用 os.Stat 做一次廉价检查（能报出确切字节数），再用
// io.LimitReader 多读 1 字节兜住 Stat 与读取之间的 TOCTOU（文件在两次
// 调用之间长大时仍然只读入上限 + 1 字节即判定超限，不会把超大文件整体
// 读进内存）。超限一律返回明确错误，不做截断——截断后的 JSON 会以
// 「解析失败」误导用户。
func readProfileForImport(path string) ([]byte, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	if fi.Size() > profileImportMaxSize {
		return nil, fmt.Errorf("profile file is too large: %d bytes exceeds the %d MiB limit",
			fi.Size(), profileImportMaxSize>>20)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	defer f.Close()

	data, err := io.ReadAll(io.LimitReader(f, profileImportMaxSize+1))
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	if int64(len(data)) > profileImportMaxSize {
		return nil, fmt.Errorf("profile file is too large: exceeds the %d MiB limit", profileImportMaxSize>>20)
	}
	return data, nil
}

func newImportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "import <file>",
		Short: "Import environment profile from a JSON file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := readProfileForImport(args[0])
			if err != nil {
				return err
			}

			var profile struct {
				Version  string             `json:"version"`
				PathMode string             `json:"pathMode"`
				Proxy    config.ProxyConfig `json:"proxy"`
				Mirrors  map[string]string  `json:"mirrors"`
				Tools    []struct {
					Tool    string `json:"tool"`
					Version string `json:"version"`
					Active  string `json:"active"`
				} `json:"tools"`
				EnvVars []struct {
					Name  string `json:"name"`
					Value string `json:"value"`
					Level string `json:"level"`
				} `json:"envVars"`
				Favorites   json.RawMessage `json:"favorites,omitempty"`
				CustomTools json.RawMessage `json:"customTools,omitempty"`
			}

			if err := json.Unmarshal(data, &profile); err != nil {
				return fmt.Errorf("parse file: %w", err)
			}

			cfg := Cfg()
			_, reg := loadCatalogAndRegistry()
			ins := installer.New(cfg, reg)
			ins.SetNoEnvWrite(NoPathFix())

			// Apply PATH mode
			if profile.PathMode != "" {
				if err := cfg.SetPathMode(profile.PathMode); err != nil {
					return fmt.Errorf("set path mode: %w", err)
				}
			}

			// Apply proxy settings
			if err := cfg.SetProxy(profile.Proxy.HTTP, profile.Proxy.HTTPS, profile.Proxy.NoProxy, profile.Proxy.Enabled); err != nil {
				return fmt.Errorf("set proxy: %w", err)
			}

			// Apply mirror settings.
			// 票 16（P1-17 / P2-15）：mirrors 来自不可信的 profile JSON，
			// 必须逐项用 util.IsSafeHTTPSExternalURL 校验（与 GUI 的
			// ImportProfile 同一函数、同一语义）：非 https、localhost、
			// 内网/保留地址一律不写入 cfg.Mirror，只报告错误。
			if len(profile.Mirrors) > 0 {
				safeMirrors := make(map[string]string, len(profile.Mirrors))
				for name, mirrorURL := range profile.Mirrors {
					if err := util.IsSafeHTTPSExternalURL(mirrorURL); err != nil {
						fmt.Printf("  ✗ mirror %s: %v\n", name, err)
						continue
					}
					safeMirrors[name] = mirrorURL
				}
				// 全部被拒绝时不覆盖既有 mirrors（不写入空集合）。
				if len(safeMirrors) > 0 {
					cfg.Mirror = safeMirrors
					cfg.Save()
				}
			}

			// Install tools
			ctx := context.Background()
			ins = installer.New(cfg, reg)
			// F-32（P2）结构性守卫命中点：这里**重新构造**了 installer，
			// 上面 :236 的门随旧实例一起被丢弃——不补这一行，profile import
			// 的安装/激活阶段会重新写注册表（noEnvWrite 回落零值 false）。
			ins.SetNoEnvWrite(NoPathFix())
			var items []installer.BatchInstallItem
			for _, t := range profile.Tools {
				items = append(items, installer.BatchInstallItem{
					ToolName: t.Tool,
					Version:  t.Version,
				})
			}

			installed := 0
			failed := 0
			results := ins.InstallMany(ctx, items)
			for _, r := range results {
				if r.Status == "unsupported" {
					fmt.Printf("  ✗ %s: unknown tool\n", r.Tool)
					failed++
					continue
				}
				if r.Status == "already_installed" {
					fmt.Printf("  • %s@%s: already installed\n", r.Tool, r.Version)
				}
				if r.Status == "installed" {
					fmt.Printf("  ✔ %s@%s: installed\n", r.Tool, r.Version)
				}
				if r.Status == "failed" {
					fmt.Printf("  ✗ %s@%s: %v\n", r.Tool, r.Version, r.Error)
					failed++
					continue
				}
				installed++

				// Set active version
				activeField := ""
				for _, t := range profile.Tools {
					if t.Tool == r.Tool {
						activeField = t.Active
						break
					}
				}
				if activeField == "true" {
					p, ok := reg.Get(r.Tool)
					if ok {
						installPath, dirErr := p.InstallDir(cfg.Core.InstallDir, r.Version)
						if dirErr != nil {
							fmt.Printf("  ✗ %s use %s: %v\n", r.Tool, r.Version, dirErr)
						} else if err := ins.Activate(p, installPath, r.Version); err != nil {
							fmt.Printf("  ✗ %s use %s: %v\n", r.Tool, r.Version, err)
						} else {
							fmt.Printf("  ✔ %s@%s: activated\n", r.Tool, r.Version)
						}
					}
				}
			}

			// Set environment variables
			for _, e := range profile.EnvVars {
				if e.Level == "user" {
					if err := env.SetUserEnvVar(e.Name, e.Value); err != nil {
						fmt.Printf("  ✗ env %s: %v\n", e.Name, err)
					}
				}
			}

			// Restore GUI favorites and custom tools into the cache dir.
			// The data is written verbatim so the GUI can pick it up on
			// next load.
			if len(profile.Favorites) > 0 {
				favPath := filepath.Join(cfg.Core.CacheDir, "favorites.json")
				if err := os.MkdirAll(cfg.Core.CacheDir, 0o755); err == nil {
					if err := os.WriteFile(favPath, profile.Favorites, 0o644); err != nil {
						fmt.Printf("  ✗ favorites: %v\n", err)
					} else {
						fmt.Printf("  ✔ favorites: restored\n")
					}
				}
			}
			if len(profile.CustomTools) > 0 {
				ctPath := filepath.Join(cfg.Core.CacheDir, "custom_tools.json")
				if err := os.MkdirAll(cfg.Core.CacheDir, 0o755); err == nil {
					if err := os.WriteFile(ctPath, profile.CustomTools, 0o644); err != nil {
						fmt.Printf("  ✗ custom tools: %v\n", err)
					} else {
						fmt.Printf("  ✔ custom tools: restored\n")
					}
				}
			}

			fmt.Printf("\nImport complete: %d installed, %d failed\n", installed, failed)
			return nil
		},
	}
}
