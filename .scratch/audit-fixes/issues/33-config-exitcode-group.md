# 33 配置枚举/退出码组（P1-27/28/29）

Status: open
Type: task
批次: B5
对应修复项: P1-27(E-08) + P1-28(E-09) + P1-29(E-10)（方案 §5 主题 D）

## 方案要点

1. **P1-27 home 相对路径**：resolveXMDevDir/expandHome 返回前强制 `filepath.Abs`；`config set core.xmdevenv_dir` 与 drive_prompt "p" 输入 IsAbs 校验（拒绝相对；UNC 显式拒绝提示）。
2. **P1-28 path.mode 三套枚举收敛**：setPathField（cli/config.go:233-237）case 改 `shim|direct|auto`（拒绝 symlink——全仓仅 :234 一处定义）；SetPathMode（config.go:259-262）同步；存量 "symlink" 由 GetPathMode 降级 shim（现状）+ doctor 提示。
3. **P1-29 isolate 退出码**：main.go Execute 错误出口加 `var ece *shim.ExitCodeError; if errors.As(err, &ece) { os.Exit(ece.Code) }`（对照 :54-56 shim 分发路径先例）。

## 守卫测试

相对/UNC home 拒绝且绝对路径用户零影响；`config set path.mode symlink` 拒绝 / `auto` 接受；isolate 子进程非零退出码原样传播。
