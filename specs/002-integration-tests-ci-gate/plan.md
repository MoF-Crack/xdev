# Implementation Plan: integration tests CI 门禁

> **文档类型**：Implementation Plan
> **状态**：Ready
> **文档版本**：0.1.0
> **创建日期**：2026-07-25
> **最后更新**：2026-07-25
> **所有者**：xinyan <zuijiuhjd@foxmail.com>
> **关联 Spec**：`specs/002-integration-tests-ci-gate/spec.md` `0.2.0` (Accepted)
> **权威范围**：定义所选技术方案、模块边界、接口、数据流和验证方案

**Branch**: `002-integration-tests-ci-gate` | **Date**: 2026-07-25 | **Spec**: `specs/002-integration-tests-ci-gate/spec.md` `0.2.0`

---

## Summary

在 CI workflow 中新增独立的 `integration-tests` job，使用 matrix 策略在 Windows、Linux、macOS 三平台并行执行 `go test -tags=integration ./...`。设置 30 分钟超时上限，任一集成测试失败时 CI 标记为 failure。同时修复了跨平台 `SysProcAttr` 类型不匹配问题，确保集成测试在非 Windows 平台正确编译运行。

---

## Technical Context

**Language/Version**: Go 1.25
**Primary Dependencies**: Go build tag (`//go:build integration`)、GitHub Actions matrix strategy
**Storage**: 无
**Testing**: `go test -tags=integration ./... -v -count=1 -timeout 25m`
**Target Platform**: Windows/Linux/macOS（三平台并行）
**Constraints**: 不修改已有 CI job 行为；集成测试 job 与快速检查 job 并行运行

---

## 技术方案

### 模块边界

| 文件 | 职责 |
|---|---|
| `.github/workflows/ci.yaml` | 新增 `integration-tests` job 定义（matrix × 3 平台） |
| `internal/integration/client_other_test.go` | 修复跨平台 `SysProcAttr` 类型签名（`any` → `*syscall.SysProcAttr`） |

### 接口

**CI job**: `integration-tests`（与 `build` job 同级并行）
**命令**: `go test -tags=integration ./... -v -count=1 -timeout 25m`
**超时**: job 级别 `timeout-minutes: 30`
**矩阵**: `os: [ubuntu-latest, windows-latest, macos-latest]`

### 数据流

```
push/PR → CI triggered
  ├── build (matrix × 3 OS) — 单元测试 + lint
  └── integration-tests (matrix × 3 OS)
        → go test -tags=integration ./... -timeout 25m
        → 所有通过 → job success
        → 任一失败 → job failure → 阻塞 PR 合并（branch protection）
```

---

## 任务分解

| 任务 | 描述 | 进度 | 提交 |
|---|---|---|---|
| T001 | 在 CI workflow 中创建 `integration-tests` job，配置 matrix 三平台、30 分钟超时、`go test -tags=integration ./...` | Completed | `fd71d89` |
| T002 | 修复非 Windows 平台 `SysProcAttr` 类型签名（`any` → `*syscall.SysProcAttr`），确保集成测试跨平台编译通过 | Completed | `d68723d` |
| T003 | 更新 spec.md 批准记录并创建 `.specify/approvals/` 审批文件 | Completed | `fd71d89` |

---

## 验证方案

- **CI 自动验证**: 每次 push/PR 触发 `integration-tests` job 运行
- **跨平台验证**: matrix 三平台（ubuntu-latest、windows-latest、macos-latest）均执行集成测试
- **本地复现**: `go test -tags=integration ./... -v -count=1`

---

## 风险与缓解

| 风险类别 | 风险描述 | 严重度 | 缓解措施 |
|---|---|---|---|
| 安全 | 集成测试可能访问真实文件系统 | 中 | 约束集成测试使用临时目录 |
| 兼容性 | 新增 CI job 可能增加 CI 队列等待时间 | 低 | 与快速检查 job 并行执行 |
| 跨平台 | 平台特定测试可能在非目标平台失败 | 中 | 每个平台 runner 只执行其适用的集成测试 |

---

## 批准记录 (Approval History)

| 阶段 | 状态 | 批准者 | 日期 | 文档版本 | 批准范围 |
|---|---|---|---|---|---|
| Plan Ready | Ready | xinyan <zuijiuhjd@foxmail.com> | 2026-07-25 | 0.1.0 | 2026-07-25-plan-002-001 实施计划已就绪 | 实施计划已就绪 |
