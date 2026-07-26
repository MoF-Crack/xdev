# Feature Specification: 下载源 Mirror 智能选择

> **文档类型**：Feature Spec
> **状态**：Approved
> **文档版本**：0.2.0
> **创建日期**：2026-07-26
> **最后更新**：2026-07-26
> **所有者**：xinyan <zuijiuhjd@foxmail.com>
> **权威范围**：定义下载源 mirror 智能选择策略的目标、范围、需求和验收标准
> **替代文档**：无

**Feature Branch**: `009-mirror-smart-selection`
**Created**: 2026-07-26
**Status**: Approved
**Input**: 用户需求"可用下载源 mirror 智能选择"

---

## Why

当前 `BuildDownloadOptions`（`internal/installer/installer.go:462`）的 mirror 选择逻辑是固定的：
1. 优先使用 manifest 中的 `mirror_url` 字段
2. 如果 `mirror_url` 为空，使用 official `url`
3. `util.ShouldApplyMirror` + `mir.Apply` 对 GitHub URL 做镜像重写作为 fallback
4. official URL 作为最终 fallback

问题：
- mirror_url 在 manifest 中是静态的，不会根据用户地理位置或网络状况动态选择
- 某些 mirror 已知 404（Spec 007 #3 中的 Haskell/Kotlin/Ninja/UV），但 manifest 仍优先使用
- 无 mirror 健康检查或延迟探测，始终按固定顺序尝试
- 用户无法配置偏好的 mirror 优先级
- 国内用户和国际用户的最佳 mirror 不同，但 manifest 只配了一个 mirror_url

受影响方：
- 国内用户：mirror_url 指向 TUNA 但可能对某些工具不适用（404）
- 国际用户：mirror_url 指向国内源但延迟更高
- 所有用户：mirror 404 时需要 fallback，浪费时间等待超时

不关闭此缺口：安装速度因 mirror 选择不佳而变慢，部分工具因 mirror 404 导致安装失败需重试。

## 目标

建立智能 mirror 选择机制：
1. 支持多个候选 mirror URL（manifest 中可配置多个，或运行时动态生成）
2. 基于延迟探测选择最快的可用 mirror
3. 用户可配置偏好 mirror 列表
4. mirror 404 时自动跳过并记录，后续安装不再尝试已知不可用的 mirror
5. 与现有 fallback 链兼容（mirror → rewritten → official）

## 范围

### 包含

- manifest 格式扩展：`mirror_urls`（复数）字段，支持多个候选 mirror
- mirror 健康缓存：记录最近 N 次 mirror 可用性结果，避免重复探测
- 延迟探测：HEAD 请求超时 < 3s 的 mirror 优先选择
- 用户偏好：`config.yaml` 中 `mirror_preference` 列表
- `BuildDownloadOptions` 增强：从候选列表中选择最佳 mirror
- mirror 不可用时自动降级到下一个候选或 official URL

### 非目标

- 修改下载器的核心下载逻辑
- 修改 SHA-256 校验逻辑
- 新增 mirror 源（只优化选择逻辑）
- CDN 智能调度（超出 xdev 范围）
- 修改 GUI 的下载管理器逻辑

---

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 多 mirror 候选自动选择最快 (Priority: P1)

manifest 配置了多个 mirror_url 时，xdev 自动探测延迟，选择最快的可用 mirror。

**Acceptance Scenarios**:
1. **Given** manifest 配置 `mirror_urls: [TUNA, USTC, npmmirror]`，**When** 用户执行安装，
    **Then** xdev 并发 HEAD 探测三个 mirror，选择延迟最低的作为 primary
2. **Given** TUNA 返回 404 但 USTC 可用，**When** 探测完成，**Then** 选择 USTC 作为 primary，
    TUNA 记录为不可用
3. **Given** 所有 mirror 不可用，**When** 探测完成，**Then** 降级到 official URL 并发出警告

### User Story 2 - mirror 不可用缓存 (Priority: P2)

xdev 记住已知不可用的 mirror，后续安装不再尝试。

**Acceptance Scenarios**:
1. **Given** TUNA 在上次安装中返回 404，**When** 再次安装同工具，**Then** 跳过 TUNA 直接尝试其他 mirror
2. **Given** mirror 不可用缓存超过 24 小时，**When** 再次安装，**Then** 重新探测（缓存过期）

### User Story 3 - 用户偏好 mirror 配置 (Priority: P3)

用户可在 `config.yaml` 中配置偏好 mirror 列表，覆盖 manifest 中的默认顺序。

**Acceptance Scenarios**:
1. **Given** config.yaml 配置 `mirror_preference: [npmmirror, TUNA]`，**When** 安装工具，
    **Then** 优先尝试 npmmirror，然后 TUNA，最后 manifest 中的其他 mirror
2. **Given** 用户未配置 mirror_preference，**When** 安装工具，**Then** 使用 manifest 默认顺序

---

## Requirements *(mandatory)*

### 功能需求 (Functional Requirements)

- **FR-001**：manifest 支持 `mirror_urls` 复数字段（向后兼容 `mirror_url` 单数）
- **FR-002**：`BuildDownloadOptions` 从候选 mirror 列表中选择最佳 mirror
- **FR-003**：并发 HEAD 探测候选 mirror（超时 3s），选择第一个可用或延迟最低的
- **FR-004**：mirror 不可用结果缓存到 `$XMDEV_HOME/cache/mirror-health.json`
- **FR-005**：缓存条目 24 小时过期，过期后重新探测
- **FR-006**：`config.yaml` 支持 `mirror_preference` 列表，优先于 manifest 默认顺序
- **FR-007**：所有 mirror 不可用时自动降级到 official URL，输出 WARNING
- **FR-008**：mirror 探测不阻塞 UI（CLI 显示 `Checking mirrors...`，GUI 异步探测）

### 非功能需求 (Non-Functional Requirements)

- **NFR-001**：性能 — mirror 探测并发执行，总耗时 < 3s（不串行等待每个 mirror）
- **NFR-002**：兼容性 — 现有 manifest 的 `mirror_url` 单数字段继续工作
- **NFR-003**：安全 — mirror 探测仅使用 HEAD 请求，不下载内容
- **NFR-004**：跨平台 — mirror 健康缓存文件格式跨平台一致（JSON）
- **NFR-005**：可维护性 — mirror 选择逻辑集中在一个函数中，不分散到多个包

---

## Success Criteria *(mandatory)*

- **SC-001**：配置了多 mirror 的工具安装时自动选择最快的可用 mirror
- **SC-002**：已知 404 的 mirror 在 24 小时内不再被尝试
- **SC-003**：用户配置 mirror_preference 后安装优先使用偏好 mirror
- **SC-004**：所有 mirror 不可用时安装仍能成功（降级到 official URL）

---

## 批准记录 (Approval History)

| 阶段 | 状态 | 批准者 | 日期 | 文档版本 | 批准范围 |
|---|---|---|---|---|---|
| Draft 创建 | Draft | `xinyan <zuijiuhjd@foxmail.com>` | 2026-07-26 | `0.1.0` | `2026-07-26-spec-009-draft-001` 创建初稿 |
| Spec 批准 | Approved | `xinyan <zuijiuhjd@foxmail.com>` | 2026-07-26 | `0.2.0` | `2026-07-26-spec-009-approved-001` 用户批准 Spec 内容 |

---

## 变更历史

| 版本 | 日期 | 变更类型 | 说明 | 批准者 |
|---|---|---|---|---|
| 0.1.0 | 2026-07-26 | Initial Draft | 用户需求"可用下载源 mirror 智能选择"创建初稿 | — |