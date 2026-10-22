package shim

import (
	"os"
	"testing"
)

// TestMain 让 shim 分发测试的“子进程替身”立即退出。
//
// 20261021 审计 P3-13：这些测试把本测试二进制当作真实工具 re-exec，并靠
// `-test.run=^$` 让它不跑用例。问题是子进程仍会由 testing 包打印
// `testing: warning: no tests to run` 与 `coverage: 0.0% of statements`，
// 污染父进程的覆盖率汇总（`go test -cover ./internal/shim` 显示 0.0%）
// 以及 `go test` 的摘要行。
//
// 用环境变量短路后，子进程不进入 testing 主循环，也就不产生任何输出；
// 生产代码的 MaybeExec 零改动。
func TestMain(m *testing.M) {
	if os.Getenv("XDEV_SHIM_CHILD") == "1" {
		os.Exit(0)
	}
	os.Exit(m.Run())
}
