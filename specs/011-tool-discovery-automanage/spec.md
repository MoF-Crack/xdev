# Feature Specification: 所有支持工具的新版本嗅探发现及自动纳入管理范围

> **文档类型**：Feature Spec
> **状态**：Draft
> **文档版本**：0.2.0
> **创建日期**：2026-07-26
> **最后更新**：2026-07-26
> **所有者**：xinyan <zuijiuhjd@foxmail.com>
> **权威范围**：定义 xdev 对全部支持工具的新版本嗅探发现与纳入管理范围（含用户可选触发策略）的目标、范围、需求和验收标准
> **替代文档**：无

**Feature Branch**: `011-tool-discovery-automanage`
**Created**: 2026-07-26
**Status**: Draft
**Input**: 用户需求"所有支持工具的新版本嗅探发现及自动加入管理范围功能"+ 修订一"嗅探触发应为用户可选策略：周期后下次启动嗅探 / 每次启动后台嗅探 / 系统服务自动嗅探 / 用户主动开启"+ 修订二"sniff-local 与现有 `internal/env/scan_local.go` 的 ScanLocalTools 重叠，应复用而非重做"

---

## Why

当前版本发现是**手动、单工具、不持久、发现≠可装**的：

1. `xdev refresh <tool>` / `refresh --all`（`internal/cli/refresh.go`）按工具 `manifest.RemoteSource`（`manifest.go:161`）调 `remote.DiscoverVersions`（`remote.go:150`），把新版本写回 `$XMDEV_HOME/manifests/<tool>.yaml`。但：
   - 完全手动触发，用户不跑 `refresh` 就永远不知道有新版本
   - `refresh --all` 串行并发 3 路（`refresh.go:127`），38 工具×每工具一次 GitHub API，易撞 rate limit（`remote.go:107`）
   - 发现结果只写本地 manifest，**不通知用户**，不进 GUI，无"有新版本"提示
2. **manifest RemoteSource 缺失**：部分 manifest 没配 `remote:`（build 工具/Android SDK 多数无），这些工具的新版本**无法被发现**，只能靠 xdev 发版更新内置 manifest
3. **发现≠管理**：即便发现新版本，`xdev install <tool>@<newver>` 依赖 manifest 的 `versions:` 列表，但发现的新版本只追加到 `VersionList` 无 archives 条目，安装会失败（找不到当前平台 archive）
4. **archive 元数据缺失**：`refresh` 只追加 `Version{Version, Stable:true}`（`refresh.go:73`），不带 archives/sha256/size，发现的版本"看得见装不上"
5. **触发方式单一**：只有手动 `refresh`，用户无法配置自动嗅探节奏；不同用户需求不同（CI 想每次启动嗅探，个人用户想周期嗅探，服务器想系统服务常驻）

受影响方：
- 所有用户：不知道已装工具有新版本可用，错过升级
- 国内用户：撞 GitHub rate limit 后 refresh 失败，无重试/退避
- 维护者：每加一个工具要手写 `remote:`，无通用嗅探器
- 不同使用场景用户：无法按需选择嗅探触发方式

不关闭此缺口：xdev 的"多版本管理"卖点打折——用户装一次就固化在旧版本，工具链沦为"快照"而非"流"。

## 目标

建立**用户可选触发策略、批量、可靠、可执行**的新版本发现与纳入管理范围体系：

1. **通用嗅探器（Universal Sniffer）**：为无 `remote:` 的工具自动推断嗅探源（GitHub repo from archive URL 模式、官方 JSON API、HTML scrape），无需手写 manifest 即可发现新版本
2. **四种用户可选触发策略**（用户配置 `sniff.strategy`）：
   - **`interval`**（默认）：到设定周期后，**下一次启动 xdev 时**触发嗅探（启动时检查上次嗅探时间戳，超 `sniff_interval` 则触发，否则跳过——惰性触发，无常驻进程）
   - **`startup`**：每次启动 xdev 触发嗅探（异步，不阻塞主命令，进程退出时持久化已嗅探结果供下次读取）
   - **`service`**：安装系统级定时任务（Windows Task Scheduler / Linux cron / macOS launchd），由系统服务定期调用 `xdev sniff --headless` 自动嗅探
   - **`manual`**：纯手动，仅 `xdev sniff` 触发，无任何自动行为
3. **archive 元数据自动补全**：发现新版本后，按 manifest 的 archive URL 模板（从已有 version 推导）生成候选 archive 条目，标记 `sha256: "0000..."`（占位，复用现有跳过校验机制），让发现的版本"看得见也可装"
4. **新版本通知**：CLI `xdev list`/`updates` 显示 `↑latest`；GUI 顶部徽标 + 通知中心
5. **纳入管理范围**（远程新版本路径）：发现→写入 manifest VersionList + 占位 archive→`xdev install <tool>@<ver>` 可用
   - 注：本地已装工具迁移**不在此 spec 范围**——现有 `internal/env/scan_local.go` 的 `ScanLocalTools` + `MigrateLocalTool` 已完整实现本地扫描与迁移，GUI 已在用；010 spec 的 `migrate` skill 复用该能力。011 不重复实现
6. **rate-limit 安全**：并发嗅探带指数退避 + ETag 复用（已有 `remote.go:200`）+ 跨进程节流令牌桶
7. **嗅探源收窄**：`internal/discovery/` 新包，固定内置 4 种 Sniffer（GitHub/JSON/HTML/ArchivePattern），**不支持插件注册自定义 Sniffer**

## 范围

### 包含

- 新增 `internal/discovery/sniffer.go`：`Sniffer` 接口 + `SnifferRegistry`（固定内置 4 种，无开放注册）
- 新增 `internal/discovery/sources/`：`github_sniffer.go`、`json_sniffer.go`、`html_sniffer.go`、`archive_pattern_sniffer.go`
- 新增 `internal/discovery/scheduler.go`：触发策略调度器，按 `sniff.strategy` 配置选择 `interval`/`startup`/`service`/`manual` 行为
- 新增 `internal/discovery/service_install.go`：系统服务安装/卸载（Windows Task Scheduler / Linux cron / macOS launchd）
- 新增 `internal/discovery/archive_filler.go`：从已有 version 推导 archive URL 模板，为新版本生成占位 archive 条目
- `internal/manifest/manifest.go`：`Tool` 增加 `AutoSniff bool` 字段（默认 true）；`Version` 增加 `Discovered bool` + `Unverified bool` 字段；`Archive` 占位机制复用现有 `sha256: "0000..."`
- `internal/cli/refresh.go` 重构：复用 `discovery.Scheduler.SniffNow()`，保留 `refresh` 命令作为手动触发入口
- 新增 CLI：`xdev sniff [--all] [--tool <name>] [--headless]`（远程嗅探，`--headless` 供系统服务调用无输出）、`xdev updates`（列出可升级）、`xdev sniff service install/uninstall/status`（管理系统服务）
- GUI：顶部"更新可用"徽标、通知中心、`xdev updates` 面板、嗅探策略配置 UI
- `config.yaml`：`sniff:` 段新增 `strategy`（interval|startup|service|manual，默认 interval）、`interval`（默认 24h）、`concurrency`（默认 3）、`rate_limit_per_hour`（默认 60）、`auto_archive_fill`（默认 true）、`last_sniff_at`（时间戳，scheduler 维护）

### 非目标

- **本地已装工具扫描与迁移**——复用现有 `internal/env/scan_local.go` 的 `ScanLocalTools`/`MigrateLocalTool`，011 不重复实现；010 spec 的 `migrate` skill 已规划复用该能力
- 修改下载器核心逻辑（发现只产出元数据，下载沿用 `internal/installer`）
- 修改 SHA-256 校验逻辑（占位 archive 走既有"全零跳过"机制 `manifest.go`）
- 自动安装/自动升级（发现≠安装，纳入管理范围 = 让其"可安装"，是否升级由用户决定）
- 实现 P2P/镜像嗅探（只支持 HTTP/GitHub API）
- 修改 Plugin 接口（嗅探器独立于 Plugin，只读 manifest 数据）
- **插件注册自定义 Sniffer**（固定 4 种内置，NFR-005 删除）
- 替换 `manifest refresh`（保留为手动入口，内部重构为调 discovery 包）

---

## User Scenarios & Testing *(mandatory)*

### User Story 1 - interval 策略：周期后下次启动嗅探 (Priority: P0)

用户配置 `strategy: interval`（默认），到周期后下次启动 xdev 时触发，惰性无常驻进程。

**Acceptance Scenarios**:
1. **Given** `sniff.strategy=interval, interval=24h`，上次嗅探 25 小时前，**When** 用户执行 `xdev list`，
    **Then** 启动时检查 `last_sniff_at` 超过 24h，触发嗅探；嗅探同步执行（带 60s 超时），完成后 `list` 输出含 `↑latest` 徽标，`last_sniff_at` 更新
2. **Given** 同上配置，上次嗅探 10 小时前，**When** 用户执行 `xdev list`，**Then** 未超 24h，不触发嗅探，直接读 `sniff-cache.json` 显示上次结果
3. **Given** 嗅探超时（60s 未完成全部 38 工具），**When** 超时，**Then** 已完成的工具结果持久化到 `sniff-cache.json`，未完成的下次启动补，主命令不阻塞超过 60s

### User Story 2 - startup 策略：每次启动后台嗅探 (Priority: P1)

用户配置 `strategy: startup`，每次启动异步嗅探，不阻塞主命令。

**Acceptance Scenarios**:
1. **Given** `sniff.strategy=startup`，**When** 用户执行 `xdev list`，**Then** 启动异步嗅探 goroutine，`list` 立即输出（读上次 cache），嗅探在后台进行
2. **Given** 嗅探 goroutine 进行中，用户命令执行完进程退出，**When** 进程退出，**Then** 已嗅探完成的部分写入 `sniff-cache.json`（原子写），未完成的下次启动补
3. **Given** 用户再次启动 xdev，**When** 检查 `sniff-cache.json`，**Then** 显示上次后台嗅探的部分结果（标注 `partial` 若未完成全量）

### User Story 3 - service 策略：系统服务自动嗅探 (Priority: P2)

用户配置 `strategy: service`，安装系统定时任务，由系统服务定期调 `xdev sniff --headless`。

**Acceptance Scenarios**:
1. **Given** `sniff.strategy=service`，**When** 用户执行 `xdev sniff service install`，
    **Then** Windows 注册 Task Scheduler 任务（默认每日触发）、Linux 写 cron 条目（默认每日 03:00）、macOS 写 launchd plist（默认每日），任务调用 `xdev sniff --all --headless`
2. **Given** 服务已安装，**When** 系统定时触发，**Then** 后台运行 `xdev sniff --all --headless`，结果写 `sniff-cache.json`，无终端输出
3. **Given** 用户执行 `xdev sniff service status`，**When** 查看，**Then** 显示服务状态（已安装/未安装、触发计划、上次运行时间、下次运行时间）
4. **Given** 用户执行 `xdev sniff service uninstall`，**When** 执行，**Then** 移除系统任务，停止自动嗅探
5. **Given** GUI 嗅探策略面板，**When** 用户选 `service`，**Then** 提示"将安装系统定时任务（<平台说明>）"，含安装/卸载/状态按钮

### User Story 4 - manual 策略：纯手动 (Priority: P1)

用户配置 `strategy: manual`，无任何自动行为，仅 `xdev sniff` 触发。

**Acceptance Scenarios**:
1. **Given** `sniff.strategy=manual`，**When** 用户执行 `xdev list`，**Then** 不触发嗅探，仅读 `sniff-cache.json`（若有）或显示"未嗅探过，运行 `xdev sniff`"
2. **Given** 同上，**When** 用户执行 `xdev sniff --all`，**Then** 手动触发全量嗅探，完成后显示结果，`last_sniff_at` 更新

### User Story 5 - 通用嗅探器处理无 remote 配置的工具 (Priority: P1)

工具 manifest 无 `remote:`，但 archive URL 有规律，嗅探器自动推断并发现新版本。

**Acceptance Scenarios**:
1. **Given** `cmake.yaml` 无 `remote:`，但已有 versions 的 url 形如 `https://github.com/Kitware/CMake/releases/download/v3.28.0/cmake-3.28.0-windows-x86_64.zip`，**When** 嗅探 cmake，
    **Then** `archive_pattern_sniffer` 从 URL 推导出 GitHub repo `Kitware/CMake`，调 GitHub Releases API 发现 v3.29.0，按 URL 模板生成新 archive 条目
2. **Given** 工具 archive URL 无规律（纯静态分发），**When** 嗅探，**Then** 该工具被标记 `sniff: unsupported`，不报错，记录到 `sniff-cache.json` 的 `unsupported` 列表
3. **Given** 用户手动在 manifest 配 `remote: type: github, url: owner/repo`，**When** 嗅探，**Then** 使用显式配置，跳过推断

### User Story 6 - 新版本通知与升级 (Priority: P1)

用户能直观看到哪些已装工具有新版本。

**Acceptance Scenarios**:
1. **Given** 已装 python@3.11.10，嗅探发现 3.13.0，**When** `xdev list`，**Then** python 行显示 `3.11.10 (↑3.13.0 available)`
2. **Given** 同上，**When** `xdev updates`，**Then** 列出所有有新版本的已装工具：`python 3.11.10 → 3.13.0  (xdev install python@3.13.0)`
3. **Given** 同上，**When** 启动 GUI，**Then** 顶部徽标显示 `3 updates`，点击展开列表，每项含"安装"按钮

### User Story 7 - 发现版本未验证提示 (Priority: P2)

嗅探发现的版本 archive URL 未经验证，安装前显式提示。

**Acceptance Scenarios**:
1. **Given** 嗅探发现 python@3.13.0，archive URL 由 ArchivePattern 推导，**When** 写入 manifest，**Then** 该 Version 标记 `discovered: true, unverified: true`
2. **Given** 用户执行 `xdev install python@3.13.0`，**When** 该版本 `unverified: true`，**Then** 安装前提示"此版本由嗅探发现，archive URL 未经验证，下载失败请手动补 sha256/URL"，用户确认后继续
3. **Given** 下载失败（URL 404），**When** 安装失败，**Then** 错误信息明确"嗅探推导的 archive URL 不可用，请手动补 manifest 或等待下个 xdev 版本"

### User Story 8 - rate-limit 安全 (Priority: P2)

嗅探不撞 GitHub rate limit，失败有退避。

**Acceptance Scenarios**:
1. **Given** `sniff.rate_limit_per_hour=60`，**When** 嗅探在一小时内尝试第 61 次，**Then** 令牌桶拒绝，记录 `rate-limited, skip`，不调 API
2. **Given** GitHub 返回 403 + `X-RateLimit-Remaining: 0`，**When** 嗅探，**Then** 该工具失败，记录 `reset_at`，不重试直到 reset 时间
3. **Given** 网络超时，**When** 嗅探某工具失败，**Then** 指数退避（1s→2s→4s），3 次失败后放弃该工具本轮，其他工具不受影响

---

## Requirements *(mandatory)*

### 功能需求 (Functional Requirements)

- **FR-001**：新增 `discovery.Sniffer` 接口：`Name()`、`Supports(t *manifest.Tool) bool`、`Sniff(ctx, t) ([]DiscoveredVersion, error)`
- **FR-002**：`DiscoveredVersion` 含 `Version string`、`ReleaseDate string`、`Archives []manifest.Archive`（占位或推导）、`Source string`（sniffer 名）、`Unverified bool`
- **FR-003**：**固定内置 4 种 Sniffer，不可外部注册**：`GitHubSniffer`（复用 `remote.discoverGitHub`，支持 `remote:` 显式配置）、`JSONSniffer`、`HTMLSniffer`、`ArchivePatternSniffer`（从已有 archive URL 推导 GitHub repo + URL 模板）；`SnifferRegistry` 包内初始化，无开放 `Register`
- **FR-004**：`ArchivePatternSniffer`：取已有 version 的 archive URL，正则提取 `github.com/<owner>/<repo>/releases/download/<tag>/...`，推导 repo 与 URL 模板，对新 tag 替换版本号生成新 archive 条目
- **FR-005**：`SnifferRegistry` 按优先级匹配：显式 `remote:` 配置 > ArchivePattern 推导 > 跳过（标记 unsupported）
- **FR-006**：`discovery.Scheduler` 按 `sniff.strategy` 配置选择行为：
  - `interval`：启动时检查 `last_sniff_at`，超 `sniff.interval` 则触发嗅探（同步，带 60s 超时，完成或超时后继续主命令）
  - `startup`：启动时异步触发嗅探 goroutine，不阻塞主命令；进程退出时持久化已完成部分（原子写）
  - `service`：不自动触发，依赖系统服务调 `xdev sniff --all --headless`；启动时仅读 cache
  - `manual`：不自动触发，仅 `xdev sniff` 手动；启动时仅读 cache
- **FR-007**：`SniffNow()` 手动触发接口（`xdev sniff`/`refresh`/`service` 调用）；并发 `sniff.concurrency`；令牌桶 `sniff.rate_limit_per_hour`
- **FR-008**：嗅探结果写 `$XMDEV_HOME/cache/sniff-cache.json`，结构：`{tools: {name: {last_sniff, etag, versions: [...], unsupported: bool, error: string}}, rate_limit: {remaining, reset_at}, last_sniff_at: timestamp, partial: bool}`
- **FR-009**：嗅探发现的新版本合并到 `$XMDEV_HOME/manifests/<tool>.yaml` 的 `VersionList`，标记 `discovered: true, unverified: true`，带占位 archive（`sha256: "0000..."`，复用现有跳过机制）
- **FR-010**：`auto_archive_fill: true` 时，ArchivePatternSniffer 为新版本生成完整 archive 条目（含 url/mirror_urls/size 占位 0）；`false` 时只追加 Version 不带 archive
- **FR-011**：`manifest.Tool` 增加 `AutoSniff bool`（默认 true）；`manifest.Version` 增加 `Discovered bool` + `Unverified bool`
- **FR-012**：CLI 新增 `xdev sniff [--all] [--tool <name>] [--headless]`（`--headless` 无终端输出，供系统服务）、`xdev updates`（列出可升级）、`xdev sniff service install/uninstall/status`
- **FR-013**：`xdev list` / `xdev doctor` 输出标注 `↑<latest>` 表示有新版本可用
- **FR-014**：GUI 顶部"更新可用"徽标（含数量），点击展开列表，每项含"安装"按钮；通知中心记录嗅探完成事件；嗅探策略配置 UI（4 种策略下拉 + interval 输入 + service install/uninstall 按钮）
- **FR-015**：`xdev refresh` 保留为手动入口，内部重构为调 `discovery.Scheduler.SniffNow()`，向后兼容
- **FR-016**：嗅探失败的工具有明确日志与 `sniff-cache.json` 记录（`error` 字段），不静默跳过；`xdev sniff` 显示上次失败列表
- **FR-017**：`discovery.service_install.go` 实现：
  - Windows：用 `schtasks` 注册 Task Scheduler 任务（默认每日触发，可配），调用 `<xdev.exe> sniff --all --headless`
  - Linux：写 `/etc/cron.d/xdev-sniff` 或用户 crontab 条目（默认每日 03:00）
  - macOS：写 `~/Library/LaunchAgents/com.xdev.sniff.plist`（默认每日）
  - `service uninstall` 移除对应任务；`service status` 显示状态
- **FR-018**：安装 `xdev install <tool>@<ver>` 时，若该版本 `unverified: true`，安装前显式提示"此版本由嗅探发现，archive URL 未经验证，下载失败请手动补"，用户确认后继续

### 非功能需求 (Non-Functional Requirements)

- **NFR-001**：性能 — 单工具嗅探 < 5s（含 HTTP），全量 38 工具并发 3 路 < 60s；`interval`/`startup` 策略启动开销 < 50ms（仅检查/注册，嗅探异步或带超时）；`manual`/`service` 启动开销 < 10ms（仅读 cache）
- **NFR-002**：兼容性 — 现有 `xdev refresh`/`refresh --all` 命令行为不变（内部改调 discovery 包）；现有 manifest 无 `discovered`/`unverified` 字段时默认 false（向后兼容）
- **NFR-003**：安全 — 嗅探只发 HTTP GET/HEAD，不下载 archive 内容；archive 占位的 sha256 全零触发既有跳过校验机制，不绕过校验；系统服务安装需用户显式执行 `service install`，不自动安装
- **NFR-004**：可靠 — 令牌桶防 rate limit；指数退避防网络抖动；ETag 复用减少请求；`startup` 策略进程退出时原子写已完成部分，不丢失
- **NFR-005**：可扩展 — 新增内置 Sniffer 只需在 `internal/discovery/sources/` 加文件实现接口并包内注册；**不支持插件注册自定义 Sniffer**（固定 4 种）
- **NFR-006**：跨平台 — `sniff-cache.json` 格式跨平台一致（JSON）；系统服务安装按平台区分（Windows Task Scheduler / Linux cron / macOS launchd）；服务路径与触发时间可配
- **NFR-007**：可观测 — 每次嗅探记录日志：工具名、sniffer 名、发现数、耗时、是否失败；`xdev sniff --verbose` 输出详情；`service status` 显示服务运行历史
- **NFR-008**：可降级 — 嗅探失败不阻塞 `xdev list`/`doctor`；GitHub API 不可用时降级到上次 cache 数据，标注 `stale`
- **NFR-009**：定位约束 — 本地已装工具扫描与迁移不在本 spec 范围，复用现有 `internal/env/scan_local.go`；Sniffer 不可外部扩展，固定 4 种内置

---

## Success Criteria *(mandatory)*

- **SC-001**：`interval` 策略下，到周期后下次启动 xdev 触发嗅探，发现新版本写入 manifest，`xdev install <tool>@<newver>` 可用
- **SC-002**：`startup` 策略下，每次启动异步嗅探不阻塞主命令，进程退出时已完成部分持久化
- **SC-003**：`service` 策略下，`service install` 安装系统定时任务，系统定期调 `xdev sniff --headless`，`service status/uninstall` 正常工作
- **SC-004**：`manual` 策略下，无自动行为，仅 `xdev sniff` 触发
- **SC-005**：无 `remote:` 配置的工具（archive URL 有规律）能被 ArchivePatternSniffer 发现新版本
- **SC-006**：`xdev list`/GUI 显示新版本可用徽标，`xdev updates` 列出可升级项
- **SC-007**：嗅探不撞 GitHub rate limit（令牌桶 + ETag 退避生效）
- **SC-008**：嗅探失败有日志与 cache 记录，不静默，下次手动 sniff 可见失败列表
- **SC-009**：发现的版本标 `unverified: true`，`xdev install` 前显式提示，下载失败有明确错误
- **SC-010**：本地工具扫描与迁移不在本 spec 实现，复用现有 `internal/env/scan_local.go`

---

## API / 契约草案

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
type SnifferRegistry struct { /* ordered list of Sniffer */ }
func (r *SnifferRegistry) Sniff(ctx, t) ([]DiscoveredVersion, error)  // first supporting sniffer wins

type SniffCache struct {
    Tools       map[string]ToolSniffResult `json:"tools"`
    RateLimit   RateLimitState             `json:"rate_limit"`
    LastSniffAt time.Time                  `json:"last_sniff_at"`
    Partial     bool                       `json:"partial"`
}

type Scheduler struct { /* strategy, interval, token bucket, concurrency */ }
func (s *Scheduler) OnStartup()                          // 按 strategy 决定是否触发
func (s *Scheduler) SniffNow(ctx, tool string) error      // 手动/headless 触发
func (s *Scheduler) LoadCache() (*SniffCache, error)
func (s *Scheduler) SaveCache(c *SniffCache) error       // 原子写

// internal/discovery/service_install.go
type ServiceInstaller interface {
    Install(schedule string) error    // 注册系统定时任务
    Uninstall() error
    Status() (*ServiceStatus, error)
}
// 实现：windowsTaskScheduler / linuxCron / macosLaunchd
```

---

## 数据流

```
xdev 启动
  └─ Scheduler.OnStartup()
        ├─ strategy=interval: 检查 last_sniff_at，超 interval 则 SniffNow（同步带 60s 超时）
        ├─ strategy=startup:  异步 goroutine SniffNow，不阻塞
        ├─ strategy=service:  不触发（系统服务已在外部定期调 sniff --headless）
        └─ strategy=manual:    不触发，仅读 cache

SniffNow(tool)
  └─ SnifferRegistry.Sniff(tool)
        └─ [GitHubSniffer | ArchivePatternSniffer | JSONSniffer | HTMLSniffer]
              └─ HTTP GET (ETag) ──> parse ──> []DiscoveredVersion
  └─ merge 到 $XMDEV_HOME/manifests/<tool>.yaml (discovered:true, unverified:true, 占位 archive)
  └─ 原子写 sniff-cache.json（startup 策略进程退出时也写）

xdev sniff --all --headless（系统服务调用）
  └─ Scheduler.SniffNow(all) ──> 无终端输出 ──> 写 cache

主命令 (list/doctor/updates)
  └─ 读 sniff-cache.json ──> 显示 ↑updates 徽标
```

---

## 风险与对策

| 风险 | 影响 | 对策 |
|------|------|------|
| ArchivePatternSniffer 误推导 URL | 生成的 archive URL 404，安装失败 | 发现版本标 `unverified: true`；`install` 前显式提示；下载失败明确错误；推导失败的工具标 unsupported |
| GitHub rate limit | 全量嗅探失败 | 令牌桶 + ETag + 指数退避 + `rate_limit_per_hour` 默认 60；失败不重试到 reset 时间 |
| 嗅探写 manifest 与用户手动编辑冲突 | 数据丢失 | 写入只追加 VersionList，不覆盖已有字段；原子写（复用 `util.AtomicWriteFile`）；备份上次 manifest 到 `.bak` |
| startup 策略进程退出嗅探未完成 | 结果丢失 | 原子写已完成部分到 cache（标 `partial:true`）；下次启动补未完成工具 |
| interval 策略同步嗅探阻塞主命令 | 用户体验差 | 带 60s 超时，超时后继续主命令，未完成工具下次补；用户可改 `startup` 异步 |
| 系统服务安装权限不足 | service 策略失败 | Windows 需管理员（提示提权）；Linux cron 写用户 crontab 无需 root；macOS launchd 用户级 |
| 系统服务调用 xdev 路径变化 | 服务失效 | `service install` 用 xdev 绝对路径（`os.Executable()`）；xdev 升级位置变化时提示重装服务 |
| 发现的版本带占位 sha256 被误用 | 安全 | 占位 sha256 全零触发既有跳过机制，不绕过校验；`unverified` 标记 + 安装前提示 |

---

## 批准记录 (Approval History)

| 阶段 | 状态 | 批准者 | 日期 | 文档版本 | 批准范围 |
|---|---|---|---|---|---|
| Draft 创建 | Draft | `xinyan <zuijiuhjd@foxmail.com>` | 2026-07-26 | `0.1.0` | `2026-07-26-spec-011-draft-001` 创建初稿 |
| Draft 修订 | Draft | `xinyan <zuijiuhjd@foxmail.com>` | 2026-07-26 | `0.2.0` | 采纳"嗅探触发应为用户可选策略（interval/startup/service/manual 四种）"修订；采纳"sniff-local 与现有 internal/env/scan_local.go 重叠，复用而非重做"修订；新增 unverified 标记；Sniffer 不可外部扩展 |

---

## 变更历史

| 版本 | 日期 | 变更类型 | 说明 | 批准者 |
|---|---|---|---|---|
| 0.1.0 | 2026-07-26 | Initial Draft | 初始设计（固定后台 Scheduler + sniff-local） | — |
| 0.2.0 | 2026-07-26 | Major Revision | 采纳用户意见：① 嗅探触发改为用户可选 4 种策略（interval 周期后下次启动 / startup 每次启动后台 / service 系统服务自动 / manual 纯手动），新增 `service_install.go` 与 `xdev sniff service` 命令；② 移除 sniff-local，本地扫描迁移复用现有 `internal/env/scan_local.go` 的 ScanLocalTools/MigrateLocalTool（010 migrate skill 也复用该能力）；③ Sniffer 不可外部扩展（删 NFR-005 插件注册），固定 4 种内置；④ 发现版本标 `unverified: true`，安装前提示 | — |