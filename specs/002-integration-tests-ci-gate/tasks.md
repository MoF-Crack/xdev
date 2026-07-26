# Tasks: integration tests CI 门禁

> **文档类型**：Task List
> **状态**：Completed
> **创建日期**：2026-07-25
> **关联 Spec**：`specs/002-integration-tests-ci-gate/spec.md`
> **关联 Plan**：`specs/002-integration-tests-ci-gate/plan.md`

## 任务列表

| # | 任务 | 负责人 | 状态 | 审查 | 提交 |
|---|---|---|---|---|---|
| T001 | 在 CI workflow 中创建 `integration-tests` job，配置 matrix 三平台 + 30 分钟超时 | xinyan | Completed | Reviewed | `fd71d89` |
| T002 | 修复非 Windows 平台 `SysProcAttr` 类型签名（`any` → `*syscall.SysProcAttr`） | xinyan | Completed | Reviewed | `d68723d` |
| T003 | 更新 spec.md 批准记录并创建审批文件 | xinyan | Completed | Reviewed | `fd71d89` |

## 验收清单

- [x] FR-001: CI workflow 包含专用 job 执行 `go test -tags=integration ./...`
- [x] FR-002: 任一集成测试失败时，对应 CI job 标记为 failure
- [x] FR-003: 集成测试 job 设置执行超时 30 分钟，超时后标记为 failure
- [x] FR-004: 集成测试 job 的失败输出包含失败测试的名称、文件和行号（Go test 默认输出）
- [x] FR-005: 本地开发者可用相同命令复现集成测试结果
- [x] CI 通过
- [x] go test 通过
