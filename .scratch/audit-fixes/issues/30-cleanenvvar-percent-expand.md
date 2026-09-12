# 30 CleanEnvVarPath %VAR% 展开

Status: resolved
Type: task
批次: B5
对应修复项: P1-21（B-08）（方案 §5 主题 C）

## 问题（复核确认的根因）

Go 的 `os.ExpandEnv` 只展开 `$var`，**%VAR% 从不触发展开**——`%JAVA_HOME%\bin` 字面量送 os.Stat 必失败 → **所有含 % 引用的条目一律被误删**（app_env_vars.go:297-331；同病 ValidateEnvVar:162-198、buildPathEntries:133-152）。

## 方案

1. 手动 regex `%([A-Za-z_][A-Za-z0-9_]*)%` 以**合并环境**（user 注册表 + machine 注册表 + 进程 env，键大小写不敏感）展开，再 os.Expand 处理 $ 形式。
2. CleanAllInvalidEnvVars 批量场景对合并环境做一次性缓存（避免 N² 枚举）。
3. **不得**用「定制 getenv 回调」方案（v1 已修正：会让全部 %-引用落入保守跳过 → 清理功能退化 no-op）。

## 守卫测试

含 `%JAVA_HOME%\bin`（注册表定义、进程未定义）的合法条目不再被删；真正失效条目仍被清理（不退化为 no-op）。

## Comments（Batch 5 实施记录）

- 改动（`internal/gui/app_env_vars.go`）：新增 `percentVarRe` + `expandPercentVars`（合并环境 = **进程 env 为底，machine/user 注册表覆盖**；键大写归一即大小写不敏感；**未命中保留原样**并由 `percentVarsUnresolved` 判无效）+ `pathEntryExists`（`%VAR%` → `os.ExpandEnv`（`$VAR` 形式）→ `os.Stat`）；`percentExpander` 惰性快照，`CleanAllInvalidEnvVars` 整批**共享一次枚举**（消除 N² 开销）；`CleanEnvVarPath` / `ValidateEnvVar` / `buildPathEntries` / `GetAllEnvVars` **三处同病同修**；新增 5 个包级可替换 seam（合并环境快照/枚举/读/写/删，默认全为生产实现）。**未**采用定制 getenv 回调方案（会让全部 %-引用落入保守跳过 → 清理退化 no-op）。
- 守卫测试：`app_env_vars_percent_test.go`（`TestCleanEnvVarPath_PercentVarEntrySurvives` / `TestValidateEnvVar_PercentVarEntryValid` / `TestCleanEnvVarPath_UndefinedPercentVarStillRemoved`（**防 no-op 回归**）/ `TestExpandPercentVars_CaseInsensitiveAndPreservingUnresolved` / `TestCleanAllInvalidEnvVars_EnumeratesMergedEnvOnce`）。
- **红线先红后绿**：旧行为 → `合法条目 %JAVA_HOME%\bin 被误删（清理后 = ""）`、`buildPathEntries … Exists:false`；缓存未共享 → `合并环境枚举次数 = 14, want 1`。还原后全绿，无探针残留。
- 有意的行为副作用：`validationStatus` 与 `ValidateEnvVar` 共用 `looksLikePathValue`，含 `%VAR%\...` 的值现在给出 valid/invalid 而非 not-applicable（这正是 B-08 的目的）。
- 已知边界：`%VAR:~0,3%` 之类 cmd 子串语法不展开（保留原样 → 判无效 → 清理）；注册表 PATH 本不应存此形态，已记录。
