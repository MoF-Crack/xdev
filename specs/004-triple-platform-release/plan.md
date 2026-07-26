# Implementation Plan: 三平台发布产物同源

> **文档类型**：Implementation Plan
> **状态**：Ready
> **文档版本**：0.1.0
> **创建日期**：2026-07-25
> **最后更新**：2026-07-25
> **所有者**：xinyan <zuijiuhjd@foxmail.com>
> **关联 Spec**：`specs/004-triple-platform-release/spec.md` `0.2.0` (Accepted)
> **权威范围**：定义所选技术方案、模块边界、接口、数据流和验证方案

**Branch**: `004-triple-platform-release` | **Date**: 2026-07-25 | **Spec**: `specs/004-triple-platform-release/spec.md` `0.2.0`

---

## Summary

通过 ldflags 在构建时注入 `GitCommit`（`${GITHUB_SHA}`）和 `BuildDate`（`$(date -u +%Y-%m-%dT%H:%M:%SZ)`）到 `internal/version` 包中，使每个发布二进制携带其构建来源的 Git commit SHA 和构建时间。所有 release job（release、release-macos、release-arm64）的构建命令均已统一使用包含 `GitCommit` 和 `BuildDate` 的 `-ldflags` 参数，为未来三平台产物同源验证（commit SHA 比对）提供数据基础。

---

## Technical Context

**Language/Version**: Go 1.25
**Primary Dependencies**: `go build -ldflags -X`、`internal/version` 包
**Storage**: 无
**Testing**: 构建后检查二进制中 version 信息
**Target Platform**: Windows/Linux/macOS
**Constraints**: 不修改已有平台构建 job 的内部逻辑；ldflags 注入必须跨平台一致

---

## 技术方案

### 模块边界

| 文件 | 职责 |
|---|---|
| `internal/version/version.go` | 新增 `GitCommit` 和 `BuildDate` 导出变量，供 ldflags 注入 |
| `.github/workflows/ci.yaml` | release job 构建命令中统一包含 `GitCommit` 和 `BuildDate` ldflags |

### 接口

**Go 变量**（`internal/version` 包）:
```go
var GitCommit = ""   // 通过 -ldflags -X 注入
var BuildDate = ""   // 通过 -ldflags -X 注入
```

**ldflags 模板**:
```
-ldflags="-s -w -X github.com/xmdemt/xdev/internal/version.GitCommit=${GITHUB_SHA} -X github.com/xmdemt/xdev/internal/version.BuildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
```

### 数据流

```
CI release job 触发
  → Git 检出（actions/checkout@v7）
  → 构建命令使用 ldflags 注入：
      GitCommit = ${GITHUB_SHA}（当前触发构建的 commit SHA）
      BuildDate = $(date -u +%Y-%m-%dT%H:%M:%SZ)（UTC 构建时间）
  → 生成的二进制内嵌了来源 commit 和构建时间
  → 未来可通过读取二进制提取 GitCommit 进行三平台比对
```

---

## 任务分解

| 任务 | 描述 | 进度 | 提交 |
|---|---|---|---|
| T001 | 在 `internal/version/version.go` 中添加 `GitCommit` 和 `BuildDate` 导出变量 | Completed | `444ec6f` |
| T002 | 在 release job 的 Linux/Windows 构建命令中添加 GitCommit 和 BuildDate ldflags | Completed | `444ec6f` |
| T003 | 在 release-macos job 的 DMG 构建命令中添加 GitCommit 和 BuildDate ldflags | Completed | `444ec6f` |
| T004 | 在 release-arm64 job 的 AppImage 构建命令中添加 GitCommit 和 BuildDate ldflags | Completed | `444ec6f` |
| T005 | 创建 Spec 004 审批文件 `.specify/approvals/2026-07-25-spec-004-001.json` | Completed | `444ec6f` |

---

## 验证方案

- **构建验证**: 触发 release workflow 后，下载二进制产物，使用 `strings` 或 `go tool nm` 检查是否包含 GitCommit SHA 和 BuildDate
- **本地验证**: 本地运行带 ldflags 的 `go build` 命令，确认 version 信息正确注入

---

## 风险与缓解

| 风险类别 | 风险描述 | 严重度 | 缓解措施 |
|---|---|---|---|
| 兼容性 | 同源验证可能阻塞合法的紧急 hotfix 单平台发布 | 中 | 紧急发布需通过安全例外流程临时豁免 |
| 跨平台 | CI runner 操作系统差异可能导致日期格式不同 | 低 | 统一使用 `date -u +%Y-%m-%dT%H:%M:%SZ`（UTC ISO 8601） |

---

## 批准记录 (Approval History)

| 阶段 | 状态 | 批准者 | 日期 | 文档版本 | 批准范围 |
|---|---|---|---|---|---|
| Plan Ready | Ready | xinyan <zuijiuhjd@foxmail.com> | 2026-07-25 | 0.1.0 | 2026-07-25-plan-004-001 实施计划已就绪 | 实施计划已就绪 |
