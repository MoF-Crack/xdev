# 26 安装器正确性组（P1-03/04/05/06/07）

Status: resolved
Type: task
批次: B5
对应修复项: P1-03(A-07) + P1-04(A-08) + P1-05(A-09) + P1-06(A-11=D-10) + P1-07(A-15/A-15b)（方案 §5 主题 A）

## 方案要点

1. **P1-03 Force 先删后下载**：次序反转——Force 分支先下载+SHA 校验成功，删除移入锁内 extract 前（推荐）；守卫：Force+不可达 URL → 原安装仍在。
2. **P1-04 zip mode**：unzipFile（extractor.go:293）`f.Mode()` → `f.Mode().Perm()`（与 tar 路径对齐）；symlink 条目维持降级 + 注释。
3. **P1-05 glob 兜底**：删除 installer.go:987 glob `*-{ver}.{fmt}` 兜底改直接报 cache miss（或保守版：前缀限定 `tool+"-"` + 命中跑 sidecar/SHA 校验——**本机已有受害者**：E:\XDev\install\llvm\lld_is_a_generic_driver._ 装 android-build-tools 内容）。
4. **P1-06 TOFU any**：TOFUArchiveIfNeeded 不代换 os/arch=any（传原始值）；UpdateArchiveSHA256 匹配先精确、再回退「条目 any 且请求键为宿主」双向兜底。
5. **P1-07 npm PATH**：prependEnvPath（npm_install.go:404-419）EqualFold 匹配（前 5 字符）；前置目录改 `filepath.Dir(tc.nodeBin)`（Unix bin 布局）。

## 守卫测试

Force+断网原安装保留；zip setuid 位剥离；glob 移除后异常竞争场景报错可重装；any 条目 pin 写回成功；Windows "Path=" 命中改造。

## Comments（Batch 5 实施记录）

- 改动：
  - **A-07**（`internal/installer/installer.go`）— Force 分支**不再**立即 `os.RemoveAll`，改为置 `forceReinstall = true` 并继续下载；删除移到「下载+SHA 校验成功后、installLock 临界区内、installByKind 之前」。断网/404 时原安装与 `current` 链接完好。
  - **A-08**（`internal/extractor/extractor.go` unzipFile）— `f.Mode()` → `f.Mode().Perm()`（原先把 setuid/setgid/sticky 与类型位一并交给 OpenFile）。
  - **A-09**（installer.go extract）— **删除** `*-<ver>.<fmt>` glob 兜底；cache miss 直接报错并带可操作引导（`clear the cache entry and reinstall`）。原 glob 可把他工具缓存（如审计实锤的 `lld_is_a_generic_driver._-<ver>.zip`）解压进本工具安装树。
  - **A-11/D-10**（installer.go TOFUArchiveIfNeeded）— 传条目**原始** os/arch（不再把 any 代换为宿主平台）；`UpdateArchiveSHA256` 失败且原值为 any/空时，用宿主平台**重试一次**（双向兜底），成功则记录日志。
  - **A-15**（`internal/installer/npm_install.go` prependEnvPath）— 键匹配改 `strings.EqualFold(e[:len("PATH=")], "PATH=")` + 长度守卫（Windows 常见 `Path=` 不再漏配成第二个 PATH 条目）。
  - **A-15b**（npm 安装 env 组装）— 追加前置 `filepath.Dir(tc.nodeBin)`（Unix 为 `<dir>/bin`），确保托管 node 真正排在 PATH 首位。
- 守卫测试：`internal/installer/installer_correctness_test.go` — I1 `TestForceInstall_DownloadFailKeepsOld`（不可达 URL + Force → 原安装标记文件/二进制/`current` 链接全在）/ I3 `TestCacheMiss_NoGlobFallback`（存在他工具畸形缓存名时仍报 cache miss 且无内容泄漏）/ I4 `TestTOFUAnyEntry_PinWrittenBack`（any/any 条目 pin 写回成功）/ I5 `TestPrependEnvPath_CaseInsensitive`（PATH/Path/path 三种键、不新增第二条目、缺失时追加一次）/ I5b `TestNPMPath_UsesNodeBinDirectory`；`internal/extractor/zip_mode_test.go` — I2 `TestUnzipFile_StripsSetuid`。
- **I2 平台说明**：Windows 的 `os.OpenFile` 完全忽略 perm 参数（实测落盘 0666），故 setuid/Perm 断言仅在 Unix 分支执行（CI Linux 覆盖）；Windows 分支退化为「恶意 mode 不致解压失败 + 内容正确」。
- 附注（测试设计）：票 19 的 IL1 原为「并发两次 ExtractAndActivate 断言一胜一败」，在被本批并行负载放大后暴露 **F-03 的概率性**（两个 goroutine 双双 ReleaseMutex 失败 → 锁泄漏 → 双双等满 5 分钟 extract 锁超时，实测一次 300s）。已改为确定性形式（单持锁者 + 序列重复安装 + 锁释放断言），并发互斥由 IL2 的子进程探针覆盖——详见票 19 Comments。
