# 21 .part.lock 互斥 + Pause 语义重做（P1-01 + P1-02 同 PR）

Status: open
Type: task
批次: B4
对应修复项: P1-01（复核重设计）+ P1-02 + N-06/N-07（方案 §5 主题 A）
耦合: **同一 PR 不可拆**（.part.lock 与 Pause 快照/次序一体）

## P1-01：.part.lock 方案（替代 v1 的 AllowResume 布尔——v1 在共享调用点行为矩阵互斥且吞 symlink 清除分支）

- 下载开始前 tryAcquire `<dest>.part.lock`（`util.AcquireFileLock` 已存在 filelock.go:30；非阻塞/1s 短超时）：拿到=无并发写者（O_EXCL 失败→lstat 常规文件→续传=崩溃残留可接管）；拿不到=真并发→报「another download in progress」（含清理引导）。
- 锁在 downloadOnce 结束（含失败）后释放；崩溃残留锁按 AcquireFileLock 陈旧回收语义（无则加 mtime 老化）。
- downloader.go:405-408 symlink 病灶清除重试分支**保留在锁内**。
- GUI StartDownload 与 CLI downloadWithProgress(:667) 统一经此机制；Resume（manager.go:240）天然满足（接管锁即续传）。

## P1-02：Pause 语义（相对 v1 的关键修订）

- ①runDownload 启动快照 `dlCtx := task.ctx`，Download 与收尾判定均用快照；收尾若发现自己非当前 ctx 静默退出不写终态。
- ②**次序回改**：锁内**先**置 `Status=Paused + pausedAt` **再** cancel（v1「cancel 后置位」在窗口内旧 goroutine 读 pausedAt==0 会走 Cancelled 分支 os.Remove(.part)——Pause 退化 Cancel）。
- ③Cancel 的 Remove 失败（Windows 无 FILE_SHARE_DELETE 必 ACCESS_DENIED）记日志，依赖 .part.lock 接管语义闭环。
- ④【N-06】task.cancel 字段改锁内访问（Cancel :264 / CleanupOnExit :332 现为锁外——race 级竞争）。
- ⑤【N-07】Pause 重复成功覆写 pausedAt——快照化后一并消除。

## 守卫测试

- Resume/崩溃续传保持现行为（.part 接管成功）；并发双开一方明确报错。
- Pause 后立即 Resume 可用（不再报 downloading）；Pause 不再丢 .part；`go test -race ./internal/downloader` 全绿（task.cancel 竞争消除）。
