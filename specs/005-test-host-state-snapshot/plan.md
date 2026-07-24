# Implementation Plan: 测试宿主状态快照

> **文档类型**：Implementation Plan
> **状态**：Ready
> **文档版本**：0.1.0
> **创建日期**：2026-07-23
> **最后更新**：2026-07-23
> **所有者**：xinyan <zuijiuhjd@foxmail.com>
> **关联 Spec**：`specs/005-test-host-state-snapshot/spec.md` `0.2.0` (Approved)
> **权威范围**：定义所选技术方案、模块边界、接口、数据流和验证方案
> **替代文档**：`docs/superpowers/plans/2026-07-23-test-host-state-snapshot.md`（执行任务清单同源）

**Branch**: `005-test-host-state-snapshot` | **Date**: 2026-07-23 | **Spec**: `specs/005-test-host-state-snapshot/spec.md` `0.2.0`

**Input**: Feature specification from `/specs/005-test-host-state-snapshot/spec.md`

---

## Summary

为 Go 测试套件提供**进程/套件级**宿主状态快照检测：在 `TestMain` 或共享 helper 中捕获基线、运行测试、再次捕获并 diff。默认覆盖 xdev 已知污染面（用户环境变量如 `GOROOT`/`GOPATH`/`JAVA_HOME`/`NODE_HOME`，Windows `HKCU\Environment`，用户级工具路径）。差异即失败。`--no-path-fix` 仍是第一道防线；本方案是第二道证明层。

---

## Technical Context

**Language/Version**: Go 1.23（与 `go.mod` 一致）
**Primary Dependencies**: 标准库 `testing`、`os`、`runtime`；Windows 注册表使用 `golang.org/x/sys/windows/registry`（若仓库已间接依赖则复用）
**Storage**: 仅测试临时目录中的瞬时 JSON/内存结构；不入库
**Testing**: `go test` 单元测试 + 集成探针
**Target Platform**: Windows 10+ 主平台；Linux/macOS 分层验证
**Project Type**: CLI/desktop tool test infrastructure
**Performance Goals**: 快照开销 ≤ 被测套件墙钟 5%
**Constraints**: 不改 CLI 默认行为；不写 `HKLM`；不自动修复污染
**Scale/Scope**: 套件级检测；非 `t.Parallel()` 细粒度

---

## 技术方案

### 模块边界与接口

| 模块 | 职责 | 输入接口 | 输出接口 | 依赖模块 |
|---|---|---|---|---|
| `internal/testkit/hostsnapshot` | 采集/比对宿主快照 | `Capture(opts) (Snapshot, error)`、`Diff(before, after) []Change` | `Snapshot`、`Change`、`Report` | `os`、`runtime`、Windows registry |
| `internal/testkit/hostsnapshot` Options | 默认白名单与排除列表 | 环境变量 / 结构体选项 | `Options` | 无 |
| 测试接入点 | `TestMain` 包装 | `RunWithHostSnapshot(m *testing.M) int` | 进程退出码 | hostsnapshot |
| CI 接入 | 默认 `go test` 路径启用 | 现有 CI 命令 | job 失败/成功 | 测试包 |

### 建议 API（实现时锁定）

```go
package hostsnapshot

type Options struct {
    EnvKeys             []string
    RegistryValues      []RegistryValue
    Paths               []string
    ExcludePathPrefixes []string
}

type Snapshot struct {
    Env      map[string]string
    Registry map[string]string
    Files    map[string]FileMeta
}

type Change struct {
    Kind   string // env|registry|file
    Key    string
    Before string
    After  string
}

func Capture(opts Options) (Snapshot, error)
func Diff(before, after Snapshot) []Change
func RunWithHostSnapshot(m *testing.M, opts Options) int
```

### 数据流

```text
TestMain / CI go test
  → Capture(before)
  → m.Run()
  → Capture(after)
  → Diff(before, after)
     → (empty) print "host state snapshot: no changes detected"; exit test code
     → (non-empty) print structured changes; exit non-zero (OR with test failures)
```

### 失败处理

| 失败模式 | 检测方式 | 恢复策略 | 用户可见影响 |
|---|---|---|---|
| 采集权限不足 | Capture 返回 error | 测试失败，不静默跳过 | 明确错误信息 |
| 发现宿主差异 | Diff 非空 | 失败并输出 Change 列表 | 阻塞合并/本地红 |
| 快照自干扰 | 路径命中 Exclude | 忽略排除前缀 | 无 |
| 关闭检测 | `XDEV_HOST_SNAPSHOT=0` | 跳过 Capture/Diff | 仅显式关闭；CI 不得设置 |

---

## 兼容性与迁移

### 向后兼容性

- 是否改变已有 CLI 参数行为？否
- 是否改变已有配置文件格式？否
- 是否废弃已有接口？否
- `--no-path-fix` 保持；实现阶段可另议拆分 `--no-env-write`，不作为本 Spec 验收阻塞项

### 数据迁移

- 是否需要数据迁移？否

---

## 安全设计

- 认证/授权方式：不适用
- 敏感数据保护：不采集 `keys/`、`.ssh`、token 文件内容；环境变量值仅针对白名单键
- 信任边界：只读宿主状态；不提升权限写 `HKLM`
- 输入验证：Options 路径规范化，拒绝全盘递归扫描

---

## 测试隔离

- **单元测试范围**：`Diff`、Options 默认值、排除前缀、跨平台分支
- **集成测试范围**：故意污染探针 + 干净集成套件回归
- **平台测试矩阵**：

| 平台 | 测试方式 | 覆盖范围 |
|---|---|---|
| Windows 10+ | 自动 | env + registry + paths 全路径 |
| Linux (amd64) | 自动/CI | env + paths |
| macOS 12+ | 编译检查 + 核心 env/paths | 分层证据 |

---

## Constitution 逐条检查

| Constitution 原则（1.0.0） | 检查结果 | 说明 | 引用 |
|---|---|---|---|
| I 安全与信任边界 | Not Applicable | 不涉及下载/签名 | — |
| II 测试必须隔离宿主状态 | Compliant | 直接提供可验证检测 | Spec FR/SC |
| III Constitution 与 Spec 权威 | Compliant | Spec 0.2.0 Approved 为任务权威 | spec.md |
| IV 双重独立审查 | Compliant | 实施时执行 | 生命周期 |
| V Windows 主平台与跨平台分层 | Compliant | Windows 全量，其他分层 | 测试矩阵 |
| VI 向后兼容 | Compliant | 不改默认 CLI 语义 | — |
| VII 可复现构建 | Compliant | 无新不可复现依赖；产物不入库 | — |

### 检查结果汇总

- Compliant：6 项
- Not Applicable：1 项
- Exception Required：0 项
- ADR Required：0 项
- Non-compliant：0 项

---

## ADR 与安全例外判断

### 需要创建的 ADR

无。

### 需要的安全例外

无。

---

## 验证方案

- **Spec 合规验证**：FR-001..007、SC-001..004 均映射到实现任务与测试
- **Constitution 合规验证**：无 Non-compliant
- **兼容性验证**：现有 `go test ./internal/...` 与 integration 套件不因假阳性失败
- **跨平台验证**：Windows 完整；Linux/macOS 至少编译 + env/path 单元测试

---

## 项目结构

### 文档 (此功能专属)

```text
specs/005-test-host-state-snapshot/
├── spec.md             # Feature Spec 0.2.0 (Approved)
├── plan.md             # 本文件 (Planned，待 Gate P → Ready)
└── reviews/            # 实施阶段审查记录（未开始）
```

### 源代码 (实现阶段)

```text
internal/testkit/hostsnapshot/
├── hostsnapshot.go
├── options.go
├── hostsnapshot_test.go
├── registry_windows.go
└── registry_stub.go

internal/integration/
└── (TestMain 接入 / 探针)
```

**Structure Decision**: 新建独立 `internal/testkit/hostsnapshot` 包，避免污染生产 `installer`/`env` 包；测试通过 `TestMain` 接入。

---

## Complexity Tracking

> 无 Constitution 违规需辩护。

---

## 实施任务草图（待 Plan Ready 后细化为 tasks.md）

详见 `docs/superpowers/plans/2026-07-23-test-host-state-snapshot.md` Task 1–6（TDD）。

---

## 批准记录 (Approval History)

| Stage | Status | Approver | Date | Version | Scope |
|---|---|---|---|---|---|
| Plan 起草 | Planned | `xinyan <zuijiuhjd@foxmail.com>` | 2026-07-23 | `0.1.0` | `2026-07-23-plan-005-planned-001` |
| Plan 批准 | Ready | `xinyan <zuijiuhjd@foxmail.com>` | 2026-07-23 | `0.1.0` | `2026-07-23-plan-005-ready-001`；用户原文「同意方案并且实施」；批准技术方案并授权 TDD 实现 |

---

## 变更历史

| 版本 | 日期 | 变更类型 | 说明 | 批准者 |
|---|---|---|---|---|
| 0.1.0 | 2026-07-23 | Initial Draft | 依据 Approved spec.md 0.2.0 生成正式 plan.md | — |
| 0.1.0 | 2026-07-23 | Ready | Planned→Ready；授权实施 | `xinyan <zuijiuhjd@foxmail.com>` |
