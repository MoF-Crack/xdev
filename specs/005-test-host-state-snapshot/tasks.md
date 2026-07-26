# Tasks: 测试宿主状态快照

> **文档类型**：Task List
> **状态**：Completed
> **创建日期**：2026-07-23
> **关联 Spec**：`specs/005-test-host-state-snapshot/spec.md`
> **关联 Plan**：`specs/005-test-host-state-snapshot/plan.md`

## 任务列表

| # | 任务 | 负责人 | 状态 | 审查 | 提交 |
|---|---|---|---|---|---|
| T001 | 实现 `internal/testkit/hostsnapshot` 包（Capture/Diff/RunWithHostSnapshot） | xinyan | Completed | Reviewed | `cac1be9` |
| T002 | 接入 testkit TestMain（env/installer/cli/integration） | xinyan | Completed | Reviewed | `43193ee` |
| T003 | CI workflow 注释 FR-004（不在 CI 中禁用快照） | xinyan | Completed | Reviewed | `43193ee` |
| T004 | SC-004 性能优化：批量注册表读取 `GetUserEnvVarsBatch` | xinyan | Completed | Reviewed | `2fb8c66` |
| T005 | 双重审查（Spec 合规 + 工程质量） | xinyan | Completed | Reviewed | `43193ee` |

## 验收清单

- [x] FR-001: Capture 覆盖用户持久 env + 路径白名单
- [x] FR-002: RunWithHostSnapshot before/after Capture + Diff
- [x] FR-003: 差异时打印 kind/key/before/after
- [x] FR-004: CI 默认启用快照（workflow 注释说明）
- [x] FR-005: 跨平台支持（Windows 注册表 + 非 Windows process env）
- [x] FR-006: 默认开启，仅 `XDEV_HOST_SNAPSHOT=0` 关闭
- [x] FR-007: Options + DefaultOptions 基于 AllowedEnvVarNames
- [x] SC-001: 泄漏探针测试通过
- [x] SC-002: 干净套件无差异
- [x] SC-003: CI 默认路径泄漏导致 job 失败
- [x] SC-004: 开销 ≤5%（优化后 2.1%）
- [x] CI 通过
- [x] go test 通过