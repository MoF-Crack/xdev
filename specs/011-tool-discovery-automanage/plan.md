# Implementation Plan: 所有支持工具的新版本嗅探发现及自动纳入管理范围

> **文档类型**：Implementation Plan
> **状态**：Ready
> **文档版本**：0.1.0
> **创建日期**：2026-07-26
> **最后更新**：2026-07-26
> **所有者**：xinyan <zuijiuhjd@foxmail.com>
> **关联 Spec**：`specs/011-tool-discovery-automanage/spec.md` `0.2.0` (Approved)
> **权威范围**：定义所选技术方案、模块边界、接口、数据流和验证方案
> **替代文档**：无

**Branch**: `011-tool-discovery-automanage` | **Date**: 2026-07-26 | **Spec**: `specs/011-tool-discovery-automanage/spec.md` `0.2.0`

**Input**: Feature specification from `/specs/011-tool-discovery-automanage/spec.md`

---

## Summary

新增 `internal/discovery/` 包：固定 4 种内置 Sniffer（GitHub/JSON/HTML/ArchivePattern），按**用户可选的 4 种触发策略**（interval/startup/service/manual）调度嗅探；发现的新版本写入 manifest（标 `discovered:true, unverified:true`，带占位 archive 复用现有"全零跳过"校验），让发现的版本"看得见也可装"；CLI/GUI 显示新版本可用徽标；`service` 策略安装系统定时任务（Win Task Scheduler / Linux cron / macOS launchd）。**本地已装工具扫描迁移复用现有 `internal/env/scan_local.go`，不在此 spec 实现**；Sniffer 不可外部扩展。

---

## Technical Context

**Language/Version**: Go 1.25.0（与 `go.mod` 一致）
**Primary Dependencies**: 标准库 `net/http`、`encoding/json`、`os/exec`、`time`、`sync`；复用现有 `internal/remote`（DiscoverVersions）、`internal/manifest`、`internal/util`、`internal/config`
**Storage**:
- `$XMDEV_HOME/cache/sniff-cache.json`（嗅探结果缓存，JSON 跨平台一致）
- `$XMDEV_HOME/manifests/<tool>.yaml`（合并发现的版本，原子写）
- `config.yaml` 的 `sniff:` 段（strategy/interval/concurrency/rate_limit_per_hour/auto_archive_fill/last_sniff_at）
- 系统服务：Win Task Scheduler 任务、Linux cron 条目、macOS launchd plist
**Testing**: `go test` 单元测试（mock HTTP server，httptest）+ 现有套件回归
**Target Platform**: Windows 10+ 主平台；Linux/macOS 编译检查 + cron/launchd 单元
**Project Type**: CLI/desktop tool（Wails GUI）
**Performance Goals**: 单工具嗅探 < 5s；全量 38 工具并发 3 路 < 60s；`interval`/`startup` 启动开销 < 50ms；`manual`/`service` 启动 < 10ms
**Constraints**: 不修改下载器/SHA-256 校验；Sniffer 固定 4 种内置不可扩展；本地扫描迁移复用 `internal/env`；现有 `xdev refresh` 向后兼容
**Scale/Scope**: 新增 `internal/discovery/` 包 + `internal/manifest` 字段扩展 + CLI `sniff`/`updates`/`sniff service` 命令 + GUI 徽标/策略 UI

---

## 技术方案

### 模块边界与接口

| 模块 | 职责 | 输入接口 | 输出接口 | 依赖模块 |
|---|---|---|---|---|
| `internal/discovery/sniffer.go` | Sniffer 接口 + SnifferRegistry（固定内置，无开放注册） | `Sniffer` 接口 | `[]DiscoveredVersion` | `manifest`、`remote` |
| `internal/discovery/sources/*.go` | 4 种内置 Sniffer 实现 | `*manifest.Tool` | `[]DiscoveredVersion`、error | `sniffer.go`、`remote`、`net/http` |
| `internal/discovery/scheduler.go` | 触发策略调度（按 strategy）+ 令牌桶限流 + cache 读写 | `Scheduler.OnStartup()`、`SniffNow()` | error、`*SniffCache` | `sniffer.go`、`config` |
| `internal/discovery/service_install.go` | 系统服务安装/卸载/状态 | `ServiceInstaller` 接口 | error、`*ServiceStatus` | `os/exec` |
| `internal/discovery/archive_filler.go` | 从已有 version 推导 archive URL 模板，生成占位 archive | `*manifest.Tool`、新 version tag | `[]manifest.Archive` | `manifest` |
| `internal/manifest/manifest.go` | Tool/Version 字段扩展 | `AutoSniff`/`Discovered`/`Unverified` 字段 | YAML 序列化兼容 | 无 |
| `internal/cli/refresh.go` | 重构为调 Scheduler.SniffNow，向后兼容 | CLI args | 调用 `discovery` | `discovery`、`config` |
| `internal/cli/sniff_cmd.go` | 新增 `xdev sniff`/`updates`/`sniff service` 命令 | CLI flags | 调用 `discovery` | `discovery`、`config` |
| `internal/cli/list.go`/`doctor.go` | 输出 `↑latest` 徽标 | 读 `sniff-cache.json` | 表格输出 | `discovery` |
| `internal/gui/` | 徽标 + 通知中心 + 策略 UI | Wails bindings | 调用 `discovery` | `discovery` |

### 建议 API（实现时锁定）

```go
// internal/discovery/sniffer.go
package discovery

type DiscoveredVersion struct {
    Version      string             `json:"version"`
    ReleaseDate  string             `json:"release_date"`
    Archives     []manifest.Archive `json:"archives"`
    Source       string             `json:"source"`
    Unverified   bool               `json:"unverified"`
}

type Sniffer interface {
    Name() string
    Supports(t *manifest.Tool) bool
    Sniff(ctx context.Context, t *manifest.Tool) ([]DiscoveredVersion, error)
}

// SnifferRegistry 固定内置 4 种，包内初始化，无开放 Register
type SnifferRegistry struct { /* ordered list */ }
func newSnifferRegistry() *SnifferRegistry  // 包内初始化 github/json/html/archivePattern
func (r *SnifferRegistry) Sniff(ctx, t) ([]DiscoveredVersion, error)

type SniffCache struct {
    Tools       map[string]ToolSniffResult `json:"tools"`
    RateLimit   RateLimitState             `json:"rate_limit"`
    LastSniffAt time.Time                  `json:"last_sniff_at"`
    Partial     bool                       `json:"partial"`
}
type ToolSniffResult struct {
    LastSniff  time.Time   `json:"last_sniff"`
    Etag       string      `json:"etag"`
    Versions   []string    `json:"versions"`
    Unsupported bool        `json:"unsupported"`
    Error      string      `json:"error,omitempty"`
}
type RateLimitState struct {
    Remaining int       `json:"remaining"`
    ResetAt   time.Time `json:"reset_at"`
}

// internal/discovery/scheduler.go
type Strategy string // interval|startup|service|manual

type Scheduler struct {
    registry    *SnifferRegistry
    cfg         *config.SniffConfig
    cachePath   string
    tokenBucket *rateLimiter
}
func NewScheduler(cfg *config.SniffConfig, cacheDir string) *Scheduler
func (s *Scheduler) OnStartup(ctx context.Context)              // 按 strategy 决定是否触发
func (s *Scheduler) SniffNow(ctx context.Context, tool string) error  // 手动/headless 触发
func (s *Scheduler) SniffAll(ctx context.Context) error
func (s *Scheduler) LoadCache() (*SniffCache, error)
func (s *Scheduler) SaveCache(c *SniffCache) error             // 原子写

// internal/discovery/sources/archive_pattern_sniffer.go
func extractGitHubRepoFromURL(url string) (owner, repo string, ok bool)
func buildArchiveTemplate(existingVersion, newTag string) ([]manifest.Archive, error)

// internal/discovery/service_install.go
type ServiceInstaller interface {
    Install(schedule string) error
    Uninstall() error
    Status() (*ServiceStatus, error)
}
type ServiceStatus struct {
    Installed    bool      `json:"installed"`
    Schedule     string    `json:"schedule"`
    LastRun      time.Time `json:"last_run"`
    NextRun      time.Time `json:"next_run"`
}
// 实现：windowsTaskScheduler / linuxCron / macosLaunchd

// internal/discovery/archive_filler.go
func FillArchivePlaceholders(t *manifest.Tool, newVer string, template []manifest.Archive) []manifest.Archive
// 生成占位 archive：url 按 template 替换版本号，sha256: "0000...", size: 0, mirror_urls 复用 template

// internal/manifest/manifest.go 扩展
type Tool struct {
    // ... 现有字段
    AutoSniff bool `yaml:"auto_sniff,omitempty"` // 默认 true（空值视为 true）
}
type Version struct {
    // ... 现有字段
    Discovered bool `yaml:"discovered,omitempty"`
    Unverified bool `yaml:"unverified,omitempty"`
}

// internal/config/config.go 扩展
type SniffConfig struct {
    Strategy        string `yaml:"strategy"`         // interval|startup|service|manual
    Interval        string `yaml:"interval"`         // 默认 24h
    Concurrency     int    `yaml:"concurrency"`      // 默认 3
    RateLimitPerHr  int    `yaml:"rate_limit_per_hour"` // 默认 60
    AutoArchiveFill bool   `yaml:"auto_archive_fill"`  // 默认 true
    LastSniffAt     string `yaml:"last_sniff_at"`     // ISO 时间戳，scheduler 维护
}
type Config struct {
    // ... 现有字段
    Sniff SniffConfig `yaml:"sniff"`
}
```

### 数据流

```text
xdev 启动
  → Scheduler.OnStartup()
      ├─ strategy=interval: 检查 last_sniff_at，超 interval → SniffNow（同步带 60s 超时）
      ├─ strategy=startup:  异步 goroutine SniffNow（不阻塞；进程退出原子写已完成部分）
      ├─ strategy=service:  不触发（系统服务已在外部定期调 sniff --headless）
      └─ strategy=manual:    不触发，仅读 cache

SniffNow(tool)
  → 令牌桶检查 rate_limit_per_hour（超限则 skip + log）
  → SnifferRegistry.Sniff(tool)
      → 优先级：显式 remote: 配置 > ArchivePattern 推导 > unsupported
      → [GitHubSniffer | ArchivePatternSniffer | JSONSniffer | HTMLSniffer]
      → HTTP GET (ETag 复用) → parse → []DiscoveredVersion{Unverified:true}
  → ArchiveFiller：为新版本生成占位 archive（sha256 全零）
  → 合并到 $XMDEV_HOME/manifests/<tool>.yaml（discovered:true, unverified:true）
      → 原子写（util.AtomicWriteFile），备份 .bak，只追加不覆盖已有字段
  → 原子写 sniff-cache.json（last_sniff_at 更新；startup 策略标 partial 若未完成全量）

xdev sniff --all --headless（系统服务调用）
  → SniffAll() → 无终端输出 → 写 cache

主命令 (list/doctor/updates)
  → 读 sniff-cache.json
  → list: 每个已装工具行比对 cache.latest 与已装版本，显示 ↑latest
  → updates: 列出所有有新版本的已装工具 + install 命令
  → GUI: 顶部徽标 + 通知中心

xdev install <tool>@<discovered_ver>
  → 查 manifest VersionList，找到该版本 unverified:true
  → 安装前提示"此版本由嗅探发现，archive URL 未经验证"
  → 用户确认 → 走现有 installer 下载流程（占位 sha256 全零触发跳过校验）
```

### 失败处理

| 失败模式 | 检测方式 | 恢复策略 | 用户可见影响 |
|---|---|---|---|
| GitHub rate limit | `X-RateLimit-Remaining: 0` 或令牌桶耗尽 | 记录 `reset_at`，不重试到 reset；其他工具不受影响 | 该工具本轮跳过，日志 `rate-limited` |
| 网络超时 | HTTP timeout | 指数退避 1s→2s→4s，3 次失败放弃该工具 | 该工具本轮失败，其他继续 |
| ArchivePattern 推导失败 | URL 无 GitHub 规律 | 标 `unsupported`，不报错 | 该工具不嗅探，记录 cache |
| 推导的 archive URL 404 | install 时下载失败 | 明确错误"嗅探推导 URL 不可用，请手动补" | 安装失败，提示用户 |
| manifest 写入冲突 | 原子写前检测文件变化 | 备份 .bak，只追加 VersionList 不覆盖 | 无数据丢失 |
| startup 进程退出嗅探未完成 | 进程退出信号 | 原子写已完成部分，标 `partial:true` | 下次启动补未完成工具 |
| interval 同步嗅探超 60s | context timeout | 已完成的写 cache，未完成下次补 | 主命令不阻塞超 60s |
| 系统服务安装权限不足 | `schtasks`/cron/launchd 返回 error | 提示提权（Win）或用用户 crontab（Linux） | service install 失败，提示原因 |
| cache 文件损坏 | JSON 解析失败 | 清空 cache 重新嗅探 | 静默重建 |

---

## 兼容性与迁移

### 向后兼容性

- 是否改变已有 CLI 参数行为？否 — `xdev refresh`/`refresh --all` 保留，内部改调 `discovery.Scheduler.SniffNow()`
- 是否改变已有配置文件格式？是 — 新增 `sniff:` 段（旧 config 无此段时用默认值 strategy=interval, interval=24h）
- 是否废弃已有接口？否
- 是否改变已有数据格式？是 — `manifest.Version` 新增 `discovered`/`unverified` 可选字段，旧 manifest 无此字段时默认 false（YAML omitempty 兼容）
- `manifest.Tool.AutoSniff` 空值视为 true（默认允许嗅探）

### 数据迁移

- 是否需要数据迁移？否 — 新字段 omitempty，旧 manifest/config 自动兼容；首次启动若无 `sniff-cache.json` 则创建空 cache

---

## 安全设计

- 认证/授权方式：不适用（本地工具）
- 敏感数据保护：嗅探只发 HTTP GET/HEAD，不下载 archive 内容；`sniff-cache.json` 不含敏感数据
- 信任边界：archive 占位 sha256 全零复用现有"跳过校验"机制，不绕过校验；发现的版本标 `unverified:true`，install 前提示
- 系统服务安装：需用户显式执行 `xdev sniff service install`，不自动安装；用 `os.Executable()` 绝对路径注册任务
- 输入验证：manifest URL 来自已有 archives（已签名/embedded）或 `remote:` 配置；ArchivePattern 只推导 GitHub URL，不接受任意输入

---

## 测试隔离

- **单元测试范围**：
  - 4 种 Sniffer（httptest mock，不依赖真实 GitHub API）
  - `ArchivePatternSniffer.extractGitHubRepoFromURL`/`buildArchiveTemplate`（纯函数）
  - `Scheduler.OnStartup`（4 种策略，mock cache 与 last_sniff_at）
  - `ServiceInstaller` 3 平台实现（mock `os/exec`）
  - `ArchiveFiller.FillArchivePlaceholders`
  - manifest 字段扩展（AutoSniff/Discovered/Unverified YAML 序列化兼容）
  - 令牌桶限流逻辑
- **集成测试范围**：`SniffNow` 端到端（httptest mock GitHub Releases API），cache 读写 + manifest 合并
- **平台测试矩阵**：

| 平台 | 测试方式 | 覆盖范围 |
|---|---|---|
| Windows 10+ | 自动 | 全路径：Sniffer + Scheduler + Task Scheduler service + cache + manifest |
| Linux (amd64) | 编译检查 + 单元测试 | cron service（mock）+ Sniffer + cache |
| macOS 12+ | 编译检查 + 核心单元 | launchd service（mock）+ config 解析 |

---

## Constitution 逐条检查

| Constitution 原则（1.0.0） | 检查结果 | 说明 | 引用 |
|---|---|---|---|
| I 安全与信任边界 | Compliant | 嗅探仅 GET/HEAD；archive 占位复用跳过校验不绕过；unverified 提示；service 显式安装 | NFR-003/FR-018 |
| II 测试必须隔离宿主状态 | Compliant | httptest mock GitHub API；service 安装 mock os/exec；不写真实系统任务 | 测试隔离 |
| III Constitution 与 Spec 权威 | Compliant | Spec 0.2.0 Approved 为任务权威 | spec.md |
| IV 双重独立审查 | Compliant | 实施时执行 Spec 合规 + 工程质量双审查 | 生命周期 |
| V Windows 主平台与跨平台分层 | Compliant | Windows 全量；其他编译+单元 | 测试矩阵 |
| VI 向后兼容 | Compliant | refresh 保留；新字段 omitempty；config sniff 段默认值 | NFR-002/数据迁移 |
| VII 可复现构建 | Compliant | 无新不可复现依赖，仅标准库 + 复用现有 | NFR-005 |

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

- **Spec 合规验证**：FR-001..018、SC-001..010 均映射到实现任务与测试
- **Constitution 合规验证**：无 Non-compliant
- **兼容性验证**：现有 `go test ./internal/...` 不因改动失败；`xdev refresh` 行为不变
- **跨平台验证**：Windows 完整；Linux/macOS 编译通过 + service 单元（mock）
- **定位验证**：确认无 sniff-local 命令（复用 env.ScanLocalTools）、Sniffer 无开放 Register、4 种策略可配

---

## 项目结构

### 文档 (此功能专属)

```text
specs/011-tool-discovery-automanage/
├── spec.md             # Feature Spec 0.2.0 (Approved)
├── plan.md             # 本文件 (Ready)
├── tasks.md            # 任务清单
└── reviews/            # 实施阶段审查记录
    ├── T001-spec-review.md
    └── T001-quality-review.md
```

### 源代码 (实现阶段)

```text
internal/discovery/
├── sniffer.go              # Sniffer 接口 + SnifferRegistry（固定内置，无开放注册）（新）
├── scheduler.go            # 触发策略调度 + 令牌桶 + cache 读写（新）
├── archive_filler.go       # 占位 archive 生成（新）
├── service_install.go      # ServiceInstaller 接口 + 平台实现（新）
└── sources/
    ├── github_sniffer.go       # GitHubSniffer（复用 remote.discoverGitHub）（新）
    ├── json_sniffer.go         # JSONSniffer（新）
    ├── html_sniffer.go         # HTMLSniffer（新）
    └── archive_pattern_sniffer.go  # ArchivePatternSniffer（URL 推导）（新）
internal/manifest/manifest.go    # Tool.AutoSniff + Version.Discovered/Unverified
internal/config/config.go        # SniffConfig + Config.Sniff
internal/cli/refresh.go          # 重构为调 Scheduler.SniffNow
internal/cli/sniff_cmd.go        # 新增 sniff/updates/sniff service 命令（新）
internal/cli/list.go             # ↑latest 徽标
internal/cli/doctor.go           # ↑latest 徽标
internal/installer/installer.go  # unverified 版本安装前提示
internal/gui/                    # 徽标 + 通知中心 + 策略 UI bindings
frontend/src/                    # 徽标 + 策略配置 UI
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
| Plan 起草 | Ready | `xinyan <zuijiuhjd@foxmail.com>` | 2026-07-26 | `0.1.0` | `2026-07-26-plan-011-001` 实施计划已就绪 |

---

## 变更历史

| 版本 | 日期 | 变更类型 | 说明 | 批准者 |
|---|---|---|---|---|
| 0.1.0 | 2026-07-26 | Initial Draft | 依据 Approved spec.md 0.2.0 生成正式 plan.md | — |