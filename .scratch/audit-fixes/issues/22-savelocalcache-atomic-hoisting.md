# 22 saveLocalCache 原子化 + 迁移刷新上提

Status: resolved
Type: task
批次: B4
对应修复项: P1-08（方案 §5 主题 A，复核修订版）

## 问题（复核修正的口径）

app_migration.go 实际 **4 个** refreshAndSaveLocalCache 调用点（:93/:495/:608/:696；:495/:608 为**批量迁移复用**的 worker——v1「被单工具迁移复用」方向写反）；GUI 包全域共 **15 个调用点/7 文件**汇入 app.go:601 的同一非原子 os.WriteFile——仅迁移层上提修不完。

## 方案（两层）

1. **核心修复落本体**：saveLocalCache 改 `util.AtomicWriteFile` + 进程内 `sync.Mutex` 串行化（排队写，不「进行中跳过」）——15 个调用点一次全收敛。
2. **刷新上提**：把逐项刷新从 worker（:495/:608）移除，上提到调用方层——批量版 :696 尾部统一刷新保留、MigrateLegacyDir(:93) 单工具场景保留一次——恢复「统一刷新一次」契约。

## 守卫测试

并发迁移多个工具 + 并发安装完成（app_install.go 各刷新点）同写 local cache：`-race` 下无竞争、文件永不撕裂、终态含全部最新状态。

## Comments（Batch 4 实施记录）

- 改动：`internal/gui/app.go` — App 增 `cacheWriteMu sync.Mutex`；`saveLocalCache` 改 `util.AtomicWriteFile` + **排队互斥**（15 个调用点/7 文件只改本体，一次全收敛）；新增 `var localCacheRefreshHook func()`（测试计数 seam，生产恒 nil）+ `refreshAndSaveLocalCache` 末尾调用（供 SC2 计数）。`internal/gui/app_migration.go` — 两个 **worker**（migrateLocalToolByJunction / migrateLocalToolByMove）内的 `refreshAndSaveLocalCache()` 移除，**上提到公开入口** `MigrateLocalToolByJunction` / `MigrateLocalToolByMove` 各刷一次；批量尾部保留唯一刷新点；`MigrateLegacyDir` 单工具路径保留一次。
- 守卫测试：`internal/gui/app_local_cache_test.go` — SC1（并发写 320 次 + 读方 1083 次尝试，**撕裂 0 次**，`json.Unmarshal` 恒完整）/ SC2（spy 计数：批量 3 工具迁移恰刷新 **1** 次；单工具仍 1 次）。
- **红线先红后绿**：SC1 红 —「并发写期间读方观察到 55 次非完整缓存内容（示例：0 字节 unexpected end of JSON input）」→ 修复后 0 次；SC2 红 —「批量迁移 3 个工具应恰刷新 1 次，实际 **4** 次（worker 内刷新未上提）」→ 修复后 1 次。探针已还原，`grep RED-PROBE` 全仓 0 残留。
- 实测发现（Windows 特性，非缺陷）：`AtomicWriteFile` 的 MoveFileEx 替换瞬间（及触发的实时杀毒扫描）会让并发读方的 CreateFile 短暂 `ERROR_SHARING_VIOLATION`（320 次写中命中约 113~127 次）。结论：原子写保证**无撕裂内容**，但不保证读方零瞬态占用；测试对瞬态允许 ≤50ms 有限重试，只对「0 字节/半截 JSON」判红线。
- 语义残留（与修复前一致，非回归）：互斥保证「排队写、不丢写」，仍是 last-writer-wins；并发下后落盘的可能是稍旧快照，下次刷新收敛（loadLocalCache 为合并语义，不会丢工具）。
- 测试 seam 说明：`scanLocalToolsForMigration = env.ScanLocalTools` 必须存在——批量路径否则会真实全盘扫描并可能写用户环境变量（污染主机）。两个 seam 均「默认=生产实现，仅测试赋值」。
