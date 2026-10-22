package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/discovery"
)

// ---------------------------------------------------------------------------
// 票 34（P1-30 / E-11）：启动嗅探三入口
// ---------------------------------------------------------------------------

// TC-34-1 挂载点守卫（复制 selfcheck_test.go 的三入口守卫写法）：
// 后台启动嗅探只允许挂在三个交互入口——无参 TUI（root 自身）、
// `xdev tui`、`xdev gui`；cd-hook 路径（`xdev hook` / `xdev env`）与
// 一次性子命令一律不得触发。
//
// 实现形态：OnStartup 只在 root.go 的 mountStartupSniff 内出现一次，
// 由 root 的 PersistentPreRunE → isInteractiveEntry 按命令身份挂载。
func TestStartupSniffMountPoints_OnlyInteractive(t *testing.T) {
	// 只扫命令挂载层文件（同 selfcheck 守卫口径：不扫实现文件）。
	mountFiles := []string{
		"root.go", "extra.go", "context.go",
		"hook.go", "env.go", "sniff_cmd.go", "isolate.go",
		"config.go", "init.go", "install.go", "refresh.go", "selfcheck.go",
	}
	onStartupRe := regexp.MustCompile(`\.OnStartup\s*\(`)
	mountCallRe := regexp.MustCompile(`mountStartupSniff\s*\(`)

	for _, f := range mountFiles {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		src := string(data)
		onStartup := len(onStartupRe.FindAllString(src, -1))
		mounts := len(mountCallRe.FindAllString(src, -1))
		if f == "root.go" {
			// 恰好一次真实触发（mountStartupSniff 体内）。
			if onStartup != 1 {
				t.Errorf("root.go OnStartup call sites = %d, want exactly 1 (mountStartupSniff 内)", onStartup)
			}
			// 一次定义 + PersistentPreRunE 内一次调用 = 2 处引用。
			if mounts != 2 {
				t.Errorf("root.go mountStartupSniff references = %d, want 2 (定义 + PersistentPreRunE 调用)", mounts)
			}
			continue
		}
		if onStartup != 0 {
			t.Errorf("%s 不得直接触发 Scheduler.OnStartup（非交互入口，票 34）", f)
		}
		if mounts != 0 {
			t.Errorf("%s 不得挂载 startStartupSniff（非交互入口，票 34）", f)
		}
	}

	// 交互入口清单恰为 tui / gui（无参 TUI 由 root 自身承担）。
	if got := strings.Join(interactiveEntryNames, ","); got != "tui,gui" {
		t.Errorf("interactiveEntryNames = %q, want \"tui,gui\"", got)
	}
}

// TC-34-1b 身份判定守卫：只有 root / tui / gui 是交互入口，
// cd-hook（hook / activate / deactivate）与 xdev env 必须为 false。
//
// 不调用 NewRootCommand：它会先跑 shim.MaybeExec(os.Args[0])，在 `go test`
// 下（进程名 cli.test）会走 shim 未安装分支并 os.Exit。这里用真实的子命令
// 构造函数挂到一个合成 root 上，覆盖的是真实的命令名判定。
func TestIsInteractiveEntry_OnlyThreeEntries(t *testing.T) {
	root := &cobra.Command{Use: "xdev"}
	root.AddCommand(
		newTuiCmd(), newGuiCmd(),
		newHookCmd(), newActivateCmd(), newDeactivateCmd(), newEnvCmd(),
		newInstallCmd(), newListCmd(), newConfigCmd(),
		newSniffCmd(), newIsolateCmd(),
	)
	cases := []struct {
		path []string
		want bool
	}{
		{nil, true}, // 无子命令：root.RunE 启动 TUI
		{[]string{"tui"}, true},
		{[]string{"gui"}, true},
		{[]string{"hook"}, false}, // cd-hook 挂载
		{[]string{"activate"}, false},
		{[]string{"deactivate"}, false},
		{[]string{"env"}, false}, // cd-hook 求值入口
		{[]string{"install"}, false},
		{[]string{"list"}, false},
		{[]string{"config", "set"}, false},
		{[]string{"sniff"}, false},
		{[]string{"isolate", "exec"}, false},
	}
	for _, tc := range cases {
		cmd, _, err := root.Find(tc.path)
		if err != nil {
			t.Fatalf("Find(%v): %v", tc.path, err)
		}
		if cmd == nil {
			t.Fatalf("Find(%v): nil command", tc.path)
		}
		if got := isInteractiveEntry(cmd); got != tc.want {
			t.Errorf("isInteractiveEntry(%q) = %v, want %v", cmd.CommandPath(), got, tc.want)
		}
	}
}

// writeSniffCacheForTest writes a minimal sniff-cache.json with the given
// last_sniff_at so the interval TTL decision is deterministic.
func writeSniffCacheForTest(t *testing.T, cacheDir string, last time.Time) {
	t.Helper()
	payload := map[string]any{
		"tools":         map[string]any{},
		"last_sniff_at": last.UTC().Format(time.RFC3339Nano),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "sniff-cache.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TC-34-1c HasStartupSniff 语义：交互入口挂载 ∧ 策略条件（含 interval TTL 到期）。
//
// 不调用 mountStartupSniff()：真实挂载会 spawn 后台 sniffAll（测试内触网）。
// 这里直接注入 sniffScheduler + 挂载标志，覆盖谓词与挂载标志的组合；
// 挂载点本身由 TC-34-1/TC-34-1b 守卫。
func TestHasStartupSniff_StrategyAndIntervalTTL(t *testing.T) {
	prevScheduler, prevMounted := sniffScheduler, startupSniffMounted
	defer func() { sniffScheduler, startupSniffMounted = prevScheduler, prevMounted }()

	cases := []struct {
		name       string
		strategy   string
		freshCache bool
		mounted    bool
		want       bool
	}{
		{"interval+TTL未到期(新鲜缓存)", "interval", true, true, false},
		{"interval+TTL到期(无缓存)", "interval", false, true, true},
		{"startup+交互入口", "startup", false, true, true},
		{"manual+交互入口", "manual", false, true, false},
		{"service+交互入口", "service", false, true, false},
		{"startup+cd-hook(hook/env)", "startup", false, false, false},
		{"interval+TTL到期但非交互命令", "interval", false, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cacheDir := t.TempDir()
			if tc.freshCache {
				writeSniffCacheForTest(t, cacheDir, time.Now())
			}
			cfg := &config.SniffConfig{
				Strategy:       tc.strategy,
				Interval:       "24h",
				Concurrency:    1,
				RateLimitPerHr: 300,
			}
			sniffScheduler = discovery.NewScheduler(cfg, cacheDir, t.TempDir())
			startupSniffMounted = tc.mounted

			if got := HasStartupSniff(); got != tc.want {
				t.Errorf("HasStartupSniff() = %v, want %v (strategy=%s mounted=%v freshCache=%v)",
					got, tc.want, tc.strategy, tc.mounted, tc.freshCache)
			}
		})
	}
}

// TC-34-2 OnInitialize 不再挂 OnStartup：Scheduler 仍被创建（供 Shutdown
// 刷缓存），但 sniff 触发只来自交互入口。
func TestOnInitialize_DoesNotTriggerStartupSniff(t *testing.T) {
	data, err := os.ReadFile("root.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	initBlock := regexp.MustCompile(`(?s)cobra\.OnInitialize\(func\(\)\s*\{(.*?)\n\t\}\)`).FindStringSubmatch(src)
	if initBlock == nil {
		t.Fatal("root.go: cobra.OnInitialize block not found")
	}
	body := initBlock[1]
	if !strings.Contains(body, "discovery.NewScheduler(") {
		t.Error("OnInitialize 必须仍创建 Scheduler（Shutdown 需要它刷缓存）")
	}
	if strings.Contains(body, ".OnStartup(") {
		t.Error("OnInitialize 不得再调用 OnStartup（票 34：cd-hook/env 也会触发嗅探）")
	}
}
