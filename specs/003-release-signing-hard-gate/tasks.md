# Tasks: 发布签名硬门禁

> **文档类型**：Task List
> **状态**：Completed
> **创建日期**：2026-07-25
> **关联 Spec**：`specs/003-release-signing-hard-gate/spec.md`
> **关联 Plan**：`specs/003-release-signing-hard-gate/plan.md`

## 任务列表

| # | 任务 | 负责人 | 状态 | 审查 | 提交 |
|---|---|---|---|---|---|
| T001 | 在 `release` job 中添加 "Verify signing secrets (hard gate)" 步骤 | xinyan | Completed | Reviewed | `d4ea53b` |
| T002 | 在 `release-macos` job 中添加相同步骤 | xinyan | Completed | Reviewed | `d4ea53b` |
| T003 | 在 `release-arm64` job 中添加相同步骤 | xinyan | Completed | Reviewed | `d4ea53b` |
| T004 | 创建 Spec 003 审批文件 | xinyan | Completed | Reviewed | `444ec6f` |

## 验收清单

- [x] FR-001: 签名步骤前检查 `XDEV_SIGNING_PRIVATE_KEY` 存在且非空
- [x] FR-002: 公钥注入步骤前检查 `XDEV_SIGNING_PUBLIC_KEY` 存在且非空
- [x] FR-003: 任一密钥检查失败时停止所有后续步骤，不产出发布产物
- [x] FR-004: 密钥检查失败日志明确标识缺失的 secret 变量名
- [x] FR-005: 提供负向测试用例（缺失密钥时 CI 按预期失败）
- [x] CI 通过
- [x] go test 通过
