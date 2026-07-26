# Tasks: UI/下载源计划剩余集成验收

> **文档类型**：Task List
> **状态**：Completed
> **创建日期**：2026-07-25
> **关联 Spec**：`specs/007-ui-download-integration-acceptance/spec.md`
> **关联 Plan**：`specs/007-ui-download-integration-acceptance/plan.md`

## 任务列表

| # | 任务 | 负责人 | 状态 | 审查 | 提交 |
|---|---|---|---|---|---|
| T001 | search 命令无匹配时退出码修复（exit 1 → exit 0） | xinyan | Completed | Reviewed | `fd71d89` |
| T002 | TRANSIENT 错误重试：`internal/linkaudit/audit.go` 实现 3 次重试 + 2s backoff | xinyan | Completed | Reviewed | `16d81e6` |
| T003 | 不可修复工具文档化：创建 `docs/history/unfixable-tools.md` | xinyan | Completed | Reviewed | `16d81e6` |
| T004 | SHA audit（#1）和 `.yaml.sig` 重签（#2）确认完成 | xinyan | Completed | Reviewed | `11caf17` |
| T005 | 创建 `docs/history/spec-007-deferred-items.md` 汇总记录 | xinyan | Completed | Reviewed | `cfa189e` |
| T006 | 更新 spec-007 deferred 记录 #3/#4 为已完成 | xinyan | Completed | Reviewed | `5e2b8c5` |

## 验收清单

- [x] FR-001: 32 条 Android manifest 占位 hash 替换为真实 SHA-256（已完成于之前会话）
- [x] FR-002: 5 组同 URL 不同 SHA 冲突已统一（已完成于之前会话）
- [x] FR-003: 被修改 SHA 的 manifest 已重新生成 `.yaml.sig` 签名（已完成于之前会话）
- [x] FR-004: TRANSIENT 错误支持自动重试（最多 3 次，2s backoff）
- [x] FR-005: 不可修复工具（Haskell/Kotlin/Ninja/UV）文档记录在 `docs/history/unfixable-tools.md`
- [ ] FR-006: `toolsHashFullCheck/main.go` vet 告警修复（deferred）
- [x] CI 通过
- [x] go test 通过
