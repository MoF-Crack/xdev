# Implementation Plan: CI Go 工具链版本一致性

> **文档类型**：Implementation Plan
> **状态**：Ready
> **文档版本**：0.1.0
> **创建日期**：2026-07-25
> **最后更新**：2026-07-25
> **所有者**：xinyan <zuijiuhjd@foxmail.com>
> **关联 Spec**：`specs/001-ci-go-version-consistency/spec.md` `0.2.0` (Accepted)
> **权威范围**：定义所选技术方案、模块边界、接口、数据流和验证方案

**Branch**: `001-ci-go-version-consistency` | **Date**: 2026-07-25 | **Spec**: `specs/001-ci-go-version-consistency/spec.md` `0.2.0`

---

## Summary

创建 `scripts/ci/check-go-version.sh` 脚本用于自动比对 `go.mod` 的 `go` 指令版本与 CI workflow 中声明的 `go-version`。该脚本从 `go.mod` 提取权威 Go 版本（使用 `go mod edit -json` 或回退文本解析），与 `.github/workflows/ci.yaml` 中 `go-version` 字段做 minor 级别比对，不一致时以非零退出码失败并输出期望版本、实际版本和修复指引。CI 中新增 "Verify Go version consistency" 步骤在所有构建之前执行。

---

## Technical Context

**Language/Version**: Go 1.25（与 `go.mod` 一致）
**Primary Dependencies**: `go mod edit -json`、`grep`、`awk`、`sed`（POSIX 工具链）
**Storage**: 无
**Testing**: `go test` + CI 验证
**Target Platform**: Windows/Linux/macOS
**Constraints**: 不修改 `go.mod` 内容；版本比对按 minor 级别匹配；检查逻辑不得硬编码版本号

---

## 技术方案

### 模块边界

| 文件 | 职责 |
|---|---|
| `scripts/ci/check-go-version.sh` | Go 版本一致性检查脚本 |
| `.github/workflows/ci.yaml` | CI workflow — 新增 "Verify Go version consistency" step |

### 接口

**脚本入口**: `bash scripts/ci/check-go-version.sh`
**退出码**: 0（一致）/ 1（不一致）
**输出**:
- 一致时：`Go version consistent: go.mod=<ver>, CI=<ver>`
- 不一致时：`::error::Go version mismatch: go.mod requires <ver> but CI uses <ver>` + 修复指引

### 数据流

```
CI "Verify Go version consistency" step
  → scripts/ci/check-go-version.sh
    → go mod edit -json → 提取 Go 版本
    → grep go-version from .github/workflows/ci.yaml
    → minor 级别比对（cut -d. -f1,2）
    → 一致 → exit 0
    → 不一致 → exit 1 + ::error:: 消息
```

---

## 任务分解

| 任务 | 描述 | 进度 | 提交 |
|---|---|---|---|
| T001 | 创建 `scripts/ci/check-go-version.sh` 脚本，实现 `go.mod` 版本提取、CI 版本提取、minor 级别比对和错误输出 | Completed | `fd71d89` |
| T002 | 在 CI workflow 的 build job 中添加 "Verify Go version consistency" step，位于前端构建和依赖下载之后 | Completed | `fd71d89` |
| T003 | 更新 spec.md 批准记录并创建 `.specify/approvals/` 审批文件 | Completed | `fd71d89` |

---

## 验证方案

- **CI 验证**: "Verify Go version consistency" step 在每次 push/PR 时自动执行
- **手动验证**: 在本地运行 `bash scripts/ci/check-go-version.sh`，确认退出码为 0
- **负向测试**: 临时修改 `go.mod` 的 `go` 指令版本，运行脚本确认非零退出码和错误输出

---

## 风险与缓解

| 风险类别 | 风险描述 | 严重度 | 缓解措施 |
|---|---|---|---|
| 兼容性 | 新增 CI 步骤可能增加构建时间 | 低 | 检查逻辑极轻量，<5 秒 |
| 跨平台 | CI runner OS 差异可能影响 go.mod 解析 | 低 | 使用 `go mod edit -json` 或跨平台文本解析 |

---

## 批准记录 (Approval History)

| 阶段 | 状态 | 批准者 | 日期 | 文档版本 | 批准范围 |
|---|---|---|---|---|---|
| Plan Ready | Ready | xinyan <zuijiuhjd@foxmail.com> | 2026-07-25 | 0.1.0 | 2026-07-25-plan-001-001 实施计划已就绪 | 实施计划已就绪 |
