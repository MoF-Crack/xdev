# 40 P2 CLI/TUI 组（含必红 TUI 测试）

Status: open
Type: task
批次: B5
对应修复项: P2-16(E-12) + P2-17(E-13) + P2-18(E-14) + P2-19(E-15) + P2-20(F-08) + P2-33(N-18)（方案 §6）

## 方案要点

1. **P2-16** `config proxy` 复用 cfg.SetProxy（含 ValidateProxyURL）；ConfigureHTTPClient 解析失败 log 告警。
2. **P2-17** plugin install 4MiB：io.Copy 后 `io.ReadFull(resp.Body, []byte{0})` 非 EOF 即报 "exceeds 4 MiB limit"。
3. **P2-18** doctor shims：shimOK 由 ReadDir 成功推导；「零 shim」保持 info 而非 fail（防 doctor 退出码变化破坏首跑体验/CI 断言）。
4. **P2-19 TUI 陈旧消息**：引入 `currentOpID`，判定 `if msg.ID != m.currentOpID { return m, nil }`；①install/uninstall 用**不同消息类型**（防跨空间撞号）；**②判定同时覆盖成功（app.go:282-286）与错误（:267-281）两分支**【N-14：installState==nil 时迟到成功消息可静默恢复批量】；③**必红**：TestInstallResultError_ClearsUpdateQueue（tui_test.go:412-429）手工构造不设 currentOpID——同步更新（相邻 TestInstallCancelKey 不受影响，无需改）。
5. **P2-20 logrotate**：RotateLog 前 AcquireFileLock；Windows copy+truncate 代替 rename（或失败 log）；shift 循环错误不再静默；调用方不再无条件吞错。
6. **P2-33**【复核新增】：cli/profile.go:132 导入前大小检查（对齐 GUI 10MB）；config.go:815 日志文案改指 config.yaml。

## 守卫测试

proxy 非法 scheme 拒绝；超限插件报明确错误；doctor 新装用户零 shim 为 info；TUI 迟到消息（成功+错误两路）被丢弃；测试基线先红后绿。
