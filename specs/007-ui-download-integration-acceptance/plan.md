# Implementation Plan: UI/下载源计划剩余集成验收

> **文档类型**：Implementation Plan
> **状态**：Ready
> **文档版本**：0.1.0
> **创建日期**：2026-07-25
> **最后更新**：2026-07-26
> **所有者**：xinyan <zuijiuhjd@foxmail.com>
> **关联 Spec**：`specs/007-ui-download-integration-acceptance/spec.md` `0.2.0` (Accepted)
> **权威范围**：定义所选技术方案、模块边界、接口、数据流和验证方案

**Branch**: `007-ui-download-integration-acceptance` | **Date**: 2026-07-25 | **Spec**: `specs/007-ui-download-integration-acceptance/spec.md` `0.2.0`

---

## Summary

完成 Spec 007 定义的 5 项集成验收事项中的 4 项：`search` 命令无匹配时退出码修复（`fd71d89`）、不可修复工具文档化（`16d81e6`）、TRANSIENT 错误自动重试（`16d81e6`）、SHA audit 和 `.yaml.sig` 重签（已在之前会话完成，确认提交 `11caf17`）。第 5 项 `toolsHashFullCheck` vet 告警修复延迟处理。所有已完成项记录在 `docs/history/spec-007-deferred-items.md` 中。

---

## Technical Context

**Language/Version**: Go 1.25
**Primary Dependencies**: `internal/linkaudit/audit.go`（TRANSIENT 重试）、`internal/cli/available.go`（search 修复）、`internal/linkaudit/ci_gate.go`（CI gate）
**Storage**: `docs/history/unfixable-tools.md`（不可修复工具文档）
**Testing**: `go test` + CI linkaudit
**Target Platform**: Windows/Linux/macOS
**Constraints**: TRANSIENT 重试最多 3 次，backoff 2 秒；不可修复工具文档不修改运行时行为

---

## 技术方案

### 模块边界

| 文件 | 职责 |
|---|---|
| `internal/cli/available.go` | search 命令无匹配时从 `fmt.Errorf`（exit 1）改为 `fmt.Printf` + `return nil`（exit 0） |
| `internal/linkaudit/audit.go` | 实现 TRANSIENT_ERROR 结果自动重试：最多 3 次，2 秒 backoff |
| `docs/history/unfixable-tools.md` | 记录 Haskell、Kotlin、Ninja、UV 四个工具的镜像不可用状态和缓解措施 |
| `docs/history/spec-007-deferred-items.md` | Spec 007 已完成和延迟项的集中记录 |

### 接口

**search 命令**:
- 原行为：无匹配时 `return fmt.Errorf("no tool matches %q", args[0])` → exit 1
- 新行为：`fmt.Printf("no tool matches %q\n", args[0])` + `return nil` → exit 0

**TRANSIENT 重试**（`internal/linkaudit/audit.go` Audit 函数）:
- 重试条件：`r.FailureReason == StatusTransientError`
- 最大重试次数：3
- Backoff：2 秒（`time.After(2 * time.Second)`）
- 上下文感知：`ctx.Done()` 可中断重试

### 数据流

```
TRANSIENT 重试流程:
Audit() 第一轮 → 检查所有 result 的 FailureReason
  → 有 TRANSIENT_ERROR? → 重试循环（最多 3 次）
    → 对每个 TRANSIENT_ERROR 条目重新 inspect()
    → 2 秒 backoff（非首次）
    → 更新 result
  → 无 TRANSIENT_ERROR? → 继续后续处理
```

---

## 任务分解

| 任务 | 描述 | 进度 | 提交 |
|---|---|---|---|
| T001 | search 命令无匹配时退出码修复（exit 1 → exit 0） | Completed | `fd71d89` |
| T002 | TRANSIENT 错误重试：`internal/linkaudit/audit.go` 中实现 3 次重试 + 2s backoff | Completed | `16d81e6` |
| T003 | 不可修复工具文档化：创建 `docs/history/unfixable-tools.md` | Completed | `16d81e6` |
| T004 | SHA audit（#1）和 `.yaml.sig` 重签（#2）确认完成 | Completed | `11caf17` |
| T005 | 创建 `docs/history/spec-007-deferred-items.md` 汇总记录 | Completed | `cfa189e` |
| T006 | 更新 spec-007 deferred 记录 #3/#4 为已完成 | Completed | `5e2b8c5` |

---

## 验证方案

- **search 修复**: `go test ./internal/cli/` — `TestSearch_MatchAndMiss` 验证无匹配时 exit 0
- **TRANSIENT 重试**: linkaudit 集成测试验证 TRANSIENT 场景下重试行为
- **不可修复工具文档**: 确认 `docs/history/unfixable-tools.md` 存在且覆盖 4 个工具
- **SHA audit/重签**: 确认所有 38 个 `.yaml` manifest 有正确 SHA256，38 个 `.yaml.sig` 签名文件对应完整

---

## 风险与缓解

| 风险类别 | 风险描述 | 严重度 | 缓解措施 |
|---|---|---|---|
| 安全 | 占位 hash 使下载完整性校验失效 | 高 | 已通过 SHA audit 替换为真实 SHA |
| 兼容性 | manifest SHA 修改后签名失效 | 中 | 已完成 `.yaml.sig` 重签 |
| 跨平台 | SHA audit 需要三平台产物但可能仅在 Windows 执行 | 中 | 同 URL 不同平台已按相同 SHA 统一 |

---

## 批准记录 (Approval History)

| 阶段 | 状态 | 批准者 | 日期 | 文档版本 | 批准范围 |
|---|---|---|---|---|---|
| Plan Ready | Ready | xinyan <zuijiuhjd@foxmail.com> | 2026-07-25 | 0.1.0 | 2026-07-25-plan-007-001 实施计划已就绪 | 实施计划已就绪 |
