# 30 CleanEnvVarPath %VAR% 展开

Status: open
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
