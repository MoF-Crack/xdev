package plugin

import (
	"os"
	"testing"
)

// TestMain 关闭“默认内存看门狗”，让测试之间不再通过**进程级堆**互相干扰。
//
// 20261021 审计 P2-8：看门狗比较的是全进程 HeapAlloc 增量；而超时类测试
// （spin / tail recursion / VMTimeout）会故意留下仍在执行的孤儿 goroutine，
// 它们的分配会被算到后续调用的基线上（整包运行时实测把一次正常的
// `available()` 误判为 64 MiB 超限）。
//
// 真正验证内存上限的用例不依赖这个默认值：它们通过
// `runWithContextLimited(..., limit)` 或 `t.Setenv("XDEV_LUA_MEM_LIMIT_MB", ...)`
// 显式注入上限，因此仍然覆盖看门狗行为；生产默认值不变。
func TestMain(m *testing.M) {
	if os.Getenv("XDEV_LUA_MEM_LIMIT_MB") == "" {
		_ = os.Setenv("XDEV_LUA_MEM_LIMIT_MB", "0")
	}
	os.Exit(m.Run())
}
