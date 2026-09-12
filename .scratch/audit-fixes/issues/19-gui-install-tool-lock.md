# 19 GUI 安装工具锁（临界区含 runInstaller）

Status: open
Type: task
批次: B4
对应修复项: P0-09（方案 §4 P0-09，含复核修订）

## 问题

GUI 两条路径绕过 M-3 锁：app_install.go:212（force RemoveAll）与 :350 ExtractAndActivate → installer.go:1426（内部无锁，仅 :1455 Activate 自取；生产取锁点全仓仅 4 处）。

## 方案（相对 v1 的关键修订）

`ExtractAndActivate` 内部把「**extract + runInstaller + ensureInstallIntegrity + PostInstall**」**整个 pre-Activate 段**包进 `util.AcquireToolLock(...)`（v1 清单漏 runInstaller——executable/script 分支 installer.go:1432，rust 类工具 GUI 安装主体），Release 后再 Activate——与 InstallWithProgress、InstallNPMPackage（npm_install.go:428/:433/:454）三处统一。GUI force RemoveAll（:212）原地包锁（无外层锁，可行；保持先删后下载现行为）。

ExtractAndActivate 生产调用点仅 :350（无嵌套死锁）；8 处测试调用方无传染。

**注记**：Windows F-03 残留使新锁概率性失效（与 P0-05 同披露口径）；F-03 最小修复（Release 检查返回值 + log）建议同批。

## 守卫测试

并发 `InstallWithProgress(force)` 与 `ExtractAndActivate` 同一 tool，断言最终树完整。
