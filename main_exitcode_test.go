//go:build !bindings

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/xmdemt/xdev/internal/shim"
)

// TC-33-2a 退出码映射：*shim.ExitCodeError（含被 %w 包装）原样传播，
// 其余错误回落为通用失败 exit 1（票 33 / P1-29 / E-10）。
func TestExitCodeForError_Mapping(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		wantCode int
		wantOK   bool
	}{
		{"直接 ExitCodeError", &shim.ExitCodeError{Code: 7}, 7, true},
		{"包装的 ExitCodeError", fmt.Errorf("isolate exec: %w", &shim.ExitCodeError{Code: 7}), 7, true},
		{"深层包装", fmt.Errorf("root: %w", fmt.Errorf("isolate: %w", &shim.ExitCodeError{Code: 130})), 130, true},
		{"普通错误", errors.New("boom"), 1, false},
		{"矛盾输入(错误却报 0)", &shim.ExitCodeError{Code: 0}, 1, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, ok := exitCodeForError(tc.err)
			if code != tc.wantCode || ok != tc.wantOK {
				t.Errorf("exitCodeForError(%v) = (%d,%v), want (%d,%v)", tc.err, code, ok, tc.wantCode, tc.wantOK)
			}
		})
	}
}

// TC-33-2b 真实子进程：以本测试二进制充当 xdev 进程，断言 main 的退出码
// 路径把子进程退出码 7 原样带出（修复前恒 1）。
//
// 选择说明：票面允许「TestMain 式 helper 子进程技巧」。这里让子进程走
// main.go 生产路径所用的同一个 exitCodeForError（与 isolate exec 生产端
// 的真实子进程用例 internal/cli/isolate_exitcode_test.go 合起来覆盖
// 「isolate 子进程 exit 7 → xdev 退出码 7」两端），避免构建真实 xdev 二进制。
func TestExitCodeForError_SubprocessPropagates(t *testing.T) {
	if os.Getenv("XDEV_EXITCODE_HELPER") == "1" {
		// 生产路径：main() 拿到被包装的 *shim.ExitCodeError 后 os.Exit(code)。
		code, ok := exitCodeForError(fmt.Errorf("isolate exec: %w", &shim.ExitCodeError{Code: 7}))
		if !ok {
			os.Exit(99)
		}
		os.Exit(code)
	}

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=TestExitCodeForError_SubprocessPropagates")
	cmd.Env = append(os.Environ(), "XDEV_EXITCODE_HELPER=1")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("child exited 0, want 7; output: %s", out)
	}
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("child run error = %v (%T), want *exec.ExitError; output: %s", err, err, out)
	}
	if got := ee.ExitCode(); got != 7 {
		t.Errorf("child exit code = %d, want 7 (修复前恒 1); output: %s", got, out)
	}
}
