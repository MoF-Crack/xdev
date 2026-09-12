package cli

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// 票 31（P1-23 / E-04）：--no-path-fix 单源一致化
// ---------------------------------------------------------------------------

// TC-31-2 `xdev init` 不得定义同名局部 flag。
//
// 局部 flag 会遮蔽 root.go 的持久 flag：`xdev init --no-path-fix` 只设置
// 局部变量，而向导内以 NoPathFix()（持久 flag → initContext → globalCtx）
// 为准的门读到 false，出现「自装门生效、向导安装阶段失效」的部分生效。
func TestInitCmd_HasNoLocalNoPathFixFlag(t *testing.T) {
	initCmd := newInitCmd()
	if f := initCmd.LocalFlags().Lookup("no-path-fix"); f != nil {
		t.Errorf("init 定义了局部 --no-path-fix（usage=%q）——会遮蔽 root 的持久 flag，必须删除", f.Usage)
	}

	// 持久 flag 必须仍然可用（继承自 root）。
	root := &cobra.Command{Use: "xdev"}
	root.PersistentFlags().Bool("no-path-fix", false, "do not modify the shell RC file")
	root.AddCommand(initCmd)
	if f := initCmd.InheritedFlags().Lookup("no-path-fix"); f == nil {
		t.Error("init 必须继承 root 的持久 --no-path-fix")
	}
	if f := initCmd.Flags().Lookup("no-path-fix"); f == nil {
		t.Error("init 的完整 flag 集必须能查到 --no-path-fix")
	}
}

// TC-31-2b cli 包内不得再出现任何局部 --no-path-fix 定义（防回归）。
func TestNoLocalNoPathFixFlagDefinition(t *testing.T) {
	defRe := regexp.MustCompile(`Flags\(\)\.BoolVar(?:P)?\([^)]*"no-path-fix"`)
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range defRe.FindAllString(string(data), -1) {
			if name == "root.go" {
				continue // root 的持久定义是唯一合法来源
			}
			t.Errorf("%s: 局部 %s 定义必须删除（票 31/E-04 单源化）", name, m)
		}
	}
}

// TC-31-2c init.go 三处门同源：全部读 NoPathFix()，且安装器门把该值交给
// installer.SetNoEnvWrite（否则向导安装阶段仍会写注册表 env）。
func TestInitWizard_GatesUsePersistentFlagOnly(t *testing.T) {
	data, err := os.ReadFile("init.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	if strings.Contains(src, "flagNoPathFix") {
		t.Error("init.go 仍引用局部 flagNoPathFix（必须改为 NoPathFix()）")
	}
	if n := strings.Count(src, "NoPathFix()"); n < 3 {
		t.Errorf("init.go NoPathFix() 引用 = %d, want >= 3（安装器 SetNoEnvWrite / shell RC 注入 / Windows PATH 三处门）", n)
	}
	if !strings.Contains(src, "ins.SetNoEnvWrite(NoPathFix())") {
		t.Error("init.go 安装器门必须为 ins.SetNoEnvWrite(NoPathFix())——向导安装阶段不得写注册表")
	}
}

// TC-31-2d 真实接线：root 的持久 --no-path-fix 送达 globalCtx（NoPathFix()==true），
// 不带 flag 时为 false。
//
// 必须在**子进程**里跑：NewRootCommand 会通过 cobra.OnInitialize 注册一个
// 进程级 initializer，之后同进程内任何 `cmd.Execute()`（例如 register_test.go
// 的独立命令）都会重新跑 initContext，从而把 globalCtx 指回真实 home——
// 那正是「测试污染 E:\XDev\plugins」的成因。子进程隔离后父进程的 initializer
// 列表保持干净。
func TestPersistentNoPathFix_ReachesRunContext(t *testing.T) {
	if os.Getenv(nopathFixHelperEnv) != "" {
		runNoPathFixHelper()
		return
	}

	t.Run("with --no-path-fix", func(t *testing.T) {
		out, err := runNoPathFixHelperProcess(t, "1")
		if err != nil {
			t.Fatalf("helper: %v\noutput: %s", err, out)
		}
		if !strings.Contains(out, "RAN=true NOPATHFIX=true") {
			t.Errorf("持久 --no-path-fix 未送达 run context（E-04 回归）; output: %q", out)
		}
	})

	t.Run("without flag", func(t *testing.T) {
		out, err := runNoPathFixHelperProcess(t, "0")
		if err != nil {
			t.Fatalf("helper: %v\noutput: %s", err, out)
		}
		if !strings.Contains(out, "RAN=true NOPATHFIX=false") {
			t.Errorf("未带 flag 时 NoPathFix() 必须为 false（防「恒 true」假绿）; output: %q", out)
		}
	})
}

const nopathFixHelperEnv = "XDEV_NOPATHFIX_HELPER"

// runNoPathFixHelperProcess 以 helper 身份重新执行测试二进制。
// mode=="1" 时给根命令传 --no-path-fix。
func runNoPathFixHelperProcess(t *testing.T, mode string) (string, error) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=TestPersistentNoPathFix_ReachesRunContext")
	cmd.Env = append(os.Environ(), nopathFixHelperEnv+"="+mode)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// runNoPathFixHelper 在子进程内跑真实命令树并打印结果。
func runNoPathFixHelper() {
	// NewRootCommand 首行会跑 shim.MaybeExec(os.Args[0])：`go test` 进程名
	// （cli.test）会走「shim 未安装」分支并 os.Exit；"xdev" 在 Lookup 前短路。
	os.Args[0] = "xdev"

	root := NewRootCommand("xdev", "test")
	ran := false
	root.AddCommand(&cobra.Command{
		Use: "nopathfix-probe",
		RunE: func(cmd *cobra.Command, args []string) error {
			ran = true
			return nil
		},
	})
	args := []string{"nopathfix-probe"}
	if os.Getenv(nopathFixHelperEnv) == "1" {
		args = append(args, "--no-path-fix")
	}
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		fmt.Printf("RAN=false NOPATHFIX=false ERR=%v\n", err)
		os.Exit(0)
	}
	fmt.Printf("RAN=%v NOPATHFIX=%v\n", ran, NoPathFix())
	os.Exit(0)
}
