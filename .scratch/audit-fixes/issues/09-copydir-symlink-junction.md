# 09 copyDir SkipDir 修正 + junction 机制重写 + 断链条目可见化

Status: open
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
