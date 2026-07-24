# 审查记录：T001-Spec 合规审查

> **文档类型**：审查记录
> **状态**：Accepted
> **文档版本**：`0.1.0`
> **创建日期**：2026-07-23
> **最后更新**：2026-07-23
> **关联功能**：`specs/005-test-host-state-snapshot/`
> **审查者**：Independent Spec Reviewer (not implementer)
> **权威范围**：本记录的审查结论、发现问题和修复验证证据

## 审查对象

| 字段 | 值 |
|---|---|
| 任务编号 | T001–T005（hostsnapshot 实现） |
| 审查类型 | Spec 合规审查 |
| 审查范围 | `internal/testkit/hostsnapshot/*`、`internal/integration/main_test.go`、commit `cac1be9` |
| 关联 Spec 版本 | `0.2.0` |
| 关联 Plan 版本 | `0.1.0` Ready |

## 逐条判定

| 审查项 | 判定 | 证据与说明 |
|---|---|---|
| FR-001 采集范围 | 通过 | `Capture` 覆盖用户持久 env（Windows `GetUserEnvVar`）与路径白名单 |
| FR-002 前后比对 | 通过 | `RunWithHostSnapshot` before/after `Capture` + `Diff` |
| FR-003 失败输出 | 通过 | 差异时打印 kind/key/before/after |
| FR-004 CI 启用 | 有条件通过 | 默认 `go test` 包 TestMain 启用；workflow 显式注释在后续任务补齐 |
| FR-005 跨平台 | 通过 | Windows 完整路径；非 Windows 走 process/user env 抽象 |
| FR-006 默认开启 | 通过 | 仅 `XDEV_HOST_SNAPSHOT=0` 关闭 |
| FR-007 可配置白名单 | 通过 | `Options` + `DefaultOptions` 基于 `AllowedEnvVarNames` |
| SC-001 泄漏探针 | 通过 | `TestCapture_DetectsUserEnvWrite`（Windows） |
| SC-002 干净套件 | 通过 | hostsnapshot 包 TestMain 输出 no changes；env/installer/cli/util 测试绿 |
| 未超范围 | 通过 | 未改 CLI 默认语义；未自动修复污染 |

## 验证证据

```text
go test ./internal/testkit/hostsnapshot -count=1
ok  ... host state snapshot: no changes detected

go test ./internal/env ./internal/installer ./internal/cli ./internal/util -count=1
ok
```

## 阻塞问题

无

## 非阻塞问题

| 编号 | 问题描述 | 严重程度 | 建议修复方式 |
|---|---|---|---|
| N1 | `env`/`util` 不能挂 hostsnapshot TestMain（import cycle） | 低 | 保留 installer/cli/testkit/integration 接入 |
| N2 | ci.yaml 需显式说明不设 `XDEV_HOST_SNAPSHOT=0` | 低 | 本轮补注释 |

## 修复与复审历史

无

## 最终结论

- **审查结论**：通过
- **结论日期**：2026-07-23
- **审查者**：Independent Spec Reviewer (not implementer)
