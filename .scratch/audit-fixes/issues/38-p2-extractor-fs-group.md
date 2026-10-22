# 38 P2 抽取器与文件系统组

Status: resolved
Type: task
批次: B5
对应修复项: P2-01(A-12) + P2-02(A-13) + P2-03(A-14) + P2-04(B-10) + P2-05(B-11) + P2-06(B-14) + P2-26(X-02) + P2-32(N-17)（方案 §6）

## 方案要点

1. **P2-01 tar TypeLink**：switch 增加 `case tar.TypeLink:`——Linkname 相对**归档根**解析 `Join(dst, linkname)`（不能照搬 TypeSymlink 的 Join(Dir(out)) 语义）；**linkname 须同 stripPath 归一**（strip≥1 归档的硬链 linkname 含顶层前缀，不归一则指向未提取路径）；目标侧显式 `isSafePath(dst, target)`；目标已提取后 os.Link，失败 warning 跳过。
2. **P2-02 UNC**：`\\` 前缀拒绝只作用于 rel 部分（Join 前），允许 UNC dst；或明确报「UNC home not supported」。
3. **P2-03 runInstaller SHA**：复检接受 `{a.SHA256, a.MirrorSHA256}` 集合。
4. **P2-04 loadFavorites**：赋值前 favMu.Lock()（两行）。
5. **P2-05 autoAppendBinToPath**：补实现 `env.AppendToUserPath(filepath.Join(value, "bin"))` 或删函数（确认产品意图二选一）。
6. **P2-06 FixLocalVersions**：每工具 AcquireToolLock（20s，失败跳过下次再修）。
7. **P2-26 HideWindow**：extractor.go:370/:395/:478 三处 exec.Command 补 `util.HideWindow(cmd)`。
8. **P2-32 CopyFile 权限**【复核新增】：fs.go:32 落盘改源文件 `Stat().Mode().Perm()`（Unix 迁移 0644 不再变 0755）。

## 守卫测试

strip≥1 归档含硬链条目正确落盘；UNC home 明确报错；其余各一例。

## Comments（Batch 5 实施记录）

- 改动（8 项全部落地）：
  - **P2-01**（`internal/extractor/extractor.go`）— 新增 `case tar.TypeLink:`：linkname 按**归档根**语义经 `stripPath(hdr.Linkname, strip)` 归一（**不照搬** TypeSymlink 的 `Join(Dir(out))` 相对条目目录语义——这是 POSIX 两类链接的语义区别）；目标侧显式 `isSafePath` 检查；目标已提取则 `os.Link`（失败先 warning，再**回退为复制**以不丢内容）；linkname 落在剥离前缀之上 → 记日志跳过；目标未提取 → warning 跳过（不失败整个解压）。
  - **P2-02**（extractor.go）— `isSafePath` 的 UNC 拒绝改为「**仅当 dst 本身不是 UNC**」（UNC 安装根不再让每个条目被误判 zip slip）；同时在 zip 与 tar 两条路径的 **rel 层**新增绝对/UNC 名拒绝（`filepath.IsAbs(rel) || strings.HasPrefix(rel, "\\\\")`），使「UNC 条目」仍被拦下。
  - **P2-03**（`internal/installer/installer.go` runInstaller）— SHA 复检接受 `{a.SHA256, a.MirrorSHA256}` 集合（EqualFold 任一命中；镜像哈希命中时记日志），错误信息同时给出两者。
  - **P2-04**（`internal/gui/app_favorites.go`）— `loadFavorites` 赋值前 `favMu.Lock()`（Wails 每个绑定调用独立 goroutine，原赋值与 RLock 读者竞争）。
  - **P2-05**（`internal/gui/app_env_vars.go`）— **决策：删除函数 + 调用点**（而非补实现）。理由：该函数是**死代码**（只查映射表即 return，无任何动作），而「补实现」意味着用户编辑 `*_HOME` 时**隐式改写其 PATH**——编辑器不该有这种副作用；PATH 归 shim/CleanupUserPath 专职机制所有。删除零行为变化，仅消除误导。决策已写入代码注释。
  - **P2-06**（`internal/gui/app_local_detect.go`）— `FixLocalVersions` 的循环体抽成 `fixLocalVersionForTool`，每个工具持 `util.AcquireToolLock(xmdeDir, tool, 20s)`（与 installer/activator 互斥；失败记日志跳过，下次再修）。
  - **P2-26**（extractor.go）— `7z x` / `7z l` / `extractViaCommand` 三处 `exec.Command` 补 `util.HideWindow`（前三处 hdiutil/ditto 原本已有；非 Windows 为 no-op）。**过程教训**：首次用 PowerShell 批量插入时误重复插入且破坏文件 UTF-8（改用 edit 工具重做，`git checkout` 恢复后逐处编辑）——**Go 源文件不要用 PowerShell `Set-Content` 改写**。
  - **P2-32**（`internal/util/fs.go` CopyFile）— 落盘 mode 取源文件 `Stat().Mode().Perm()`；**已存在的目标保留自身 mode**（不再一律 0755）。
- 守卫测试：`internal/extractor/hardlink_unc_test.go`（`TestExtractTar_HardLinkMaterialized`（strip=1 + 硬链内容/同 inode）/ `TestExtractTar_DanglingHardLinkSkipped` / `TestIsSafePath_UNCDestinationTolerated`）；`internal/installer/installer_correctness_test.go` 追加 `TestRunInstaller_AcceptsMirrorSHA`（镜像哈希通过 + 什么都不匹配仍拒）；`internal/util/copyfile_mode_test.go`（`TestCopyFile_PreservesSourceMode`：源 0644 保留、既有目标 0600 不被改；Windows 分支按平台说明）。
- 平台说明：**Windows 的 `os.OpenFile` 完全忽略 perm 参数**（实测落盘 0666），故 P2-32 的 mode 断言仅在 Unix 分支执行（CI Linux 覆盖）；同理 `TestUnzipFile_StripsSetuid`（票 26 A-08）的 setuid 断言也仅 Unix 生效。
- 验证：`go build ./...` exit 0；`go vet ./internal/installer/ ./internal/util/ ./internal/extractor/` exit 0；三包 `-count=1` 全绿。
