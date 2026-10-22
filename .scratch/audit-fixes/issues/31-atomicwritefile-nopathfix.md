# 31 AtomicWriteFile 改造 + no-path-fix 一致化（P1-23/24）

Status: resolved（含 P1-24 本体未落地的披露，见 Comments）
Type: task
批次: B5
对应修复项: P1-23(E-04) + P1-24(E-05=F-07) + N-15（方案 §5 主题 C/D）

## 方案要点

1. **P1-24**：AtomicWriteFile（fs.go:215-225）改 `os.CreateTemp(dir, base+".tmp-*")` → 写入 + `Sync()` + `Chmod(perm)`（CreateTemp 0600 需还原；Windows 0600 不触发只读）→ rename → 失败清理。**同型固定 tmp 克隆 4 处一并迁移**（v1 清单 8+2，复核扩为 8+4）：writeHomeCache（config.go:1206-1213）、MirrorHealthCache.Save（installer/mirror_selector.go:103-130）、**update/policy.go writeJSONAtomic（:141-159——无 Sync 无 perm 还原恒 0600）**、**toolsHashFullCheck progress.go Progress.Save（:99）**。
2. **P1-23**：删除 init.go:73 本地 --no-path-fix flag（复用 root.go:109 持久 flag）；:169 与 :219/:254 一致化用同一来源（现状「部分生效」——自装门有效、仅向导安装阶段失效——测试覆盖该现状后统一）。断言 `xdev init --no-path-fix` 后 NoPathFix()==true。

## 守卫测试

100 goroutine 并发写同一路径循环读取校验（P1-24）；CI e2e 的 --no-path-fix 用例语义不变（P1-23）。

## Comments（Batch 5 实施记录）

- **P1-24 收敛核验表（8 调用点全部走 `util.AtomicWriteFile`）**：`internal/config/config.go`（saveLocked / UpdateConfigPaths）、`internal/discovery/scheduler.go`（sniff cache ×2 + `.bak`）、`internal/cli/refresh.go`、`internal/manifest/manifest.go` ×3、`internal/gui/app_custom_tools.go`（审计写 :203，实际已漂移到 :294）；另 `internal/gui/app.go`、`internal/shim/shim.go`（票 28 新增）。
- **4 克隆迁移**：`writeHomeCache`（config.go → 新 `writeFileCreateTemp`：CreateTemp + Sync + 权限还原 + Windows rename 重试 ~55ms）✅；`internal/update/policy.go writeJSONAtomic`（补 Sync + Chmod 0644）✅；❌ `internal/installer/mirror_selector.go` 与 `toolsHashFullCheck/progress.go` **仍为固定 `.tmp`**（白名单外，只报告）。
- **⚠ 票面前提不成立（重要披露）**：票面「P1-24 本体已在上一批落地」与 HEAD 不符——`internal/util/fs.go` 的 `AtomicWriteFile` **仍是固定 `path + ".tmp"`**，因此 8 个调用点当前实际仍是固定 tmp 语义（只是集中在一个函数里）。守卫 `TestNoFixedTmpIntermediateFiles` 用 `fixedTmpExceptions` 登记了这 4 处（含 `internal/downloader/downloader.go` 的 sidecar 固定 tmp），新增未登记点立刻变红；这些点修好后只需从例外表删行。**建议后续单开一票修本体 + 3 处克隆**（本体修好可显著提升并发健壮性）。
- **P1-23/E-04**：删除 `internal/cli/init.go` 的局部 `--no-path-fix`（它会遮蔽 root 的持久 flag）；`runInitWizard` 去参；三处门（`ins.SetNoEnvWrite(NoPathFix())` / shell RC / Windows PATH）统一读持久 flag。
- 守卫测试：`internal/config/atomicwrite_convergence_test.go`（`TestNoFixedTmpIntermediateFiles` grep 守卫 + `TestWriteFileCreateTemp_DoesNotUseFixedTmpName`（预建 `<target>.tmp` 目录占位，唯名 tmp 不受影响）+ `TestWriteFileCreateTemp_ConcurrentWritersNoTornReads`（100 写 × 8 读，变长负载结构自校验）+ `TestAtomicWriteCallSitesConverged`）；`internal/cli/nopathfix_flag_test.go`（局部 flag 不存在 ×2 + 三处门源码守卫 + **子进程**真跑 `NewRootCommand` 验证接线）。
- **红绿记录**：writeHomeCache 回退固定 tmp → grep 守卫红（`仍在使用固定 .tmp`）+ 确定性守卫红（`open …home.cache.tmp: is a directory`）；恢复局部 flag → 三条 flag 守卫红（`init 定义了局部 --no-path-fix…会遮蔽 root 的持久 flag` 等）。还原后全绿，`git grep REDPROBE` 无残留。
- **e2e（手工）**：`xdev.exe --xmdevenv-dir <tmp> --no-path-fix init --yes` → exit 0、**USER PATH 与 MACHINE PATH 均未变**、自装到 `<tmp>\current\xdev\xdev.exe`、无 RC 注入。
- 并发守卫判据诚实说明：固定 tmp 探针下读者未稳定撕裂（记录为噪声/健壮性守卫），判别力主要交给上面两条确定性守卫。
- **本票实施过程中发生并已修复的宿主事故（详见票 34/会话记录）**：测试在进程内构造 root 命令 → cobra 进程级 `OnInitialize` → `initContext("")` 解析回真实 home → 覆写用户 `E:\XDev\config.yaml`（已从 .bak 合并恢复并独立核验：mirror 8 条真实、proxy 完整、ai 段完整、无测试假值）。加固：`injectHome` 现在同时 `t.Setenv("XMDEV_HOME", home)`，使任何绕过注入的 `config.Load("")` 也只能落到 temp home（实测 cli 整包测试后真实 config.yaml mtime 不变）。
