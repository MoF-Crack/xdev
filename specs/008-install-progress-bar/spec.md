# Feature Specification: 工具安装进度条优化

> **文档类型**：Feature Spec
> **状态**：Approved
> **文档版本**：0.2.0
> **创建日期**：2026-07-26
> **最后更新**：2026-07-26
> **所有者**：xinyan <zuijiuhjd@foxmail.com>
> **权威范围**：定义工具安装下载进度显示优化的目标、范围、需求和验收标准
> **替代文档**：无
> **Constitution**：`1.0.0`
**Feature Branch**: `008-install-progress-bar`
**Created**: 2026-07-26
**Status**: Approved
**Input**: 用户需求"工具安装进度条优化"

---

## Why

当前工具安装下载进度通过 `progressWriter`（`internal/downloader/downloader.go`）以纯文本方式
输出到 stderr，每 200ms 刷新一次，格式为 `\r  desc  current / total  (xx%)`。

存在的问题：
- 进度信息无视觉层级（下载进度、解压进度、安装进度混在一起）
- 批量安装时无法看到整体进度（只显示单文件进度）
- 无下载速度和剩余时间估算
- GUI 模式的 `OnProgress` 回调已存在但未充分利用
- 无进度条视觉元素（纯文字百分比，非进度条图形）

受影响方：
- 所有通过 CLI/TUI 安装工具的用户（进度可读性差）
- GUI 安装界面（进度数据已有但展示不充分）
- 批量安装场景（缺少整体进度概览）

不关闭此缺口：安装体验差影响用户首次使用印象，批量安装时用户无法判断还需多久。

## 目标

优化工具安装的进度显示：
1. 下载阶段：显示进度条 + 速度 + 剩余时间
2. 解压阶段：显示解压进度
3. 安装阶段：显示安装步骤
4. 批量安装：显示整体进度（当前/总数 + 当前工具进度）

## 范围

### 包含

- `progressWriter` 增强：速度计算、剩余时间估算
- CLI 进度条视觉化（`\r` + `[====>    ]` 风格）
- 批量安装整体进度（`installer.InstallBatch` 或等效）
- GUI OnProgress 回调数据增强（速度、剩余时间）
- TUI 进度展示优化（利用 bubbletea 能力）

### 非目标

- 修改下载器的核心下载逻辑（断点续传、SHA 校验等）
- 修改安装路径选择逻辑
- 新增安装功能（只优化已有安装的展示）
- 修改 manifest 格式

---

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 单工具安装进度条 (Priority: P1)

用户执行 `xdev install python` 时，终端显示可视化进度条，包含下载速度和剩余时间。

**Acceptance Scenarios**:
1. **Given** 用户执行 `xdev install python`，**When** 下载开始，**Then** 终端显示 `[====>      ] 45% 12.3 MB/s ETA 8s`
2. **Given** 下载完成进入解压，**When** 解压进行中，**Then** 显示 `Extracting... [======>    ] 70%`
3. **Given** 解压完成进入安装，**When** 安装链接创建中，**Then** 显示 `Installing... ✓ Done`

### User Story 2 - 批量安装整体进度 (Priority: P2)

用户执行 `xdev install python go node` 时，显示整体进度（3 个工具中的第 2 个）+ 当前工具进度。

**Acceptance Scenarios**:
1. **Given** 批量安装 3 个工具，**When** 第 2 个工具下载中，**Then** 显示 `[2/3] Installing go... [====>  ] 60% 8.5 MB/s`
2. **Given** 第 2 个工具安装完成，**When** 切换到第 3 个，**Then** 显示 `[3/3] Installing node... Starting download`

### User Story 3 - GUI 进度数据增强 (Priority: P3)

GUI 安装界面的进度回调包含速度和剩余时间数据。

**Acceptance Scenarios**:
1. **Given** GUI 安装工具中，**When** OnProgress 回调被调用，**Then** 回调参数包含 downloaded、total、speed、eta
2. **Given** GUI 显示进度条，**When** 下载速度变化，**Then** 速度和 ETA 实时更新

---

## Requirements *(mandatory)*

### 功能需求 (Functional Requirements)

- **FR-001**：`progressWriter` 增加速度计算（bytes/sec），基于最近 N 次采样的移动平均
- **FR-002**：`progressWriter` 增加剩余时间估算（ETA = remaining_bytes / speed）
- **FR-003**：CLI 输出改为进度条格式：`[====>    ] 45% 12.3 MB/s ETA 8s`
- **FR-004**：解压阶段显示进度（基于已解压字节数 / 总字节数）
- **FR-005**：批量安装时显示 `[N/M]` 前缀标识当前工具序号和总数
- **FR-006**：`OnProgress` 回调签名扩展或新增回调包含 speed 和 eta 参数
- **FR-007**：TUI 模式利用 bubbletea 的 progress 组件显示进度

### 非功能需求 (Non-Functional Requirements)

- **NFR-001**：性能 — 进度计算和渲染开销 < 1% CPU，不阻塞下载
- **NFR-002**：兼容性 — 管道模式下（非 TTY）退回纯文本百分比输出
- **NFR-003**：跨平台 — 进度条字符在 Windows/Linux/macOS 终端一致显示
- **NFR-004**：可维护性 — 进度渲染逻辑集中，不在多处重复

---

## Success Criteria *(mandatory)*

- **SC-001**：单工具安装时终端显示进度条 + 速度 + ETA
- **SC-002**：批量安装时显示 `[N/M]` 整体进度
- **SC-003**：非 TTY 管道模式退回纯文本，不输出进度条字符
- **SC-004**：GUI OnProgress 回调包含 speed 和 eta 数据

---

## 批准记录 (Approval History)

| 阶段 | 状态 | 批准者 | 日期 | 文档版本 | 批准范围 |
|---|---|---|---|---|---|
| Draft 创建 | Draft | `xinyan <zuijiuhjd@foxmail.com>` | 2026-07-26 | `0.1.0` | `2026-07-26-spec-008-draft-001` 创建初稿 |
| Spec 批准 | Approved | `xinyan <zuijiuhjd@foxmail.com>` | 2026-07-26 | `0.2.0` | `2026-07-26-spec-008-approved-001` 用户批准 Spec 内容 |

---

## 变更历史

| 版本 | 日期 | 变更类型 | 说明 | 批准者 |
|---|---|---|---|---|
| 0.1.0 | 2026-07-26 | Initial Draft | 用户需求"工具安装进度条优化"创建初稿 | — |