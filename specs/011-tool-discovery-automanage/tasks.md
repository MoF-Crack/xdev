# Tasks: 所有支持工具的新版本嗅探发现及自动纳入管理范围

> **文档类型**：Task List
> **状态**：Ready
> **创建日期**：2026-07-26
> **关联 Spec**：`specs/011-tool-discovery-automanage/spec.md` `0.2.0`
> **关联 Plan**：`specs/011-tool-discovery-automanage/plan.md` `0.1.0`

## 任务列表

> 任务按依赖顺序编排：manifest/config 扩展（T001-T002）→ Sniffer 核心层（T003-T008）→ 调度与服务（T009-T012）→ 集成与 UI（T013-T019）→ 审查（T020）
> 标注 ⚡ 的任务可并行（无写冲突）。

| # | 任务 | 依赖 | 状态 | 审查 | 提交 |
|---|---|---|---|---|---|
| T001 | `internal/manifest/manifest.go` 扩展：`Tool` 增 `AutoSniff bool`（空值视为 true，`yaml:"auto_sniff,omitempty"`）；`Version` 增 `Discovered bool` + `Unverified bool`（均 `omitempty`，旧 manifest 默认 false 兼容）；YAML 序列化/反序列化回归测试 | — | Ready | — | — |
| T002 | `internal/config/config.go` 扩展：新增 `SniffConfig{Strategy, Interval, Concurrency, RateLimitPerHour, AutoArchiveFill, LastSniffAt}` + `Config.Sniff SniffConfig`；默认值 strategy=interval/interval=24h/concurrency=3/rate_limit_per_hour=60/auto_archive_fill=true；旧 config 无 `sniff:` 段时用默认值 | T001 | Ready | — | — |
| T003 | 新增 `internal/discovery/sniffer.go`：`Sniffer` 接口（Name/Supports/Sniff）、`DiscoveredVersion` 结构（含 Unverified）、`SnifferRegistry`（包内 `newSnifferRegistry` 初始化 4 种，无开放 Register）、`SniffCache`/`ToolSniffResult`/`RateLimitState` 结构 | T001 | Ready | — | — |
| T004 | 新增 `internal/discovery/sources/github_sniffer.go`：`GitHubSniffer` 复用 `remote.DiscoverVersions`（支持 `remote:` 显式配置），包装为 `[]DiscoveredVersion`（Unverified=false，因走官方配置） | T003 | Ready | — | — |
| T005 | 新增 `internal/discovery/sources/json_sniffer.go` + `html_sniffer.go`：复用 `remote.discoverJSON`/`discoverHTML`，包装为 `[]DiscoveredVersion` | T003 | Ready | — | — |
| T006 | 新增 `internal/discovery/sources/archive_pattern_sniffer.go`：`extractGitHubRepoFromURL`（正则提取 `github.com/<owner>/<repo>/releases/download/...`）、`buildArchiveTemplate`（从已有 version 的 archive URL 推导模板）；`ArchivePatternSniffer.Supports` 检测 URL 有 GitHub 规律；`Sniff` 调 GitHub Releases API 发现新 tag，按模板生成新 archive；推导失败标 unsupported | T003,T004 | Ready | — | — |
| T007 | 新增 `internal/discovery/archive_filler.go`：`FillArchivePlaceholders(t, newVer, template)` 生成占位 archive（url 按 template 替换版本号，sha256="0000...", size=0, mirror_urls 复用 template）；`auto_archive_fill=false` 时返回空（仅追加 Version 无 archive） | T003 | Ready | — | — |
| T008 | 新增 `internal/discovery/scheduler.go`：`Scheduler` 持 `SnifferRegistry` + `SniffConfig` + cachePath + 令牌桶；`OnStartup(ctx)` 按 strategy 选择行为（interval 检查 last_sniff_at 超时则 SniffNow 同步带 60s 超时；startup 异步 goroutine 不阻塞；service/manual 仅读 cache）；`SniffNow(ctx, tool)`/`SniffAll(ctx)` 含令牌桶限流 + 指数退避 + ETag 复用；`LoadCache`/`SaveCache` 原子写（startup 进程退出也写已完成部分标 partial） | T003,T002 | Ready | — | — |
| T009 | 新增 `internal/discovery/service_install.go`：`ServiceInstaller` 接口 + 3 平台实现：`windowsTaskScheduler`（`schtasks /create` 注册每日任务调 `<xdev.exe> sniff --all --headless`）、`linuxCron`（写用户 crontab 条目）、`macosLaunchd`（写 `~/Library/LaunchAgents/com.xdev.sniff.plist`）；`Install(schedule)`/`Uninstall()`/`Status()`；用 `os.Executable()` 绝对路径 | T002 | Ready | — | — |
| T010 | `internal/cli/refresh.go` 重构：`refreshOne`/`refreshAll` 内部改调 `Scheduler.SniffNow()`/`SniffAll()`，保持 CLI 输出格式与行为不变（向后兼容）；移除直接调 `remote.DiscoverVersions` 的旧逻辑 | T008 | Ready | — | — |
| T011 | 新增 `internal/cli/sniff_cmd.go`：`xdev sniff [--all] [--tool <name>] [--headless]`（调 SniffNow/SniffAll，`--headless` 无终端输出供服务调用）、`xdev updates`（读 cache 列出有新版本的已装工具 + install 命令）、`xdev sniff service install/uninstall/status`（调 ServiceInstaller）；注册到 root 命令 | T008,T009 | Ready | — | — |
| T012 | `internal/cli/list.go` + `doctor.go` 增强：读 `sniff-cache.json`，每个已装工具行比对 cache.latest 与已装版本，输出 `↑<latest>` 徽标；`xdev list`/`doctor` 不阻塞（读 cache 不触发嗅探，嗅探由 OnStartup 按策略） | T008 | Ready | — | — |
| T013 | `internal/installer/installer.go` 增强：`xdev install <tool>@<ver>` 时查 manifest VersionList，若该版本 `unverified: true` 则安装前显式提示"此版本由嗅探发现，archive URL 未经验证，下载失败请手动补 sha256/URL"，用户确认后继续；下载失败（URL 404）错误信息明确 | T001 | Ready | — | — |
| T014 | `internal/cli/sniff_cmd.go` 合并发现的版本到 manifest：`SniffNow` 完成后调 `manifest.MarshalTool` 写回 `$XMDEV_HOME/manifests/<tool>.yaml`（只追加 VersionList 不覆盖已有字段，标 `discovered:true, unverified:true`，带占位 archive），原子写 + 备份 .bak | T007,T008 | Ready | — | — |
| T015 | 新增 `internal/discovery/sniffer_test.go` + `scheduler_test.go` + `service_install_test.go`：httptest mock GitHub Releases API；4 种 Sniffer 单元；`extractGitHubRepoFromURL`/`buildArchiveTemplate` 纯函数；Scheduler 4 种策略（mock cache/last_sniff_at）；ServiceInstaller 3 平台（mock os/exec）；令牌桶限流；ArchiveFiller；不依赖真实 API | T003-T009 | Ready | — | — |
| T016 | GUI bindings（`internal/gui/`）：新增 `SniffNow`/`SniffAll`/`LoadSniffCache`/`GetAvailableUpdates`/`InstallSniffService`/`UninstallSniffService`/`SniffServiceStatus`/`SetSniffStrategy`；`GetAvailableUpdates` 读 cache 返回更新列表；现有 list/doctor bindings 返回含 `↑latest` 标记 | T008,T011 | Ready | — | — |
| T017 | GUI 前端：顶部"更新可用"徽标（含数量，点击展开列表，每项含"安装"按钮调 install 流程）；通知中心记录嗅探完成事件；嗅探策略配置 UI（4 种 strategy 下拉 + interval 输入 + service install/uninstall/status 按钮 + auto_archive_fill 开关）；`xdev updates` 面板 | T016 | Ready | — | — |
| T018 | 更新 `README.md`：`xdev sniff`/`xdev updates`/`xdev sniff service` 命令说明；4 种触发策略说明；本地工具迁移指向 `xdev ai run migrate`/现有 GUI 迁移（复用 env.ScanLocalTools，不在本 spec） | T011 | Ready | — | — |
| T019 | 更新 `build.ps1`/`build.sh`：确认三平台编译通过（无新 cgo 依赖，service_install 用 os/exec 调系统命令）；Windows Task Scheduler / Linux cron / macOS launchd 命令正确性 | T009 | Ready | — | — |
| T020 | 双重独立审查（Spec 合规 + 工程质量）：Spec 合规审查覆盖 FR-001..018、SC-001..010、NFR-001..009；工程质量审查覆盖模块边界、4 种策略、令牌桶、原子写、向后兼容、定位约束（无 sniff-local、Sniffer 无开放 Register、复用 env.ScanLocalTools） | T001-T019 | Ready | — | — |

## 验收清单

> 映射 spec.md 0.2.0 的 FR-001..018 与 SC-001..010，及 Constitution 合规。

### Manifest/Config 扩展
- [ ] FR-011: Tool.AutoSniff + Version.Discovered/Unverified 字段（omitempty 兼容）
- [ ] T001: manifest YAML 序列化/反序列化回归（旧 manifest 无新字段默认 false）

### Sniffer 核心层
- [ ] FR-001: Sniffer 接口（Name/Supports/Sniff）
- [ ] FR-002: DiscoveredVersion 结构（含 Unverified）
- [ ] FR-003: 固定内置 4 种 Sniffer，SnifferRegistry 包内初始化无开放 Register
- [ ] FR-004: ArchivePatternSniffer 从 archive URL 推导 GitHub repo + URL 模板
- [ ] FR-005: SnifferRegistry 优先级（显式 remote: > ArchivePattern > unsupported）
- [ ] FR-009: 发现的新版本合并到 manifest，标 discovered:true, unverified:true，占位 archive
- [ ] FR-010: auto_archive_fill 控制是否生成 archive 条目

### 调度与服务
- [ ] FR-006: Scheduler 按 strategy 选择行为（interval/startup/service/manual 四种）
- [ ] FR-007: SniffNow 手动触发，并发 concurrency，令牌桶 rate_limit_per_hour
- [ ] FR-008: sniff-cache.json 结构（tools/rate_limit/last_sniff_at/partial）
- [ ] FR-017: service_install.go 三平台实现（Win Task Scheduler / Linux cron / macOS launchd）
- [ ] NFR-004: 令牌桶 + 指数退避 + ETag 复用 + 原子写已完成部分

### CLI/GUI 集成
- [ ] FR-012: xdev sniff [--all] [--tool] [--headless] + xdev updates + xdev sniff service install/uninstall/status
- [ ] FR-013: xdev list/doctor 输出 ↑latest 徽标
- [ ] FR-014: GUI 徽标 + 通知中心 + 策略配置 UI + updates 面板
- [ ] FR-015: xdev refresh 保留为手动入口，内部调 Scheduler.SniffNow，向后兼容
- [ ] FR-016: 嗅探失败有日志与 cache 记录（error 字段），不静默
- [ ] FR-018: install unverified 版本前显式提示

### Success Criteria
- [ ] SC-001: interval 策略，到周期后下次启动触发嗅探，发现新版本写入 manifest 可安装
- [ ] SC-002: startup 策略，每次启动异步嗅探不阻塞，进程退出持久化已完成部分
- [ ] SC-003: service 策略，service install 安装系统任务，定期调 sniff --headless，status/uninstall 正常
- [ ] SC-004: manual 策略，无自动行为，仅 xdev sniff 触发
- [ ] SC-005: 无 remote: 配置的工具（archive URL 有规律）能被 ArchivePatternSniffer 发现
- [ ] SC-006: xdev list/GUI 显示新版本徽标，xdev updates 列出可升级项
- [ ] SC-007: 嗅探不撞 GitHub rate limit（令牌桶 + ETag 退避）
- [ ] SC-008: 嗅探失败有日志与 cache 记录，不静默，下次可见失败列表
- [ ] SC-009: 发现版本标 unverified:true，install 前提示，下载失败有明确错误
- [ ] SC-010: 本地工具扫描迁移不在本 spec，复用 internal/env/scan_local.go

### Constitution / 工程质量
- [ ] go vet 通过
- [ ] go test ./internal/discovery/... 通过（httptest mock，不依赖真实 API）
- [ ] go test ./internal/manifest/... 通过（新字段兼容）
- [ ] go test ./internal/config/... 通过（sniff 段默认值）
- [ ] go build 三平台编译通过（Windows/Linux/macOS）
- [ ] NFR-005 定位约束：Sniffer 无开放 Register，固定 4 种内置
- [ ] NFR-009 定位约束：无 sniff-local 命令，复用 env.ScanLocalTools
- [ ] NFR-002 向后兼容：xdev refresh 行为不变，旧 manifest/config 兼容
- [ ] Constitution I-VII 全 Compliant（见 plan.md 逐条检查）

## 依赖关系图

```text
T001(manifest扩展) ──┬── T002(config扩展) ──────────────────┐
                       │                                      │
                       └── T003(sniffer.go) ──┬── T004(github)─┤
                                              ├── T005(json/html)─┤
                                              ├── T006(archive_pattern)──┤
                                              └── T007(archive_filler)──┤
                                                                       │  │
                       T002 ── T008(scheduler) ── T003 ───────────────┤  │
                                              │                     │  │
                       T002 ── T009(service_install) ────────────────┤  │
                                              │                     │  │
                       T008 ── T010(refresh重构)                     │  │
                       T008,T009 ── T011(sniff_cmd.go) ─────────────┤  │
                       T008 ── T012(list/doctor徽标) ───────────────┤  │
                       T001 ── T013(installer unverified提示) ──────┤  │
                       T007,T008 ── T014(manifest合并) ─────────────┤  │
                       T003-T009 ── T015(tests) ────────────────────┤  │
                                              │                     │  │
                       T008,T011 ── T016(gui bindings) ─────────────┤  │
                       T016 ── T017(gui frontend) ──────────────────┤  │
                       T011 ── T018(README) ────────────────────────┤  │
                       T009 ── T019(build脚本) ─────────────────────┘  │
                       T001-T019 ── T020(双重审查) ────────────────────┘
```