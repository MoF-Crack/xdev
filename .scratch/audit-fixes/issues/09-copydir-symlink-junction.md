# 09 copyDir SkipDir 修正 + junction 机制重写 + 断链条目可见化

Status: resolved
Type: task
批次: B2
对应修复项: P0-04 + N-01（方案 §4 P0-04；机制按复核修订版）

## 问题

1. util/fs.go:97-101 对目录符号链接条目返回 `filepath.SkipDir`——Go 语义（1.26.5 源码 path.go 证实）：非目录条目返回 SkipDir 跳过**父目录剩余条目** → 兄弟静默漏拷；调用方（app_migration.go:571→:586 RemoveAll 源、env/migrate.go:126+138、rollbackMove :617-634）构成不可恢复数据丢失。
2. Windows junction（ModeIrregular）被当普通文件 CopyFile 响亮失败（scoop 布局迁移必败）。
3. 【N-01】fs.go:89-95 EvalSymlinks/os.Stat 失败的符号链接条目 `return nil` 静默跳过——**第二条静默丢数据路径**（断链/不可达目标不报错不拷贝，随后 RemoveAll 删源）。

## 方案（注意：junction 机制与 v1 不同——EvalSymlinks 对末位 junction **返回自身而非报错**，按 v1 字面实施修复零效果）

1. symlink 目录分支 `filepath.SkipDir` → `return nil`。
2. junction 分支：条件扩 `info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0`；`filepath.EvalSymlinks(path)` → **`resolved == path` 或 err 时转 `os.Readlink`（对 junction 支持）+ `os.Stat` 兜底** → 目标为目录则递归拷贝（用解析出的目标路径，保留 `resolved == path` 护栏防自递归）后 `return nil`；全失败才按普通文件处理。
3. 断链条目：EvalSymlinks/os.Stat 失败改「warning 日志 + skipped 计数」，copyDir 进度/返回值携带 skipped 数（迁移结果可见；保守跳过不卡死）。

## 守卫测试

- `aaa.txt / current(链) / v2.0.1 / zzz.txt` 兄弟用例断言全拷。
- 断链条目用例：warning + skipped 计数 ≥1。
- junction 实测单测（util.CreateJunction / mklink /J，CI windows-latest 可跑）：**按修正机制断言目标被递归拷贝**（v1 机制下此测试必红——正是护栏）。

## Comments（Batch 2 实施记录）

- 改动：`internal/util/fs.go` — 链接分支重写（`ModeSymlink|ModeIrregular` 条件扩展；EvalSymlinks → 返回自身/失败时 Readlink 兜底 → Stat 判定；**`filepath.SkipDir` → `return nil`**（兄弟漏拷根因）；断链/不可解析条目 WARN 日志 + skipped 计数；`samePath` 自递归护栏）；新增 `CopyDirReport(src, dst, onProgress) (files, skipped, err)` + 内部 `copyStats`（CopyDir/CopyDirProgress 签名不变，兼容全部既有调用方）；`internal/gui/app_migration.go` 的跨盘迁移回退路径改用 CopyDirReport，skipped>0 时 logWarn 显式暴露（源目录即将 RemoveAll）。
- **PoC 先红后绿（方法论记录）**：修复前用同一组测试跑出红——
  - C3 红：`CopyDir: read ...\current: Incorrect function.`（junction 走 CopyFile 响亮失败）
  - C4 红：`CopyDirReport: open ...\dangling: The system cannot find the file specified.`（断链被当普通文件，复制失败且不可见）
  - C5 红：`CopyDirReport: open ...\selfloop: The name of the file cannot be resolved by the system.`
  - C1 在 Windows 因 symlink 权限 skip（C3 的 junction 变体覆盖同一缺陷；Unix CI 跑 C1 实体）。
  修复后同一命令：C3/C4/C5 全 PASS（`ok internal/util 1.997s`）。
- 守卫测试：`internal/util/copydir_poc_test.go` — TestCopyDir_SymlinkSiblingsAllCopied（C1）/ TestCopyDir_JunctionFollowed_Windows（C3）/ TestCopyDir_BrokenLinkSkippedWithWarning（C4：skipped==1 + WARN 日志含条目名 + 正常文件已拷）/ TestCopyDir_SelfReferentialGuard（C5：10s 终止护栏 + skipped≥1）。
- **实证发现**：Go 1.26.5/Windows 对 junction 的 `Lstat` 返回 `ModeIrregular`（无 ModeSymlink/ModeDir）、`EvalSymlinks` 返回自身、`Readlink` 返回真实目标——与方案 v2 的机制修订完全一致（v1 的「EvalSymlinks 报错」假设下修复零效果）。
- C6（scoop 布局集成冒烟）：由 C3（junction + 多版本目录兄弟）+ 迁移回退路径的 skipped 可见化在单测层等价覆盖；真机 scoop 迁移冒烟未执行（非阻塞）。
