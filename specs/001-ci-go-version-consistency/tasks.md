# Tasks: CI Go 工具链版本一致性

> **文档类型**：Task List
> **状态**：Completed
> **创建日期**：2026-07-25
> **关联 Spec**：`specs/001-ci-go-version-consistency/spec.md`
> **关联 Plan**：`specs/001-ci-go-version-consistency/plan.md`

## 任务列表

| # | 任务 | 负责人 | 状态 | 审查 | 提交 |
|---|---|---|---|---|---|
| T001 | 创建 `scripts/ci/check-go-version.sh` 脚本，实现 go.mod 版本提取、CI 版本提取、minor 级别比对和错误输出 | xinyan | Completed | Reviewed | `fd71d89` |
| T002 | 在 CI workflow 的 build job 中添加 "Verify Go version consistency" step | xinyan | Completed | Reviewed | `fd71d89` |
| T003 | 更新 spec.md 批准记录并创建 `.specify/approvals/` 审批文件 | xinyan | Completed | Reviewed | `fd71d89` |

## 验收清单

- [x] FR-001: CI 从 `go.mod` 提取 `go` 指令值作为权威版本
- [x] FR-002: CI 将所有使用 Go 的 job 中声明的 `go-version` 与权威版本比对
- [x] FR-003: 不一致时 CI 明确失败并输出期望版本、实际版本和 CI 配置文件路径
- [x] FR-004: 本地提供等效的脚本来验证版本一致性
- [x] FR-005: 版本比对规则为 minor 级别匹配（`go 1.23` 匹配 `1.23.0`、`1.23.4`）
- [x] CI 通过
- [x] go test 通过
