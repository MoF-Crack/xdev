# 03 Uninstall 嵌套锁自死锁释放

Status: resolved
Type: task
批次: B1
对应修复项: P0-05（方案 §4 P0-05）

## 问题

installer.go Uninstall :1501 取锁全程持锁，:1544 锁内调 Activate（:1178 再取同名锁）→ Unix flock 同进程双 ofd 自死锁 30s；re-point 永久失败、current 链接悬空（:1537 已删）。InstallWithProgress（Spec 037）已修同型，Uninstall 漏修。

## 方案

re-point 分支前显式 `lock.Release()`（mutex.go:53-60 判空幂等，defer 二次 Release 为 no-op）：
```go
if otherVer := i.findOtherVersion(p); otherVer != "" {
    otherPath := p.InstallDir(...)
    lock.Release() // Release before re-acquiring in Activate
    if err := i.Activate(p, otherPath, otherVer); err != nil { ... }
}
```
不重取锁（分支后仅 InvalidateShimIndex 内存操作）。Windows F-03 残留按方案披露（遗留观察项 2）。

## 守卫测试

Unix 上两版本场景卸载激活版本：断言不卡 30s、re-point 成功、`current/<tool>` 重建。

## Comments（Batch 1 实施记录）

- 改动：`internal/installer/installer.go` Uninstall re-point 分支前显式 `lock.Release()`（对照 InstallWithProgress Spec 037 同型注释：Unix flock per-ofd 自死锁 + Windows 命名互斥体仅线程内可重入）；分支后不重取锁（余下仅 InvalidateShimIndex 内存操作）；defer 二次 Release 为 no-op（mutex.go 判空幂等）。
- 守卫测试：`internal/installer/uninstall_repoint_test.go` — TestUninstall_ActivatedVersion_RepointsFast（实测 1.18s < 2s 阈值，重死锁时 ~30s；断言 current→B、shim 重建、旧目录清除）/ TestUninstall_LastVersion_RemovesLink（1.28s；current 链接删除、shims 清除）。Windows F-03 残留按方案披露为遗留观察项。
