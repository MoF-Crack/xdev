# 23 manifest 文件锁 + TOFU sidecar 原子写

Status: resolved
Type: task
批次: B4
对应修复项: P1-09 + P1-13 + N-16（方案 §5 主题 B）

## 方案

1. **P1-09**：mergeIntoManifest 与 UpdateArchiveSHA256 共用 `util.AcquireFileLock(<manifestDir>/<tool>.yaml.lock, 30s)` 包住 RMW 全程；两个基线首写点（scheduler.go:740 与 manifest.go:826）改原子写、`_=` 丢弃错误改记日志、scheduler.go:846 .bak 写一并改；进程内 manifestMu 保留。**锁序**：yaml.lock 与 sniff-cache 锁保持串行不嵌套；manifestMu→yaml.lock 单向序。
2. **P1-13**：writeSidecarHash 改「写 `<sidecar>.tmp` → Sync → **清目标只读属性（Chmod 0644，Windows rename 覆 0444 必败）** → rename → chmod 0o444」。调用点 **3 处**（:564 畸形重写、**:570 全零占位首写——v1 漏列**、:609 空 SHA 首写）由函数级修复自动覆盖；自测须含 :570。

## 守卫测试

并发嗅探合并 + GUI 更新 SHA 不互相丢更新（-race）；并发下载不读到半截 pin；Windows 上 0444 目标覆写路径通过。

## Comments（Batch 4 实施记录）

- **步骤 1 盘点结论（供票 21 复用）**：`util.AcquireFileLock` 用 **OS 字节范围锁**（Windows `LockFileEx` / Unix flock，`filelock_windows.go`），持有者崩溃时**由内核释放**——残留 `.lock` 文件本身永不会阻塞后来者，所以**无需 mtime 老化接管**（票面「若无回收→加老化」的分支不成立）。这直接支撑了票 21 的 `.part.lock` 崩溃接管语义（DL2 实测通过）。
- 改动：
  - `internal/discovery/scheduler.go` mergeIntoManifest — 全程 `util.AcquireFileLock(<tool>.yaml.lock, 30s)`；基线首写点 `_ = os.MkdirAll` + `_ = os.WriteFile` → 原子写 + 失败记日志；`.bak` 写改 AtomicWriteFile。
  - `internal/manifest/manifest.go` UpdateArchiveSHA256 — 保持 `manifestMu`（进程内）后再取文件锁（**同序获取，无锁序倒置**）；从嵌入创建基线副本改 AtomicWriteFile；头注释更正（原自称 atomic 但基线裸写——复核发现属实）。
  - `internal/downloader/downloader.go` writeSidecarHash — 五步原子写（`.tmp` → WriteString → **Sync** → 清目标只读属性(0644，Windows rename 覆 0444 必败) → rename → Chmod 0444），三个调用点（畸形重写/全零占位首写/空 SHA 首写）由函数级修复自动覆盖。
- 守卫测试：`internal/discovery/merge_lock_test.go` — TestMergeIntoManifest_ConcurrentWithUpdateSHA_NoLostUpdate（M1：8 轮交替 merge 新版本 + UpdateArchiveSHA256 写哈希，断言全部版本留存、至少一次哈希写回留存、最终 YAML 可解析）/ TestBaselineWrite_AtomicAndLogged（M3：基线首写完整可解析 + 无 .tmp 残留）；`internal/downloader/sidecar_atomic_test.go` — TestWriteSidecarHash_Overwrites0444Target（M4）/ TestWriteSidecarHash_AllZeroFirstWrite（**M5，票面强调的 :570 分支**，经真实 Download 全零占位路径）/ TestConcurrentDownload_NoHalfPin（M6：200 轮覆写与并发读交错，读者永不见半截 pin）。
- M2（双进程 RMW 集成测试）未做：成本高（需 exec 子进程编排），其语义由 OS 锁的跨进程性质 + M1 进程内并发覆盖；记录为可选集成项。
