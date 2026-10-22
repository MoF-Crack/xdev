# 28 manifest 加载/resolver/shim 索引组（P1-11/14/15）

Status: resolved
Type: task
批次: B5
对应修复项: P1-11(D-06) + P1-14(D-09) + P1-15(C-04) + N-20 注释（方案 §5 主题 B）

## 方案要点

1. **P1-11**：loadMergedCatalog 单文件解析失败分支（manifest.go:415-416）加 `log.Printf("[manifest] WARNING: skip unparseable %s: %v")`；容错语义保留；**顺带更正 :374-378 函数注释**（「嵌入优先」实现 vs 注释「disk as override」矛盾——N-20）。
2. **P1-14**：Resolve 的 KindExact（resolver.go:80）两侧 TrimPrefix("v")，精确匹配优先（先原始串全等，miss 再双侧去 v）——对内置（无 v 前缀）与自定义工具（Pattern 捕获组含 v）均无回归。
3. **P1-15**：scanFor 磁盘命中后 `os.Stat(entry.path)` 失败 → **强制 buildShimIndexMap 现扫**并以现扫结果回答（只回落 sync.Once 内存索引会回环）；writeDiskShimIndex（shim.go:273 裸 os.WriteFile）改 AtomicWriteFile。

## 守卫测试

坏 yaml → 日志 + embedded 基线生效；`Resolve(Parse("v3.12.0"), ["3.12.0"])` == "3.12.0" 且 `Resolve(Parse("v1.2.3"), ["v1.2.3"])` == "v1.2.3"；陈旧磁盘索引 → 现扫兜底（恢复 bf69b85 透传语义）。

## Comments（Batch 5 实施记录）

- 改动：
  - **D-06**（`internal/manifest/manifest.go` loadMergedCatalog 磁盘并行加载）— 容错语义保留（单条跳过），读失败与解析失败两个静默分支各补一条 `WARNING: skip unreadable/unparseable <file>` 日志（`log` 包自带互斥，可安全用于并行 goroutine）。未动票 23 的锁/原子写与票 27 的比较器。
  - **D-09**（`internal/resolver/resolver.go` KindExact）— 方案 B：先原始串全等（零回归），miss 后**双侧** TrimPrefix("v") 比较；**不**规范化 available 本身（自定义工具带 v 的产出仍可匹配），返回值恒为 available 中的原始串（调用方据此拼安装目录）。
  - **C-04**（`internal/shim/shim.go`）— `scanFor` 磁盘索引命中后先 `os.Stat(entry.path)`：存在则照旧；失败（陈旧死路径）→ 调 `buildShimIndexMap` **现扫**并以现扫结果回答，现扫亦无该别名 → 明确未命中（Lookup 报 not installed → MaybeExec 走 Spec 038 PATH 透传）。**刻意不回落 `sync.Once` 内存索引**（可能正是陈旧来源，会回环）。`writeDiskShimIndex` 的裸 `os.WriteFile` → `util.AtomicWriteFile`。
  - **N-20 注释** — 只读确认 `loadMergedCatalog` 注释已由票 13 更正为 EMBEDDED-FIRST，与实现一致 → 未重复修改。
- 守卫测试：`internal/manifest/manifest_skip_warning_test.go`（28-1：坏 YAML 告警 + 坏条跳过 + 嵌入基线仍生效；悬空符号链接覆盖读失败分支）/ `internal/resolver/resolver_vprefix_test.go`（28-2：双向 v 前缀 5 子用例 + 去 v 不得变模糊匹配 + Range 未波及）/ `internal/shim/shim_index_stale_test.go`（28-3：陈旧索引现扫兜底、无活副本时报未命中、新鲜索引仍短路〔防「每次现扫」回归〕、索引原子写无 .tmp 残留）。
- **红线先红后绿**：D-06 红（缺告警）、D-09 红（`version v3.12.0 not found` / `version 3.12.0 not found`）、C-04 红（`must not dispatch to stale path …npm-deleted.exe` / 死路径被透传）→ 还原后全绿，`grep RED-CHECK-DISABLED|if false` 命中 0。诚实说明：`TestWriteDiskShimIndex_AtomicWriteLeavesNoResidue` **无法红**（单进程断言不到原子性），属弱守卫。
- 验证：`go build ./...` exit 0；`go vet` exit 0；`go test ./internal/manifest ./internal/resolver ./internal/shim -count=1` 全绿（shim 35 PASS/0 FAIL/0 SKIP）；`./internal/plugin`（resolver.Resolve 的两个调用方）无回归。
- **白名单外发现（未改，建议后续出票）**：①`manifest.go:318-326` 的 `LoadCatalog` 文档注释仍是错的（写「on-disk version takes priority」，实现是嵌入优先 + 磁盘仅追加新版本）——票 13 漏改的同类（N-20）；②两条加载路径容错语义不一致：有 embeddedFS 走 loadMergedCatalog（跳过+告警），无 embeddedFS 走 `loadFromDir`（首个解析错误即整体失败）；③`util.AtomicWriteFile` 用固定 `<path>.tmp`，多写者并发下可能一个 rename 失败（仅记日志）或遗留 .tmp（最终文件不截断，C-04 需要的性质已满足）；④`shim` 内存索引是包级全局而 scanFor 收 xmdeHome 参数——多 home 切换下是既有隐患。
- C-04 性能说明：正常命中仅多 1 次 `os.Stat`；现扫只发生在「索引路径已消失」时（成本 ≈ current/* 浅层 walk），且现扫结果不回写（避免热路径写放大）；某别名长期陈旧时每次 Lookup 都会重扫——如需优化可另票做一次性惰性修复。
- 环境附录：`internal/shim` 非 verbose 输出带 `[no tests to run]` 后缀是**既有外观噪声**（自执行测试 fork 测试二进制且 `cmd.Stderr = os.Stderr`，子进程告警进入 cmd/go 捕获输出），非失败，改动前即存在。
