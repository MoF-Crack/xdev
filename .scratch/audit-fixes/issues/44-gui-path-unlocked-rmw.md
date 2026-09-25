# Issue 44: GUI PATH 清理走「锁外读 + 锁内盲写」——并发 xdev 进程的 RMW 竞态

Status: open（本轮已复核确认，修复设计定稿待实施）
Type: bug
Severity: high（P2；与 P0-11 锁体系同一威胁模型——静默丢用户 PATH 条目）
Discovered: 2026-09-25，第六轮审计（env/host-mutation 分路）
来源票：audit-fixes 票 10（P0-11 锁体系）收口后的复扫——锁体系补齐了
`CleanupUserPath` 等九个 mutator，但漏了 GUI 的 `CleanEnvVarPath` 链路。

## 根因

- `internal/gui/app_env_vars.go:492`（读）与 `:536`（写）：
  `cleanEnvVarPath` 先经 `envGetEnvVarFn`（无锁）读 PATH，计算出清理后的
  新值，再经 `envSetEnvVarFn` → `env.SetEnvVar` 的 PATH 分支落盘——而该分支
  （`win_path_windows.go` SetEnvVar 的 `withUserPathLock(WriteUserPath(value))`）
  的锁**只包住盲写**，读发生在锁外。
- name="Path" 被 GUI 特意放行（`app_env_vars.go:483` 的
  `IsProtectedSystemEnvVar(name) && !EqualFold(name, "path")`）。
- 机器级分支（`WriteSystemPath`）完全没有锁。

## 失败场景

GUI「清理无效 PATH 条目」与 `xdev doctor` / `xdev install`（其 PATH RMW
全程持锁）并发：GUI 在锁外读到快照 → CLI 持锁写入新条目 → GUI 锁内盲写
把 CLI 刚写的条目**静默覆盖丢弃**。双向皆有（GUI 写晚丢 CLI 的，CLI 写晚
丢 GUI 的——取决于交错）。

## 修复设计（定稿）

1. `internal/env` 新增两个锁内 RMW 助手（Windows 实现 + `win_path_other.go`
   直通 stub，复用 `withUserPathLock`——`CleanupSystemPath` 已证明跨键复用
   同锁无死锁）：
   - `TransformUserPath(transform func(cur string) (next string, changed bool, err error)) (bool, error)`
   - `TransformSystemPath(transform ...)` 同型
   读经 `userPathReadFn`/`ReadUserPath`、写经 `userPathWriteFn`（防污染
   TestMain 的内存后端对 transform 同样生效）；changed=false 或 next==cur
   跳过写；写成功后 `BroadcastEnvironmentChange()`。
2. GUI 接线：`cleanEnvVarPath` 对 `EqualFold(name, "path")` 分流到 transform
   助手，把「条目存在性清理」放进 transform 闭包（捕获 percentExpander）；
   其余键维持现状（`envGetEnvVarFn`/`envSetEnvVarFn` 测试接缝不变）。
3. 测试接缝：gui 侧加 `envTransformPathFn` 包级变量（默认指向 env 助手），
   `app_env_vars_percent_test.go` 的两条 CleanEnvVarPath 守卫补注入——
   transform 假实现对齐现有 fake store 的 get/set。
4. 守卫测试（先红后绿）：复刻 ticket 10 的 K1 模式——transform 执行期间
   从另一 goroutine 写入 PATH，断言该写入不被 transform 的落盘丢弃。

## 关联

- P2-3 的两个广播缺口（SetEnvVar PATH 分支、卸载 env 删除）已在本轮修复
  （见 commit）；本票只余锁内 RMW 本体。
- P3-13（SetWindowsUserPath 空串尾部空条目 / no-op 重写）与 P3-12
  （UpdateShimDirInUserPath 吞错）建议随本票一并处理（同在 PATH mutator 族）。
