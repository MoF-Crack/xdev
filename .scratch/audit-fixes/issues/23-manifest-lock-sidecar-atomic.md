# 23 manifest 文件锁 + TOFU sidecar 原子写

Status: open
Type: task
批次: B4
对应修复项: P1-09 + P1-13 + N-16（方案 §5 主题 B）

## 方案

1. **P1-09**：mergeIntoManifest 与 UpdateArchiveSHA256 共用 `util.AcquireFileLock(<manifestDir>/<tool>.yaml.lock, 30s)` 包住 RMW 全程；两个基线首写点（scheduler.go:740 与 manifest.go:826）改原子写、`_=` 丢弃错误改记日志、scheduler.go:846 .bak 写一并改；进程内 manifestMu 保留。**锁序**：yaml.lock 与 sniff-cache 锁保持串行不嵌套；manifestMu→yaml.lock 单向序。
2. **P1-13**：writeSidecarHash 改「写 `<sidecar>.tmp` → Sync → **清目标只读属性（Chmod 0644，Windows rename 覆 0444 必败）** → rename → chmod 0o444」。调用点 **3 处**（:564 畸形重写、**:570 全零占位首写——v1 漏列**、:609 空 SHA 首写）由函数级修复自动覆盖；自测须含 :570。

## 守卫测试

并发嗅探合并 + GUI 更新 SHA 不互相丢更新（-race）；并发下载不读到半截 pin；Windows 上 0444 目标覆写路径通过。
