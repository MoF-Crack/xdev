package cli

import (
	"fmt"
	"log"
	"path/filepath"
	"regexp"
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
	case "self_update_mode":
		return c.SelfUpdateMode, nil
	case "self_update_stale":
		return strconv.FormatBool(c.SelfUpdateStale), nil
	case "manifest_ttl":
		return c.ManifestTTL, nil
	}
	return "", fmt.Errorf("unknown core field %q", field)
}

func setCoreField(c *config.CoreConfig, field, value string) error {
	switch field {
	case "xmdevenv_dir":
		// 票 33（P1-27 / E-08）：home 必须是本地绝对路径——相对路径会让
		// 所有派生目录随进程 CWD 漂移；UNC（\\server\share）无法承载
		// shim/注册表/缓存语义，显式拒绝并给出提示。
		if err := validateLocalAbsPath(value); err != nil {
			return err
		}
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
	case "self_update_mode":
		// Spec 034: 枚举校验（prompt | auto | notify）
		switch value {
		case "prompt", "auto", "notify":
			c.SelfUpdateMode = value
		default:
			return fmt.Errorf("invalid self_update_mode %q (want prompt|auto|notify)", value)
		}
	case "self_update_stale":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		c.SelfUpdateStale = b
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
		// 票 33（P1-28 / E-09）：与 config.SetPathMode / GetPathMode 同源的
		// 三值枚举（shim | direct | auto）。"symlink" 是历史遗留值，全仓
		// 无实现，此处统一拒绝（存量值由 GetPathMode 降级为 shim）。
		switch value {
		case "shim", "direct", "auto":
			p.Mode = value
		default:
			return fmt.Errorf("invalid mode %q (want shim | direct | auto)", value)
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

// validateLocalAbsPath 校验 home 一类路径必须是本地绝对路径。
//
// 票 33（P1-27 / E-08）：`config set core.xmdevenv_dir <v>` 与 init 向导的
// 自定义路径输入（drive_prompt "p" 分支）共用本校验：
//   - 相对路径拒绝（含 Windows 的 "C:foo" 驱动器相对形式、"\foo" 根相对形式）
//   - UNC 路径拒绝（`\\server\share` / `//server/share`）——xdev home 需要
//     shim 目录、注册表 PATH 写入与跨进程缓存，网络路径语义不成立
//
// 绝对本地路径（D:\XDev、/opt/xdev）不受影响。
func validateLocalAbsPath(value string) error {
	v := strings.TrimSpace(value)
	if v == "" {
		return fmt.Errorf("path must not be empty")
	}
	if isUNCPath(v) {
		return fmt.Errorf("UNC path %q is not supported (xdev home must be a local absolute path, e.g. D:\\XDev)", value)
	}
	if !isLocalAbsPath(v) {
		return fmt.Errorf("path %q must be absolute (relative paths are rejected, e.g. D:\\XDev)", value)
	}
	return nil
}

// isUNCPath reports whether p looks like a UNC / network path.
func isUNCPath(p string) bool {
	return strings.HasPrefix(p, `\\`) || strings.HasPrefix(p, "//")
}

// isLocalAbsPath reports whether p is an absolute local path for the host OS.
// On non-Windows hosts the Windows drive-rooted form (C:\... / C:/...) is also
// accepted: drive_prompt / `config set core.xmdevenv_dir` are exercised by
// cross-platform unit tests that feed Windows-style paths on any host.
func isLocalAbsPath(p string) bool {
	if filepath.IsAbs(p) {
		return true
	}
	if len(p) >= 3 && p[1] == ':' && (p[2] == '\\' || p[2] == '/') {
		c := p[0]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
			return true
		}
	}
	return false
}

// proxyFlagState 收集 `config proxy` 一次调用携带的 flag 输入。
//
// 票 40（P2-16 / E-12）：把「flag → 生效值 → cfg.SetProxy」这套合并逻辑
// 从 RunE 里抽成纯数据 + apply 方法，一方面让 CLI 与 GUI 的
// App.SetProxyConfig 一样只经由 config.SetProxy 落盘（唯一写入路径），
// 另一方面让守卫测试能直接驱动「非法 scheme 被拒」，而不必构造 cobra
// 命令——构造/执行 root 命令会触发进程级 OnInitialize → initContext("")
// → 解析回真实 home，测试绝不允许触碰真机 home。
type proxyFlagState struct {
	HTTP    string
	HTTPS   string
	NoProxy string
	Enable  bool

	HTTPSet    bool
	HTTPSSet   bool
	NoProxySet bool
	EnableSet  bool
	DisableSet bool
}

// touched reports whether the invocation carried at least one write flag.
// No flag at all means "show the current settings" (read-only).
func (s proxyFlagState) touched() bool {
	return s.HTTPSet || s.HTTPSSet || s.NoProxySet || s.EnableSet || s.DisableSet
}

// apply 把 flag 合并进配置并调用 cfg.SetProxy 落盘。
//
// 语义与 GUI 的 App.SetProxyConfig 完全一致（同一 cfg.SetProxy：含
// util.ValidateProxyURL 校验 + 原子保存）。未显式给出的字段保持现值
// （部分更新），--disable 与 --enable 同时出现时 disable 优先（与旧行为
// 一致）。校验失败不再静默：记一条脱敏日志后返回错误（旧实现自行拼字段
// 并直接 Save，非法 scheme 会被原样写进 config.yaml，进而在运行期静默
// 退化为直连）。
func (s proxyFlagState) apply(cfg *config.Config) error {
	httpVal := cfg.Proxy.HTTP
	if s.HTTPSet {
		httpVal = s.HTTP
	}
	httpsVal := cfg.Proxy.HTTPS
	if s.HTTPSSet {
		httpsVal = s.HTTPS
	}
	noProxyVal := cfg.Proxy.NoProxy
	if s.NoProxySet {
		noProxyVal = s.NoProxy
	}
	enabledVal := cfg.Proxy.Enabled
	if s.EnableSet {
		enabledVal = s.Enable
	}
	if s.DisableSet {
		enabledVal = false
	}

	if err := cfg.SetProxy(httpVal, httpsVal, noProxyVal, enabledVal); err != nil {
		// util.ValidateProxyURL 的注释要求代理凭据不得写入日志，而
		// url.Parse 的错误文本会原样回显含凭据的 URL（`http://user:pass@…`），
		// 因此日志里先做 userinfo 脱敏。
		log.Printf("[config] WARNING: proxy update rejected: %s", redactProxyCredentials(err.Error()))
		return fmt.Errorf("set proxy: %w", err)
	}
	return nil
}

// proxyUserinfoPattern 匹配 URL 的 userinfo 段（`//user:pass@`）。
var proxyUserinfoPattern = regexp.MustCompile(`//[^/@\s]*@`)

// redactProxyCredentials 把错误文案里的 `user:pass@` 替换为 `<redacted>@`。
//
// 这里刻意不用 util.SanitizeURL：它在 url.Parse 失败时原样返回输入，而
// 「URL 畸形到解析失败」恰恰是最需要脱敏的场景（错误文本里带着原始 URL）。
func redactProxyCredentials(msg string) string {
	return proxyUserinfoPattern.ReplaceAllString(msg, "//<redacted>@")
}

func newConfigProxyCmd() *cobra.Command {
	var httpProxy, httpsProxy, noProxy string
	var enable bool
	cmd := &cobra.Command{
		Use:   "proxy",
		Short: "View and configure download proxy",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := Cfg()
			// 票 40（P2-16）：flag 状态一次性收集后交给 apply——避免旧实现
			// 「逐字段改 cfg + 自行 Save」绕开 SetProxy 校验的分叉路径。
			state := proxyFlagState{
				HTTP:       httpProxy,
				HTTPS:      httpsProxy,
				NoProxy:    noProxy,
				Enable:     enable,
				HTTPSet:    cmd.Flags().Changed("http"),
				HTTPSSet:   cmd.Flags().Changed("https"),
				NoProxySet: cmd.Flags().Changed("no-proxy"),
				EnableSet:  cmd.Flags().Changed("enable"),
				DisableSet: cmd.Flags().Changed("disable"),
			}
			if !state.touched() {
				fmt.Printf("Enabled:  %v\n", cfg.Proxy.Enabled)
				fmt.Printf("HTTP:     %s\n", cfg.Proxy.HTTP)
				fmt.Printf("HTTPS:    %s\n", cfg.Proxy.HTTPS)
				fmt.Printf("NoProxy:  %s\n", cfg.Proxy.NoProxy)
				return nil
			}
			if err := state.apply(cfg); err != nil {
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
