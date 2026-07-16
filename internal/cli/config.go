package cli

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/xmdemt/xdev/internal/config"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "View and edit xdev configuration",
	}
	cmd.AddCommand(newConfigListCmd(), newConfigGetCmd(), newConfigSetCmd(), newConfigProxyCmd())
	return cmd
}

func newConfigListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Print every config key with its current value",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := Cfg()
			rows := [][]string{
				{"core.xmdevenv_dir", cfg.Core.XMDevDir},
				{"core.download_dir", cfg.Core.DownloadDir},
				{"core.install_dir", cfg.Core.InstallDir},
				{"core.shim_dir", cfg.Core.ShimDir},
				{"core.cache_dir", cfg.Core.CacheDir},
				{"core.env_dir", cfg.Core.EnvDir},
				{"core.manifest_dir", cfg.Core.ManifestDir},
				{"core.log_level", cfg.Core.LogLevel},
				{"core.self_update", strconv.FormatBool(cfg.Core.SelfUpdate)},
				{"core.manifest_ttl", cfg.Core.ManifestTTL},
				{"path.mode", cfg.Path.Mode},
				{"path.inject_to_shell_rc", strconv.FormatBool(cfg.Path.InjectToShellRC)},
				{"path.inject_to_windows_path", strconv.FormatBool(cfg.Path.InjectToWindowsPath)},
			}
			for k, v := range cfg.Mirror {
				rows = append(rows, []string{"mirror." + k, v})
			}
			sort.Slice(rows, func(i, j int) bool { return rows[i][0] < rows[j][0] })
			for _, r := range rows {
				fmt.Printf("%-32s %s\n", r[0], r[1])
			}
			return nil
		},
	}
}

func newConfigGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Print a single config value (e.g. mirror.python)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := configGet(Cfg(), args[0])
			if err != nil {
				return err
			}
			fmt.Println(v)
			return nil
		},
	}
}

func newConfigSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Update a config value and save the file",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := configSet(Cfg(), args[0], args[1]); err != nil {
				return err
			}
			if err := Cfg().Save(); err != nil {
				return err
			}
			fmt.Printf("✔ %s = %s\n", args[0], args[1])
			return nil
		},
	}
}

// configGet is a small dotted-key lookup over Config.
//
// Supported keys: core.*, path.*, mirror.*.
func configGet(cfg *config.Config, key string) (string, error) {
	parts := strings.SplitN(key, ".", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid key %q (expected section.field)", key)
	}
	section, field := parts[0], parts[1]
	switch section {
	case "core":
		v, err := coreField(&cfg.Core, field)
		if err != nil {
			return "", err
		}
		return v, nil
	case "path":
		v, err := pathField(&cfg.Path, field)
		if err != nil {
			return "", err
		}
		return v, nil
	case "mirror":
		return cfg.Mirror[field], nil
	}
	return "", fmt.Errorf("unknown section %q", section)
}

// configSet is the write counterpart of configGet.
func configSet(cfg *config.Config, key, value string) error {
	parts := strings.SplitN(key, ".", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid key %q (expected section.field)", key)
	}
	section, field := parts[0], parts[1]
	switch section {
	case "core":
		return setCoreField(&cfg.Core, field, value)
	case "path":
		return setPathField(&cfg.Path, field, value)
	case "mirror":
		if cfg.Mirror == nil {
			cfg.Mirror = map[string]string{}
		}
		cfg.Mirror[field] = value
		return nil
	}
	return fmt.Errorf("unknown section %q", section)
}

func coreField(c *config.CoreConfig, field string) (string, error) {
	switch field {
	case "xmdevenv_dir":
		return c.XMDevDir, nil
	case "download_dir":
		return c.DownloadDir, nil
	case "install_dir":
		return c.InstallDir, nil
	case "shim_dir":
		return c.ShimDir, nil
	case "cache_dir":
		return c.CacheDir, nil
	case "env_dir":
		return c.EnvDir, nil
	case "manifest_dir":
		return c.ManifestDir, nil
	case "log_level":
		return c.LogLevel, nil
	case "self_update":
		return strconv.FormatBool(c.SelfUpdate), nil
	case "manifest_ttl":
		return c.ManifestTTL, nil
	}
	return "", fmt.Errorf("unknown core field %q", field)
}

func setCoreField(c *config.CoreConfig, field, value string) error {
	switch field {
	case "xmdevenv_dir":
		c.XMDevDir = value
	case "download_dir":
		c.DownloadDir = value
	case "install_dir":
		c.InstallDir = value
	case "shim_dir":
		c.ShimDir = value
	case "cache_dir":
		c.CacheDir = value
	case "env_dir":
		c.EnvDir = value
	case "manifest_dir":
		c.ManifestDir = value
	case "log_level":
		c.LogLevel = value
	case "self_update":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		c.SelfUpdate = b
	case "manifest_ttl":
		c.ManifestTTL = value
	default:
		return fmt.Errorf("unknown core field %q", field)
	}
	return nil
}

func pathField(p *config.PathConfig, field string) (string, error) {
	switch field {
	case "mode":
		return p.Mode, nil
	case "inject_to_shell_rc":
		return strconv.FormatBool(p.InjectToShellRC), nil
	case "inject_to_windows_path":
		return strconv.FormatBool(p.InjectToWindowsPath), nil
	case "shell_rc":
		return p.ShellRC, nil
	}
	return "", fmt.Errorf("unknown path field %q", field)
}

func setPathField(p *config.PathConfig, field, value string) error {
	switch field {
	case "mode":
		switch value {
		case "shim", "symlink", "direct":
			p.Mode = value
		default:
			return fmt.Errorf("invalid mode %q (use shim | symlink | direct)", value)
		}
	case "inject_to_shell_rc":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		p.InjectToShellRC = b
	case "inject_to_windows_path":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		p.InjectToWindowsPath = b
	case "shell_rc":
		p.ShellRC = value
	default:
		return fmt.Errorf("unknown path field %q", field)
	}
	return nil
}

func newConfigProxyCmd() *cobra.Command {
	var httpProxy, httpsProxy, noProxy string
	var enable bool
	cmd := &cobra.Command{
		Use:   "proxy",
		Short: "View and configure download proxy",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := Cfg()
			if !cmd.Flags().Changed("http") && !cmd.Flags().Changed("https") &&
				!cmd.Flags().Changed("no-proxy") && !cmd.Flags().Changed("enable") &&
				!cmd.Flags().Changed("disable") {
				fmt.Printf("Enabled:  %v\n", cfg.Proxy.Enabled)
				fmt.Printf("HTTP:     %s\n", cfg.Proxy.HTTP)
				fmt.Printf("HTTPS:    %s\n", cfg.Proxy.HTTPS)
				fmt.Printf("NoProxy:  %s\n", cfg.Proxy.NoProxy)
				return nil
			}
			if cmd.Flags().Changed("http") {
				cfg.Proxy.HTTP = httpProxy
			}
			if cmd.Flags().Changed("https") {
				cfg.Proxy.HTTPS = httpsProxy
			}
			if cmd.Flags().Changed("no-proxy") {
				cfg.Proxy.NoProxy = noProxy
			}
			if cmd.Flags().Changed("enable") {
				cfg.Proxy.Enabled = enable
			}
			if cmd.Flags().Changed("disable") {
				cfg.Proxy.Enabled = false
			}
			if err := cfg.Save(); err != nil {
				return err
			}
			fmt.Println("Proxy configuration updated")
			return nil
		},
	}
	cmd.Flags().StringVar(&httpProxy, "http", "", "HTTP proxy URL")
	cmd.Flags().StringVar(&httpsProxy, "https", "", "HTTPS proxy URL")
	cmd.Flags().StringVar(&noProxy, "no-proxy", "", "No proxy hosts")
	cmd.Flags().BoolVar(&enable, "enable", false, "Enable proxy")
	cmd.Flags().Bool("disable", false, "Disable proxy")
	return cmd
}
