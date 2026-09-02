# 01 — F1: 零参数 shim 误启 GUI 修复

Status: done
Spec: 035（F1，P0）
Files: `main.go`
Blocks: none

## 改动（最小）

`main.go` 零参数分支在 `runGUI()` 前先做 shim 判定：

```go
import ( /* 新增 */ "path/filepath"; "github.com/xmdemt/xdev/internal/shim" )

if len(os.Args) <= 1 {
    // 零参数调用的工具 shim（python/node 等进 REPL 的工具）必须照常分发，
    // 不能落入 GUI 分支（Spec 035 F1）。MaybeExec 对 xdev/xdev-*/xdev-gui*/
    // wailsbindings 名字在 Lookup 前短路返回 false，真 GUI 启动零影响。
    if handled, err := shim.MaybeExec(filepath.Base(os.Args[0])); handled {
        if err != nil {
            if exitErr, ok := err.(*shim.ExitCodeError); ok {
                os.Exit(exitErr.Code)
            }
            fmt.Fprintln(os.Stderr, "shim:", err)
            os.Exit(1)
        }
        os.Exit(0)
    }
    runGUI()
    return
}
```

错误处理与 `internal/cli/root.go:51-58` 逐行同构。root.go 不动。

## 验收

> main 分支本身在 main 包（无测试文件），无法直接单测 main()——守卫测试覆盖 MaybeExec 语义（分派判定入口），main 分支零参数先判定的接线靠手动冒烟验证。

- [ ] `go build ./...` 通过（工具链 GOROOT=E:\XDev\install\go\1.26.5\go）
- [ ] 扩展 `internal/shim/shim_test.go`：别名 basename + 零参数 os.Args → handled=true；xdev basename → handled=false 且无副作用（复用 :341-377 TestMaybeExec_* 模式）
- [ ] 手动冒烟：构建副本改名 `python.exe` 零参数调用 → 走 shim 分发（错误输出或 REPL），不弹 GUI；`xdev.exe` 零参数 → GUI 正常启动
