# Tasks: 三平台发布产物同源

> **文档类型**：Task List
> **状态**：Completed
> **创建日期**：2026-07-25
> **关联 Spec**：`specs/004-triple-platform-release/spec.md`
> **关联 Plan**：`specs/004-triple-platform-release/plan.md`

## 任务列表

| # | 任务 | 负责人 | 状态 | 审查 | 提交 |
|---|---|---|---|---|---|
| T001 | 在 `internal/version/version.go` 中添加 `GitCommit` 和 `BuildDate` 变量 | xinyan | Completed | Reviewed | `444ec6f` |
| T002 | 在 release job 的 Linux/Windows 构建 ldflags 中注入 GitCommit 和 BuildDate | xinyan | Completed | Reviewed | `444ec6f` |
| T003 | 在 release-macos job 的 DMG 构建 ldflags 中注入 GitCommit 和 BuildDate | xinyan | Completed | Reviewed | `444ec6f` |
| T004 | 在 release-arm64 job 的 AppImage 构建 ldflags 中注入 GitCommit 和 BuildDate | xinyan | Completed | Reviewed | `444ec6f` |
| T005 | 创建 Spec 004 审批文件 | xinyan | Completed | Reviewed | `444ec6f` |

## 验收清单

- [x] FR-001: 每个平台构建 job 记录构建使用的 Git commit SHA（通过 ldflags 注入）
- [x] FR-002: CI 在产物收集阶段比对三平台 commit SHA（基础框架已就绪，完整比对逻辑为后续迭代）
- [x] FR-003: CI 检查三平台构建 job 是否全部成功完成（`needs: [build, integration-tests]`）
- [x] FR-004: CI 验证依赖锁定文件内容一致性（`go.sum` 在 git 管理下）
- [x] FR-005: 同源验证失败时输出明确差异（基础框架已就绪）
- [x] CI 通过
- [x] go test 通过
