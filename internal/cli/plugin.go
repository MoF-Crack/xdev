package cli

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/xmdemt/xdev/internal/plugin"
	"github.com/xmdemt/xdev/internal/util"
)

func newPluginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plugin",
		Short: "Manage Lua plugins",
		Long: `Manage Lua plugins that extend xdev with custom tools.

Lua plugins are .lua files stored in <xdev-home>/plugins/ — the same
directory that plugin registration scans (single source of truth).
Each plugin defines a PLUGIN table and Lua functions that
xdev calls to discover versions, download archives and set
environment variables.`,
	}

	cmd.AddCommand(
		newPluginListCmd(),
		newPluginInstallCmd(),
		newPluginCreateCmd(),
		newPluginRemoveCmd(),
	)

	return cmd
}

// newPluginListCmd implements `xdev plugin list`.
func newPluginListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List installed Lua plugins",
		RunE: func(cmd *cobra.Command, args []string) error {
			pluginsDir := pluginDir()
			entries, err := os.ReadDir(pluginsDir)
			if err != nil {
				if os.IsNotExist(err) {
					fmt.Println("(no Lua plugins installed)")
					return nil
				}
				return fmt.Errorf("read plugins dir: %w", err)
			}

			engine := plugin.NewLuaEngine()
			headers := []string{"Name", "Display Name", "Category", "File"}
			var rows [][]string
			for _, e := range entries {
				if e.IsDir() || !strings.HasSuffix(e.Name(), ".lua") {
					continue
				}
				path := filepath.Join(pluginsDir, e.Name())
				p, err := engine.LoadPlugin(path)
				if err != nil {
					fmt.Fprintf(stderr(), "⚠ failed to load %s: %v\n", e.Name(), err)
					continue
				}
				rows = append(rows, []string{
					p.Name(),
					p.DisplayName(),
					p.Category(),
					e.Name(),
				})
			}
			if len(rows) == 0 {
				fmt.Println("(no Lua plugins installed)")
				return nil
			}
			renderTable(headers, rows)
			return nil
		},
	}
}

// newPluginInstallCmd implements `xdev plugin install <url>`.
func newPluginInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install <url>",
		Short: "Download and install a Lua plugin from a URL",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rawURL := args[0]
			pluginsDir := pluginDir()
			if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
				return fmt.Errorf("create plugins dir: %w", err)
			}

			// Security (H-3 fix): only HTTPS plugin sources are allowed.
			if u, parseErr := url.Parse(rawURL); parseErr != nil || u.Scheme != "https" {
				return fmt.Errorf("plugin install URL must use HTTPS: %q", rawURL)
			}

			// Derive filename from URL; the name must be a single safe
			// path component confined under the plugins dir.
			filename := filepath.Base(rawURL)
			if !strings.HasSuffix(filename, ".lua") {
				filename += ".lua"
			}
			if _, err := util.SafeName(filename); err != nil {
				return fmt.Errorf("unsafe plugin file name %q: %w", filename, err)
			}
			dest := filepath.Join(pluginsDir, filename)
			if err := util.ValidateSubPath(pluginsDir, dest); err != nil {
				return fmt.Errorf("plugin path %q escapes plugins dir: %w", dest, err)
			}

			fmt.Printf("Downloading %s ...\n", rawURL)
			client := &http.Client{
				Timeout:       5 * time.Minute,
				CheckRedirect: util.SecureRedirectCheck(10),
			}
			resp, err := client.Get(rawURL)
			if err != nil {
				return fmt.Errorf("download failed: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
			}

			f, err := os.Create(dest)
			if err != nil {
				return fmt.Errorf("create file: %w", err)
			}
			defer f.Close()

			// Security (H-3 fix): cap the download at 4 MiB — Lua plugin
			// scripts are small text files; an unbounded io.Copy lets a
			// hostile server fill the disk.
			const maxPluginSize = 4 << 20
			if _, err := io.Copy(f, io.LimitReader(resp.Body, maxPluginSize)); err != nil {
				os.Remove(dest)
				return fmt.Errorf("write file: %w", err)
			}

			// Validate the plugin loads correctly.
			engine := plugin.NewLuaEngine()
			p, err := engine.LoadPlugin(dest)
			if err != nil {
				os.Remove(dest)
				return fmt.Errorf("plugin validation failed: %w", err)
			}

			fmt.Printf("✔ installed Lua plugin %s (%s) -> %s\n", p.Name(), p.DisplayName(), dest)
			return nil
		},
	}
}

// newPluginCreateCmd implements `xdev plugin create <name>`.
func newPluginCreateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "create <name>",
		Short: "Create a Lua plugin template file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			// Security (H-1 fix): the name becomes both a file name under
			// the plugins dir and Lua string literals in the template —
			// validate it as a single safe path component (no ../, no
			// separators) and additionally reject Lua string-breaking
			// characters (quote/backslash) so the template cannot be turned
			// into executable Lua on the next plugin load.
			if _, err := util.SafeName(name); err != nil {
				return fmt.Errorf("invalid plugin name %q: %w", name, err)
			}
			if strings.ContainsAny(name, "\"'\\\\") {
				return fmt.Errorf("invalid plugin name %q: must not contain quotes or backslashes (Lua template safety)", name)
			}
			pluginsDir := pluginDir()
			if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
				return fmt.Errorf("create plugins dir: %w", err)
			}

			filename := name
			if !strings.HasSuffix(filename, ".lua") {
				filename += ".lua"
			}
			dest := filepath.Join(pluginsDir, filename)
			if err := util.ValidateSubPath(pluginsDir, dest); err != nil {
				return fmt.Errorf("plugin path %q escapes plugins dir: %w", dest, err)
			}

			if _, err := os.Stat(dest); err == nil {
				return fmt.Errorf("plugin file already exists: %s", dest)
			}

			template := fmt.Sprintf(`-- Lua plugin for %s
-- Place this file in $XMDEV_HOME/plugins/

PLUGIN = {
    name = "%s",
    display_name = "%s",
    category = "system",   -- language | mobile | build | system
    shims = {},            -- e.g. {"mytool", "mytoolctl"}
}

-- Return the list of available versions.
function available()
    return {
        -- "1.0.0",
        -- "1.1.0",
    }
end

-- Return download info for a specific version + OS + arch.
-- os: "windows" | "darwin" | "linux"
-- arch: "amd64" | "arm64" | "x86_64"
function archive(version, os, arch)
    return {
        url = "https://example.com/%s/" .. version .. "/%s-" .. version .. "-" .. os .. "-" .. arch .. ".zip",
        format = "zip",   -- zip | tar.gz | tar.xz
        strip = 1,        -- number of leading path components to strip
    }
end

-- Optional: return environment variables to set when this tool is active.
-- function env_vars(install_path)
--     return {
--         MYTOOL_HOME = install_path,
--     }
-- end
`, name, name, name, name, name)

			if err := os.WriteFile(dest, []byte(template), 0o644); err != nil {
				return fmt.Errorf("write template: %w", err)
			}

			fmt.Printf("✔ created Lua plugin template: %s\n", dest)
			return nil
		},
	}
}

// newPluginRemoveCmd implements `xdev plugin remove <name>`.
func newPluginRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove an installed Lua plugin",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			// Security (H-2 fix): validate the name before using it to
			// build the deletion path — `plugin remove ../../x.lua` must
			// never delete outside the plugins dir.
			if _, err := util.SafeName(name); err != nil {
				return fmt.Errorf("invalid plugin name %q: %w", name, err)
			}
			pluginsDir := pluginDir()
			filename := name
			if !strings.HasSuffix(filename, ".lua") {
				filename += ".lua"
			}
			path := filepath.Join(pluginsDir, filename)
			if err := util.ValidateSubPath(pluginsDir, path); err != nil {
				return fmt.Errorf("plugin path %q escapes plugins dir: %w", path, err)
			}
			if err := os.Remove(path); err != nil {
				if os.IsNotExist(err) {
					return fmt.Errorf("plugin %q not found at %s", name, path)
				}
				return fmt.Errorf("remove plugin: %w", err)
			}
			fmt.Printf("✔ removed Lua plugin %s (%s)\n", name, path)
			return nil
		},
	}
}

// pluginDir returns the path to the Lua plugins directory.
func pluginDir() string {
	return filepath.Join(Cfg().Core.XMDevDir, "plugins")
}
