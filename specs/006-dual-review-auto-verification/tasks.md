# Tasks: 双重审查和批准记录自动验证

> **文档类型**：Task List
> **状态**：Completed
> **创建日期**：2026-07-25
> **关联 Spec**：`specs/006-dual-review-auto-verification/spec.md`
> **关联 Plan**：`specs/006-dual-review-auto-verification/plan.md`

## 任务列表

| # | 任务 | 负责人 | 状态 | 审查 | 提交 |
|---|---|---|---|---|---|
| T001 | `scripts/docs/validate-approvals.ps1` 审批验证脚本已存在并整合到 governance CI | xinyan | Completed | Reviewed | `ba5310d` |
| T002 | Governance CI 阻塞模式启用，验证失败时阻塞 PR 合并 | xinyan | Completed | Reviewed | `e91310c` |
| T003 | Spec 006 已审批，确认现有验证基础设施满足双重审查自动验证需求 | xinyan | Completed | Reviewed | `fd71d89` |

## 验收清单

- [x] FR-001: 验证脚本扫描活跃 Spec 目录的 `tasks.md`，提取 `Verified` 状态的任务 ID
- [x] FR-002: 验证脚本检查 `reviews/` 目录下每个 `Verified` 任务存在两类审查记录
- [x] FR-003: 验证脚本比对审查者与实现者身份，重复时报告违规
- [x] FR-004: 缺失审查记录或独立性违规时返回非零退出码
- [x] FR-005: CI governance workflow 调用验证脚本并将输出纳入治理报告
- [x] CI 通过
- [x] go test 通过
