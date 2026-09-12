# 14 CLI profile 导出白名单（GUI 无需改）

Status: resolved
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

## Comments（Batch 3 实施记录）

- 改动：`internal/cli/profile.go` — 新增 `collectExportEnvVars()`（`env.GetAllUserEnvVars()` 后逐名 `util.IsAllowedEnvVarName` 过滤，返回 envVars + skipped）；新增 `profileExportSummary(path, skipped)` → `Profile exported to %s (skipped %d non-xdev variables)`，经 `cmd.OutOrStdout()` 打印（默认仍 stdout，可见行为不变，只为可测）。GUI 零改动（复核裁决已确认 GUI 走固定 21 名枚举）。
- 守卫测试：`internal/cli/profile_export_whitelist_test.go`（windows）— TestProfileExport_WhitelistOnly（真实 newExportCmd + 真实 HKCU 写入 MY_SECRET/JAVA_HOME + save/restore + t.Cleanup；断言导出 JSON 无 MY_SECRET、有 JAVA_HOME、skipped==baseline+1）/ TestProfileExportSummary_SkippedWording（skipped=1 文案钉死）。
- **变异校验（守卫有效性）**：临时去掉白名单过滤 → 导出 JSON 出现 MY_SECRET、**GITHUB_TOKEN（真实 github_pat_…）**、Path、TEMP/TMP、OneDrive、CODEX_HOME 等 15 项 → FAIL。**现网泄露实锤：修复前 `xdev profile export` 会把用户的 GitHub PAT 明文写进团队共享的 profile 文件**（比审计描述的「可能泄露 MY_SECRET」更严重）。已还原，无残留。
- 测试断言偏离说明：票面要求断言摘要含 `skipped 1`，但本机 HKCU 本就有 14 个非白名单变量，硬编码 1 必误报 → 端到端用例断言 `skipped == baseline+1`，字面文案由独立用例钉死。
- Release note 条目：**CLI profile 导出只包含 xdev 管理的环境变量**（白名单外名称静默跳过并在摘要中计数）——团队共享 profile 不再泄露无关环境变量。
