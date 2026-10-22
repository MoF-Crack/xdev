package cli

import (
	"errors"
	"os"
	"testing"

	"github.com/xmdemt/xdev/internal/shim"
)

// TC-33-2 isolate 子进程退出码传播（生产端）。
//
// 真实子进程：本测试二进制以 helper 身份重新执行自己并立即 os.Exit(7)，
// `xdev isolate --mode none exec -- <helper>` 必须把 7 包成
// *shim.ExitCodeError 返回给上层（main.go 再据此 os.Exit(7)，见
// 仓库根 main_exitcode_test.go 的子进程用例）。修复前 main.go 无条件
// os.Exit(1)，调用方恒看到 1。
func TestIsolateExecCmd_PropagatesChildExitCode(t *testing.T) {
	// helper 分支：子进程进程内直接以 7 退出。
	if os.Getenv("XDEV_ISOLATE_EXIT_HELPER") == "1" {
		os.Exit(7)
	}

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDEV_ISOLATE_EXIT_HELPER", "1")

	prevMode := isolateMode
	isolateMode = "none"
	defer func() { isolateMode = prevMode }()

	cmd := newIsolateExecCmd()
	cmd.SetArgs([]string{"--", exe, "-test.run=TestIsolateExecCmd_PropagatesChildExitCode"})

	err = cmd.Execute()
	if err == nil {
		t.Fatal("isolate exec: want exit-code error, got nil")
	}
	var ece *shim.ExitCodeError
	if !errors.As(err, &ece) {
		t.Fatalf("error type = %T (%v), want *shim.ExitCodeError", err, err)
	}
	if ece.Code != 7 {
		t.Errorf("ExitCodeError.Code = %d, want 7", ece.Code)
	}
}
