# Implementation Plan: 工具安装进度条优化

> **文档类型**：Implementation Plan
> **状态**：Ready
> **文档版本**：0.1.0
> **创建日期**：2026-07-26
> **最后更新**：2026-07-26
> **所有者**：xinyan <zuijiuhjd@foxmail.com>
> **关联 Spec**：`specs/008-install-progress-bar/spec.md` `0.2.0` (Approved)
> **权威范围**：定义所选技术方案、模块边界、接口、数据流和验证方案
> **替代文档**：无

**Branch**: `008-install-progress-bar` | **Date**: 2026-07-26 | **Spec**: `specs/008-install-progress-bar/spec.md` `0.2.0`

**Input**: Feature specification from `/specs/008-install-progress-bar/spec.md`

---

## Summary

为工具安装下载进度显示提供**进度条视觉化 + 速度/ETA 估算 + 批量安装整体进度**三项增强。在现有 `progressWriter`（`internal/downloader/downloader.go`）中增加移动平均速度计算与进度条输出，新增 `OnProgressEx` 回调携带速度/ETA 数据供 GUI 使用，批量安装时在 `Installer.InstallMany` 和 `Manager` 中传递 `[N/M]` 批量前缀。

---

## Technical Context

**Language/Version**: Go 1.25（与 `go.mod` 一致）
**Primary Dependencies**: 标准库 `fmt`/`strings`/`time`/`os`；`github.com/charmbracelet/x/term`（TTY 检测，已间接依赖）
**Testing**: `go test` 单元测试 + `go vet` 静态检查
**Target Platform**: Windows 10+ 主平台；Linux/macOS 终端兼容
**Project Type**: CLI/desktop tool
**Performance Goals**: 进度计算和渲染开销 < 1% CPU
**Constraints**: 不改下载器核心逻辑；管道模式退回纯文本；不新增外部依赖

---

## 技术方案

### 模块边界与接口

| 模块 | 职责 | 输入接口 | 输出接口 | 依赖模块 |
|---|---|---|---|---|
| `downloader.progressWriter` | 下载进度计算与渲染 | `Write(b []byte)` | stderr 进度条 / `OnProgress`/`OnProgressEx` 回调 | `util`、`term`、`os` |
| `downloader.Manager` | 批量下载任务管理 | `StartDownload()` / `OnProgress` 回调包装 | `Task.Speed`/`Task.ETA`/`Task.BatchIndex`/`Task.BatchTotal` | `downloader` |
| `installer.Installer` | 批量安装编排 | `InstallMany(items)` | 含 `[N/M]` 前缀的 `ProgressDesc` | `downloader` |

### 建议 API（实现时锁定）

```go
// downloader.Options 新增字段
OnProgressEx func(downloaded, total int64, speed int64, eta time.Duration)

// progressWriter 增强字段
type progressWriter struct {
    // ... 已有字段
    onProgressEx func(downloaded, total int64, speed int64, eta time.Duration)
    speed        int64         // 移动平均速度 bytes/sec
    eta          time.Duration // 剩余时间估算
    samples      []int64       // 最近 5 次采样 byte 增量
    lastBytes    int64         // 上次采样的 current 值
    isTTY        bool          // stderr 是否为终端
}

// downloader.Task 新增字段
BatchIndex  int `json:"batchIndex,omitempty"`
BatchTotal  int `json:"batchTotal,omitempty"`

// installer.InstallOptions 新增字段
BatchIndex int
BatchTotal int
```

### 速度计算算法

```
每 200ms 采样一次：
  1. delta = current - lastBytes
  2. samples 追加 delta，保持最多 5 个
  3. speed = sum(samples) / len(samples) * 5  (因为每个样本跨 200ms)
  4. ETA = (total - current) / speed  (秒)
```

### 进度条视觉格式

```
TTY 模式: \r  [====================] 100% 12.3 MB/s ETA 0s
          \r  [=========>          ]  45% 12.3 MB/s ETA 8s
          \r  [>                    ]   0%

非 TTY 模式: \r  desc  xxx / xxx  (xx%)
```

### 数据流

```text
Installer.InstallMany
  → InstallWithProgress(ctx, p, ver, callback, opts{BatchIndex, BatchTotal})
    → downloadWithProgress(ctx, tool, ver, arch, callback, batchIdx, batchTotal)
      → ProgressDesc 拼接 "[N/M] tool@ver"
      → downloader.Download(ctx, opts{..., OnProgress, OnProgressEx})
        → progressWriter.Write(b)
          → 采样 → 移动平均 speed → ETA
          → OnProgress / OnProgressEx 回调
          → 进度条输出至 stderr (TTY 检测分支)

Manager.runDownload
  → 包装 OnProgress/OnProgressEx → 更新 Task.Speed/ETA
  → 若 BatchTotal>0, ProgressDesc 加 "[N/M] " 前缀
```

### 失败处理

| 失败模式 | 检测方式 | 恢复策略 | 用户可见影响 |
|---|---|---|---|
| TTY 检测失败 | `term.IsTerminal` 返回 false | 退回纯文本格式 | 无进度条字符，纯文字百分比 |
| 速度采样不足（前 1s） | samples 长度 < 5 | 用已有样本计算，可能不准确 | 前几秒速度和 ETA 为空 |
| ETA 估算偏大/偏小 | speed 波动 | 每 200ms 重新计算，自然收敛 | ETA 值动态更新 |
| OnProgressEx 未设置 | nil 检查 | 跳过回调 | 无影响 |

---

## 兼容性与迁移

### 向后兼容性

- 是否改变已有 CLI 参数行为？否
- 是否改变已有配置文件格式？否
- 是否废弃已有接口？否（`OnProgress` 保留，新增 `OnProgressEx`）
- 管道模式保持纯文本输出

### 数据迁移

- 是否需要数据迁移？否

---

## 安全设计

- 认证/授权方式：不适用
- 敏感数据保护：不涉及
- 信任边界：不涉及

---

## 测试隔离

- **单元测试范围**：`progressWriter` speed/ETA 计算逻辑；TTY 分支
- **集成测试范围**：无（进度显示不影响功能正确性）
- **平台测试矩阵**：

| 平台 | 测试方式 | 覆盖范围 |
|---|---|---|
| Windows 10+ | 自动 | 编译 + go vet + go test |
| Linux (amd64) | 自动/CI | 编译检查 |
| macOS | 编译检查 | 编译检查 |

---

## Constitution 逐条检查

| Constitution 原则（1.0.0） | 检查结果 | 说明 | 引用 |
|---|---|---|---|
| I 安全与信任边界 | Not Applicable | 不涉及下载/签名 | — |
| II 测试必须隔离宿主状态 | Not Applicable | 不涉及宿主状态变更 | — |
| III Constitution 与 Spec 权威 | Compliant | Spec 0.2.0 Approved 为任务权威 | spec.md |
| IV 双重独立审查 | Compliant | 实施时执行 | 生命周期 |
| V Windows 主平台与跨平台分层 | Compliant | Windows 全量，其他需要编译检查 | 测试矩阵 |
| VI 向后兼容 | Compliant | 保持 `OnProgress`、非 TTY 纯文本 | — |
| VII 可复现构建 | Compliant | 无新不可复现依赖 | — |

### 检查结果汇总

- Compliant：4 项
- Not Applicable：3 项
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
- **兼容性验证**：`go vet ./internal/downloader/... ./internal/installer/...` 通过；`go test ./internal/downloader/...` 通过；管道模式（非 TTY）退回纯文本

---

## 项目结构

### 文档 (此功能专属)

```text
specs/008-install-progress-bar/
├── spec.md             # Feature Spec 0.2.0 (Approved)
├── plan.md             # 本文件 (Ready)
├── tasks.md            # 实施任务清单 (Completed)
└── reviews/            # 实施阶段审查记录
    ├── T001-spec-review.md
    └── T001-quality-review.md
```

### 源代码 (修改文件)

```text
internal/downloader/downloader.go  # progressWriter 增强
internal/downloader/manager.go      # Task 增加 BatchIndex/BatchTotal
internal/installer/installer.go     # InstallMany 传递批量信息
```

**Structure Decision**: 不新建文件，在现有代码中扩展。不改变核心下载逻辑。

---

## Complexity Tracking

> 无 Constitution 违规需辩护。

---

## 实施任务草图（待 Plan Ready 后细化为 tasks.md）

详见 `tasks.md`。

---

## 批准记录 (Approval History)

| Stage | Status | Approver | Date | Version | Scope |
|---|---|---|---|---|---|
| Plan 起草 | Planned | `xinyan <zuijiuhjd@foxmail.com>` | 2026-07-26 | `0.1.0` | `2026-07-26-plan-008-001` |
| Plan 批准 | Ready | `xinyan <zuijiuhjd@foxmail.com>` | 2026-07-26 | `0.1.0` | `2026-07-26-plan-008-001` 实施计划已就绪 |

---

## 变更历史

| 版本 | 日期 | 变更类型 | 说明 | 批准者 |
|---|---|---|---|---|
| 0.1.0 | 2026-07-26 | Initial Draft | 依据 Approved spec.md 0.2.0 生成正式 plan.md | — |
| 0.1.0 | 2026-07-26 | Ready | Planned→Ready；授权实施 | `xinyan <zuijiuhjd@foxmail.com>` |
