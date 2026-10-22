# 32 自更新自愈组（P1-25/26）

Status: resolved
Type: task
批次: B5
对应修复项: P1-25(E-06) + P1-26(E-07)（方案 §5 主题 D）

## 方案要点

1. **P1-25 replaceBinaryWindows**：①update.go:383 删 .old 前置条件 `if _, err := os.Stat(currentExe); err == nil`；currentExe 不存在时先把 .old rename 回 currentExe 再继续（自愈恢复）；②copyAndReplace 改「写 currentExe+".new" → rename 覆盖」（运行中 exe 仍会失败但不再留半截损坏；启动清理陈旧 .new）。
2. **P1-26 ReplaceStaleCLICopy**：stale.go:98 CopyFile 改「写 copyPath+".tmp" → SHA 校验 → rename」；出错/mismatch 时 Remove(.tmp) 并**删除已损坏 copyPath**（EnsureSelfInstalled 缺失分支下次重拷——自愈）。

## 守卫测试

崩溃恢复场景（currentExe 缺失 + .old 完好）→ 二进制恢复；损坏副本探测失败后能自愈（v1 下永久跳过）。

## Comments（Batch 5 实施记录）

- 改动：
  - **E-06**（`internal/update/update.go`）— 新增 `restoreFromBackup`：currentExe 缺失且 `.old` 完好时**先把 `.old` rename 回活路径再继续**（替代原来先 `os.Remove(backup)` 删掉唯一可恢复副本的行为）；`copyAndReplace` 改「写 `currentExe+".new"` → Sync → rename 覆盖」，并在替换入口清理陈旧 `.new`（不再直接截断目标）。
  - **E-07**（`internal/update/stale.go`）— 新增 `replaceCopyAtomic`（`os.CreateTemp` 唯名 tmp → SHA-256 校验 → rename；失败清 .tmp）与 `removeDamagedCopy`（校验不符时**删除已损坏的 copyPath**，使下次 `EnsureSelfInstalled` 重拷自愈；仅删普通文件、绝不删目录）；权限跟随源文件（CreateTemp 是 0600，必须还原可执行位）。
- 守卫测试：`internal/update/selfheal_test.go` — `TestRestoreFromBackup_HealsMissingExe` / `_NonTriggeringCases` / `TestReplaceBinaryWindows_InterruptedUpdateKeepsRecoverableBinary` / `_RecoversThenReplaces` / `TestCopyAndReplace_StagingThenRename` / `_NoDirectTruncate` / `TestReplaceCopyAtomic_HashMismatchDeletesDamagedCopy` / `_SuccessPublishesVerifiedCopy` / `TestReplaceStaleCLICopy_HealsHalfWrittenCopy`。
- **红线先红后绿**：restoreFromBackup 改 no-op → `healed = false, want true` + `currentExe 丢失（自愈失败）: … The system cannot find the file specified`（正是审计场景：崩溃后二进制永久丢失）；`replaceCopyAtomic` 回退 v1 直写 → `replaceCopyAtomic with wrong hash = nil, want error`（v1 把未校验内容发布出去）。还原后全绿，无探针残留。
- 验证：`go test ./internal/update/ -count=1` ok；`go build ./...` exit 0。
- 测试安全：全部用 temp dir + 假二进制构造，**未触碰** `build/bin` 或运行中的 xdev 二进制。
