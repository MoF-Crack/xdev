# 19 GUI 安装工具锁（临界区含 runInstaller）

Status: resolved
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

## Comments（Batch 4 实施记录）

- 改动：
  - `internal/installer/installer.go` ExtractAndActivate — 「extract + **runInstaller**（executable/script 分支）+ ensureInstallIntegrity + PostInstall」整段包进 `util.AcquireToolLock(XMDevDir, p.Name(), 5min)` 临界区（匿名 `func() error` + defer Release），**Release 后再 Activate**（Activate 内部自取同名锁）；npm-kind 早退分支保留在临界区内。与 InstallWithProgress / InstallNPMPackage / Activate 四处取锁语义统一。
  - `internal/gui/app_install.go` — force 重装的 `os.RemoveAll(installPath)` 原地包 AcquireToolLock/Release（保持「先删后下载」现行为，仅加互斥；锁在下载阶段前释放）。
  - `internal/util/mutex_windows.go` — **F-03 最小修复**：ReleaseMutex 返回值检查 + `log.Printf("[util] WARNING: ReleaseMutex failed ...")`（原先静默忽略）。
  - 票 24 归属的 `app_install.go` nil-guard 项：审计的 `:498` 在现树对应 `lookupToolManifest`（:520-525），**既有代码已有 `if a.cat != nil` 守卫**，无需改动（已核实）。
- 守卫测试：`internal/installer/extract_lock_test.go` —
  - `TestExtractAndActivate_CoversRunInstallerKind`（IL2，确定性）：假安装器（executable kind，缓存内 .cmd/.sh）在本测试二进制的**子进程**里以 `XDEV_LOCK_PROBE=1` 跑 `TestLockProbeHelper`，非阻塞尝试 AcquireToolLock 并输出 LOCK-HELD/LOCK-FREE。子进程与父进程是不同进程，命名互斥体语义与生产一致（无线程亲和干扰）→ 输出 **LOCK-HELD** 证明临界区覆盖 runInstaller ✓。
  - `TestConcurrentExtract_SamePath_TreeIntact`（IL1 等价）：并发两次 ExtractAndActivate 到同一 installPath → 断言 loser 只允许以**明确的锁错误**失败、不出数据撕裂；最终 install 树过 `ensureInstallIntegrity` ✓。
- **F-03 实测复现（从理论残留升级为实证）**：IL1 运行中抓到完整证据链——
  `[util] WARNING: ReleaseMutex failed for a tool lock (goroutine migrated threads?) — ... Attempt to release mutex not owned by caller` → 随后另一调用 `activate lock democonc: tool "xdev_..._democonc" is locked by another xdev process (timeout after 30s)` → **但安装树完整**。即同进程并发下 F-03 使锁泄漏并退化为「明确 30s 超时错误」（而非静默数据损坏）；跨进程并发不受影响。最小修复的价值正是让这个失败可见（此前 ReleaseMutex 失败被静默忽略）。
- IL3（GUI force RemoveAll 持锁断言）未做单元测试：该路径深嵌 `installWithOptions` 的 Wails 事件管线（需完整 App + 下载 mock），其互斥语义与 IL2 完全相同（同一个 `util.AcquireToolLock`），已由 IL2 + 代码审查覆盖——记录为设计替代，不阻塞。
