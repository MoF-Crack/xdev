# Implementation Plan: 下载源 Mirror 智能选择

> **文档类型**：Implementation Plan
> **状态**：Ready
> **文档版本**：0.1.0
> **创建日期**：2026-07-26
> **最后更新**：2026-07-26
> **所有者**：xinyan <zuijiuhjd@foxmail.com>
> **关联 Spec**：`specs/009-mirror-smart-selection/spec.md` `0.2.0` (Approved)
> **权威范围**：定义所选技术方案、模块边界、接口、数据流和验证方案
> **替代文档**：无

**Branch**: `009-mirror-smart-selection` | **Date**: 2026-07-26 | **Spec**: `specs/009-mirror-smart-selection/spec.md` `0.2.0`

**Input**: Feature specification from `/specs/009-mirror-smart-selection/spec.md`

---

## Summary

为 `BuildDownloadOptions` 提供**智能 mirror 选择**：manifest 支持 `mirror_urls` 复数字段（向后兼容 `mirror_url`），并发 HEAD 探测候选 mirror 延迟，缓存已知不可用 mirror 24h，用户可通过 `config.yaml` 配置 `mirror_preference` 列表。所有 mirror 不可用时自动降级到 official URL。

---

## Technical Context

**Language/Version**: Go 1.23（与 `go.mod` 一致）
**Primary Dependencies**: 标准库 `net/http`、`time`、`encoding/json`、`sync`
**Storage**: `$XMDEV_HOME/cache/mirror-health.json`（JSON 格式，跨平台一致）
**Testing**: `go test` 单元测试 + 现有套件回归
**Target Platform**: Windows 10+ 主平台；Linux/macOS 编译检查
**Project Type**: CLI/desktop tool
**Performance Goals**: mirror 探测并发执行，总耗时 < 3s
**Constraints**: 不改下载器核心逻辑；不改 SHA-256 校验；不新增 mirror 源
**Scale/Scope**: 包级增强；不引入新外部依赖

---

## 技术方案

### 模块边界与接口

| 模块 | 职责 | 输入接口 | 输出接口 | 依赖模块 |
|---|---|---|---|---|
| `internal/installer/mirror_selector` | mirror 健康缓存管理 + 并发延迟探测 | `LoadMirrorHealthCache(cacheDir)`、`SelectBestMirror(candidates, cache, timeout)` | `MirrorHealthCache`、`(string, error)` | `net/http`、`time`、`encoding/json` |
| `internal/manifest` Archive | 提供有效 mirror URL 列表 | `EffectiveMirrorURLs() []string` | `[]string` | 无 |
| `internal/installer` Installer | 构建下载选项，调用 SelectBestMirror | `BuildDownloadOptions(tool, ver, archive)` | `(string, []string, string)` | mirror_selector、manifest、config |
| `internal/config` CoreConfig | 存储用户 mirror 偏好 | `MirrorPreference []string` | `yaml` 序列化 | 无 |

### 建议 API（实现时锁定）

```go
// internal/installer/mirror_selector.go

type MirrorHealth struct {
    URL         string    `json:"url"`
    Available   bool      `json:"available"`
    LastChecked time.Time `json:"last_checked"`
    LatencyMs   int64     `json:"latency_ms"`
}

type MirrorHealthCache struct {
    entries map[string]MirrorHealth
    path    string
    mu      sync.Mutex
}

func LoadMirrorHealthCache(cacheDir string) *MirrorHealthCache
func (c *MirrorHealthCache) IsAvailable(url string) bool
func (c *MirrorHealthCache) RecordResult(url string, available bool, latencyMs int64)
func (c *MirrorHealthCache) Save() error
func (c *MirrorHealthCache) IsExpired(url string) bool
func SelectBestMirror(candidates []string, cache *MirrorHealthCache, timeout time.Duration) (string, error)
```

### 数据流

```text
BuildDownloadOptions(tool, ver, archive)
  → Build candidate list:
     1. config.MirrorPreference (user-configured preferred mirrors)
     2. archive.EffectiveMirrorURLs() (manifest mirror_urls + mirror_url)
     3. if util.ShouldApplyMirror: i.mir.Apply(tool, url)
     4. official URL (always last)
  → Filter candidates: remove known-unavailable from health cache
  → If only 1 candidate remains: use it directly
  → If multiple candidates remain:
      SelectBestMirror(candidates, cache, 3s)
        → Concurrent HEAD probes (goroutine per candidate)
        → First 2xx/3xx response → cancel others, return this URL
        → If none 2xx/3xx → return lowest latency (or all failed → error)
  → Record probe results to health cache
  → Save health cache to disk
  → Return (primaryURL, fallbackURLs, cacheDestPath)
```

### 失败处理

| 失败模式 | 检测方式 | 恢复策略 | 用户可见影响 |
|---|---|---|---|
| 所有 mirror 不可用 | SelectBestMirror 返回 error | 降级到 official URL，输出 WARNING | 安装仍成功，速度可能较慢 |
| 健康缓存文件损坏 | JSON 解析失败 | 清空缓存重新探测 | 无（静默重建） |
| 健康缓存写入失败 | Save 返回 error | 仅 log WARNING，不阻止安装 | 无 |
| HEAD 探测全部超时 | 3s 后无响应 | 降级到下一个候选 | 安装稍慢（等待 3s） |
| 并发探测 goroutine 泄漏 | context cancellation | 通过 context.WithTimeout 确保清理 | 无 |

---

## 兼容性与迁移

### 向后兼容性

- 是否改变已有 CLI 参数行为？否
- 是否改变已有配置文件格式？是 — 新增可选 `mirror_preference` 字段
- 是否废弃已有接口？否 — `mirror_url` 继续工作
- `mirror_urls` 为空时自动回退到 `mirror_url`（单数），现有 manifest 无需修改

### 数据迁移

- 是否需要数据迁移？否
- 健康缓存文件首次使用时自动创建

---

## 安全设计

- 认证/授权方式：不适用
- 敏感数据保护：不采集；mirror URL 为公开信息
- 信任边界：仅 HEAD 请求探测；不下载内容（NFR-003）
- 输入验证：URL 列表来自 manifest（已签名）或用户 config；探测前不做额外验证

---

## 测试隔离

- **单元测试范围**：`MirrorHealthCache` 全部方法、`EffectiveMirrorURLs`、候选列表构建
- **集成测试范围**：`BuildDownloadOptions` 增强后的行为（无需真实网络，通过 mock HTTP server）
- **平台测试矩阵**：

| 平台 | 测试方式 | 覆盖范围 |
|---|---|---|
| Windows 10+ | 自动 | 全路径：manifest + config + cache |
| Linux (amd64) | 编译检查 + 单元测试 | config + manifest 加载 |
| macOS 12+ | 编译检查 + 核心单元 | config 解析 |

---

## Constitution 逐条检查

| Constitution 原则（1.0.0） | 检查结果 | 说明 | 引用 |
|---|---|---|---|
| I 安全与信任边界 | Compliant | 仅 HEAD 请求，不下载内容 | NFR-003 |
| II 测试必须隔离宿主状态 | Compliant | 不写注册表；cache 在可控路径 | — |
| III Constitution 与 Spec 权威 | Compliant | Spec 0.2.0 Approved 为任务权威 | spec.md |
| IV 双重独立审查 | Compliant | 实施时执行 | 生命周期 |
| V Windows 主平台与跨平台分层 | Compliant | Windows 全量，其他编译+单元 | 测试矩阵 |
| VI 向后兼容 | Compliant | mirror_url 单数继续工作 | FR-001 / NFR-002 |
| VII 可复现构建 | Compliant | 无新不可复现依赖 | — |

### 检查结果汇总

- Compliant：7 项
- Not Applicable：0 项
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

- **Spec 合规验证**：FR-001..008、SC-001..004 均映射到实现任务与测试
- **Constitution 合规验证**：无 Non-compliant
- **兼容性验证**：现有 `go test ./internal/...` 不因改动失败
- **跨平台验证**：Windows 完整；Linux/macOS 至少编译通过

---

## 项目结构

### 文档 (此功能专属)

```text
specs/009-mirror-smart-selection/
├── spec.md             # Feature Spec 0.2.0 (Approved)
├── plan.md             # 本文件 (Ready)
├── tasks.md            # 任务清单
└── reviews/            # 实施阶段审查记录
    ├── T001-spec-review.md
    └── T001-quality-review.md
```

### 源代码 (实现阶段)

```text
internal/manifest/manifest.go       # Archive.MirrorURLs + EffectiveMirrorURLs()
internal/installer/mirror_selector.go # MirrorHealthCache + SelectBestMirror (新文件)
internal/installer/installer.go      # BuildDownloadOptions 增强
internal/config/config.go            # CoreConfig.MirrorPreference
```

---

## Complexity Tracking

> 无 Constitution 违规需辩护。

---

## 实施任务草图

详见 `tasks.md`。

---

## 批准记录 (Approval History)

| Stage | Status | Approver | Date | Version | Scope |
|---|---|---|---|---|---|
| Plan 起草 | Ready | `xinyan <zuijiuhjd@foxmail.com>` | 2026-07-26 | `0.1.0` | `2026-07-26-plan-009-001` 实施计划已就绪 |

---

## 变更历史

| 版本 | 日期 | 变更类型 | 说明 | 批准者 |
|---|---|---|---|---|
| 0.1.0 | 2026-07-26 | Initial Draft | 依据 Approved spec.md 0.2.0 生成正式 plan.md | — |
