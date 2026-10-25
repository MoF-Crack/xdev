//go:build !windows

package shim

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"

	"github.com/xmdemt/xdev/internal/envctx"
)

// run replaces the current process with the real tool. The shim must
// not stay resident for the child lifetime (Spec 030).
//
// 2026-10-25（P2）：新增 ctx 参数。非 nil 时把解析出的环境上下文注入
// 给即将 exec 的子进程 —— 这是非 Windows 平台**唯一**能生效的 env 途径
// （注册表写入在 Unix 上是空实现，见 env/win_path_other.go）。
//
// unix.Exec 直接替换进程镜像，所以环境必须在 exec 前算好；
// ctx.Apply 返回新切片，不修改 os.Environ()。
func run(target string, args []string, ctx *envctx.Context) error {
	argv := make([]string, 0, 1+len(args))
	argv = append(argv, target)
	argv = append(argv, args...)

	env := os.Environ()
	if ctx != nil && !ctx.Empty() {
		env = ctx.Apply(env)
	}

	if err := unix.Exec(target, argv, env); err != nil {
		return fmt.Errorf("exec %s: %w", target, err)
	}
	return nil
}
