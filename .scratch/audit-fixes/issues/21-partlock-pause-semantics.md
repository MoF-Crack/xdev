# 21 .part.lock 互斥 + Pause 语义重做（P1-01 + P1-02 同 PR）

Status: resolved
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

## Comments（Batch 4 实施记录）

- **陈旧锁盘点结论（票 23 共用）**：`util.AcquireFileLock` 是 OS 字节范围锁（Windows `LockFileEx` / Unix flock）——持有者退出即由内核释放，**残留锁文件不阻塞后来者**，因此**无需 mtime 老化接管**（票面备选分支不成立）。这正是崩溃续传接管成立的基础。
- 改动：
  - `internal/downloader/downloader.go` downloadOnce — 打开 `.part` **之前** `util.AcquireFileLock(<dest>.part.lock, 1s)`，拿不到返回「another download is already in progress」明确错误（含清理引导）；`defer Release()`（成功/失败都释放）；M-4 的 symlink 病灶清除重试分支**保留在锁内**（不被「并发」语义吞掉，DL3 断言）。
  - `internal/downloader/manager.go`：
    - runDownload — 启动时快照 `dlCtx := task.ctx`；`Download(dlCtx, ...)`；收尾**先比对 `task.ctx != dlCtx`**，过期代（Pause→Resume 已换代 / 已被 Cancel 写终态）**静默退出不写终态**（原逻辑是「旧 goroutine 收尾把已恢复的下载标成 Cancelled 并删 .part」）。
    - Pause — **次序回改**：锁内先 `pausedAt=Downloaded; Status=Paused` 再 `cancel()`；重复 Pause 由状态门天然拒绝（N-07：pausedAt 不再被覆写）。
    - Cancel — `task.cancel()` 移入 `task.mu` 锁内（N-06 竞争）；`os.Remove` 失败**记日志**（Windows 无 FILE_SHARE_DELETE 必 ACCESS_DENIED），靠 `.part.lock` 接管语义闭环。
    - CleanupOnExit — 状态读取与 cancel 同一次加锁（N-06）；task.mu 是叶子锁，与 m.mu.RLock 无死锁。
    - Resume — 注释登记「立即 Resume 的共写窗口」残留（旧 goroutine 最后一笔 Write 可落在新 goroutine seek 之后；hash 门兜住），按票面弱化措辞。
- 守卫测试：`internal/downloader/part_lock_test.go` — DL1（并发同 dest：**恰一成功一失败**且失败信息含 "another download is already in progress"，胜者归档完整）/ DL2（崩溃残留 .part + 陈旧 .part.lock 被接管续传成功）/ DL3（符号链接 .part 在锁内清除，目标文件未被写穿）/ DL4（**Pause → 立即 Resume 成功**）/ DL5（Pause 后 .part 仍在且状态为 paused 而非 cancelled）/ DL6（写者持句柄时 Cancel 不 panic，状态 cancelled）/ DL7（第二次 Pause 被状态门拒绝且 pausedAt 不被覆写）/ DL9（Pause → 等待 → Resume 回归完成）。
- DL1 测试设计实证：首版服务延迟 300ms 时「两个都成功」——**那是合法行为**（先完成的下载释放锁，后者随后取得并正常下载）；把延迟提到 2s（覆盖 1s 锁获取超时）后确定性得到「一胜一败」。
- **DL8（-race）本机不可执行**：唯一可用 MinGW 8.1.0 的导入库与 Go 1.26 race 运行时不兼容（实测 `__imp___acrt_iob_func` SDYNIMPORT 链接错误；WSL 未安装）。替代证据：N-06 的 task.cancel 全部移入锁内（代码级消除竞争面）+ 相关测试 `-count` 重复运行稳定；race 门待 CI/具备现代工具链的环境执行。
