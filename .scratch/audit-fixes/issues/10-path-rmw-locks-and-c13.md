# 10 PATH RMW 四函数锁 + C-13 双函数大小写

Status: resolved
Type: task
批次: B2
对应修复项: P0-11 + P2-10 + N-11（方案 §4 P0-11 / §6 P2-10）

## 问题

1. CleanupUserPath(:1128)、UpdateShimDirInUserPath(:1320)、CleanupSystemPath(:1057)、cleanupLegacyPath(env/migrate.go:155) 四个 PATH 读改写无锁（同文件 9 个 mutator 有锁）→ 并发安装静默回滚对方 PATH 写入。
2. SetWindowsUserPath(:114-119) 与 RemoveFromWindowsUserPath(:144)【N-11】大小写敏感比较 → 重复前置/删除失配。

## 方案（跨平台编译警示）

1. 四函数体包 `withUserPathLock`。**cleanupLegacyPath 在无 build tag 的 env/migrate.go（全平台编译）而 withUserPathLock 仅 win_path_windows.go（win_path_other.go 无 stub）**——把锁调用放进该函数已有 GOOS guard 内，或在 win_path_other.go 补 stub；否则三平台 CI 红。
2. CleanupSystemPath 复用同锁（不同注册表键，只增串行无死锁）。
3. withUserPathLock 锁超时/home 解析失败降级处加 log.Printf 警告（行为保留）。
4. SetWindowsUserPath 判重与 RemoveFromWindowsUserPath 匹配改 `strings.EqualFold(filepath.Clean(p), target)`（两函数一并修）。

## 守卫测试

goroutine A 循环 AddCurrentBinPaths、B 并发 CleanupUserPath，断言 A 最后写入条目仍存在；大小写变体 PATH 条目被正确识别（`e:\xdev\shims` vs `E:\XDev\shims`）。

## Comments（Batch 2 实施记录）

- 改动：`internal/env/win_path_windows.go` — `CleanupUserPath` / `CleanupSystemPath` / `UpdateShimDirInUserPath` 三函数体抽出 `*Locked` 变体并整体包 `withUserPathLock`（第九个 mutator 模式补齐）；`withUserPathLock` 两处降级分支（home 解析失败 / 锁获取失败）补 `log.Printf("[env] WARN: userpath lock degraded ...")`（行为保留：仍执行 fn）；`internal/env/migrate.go` 的 `cleanupLegacyPath` 抽出 `cleanupLegacyPathLocked`，锁调用放在既有 `runtime.GOOS == "windows"` guard 内；`internal/env/win_path_other.go` 补 `withUserPathLock(fn) error` 直通 stub（跨平台文件可调用，防未来再漏）。C-13：`SetWindowsUserPath` 判重与 `RemoveFromWindowsUserPath` 匹配改 `strings.EqualFold(filepath.Clean(p), shimDir)`。
- 守卫测试：`internal/env/path_rmw_test.go` — TestConcurrentPathRMW_NoLostWrite（K1：A 循环 20 次 AddCurrentBinPaths 与 B 循环 20 次 CleanupUserPath 并发，断言两侧效果都在最终 PATH 中——不相交条目集设计使「丢失写入」可确定性判定）/ TestWithUserPathLock_DegradesWithLog（K2：USERPROFILE 指向文件使 MkdirAll 失败 → 降级执行 + WARN 日志）/ TestSetWindowsUserPath_CaseInsensitiveDedupe（K3）/ TestRemoveFromWindowsUserPath_CaseInsensitive（K4）。
- **K5 跨平台编译门**：本机 `GOOS=linux go build ./...` 与 `GOOS=darwin go build ./internal/env/` 均 exit 0（stub/guard 生效）。
- K1 在无 race 环境下以 `-count=5` 重复跑通过（本机 race 运行时受 MinGW 8.1.0 限制不可用，见 B2 门禁记录）。
