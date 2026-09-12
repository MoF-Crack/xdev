# 32 自更新自愈组（P1-25/26）

Status: open
Type: task
批次: B5
对应修复项: P1-25(E-06) + P1-26(E-07)（方案 §5 主题 D）

## 方案要点

1. **P1-25 replaceBinaryWindows**：①update.go:383 删 .old 前置条件 `if _, err := os.Stat(currentExe); err == nil`；currentExe 不存在时先把 .old rename 回 currentExe 再继续（自愈恢复）；②copyAndReplace 改「写 currentExe+".new" → rename 覆盖」（运行中 exe 仍会失败但不再留半截损坏；启动清理陈旧 .new）。
2. **P1-26 ReplaceStaleCLICopy**：stale.go:98 CopyFile 改「写 copyPath+".tmp" → SHA 校验 → rename」；出错/mismatch 时 Remove(.tmp) 并**删除已损坏 copyPath**（EnsureSelfInstalled 缺失分支下次重拷——自愈）。

## 守卫测试

崩溃恢复场景（currentExe 缺失 + .old 完好）→ 二进制恢复；损坏副本探测失败后能自愈（v1 下永久跳过）。
