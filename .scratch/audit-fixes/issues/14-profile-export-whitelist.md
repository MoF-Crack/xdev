# 14 CLI profile 导出白名单（GUI 无需改）

Status: open
Type: task
批次: B3
对应修复项: P0-12（方案 §4 P0-12）

## 问题

cli/profile.go:82-92 export 用 GetAllUserEnvVars 全量导出 HKCU 全部用户环境变量（注释宣称「set by xdev」）→ 团队共享文件泄露 GITHUB_TOKEN 等；import 侧白名单——能力不对称。

## 复核裁决（已并入方案）

**GUI 侧无需改动**：ExportProfile 走 a.GetEnvVars()（app_env_vars.go:51-57 固定 21 名枚举）且仅导 Source=="user"（app_profile.go:91-99）；internal/gui 全包无 GetAllUserEnvVars 调用。v1 正文「GUI 走同一数据面」为误，勿照改 GUI。

## 方案

CLI 导出按 util.AllowedEnvVarNames 白名单过滤（与 import 对称）；白名单外静默跳过 + 摘要打印 `skipped N non-xdev variables`。

## 守卫测试

`setx MY_SECRET abc` → export → JSON 不含 MY_SECRET、含 JAVA_HOME（若设置过）；release note 说明语义变化。
