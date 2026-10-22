# Issue 45: shim 分发器刷新/安装机制 P3 簇（第六轮审计 shim 分路）

Status: open（P3 簇，逐项可独立实施）
Type: bug
Severity: medium（P3×7；无一破坏当前正确性，均为加固/一致性/收敛项）
Discovered: 2026-09-25，第六轮审计（shim dispatcher 链，含 E:\XDev 实机取证）

## 簇内条目（均可独立验证）

1. **P3-3 归属判定仅按扩展名形状**（refresh.go:49-51）：shims/ 是 PATH 前置
   目录，用户自放工具是自然用法；现规则下裸名/.exe 之外的文件不碰，但
   裸名/.exe 的**任何**文件（用户的 gsudo.exe）都会被 rename 舞步内容替换。
   修复：覆写前做归属校验（字节头与 selfPath 一致 / 名字在 manifest
   `shims:` 名单并集内），不一致跳过 + 告警；同步修正 refresh.go:23-24
   「不碰非分发器文件」的过度声明注释。
2. **P3-4 .old 残留不主动清**（实证：运行中 exe 可 rename、delete 被拒）：
   旧分发器进程存活期间 `os.Remove(oldPath)` 静默失败，之后该条目变成
   SameFile no-op，残留永久留存（每别名每更新代 ≤1 份 21MB）。
   修复：RefreshDispatchers 每处理完一条目后 best-effort `os.Remove(dst+".old")`。
3. **P3-5 CopyFile 兜底原地覆写**（shim.go:579-581）：rename 失败时回退
   `placeWindowsShim` → CopyFile 以 O_TRUNC 原地改写现分发器，并发调用方会
   读到撕裂 exe。修复：rename 舞步失败即返回错误（调用方计 failed，下次
   刷新重试），不回退原地覆写。
4. **P3-6 stale.go 注释与实现矛盾**：注释宣称镜像语义覆盖「同版本不同
   构建（本地重编）」，实现按 `probed == version.Version` 短路。修复二选一：
   SHA-256 对比（真镜像语义）或改注释承认版本串语义。
5. **P3-7 分发器硬链接源不一致**：Activate 链 `ResolveShimExe()`（current/xdev）
   vs 刷新链 `os.Executable()`——两群同内容异 inode 的分发器每次 GUI 启动
   全量 rename 舞步（churn + 空窗），FAT32 上 copy→hardlink 收敛承诺失效。
   修复：统一单一规范源。
6. **P3-8 索引并发缺陷（先在）**：`shimIndexOnce` 在 `shimIndexMu` 下重置但
   `scanFor` 的 `Do` 无锁（race-detector 可见）；`writeDiskShimIndex` →
   `util.AtomicWriteFile` 用固定 `<path>.tmp`——两进程并发 Invalidate 会互相
   截断同 tmp 名后 rename 发布损坏索引（P1-24 教训未应用到此处）。
   修复：AtomicWriteFile 唯名 tmp；Once 读写一致加锁。
7. **P3-9 refreshUnixEntry 非原子**（remove→symlink 窗口：别名短暂缺失，
   symlink 失败则分发器**丢失**）。修复：临时名 symlink + rename 覆盖。

## 附带观察（不修，记录在案）

- `.cmd` 分发经 cmd.exe 重展开参数（BatBadBut 类）——nvm/scoop 同款取舍，
  属 Windows 分发 .cmd 的固有属性。
- 磁盘索引把 `npm` 键指向无扩展名 bash 脚本；Windows 分发靠 Go exec 的
  lookExtensions 兜到 npm.cmd（实测 npm.EXE → 11.19.1 exit 0）。脆弱点：
  PATHEXT 缺 .CMD 的机器或未来绕过 os/exec 的 run 实现会静默断。建议在
  addExecutables 的 Windows 侧给 .cmd/.exe 优于无扩展名兄弟文件的排序。

## 验证要求

每项先红后绿；P3-4 复用本会话的锁语义实证法（运行中 exe：rename 成功、
delete 拒绝）。
