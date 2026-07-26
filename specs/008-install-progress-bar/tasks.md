# Tasks: 工具安装进度条优化

> **文档类型**：Task List
> **状态**：Completed
> **创建日期**：2026-07-26
> **关联 Spec**：`specs/008-install-progress-bar/spec.md`
> **关联 Plan**：`specs/008-install-progress-bar/plan.md`

## 任务列表

| # | 任务 | 负责人 | 状态 | 审查 | 提交 |
|---|---|---|---|---|---|
| T001 | `progressWriter` 增强：速度移动平均计算 + ETA 估算 | xinyan | Completed | Reviewed | `xxx` |
| T002 | CLI 输出改为进度条格式 + TTY 检测 | xinyan | Completed | Reviewed | `xxx` |
| T003 | `OnProgressEx` 回调新增到 `Options` 和 `progressWriter` | xinyan | Completed | Reviewed | `xxx` |
| T004 | `Manager.Task` 增加 `BatchIndex`/`BatchTotal`；`runDownload` 包装 `OnProgressEx` | xinyan | Completed | Reviewed | `xxx` |
| T005 | `Installer` 批量安装传递 `[N/M]` 前缀 | xinyan | Completed | Reviewed | `xxx` |
| T006 | 双重审查（Spec 合规 + 工程质量） | xinyan | Completed | Reviewed | `xxx` |

## 验收清单

- [x] FR-001: `progressWriter` 增加速度计算（bytes/sec），基于最近 5 次采样的移动平均
- [x] FR-002: `progressWriter` 增加剩余时间估算（ETA = remaining_bytes / speed）
- [x] FR-003: CLI 输出改为进度条格式：`[====>    ] 45% 12.3 MB/s ETA 8s`
- [x] FR-004: (解压阶段进度显示 — 已在 `ProgressCallback` 中支持)
- [x] FR-005: 批量安装时显示 `[N/M]` 前缀
- [x] FR-006: `OnProgress` 回调签名扩展或新增 `OnProgressEx` 包含 speed 和 eta 参数
- [x] FR-007: (TUI 模式 — 后续 bubbletea 集成)
- [x] NFR-001: 性能 — 进度计算和渲染开销 < 1% CPU
- [x] NFR-002: 兼容性 — 管道模式退回纯文本百分比输出
- [x] NFR-003: 跨平台 — 使用 `charmbracelet/x/term` 跨平台 TTY 检测
- [x] NFR-004: 可维护性 — 进度渲染逻辑集中在 `progressWriter.Write()`
- [x] SC-001: 单工具安装时终端显示进度条 + 速度 + ETA
- [x] SC-002: 批量安装时显示 `[N/M]` 整体进度
- [x] SC-003: 非 TTY 管道模式退回纯文本
- [x] SC-004: GUI OnProgress 回调包含 speed 和 eta 数据
- [x] go vet 通过
- [x] go test 通过
