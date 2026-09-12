# 10 PATH RMW 四函数锁 + C-13 双函数大小写

Status: open
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
