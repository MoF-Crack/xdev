//go:build !windows

package shim

// jobKillOnClose 在非 Windows 上是空操作。
//
// Unix 不需要它：`run_unix.go` 用 execve 语义（unix.Exec）**替换**当前进程，
// shim 不留在进程树里，因此不存在「上层杀掉的只是 shim、真实工具变孤儿」的问题。
// 这正是两平台在这里不对称的原因，不是遗漏。
func jobKillOnClose() (release func(), ok bool) {
	return func() {}, false
}
