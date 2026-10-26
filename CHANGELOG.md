# Changelog

All notable changes to XDev are documented in this file.
Format based on [Keep a Changelog](https://keepachangelog.com/), adheres to [SemVer](https://semver.org/).

## [1.0.0-alpha.2] - 2026-10-25

本版是架构优化 + 环境变量体系重建的合集。四个阶段：P0（缺陷）、P1（path_append）、
P2（进程级 env 注入）、P3（项目级多版本），随后是三项架构优化与包管理器数据化。

### Added

- **项目级多版本（`.xdev.yaml`）**：从 CWD 逐级向上查找项目配置，`shim.Lookup` 优先
  按它解析版本（未命中静默回落全局 `current`）。终于可以 A 项目用 python 3.11、
  B 项目用 3.12 而无需来回 `xdev use`。版本规格复用既有 resolver 语义
  （`latest` / `lts` / `3.12` / `^3.12` / `~3.11`），且只在本机**已安装**集合上解析
  （不联网，避免执行命令时突然触发下载）
- **环境变量首次在非 Windows 生效**：新增 `internal/envctx`，把工具 env 从「写注册表」
  改为「注入子进程环境」。此前 `setUserEnvVars` 开头就 `if runtime.GOOS != "windows" { return nil }`，
  macOS/Linux 上 JAVA_HOME / GOROOT 从未被设置，Maven/Gradle 系工具链直接不可用
- **manifest 新增 `path_append` 字段**：声明激活时要加入 PATH 的目录，模板支持
  `{{install}}` / `{{version}}` / `{{tool}}` 与 `{{<ENVNAME>}}`（引用同 manifest 内已定义的变量）。
  修复「`go install` / `cargo install` 装完找不到工具」——官方文档明确要求把
  `$GOPATH/bin`、`$CARGO_HOME/bin` 加入 PATH
- **`xdev pm` 命令族**：`pm list [--all]` 列出包管理器及状态，
  `pm install <name> [--dry-run]` 执行官方安装脚本。此前该能力只在 GUI 里，
  CLI/TUI 完全看不到
- **`xdev hook` 恢复实际输出**：按 `.xdev.yaml` 输出 PATH 与环境变量（此前是 `return nil`
  空实现，而 `xdev activate` 仍会装一个调用它的 cd hook）
- **`ProgressPhase.String()`**：UI 层可直接当展示文案用，不再与 installer 的阶段枚举两份真相

### Changed

- **包管理器定义数据化**：13 个定义从 `gui/app_local_detect.go` 的 Go 常量搬到
  `manifests/package-managers.yaml`，随 manifest 一起进入 Ed25519 签名覆盖范围。
  其中 5 个带**会被直接执行的命令**（scoop/brew/choco 的 install_command，
  yarn/pnpm 的 enable_command）——放在代码里时改一行就能改掉用户机器上会执行的命令
- **GUI 安装编排收敛到 installer**：`app_install.go` 的 330 行自建编排替换为一次
  `InstallWithProgress` 调用（新增 `InstallOptions.DownloadFunc` 钩子注入
  `downloader.Manager`，保留 pause/resume/cancel）。这消除了唯一**已经造成过数据损坏**的
  重复实现——F-05：GUI 旧代码在下载之前就 `os.RemoveAll`，下载失败会摧毁完好安装
- **`Activate` 拆为 plan/apply**：新增 `activationPlan` 承载「要做什么」与「做到了哪一步」，
  `rollback` 据此逐项撤销。修复前函数结尾是 `_ = junctionCreated` / `_ = envVarsWritten`
  ——两个变量被显式丢弃，说明回滚记账已死，任何新增激活步骤都会静默漏回滚
- **catalog 装配统一**：新增 `plugin.BuildRegistry` 作为唯一入口。此前四处各写一遍且语义
  分叉——`hook` 与 `shim` 只做 `NewGeneric`，**拿不到 Lua/vfox 插件**，导致插件型工具的
  env 注入与项目级版本切换完全失效
- **`env` 包职责收敛**：`scan_local.go` + `migrate.go` 搬到新包 `internal/localmigrate`。
  此前 `env` 反向依赖 `hostscan` 与 `toolmeta`
- **`path.mode` 重新接受 `auto`**：P3 恢复 hook 后 auto 才名副其实（此前因 hook 是空壳而拒绝）
- Go 构建工具链 1.25.0 → 1.27.0；分发默认版本 1.26.5 → 1.27.3（1.26.5 存在中间信任漏洞）

### Fixed

- **POSIX PATH 单引号导致 `$PATH` 不展开（严重）**：`export PATH='%s:$PATH'` 中单引号内的
  `$PATH` 不展开，eval 后 PATH 变成字面量，用户原有 PATH 被**彻底覆盖**。
  `env.go` 那处写进 shell RC 文件，每次开 shell 都错一次。
  同时改正 `hook_test.go` 中把坏字符串断言成正确行为的测试
- **shim 频繁弹出终端窗口**：shim 是 GUI 子系统二进制，被 IDE 无窗口调用时 Windows 会
  分配控制台，子进程继承后窗口可见；语言服务器是长驻进程所以关不掉、重建后又在。
  新增 `hasVisibleConsole()`（同时查窗口可见性——IDE 集成终端的 `GetConsoleWindow`
  返回非 0 但 `IsWindowVisible` 为 false），无可见控制台时给子进程加 `CREATE_NO_WINDOW`
- **4 处裸 `os.Readlink` 未校验目标存在**：悬空链接仍给幽灵版本打 ★，
  `xdev current` 还会打印无效行。改用 `util.ActiveVersion`
- **env 白名单写删不对称**：写侧不过白名单、删侧过，导致白名单外的变量写了删不掉，
  且 `EnrichFromRegistry` 也读不到。写侧同步校验并显式报错
- **`ANDROID_HOME` 指向错误**：6 个 Android 组件都设 `install_root: android-sdk`，
  故 SDK 根是 `{{install}}\..\..`；ndk / platform-tools / platform 三个写成 `..`（组件自己目录），
  导致 Gradle Android 插件与 sdkmanager 找不到同级组件
- **Haskell 的 `GHC_HOME` 是不存在的变量**：GHC 没有这个变量，是照 JAVA_HOME 类推出来的
- **Flutter 缺 `FLUTTER_ROOT`**、**Java 缺 `JDK_HOME`**、**Deno 缺 `DENO_DIR`**
  （`DENO_INSTALL` 只是安装脚本用的，运行时读的是 `DENO_DIR`）
- **PHP / .NET / Python 变量补全**：.NET 补 `DOTNET_ROOT_X64`（官方有架构后缀优先级）、
  Python 补 `PYTHONUSERBASE` 与 `PIP_CACHE_DIR`（避免跨版本污染）
- 删除死代码 `env.WriteEnvFragment`（全仓无调用方）与 `installer.removeEnvFragment`
  （删一个从未被写过的文件）
- 前端依赖漏洞 7 → 0（含 2 个 critical：tinypool 的 Prototype Pollution → RCE）

### Infrastructure

- CI 的 Go 版本同步到 1.27；交叉编译门禁不再拉入 CGO 后端；
  release job 不再交叉编译 linux-arm64（交由原生 arm64 runner）
- macOS 构建显式开启 CGO（arm64 runner 上 `GOARCH=amd64` 会关闭 CGO）
- 修复 `TestF14` 对照组在 CI 上的时序假设失效（chunkedSlowServer 改为无限流，
  使「Cancel 时下载仍在进行」成为确定性前提而非时间猜测）

## [1.0.0-alpha.1] - 2026-10-24

### Added
- Spec 039：dsh（DeepSeek Harness）安装管理——分类对齐 OpenCode（development_tools），默认 npm 托管安装（`install_kind: npm`：`npm install -g --prefix <installDir>/<version>`，npm 负责依赖树解析、registry 完整性校验与启动器生成）；安装前 npm 依赖预检（xdev 管理的 Node 优先，缺失/版本不足硬失败并提示 `xdev install node`）
- npm 安装通道（`internal/installer/npm_install.go`）：CLI（InstallWithProgress 跳过归档下载）与 GUI（下载管理器旁路 + installProgress/installError 事件契约一致）双管线；npm 全局布局（win `dsh.cmd` / unix `bin/dsh`）命中既有 shim 分发与完整性门，零下游改动
- 预发布版本语义（SemVer 2.0.0）：`util.CompareSemver` 升级（`1.0.0 > 1.0.0-rc.1 > 1.0.0-alpha.9`、rc-only 版本流 latest 可推进）；嗅探合并的 latest 预发布抢占保护（稳定 latest 不被 alpha/rc 劫持）+ `stable` 标志按版本串推导；dsh remote pattern 接受 `-(alpha|beta|rc).N`（npm `latest` dist-tag 实测为 rc，不接受则零版本可发现）；GUI 版本列表预发行徽标（i18n en/zh）

### Changed
- 嗅探合并发现的稳定版本现在写入 `stable: true`（此前隐式 false）；npm-kind 版本豁免「归档 URL 未验证」确认弹窗（无归档下载，npm 自带完整性校验）

### Fixed
- 代码审查 13 项（详见 `docs/audits/2026/2026-10-24-code-review.md`），已修复 11 项：
  - S-03（严重）npm `.cmd` 启动器路径经 `cmd /c` 二次解析可导致命令注入——含 `&|<>^%"` 等元字符的路径现被拒绝（与 Round 9 junction 同类缺陷）
  - S-01 `SaveCache` 抢锁失败仍写盘（静默丢写）→ 改为拒绝无锁写
  - S-02 `UpdateShimDirInUserPath` 吞掉写错误并无条件广播 → 失败时记日志且不广播
  - S-04 `WriteSystemPath` 缺 32767 长度闸门（两处调用点漏检）→ 闸门下沉到函数内部
  - S-05 `SetEnvVar` machine 分支不持 `userpath` 锁直写 → 纳入锁
  - S-08 shim `defer release()` 在 panic 时清零 kill-on-close（产生孤儿）→ 改为正常返回后才释放
  - S-09 `SwitchToLocalVersion` 缺受管树校验 → 补齐
  - S-10 下载 URL 无 SSRF 私网拦截 → 新增校验（放行回环、DNS 失败放行）
  - S-13（严重）fallback URL 绕过 SSRF 校验 → fallback 同样校验（S-10 的自身缺口）
  - S-11 删除死代码 `_setxSetPath`；S-12 删除 `maxWinRegValueLen` 局部常量 shadow

## [0.4.0] - 2026-09-02

### Added
- Spec 037 阶段 1：批量「全部更新」并行安装——确认后的清单交 `useParallelBatch` 引擎并行执行（每项独立 `useInstall` 会话，事件按 taskId 路由）；`InstallProgressDialog` 多项模式（每项独立进度行 + 逐项取消 + 批量取消）；工具栏后台指示器批量时显示「N 项安装进行中」；守卫测试 35 用例（useParallelBatch 8 + useBatchUpdates 18 + 集成 5 + 对话框 4）+ e2e 并行基线。单装路径不变；批量期间单装入口 busy 守卫。旧「void 返回中止批量」缺陷在新架构下结构性不可能（专项不变量测试锁定）
- i18n parity 守卫测试：en/zh 键集与占位符集必须完全一致（运行时 fallback 不再掩盖翻译缺失）
- 环境变量写入门禁（L-3 收口）：`SetEnvVar`/`DeleteEnvVar` 的 PATH 路由到专用 writer（32,767 字符上限 + RMW 锁）、machine 层白名单、值长度上限；守卫测试 `setenvvar_guard_test.go`

### Changed
- CI 发布产物收口（单二进制语义对齐）：删除 macOS GUI DMG 重复构建（与 CLI DMG 同一产物）；AppImage 产物名 `xdev-gui-*` → `xdev-*`（desktop Exec/AppRun 同步）；release 段构建去掉过时的 `-tags gui`
- 前端事件监听器泄漏修复（TOFU/self-update 首次向导后双弹窗）；`repair()` 重置 `cancelRequested`（真实错误不再被吞成 Cancelled 且中断批量）；profile 导出导入路径分隔符按平台自适应

### Fixed
- 安全审计 21 项修复（C-1 env shell 注入 / H-1~H-3 plugin 三连 / M-1 token 分页外泄 / M-2 自更新重定向 / M-3 安装锁 / M-4 续传符号链接竞态 / M-5 manifest 名穿越 / M-6 docker exec 注入 / L-1 SSH 字段 / L-2 PATH TOCTOU / GUI catMu / sshpass 密码泄漏等，详见提交 07415a7）

## [0.3.3-rc.2] - 2026-08-23

### Fixed
- Spec 035（三轮审查问题修复）：
  - F1（P0）：零参数工具 shim 调用不再误启 GUI——`main.go` 无参分支先做 `shim.MaybeExec` 判定（python/node 等零参数进 REPL 的工具恢复正常分发，同时收窄不可信 PATH 下同名 shim 启动完整 GUI 的攻击面）
  - F2（P0）：GUI「一键更新全部」重构为 `useBatchUpdates` composable——unverified（嗅探）版本逐项弹确认（FR-018），拒绝则跳过该项继续；事件驱动等待（`useInstall.waitForCompletion`，含取消/完成竞态防护）；取消=中止整批；结束报真实汇总「成功 N/失败 M/跳过 K」（原实现对 unverified 版本弹非阻塞对话框后立即 return，循环空转 + 无条件假成功）
  - F3/F4（P1）：TUI 更新队列三处残留清理（安装失败/取消/registry 未命中，防止后续普通安装意外续跑「全部更新」）；更新视图内安装进度 overlay 替换渲染 + Esc 先取消安装再退视图 + 批量上下文「Batch N/M」行
  - F11（P2）：下载轮询 1h 超时后补发 `installError` 事件——前端进度框/批量等待器不再永挂（此前仅能手动 Cancel 逃生）
  - F5（P2）：`PhaseInstalling` percent=-1（滚动安装器不定进度）不再显示 `-1%`/清空进度条——保持最后已知进度只换文案，与 GUI 语义对齐
  - F6/F7（P2）：自更新 `findAsset` 删除 partial-match 回退（防误选 `xdev-gui-*` 归档）；`parseSHA256SUMS` 仅认 `sha256sum` 标准双空格（安全路径收紧）
  - F8/F10（P2）：`runXdevCommand` 自身可执行路径优先（PATH 同名注入面收窄）；`resolveKind` 导出为 `installer.ResolveKind`（含 nil-guard），GUI 阶段判定与安装器同源防漂移
  - F9（P2）：TUI help 键表补 `[x] Available updates (sniff)`
- Spec 036（第二轮审查问题修复）：
  - N1（P2）：TUI 更新视图内联渲染错误信息——批量更新失败/registry 未命中后错误直接可见（此前需退回主视图才能看到 errBar）
  - N2（P2）：安装互斥守卫——安装进行中所有二级安装入口（更新列表行/更新徽标弹层/详情抽屉/版本管理器/修复）在 `startInstallConfirmed`/`startRepairConfirmed` 收敛点统一拦截并提示「已有安装进行中」；批量更新入口守卫 + 逐项预检（Run in Background 后门场景批量让位）+ 启动失败不再误关正在进行的安装进度框
  - N3（P2）：删除 TUI 更新视图「更新中: N/M」死代码（Spec 035 F4 替换渲染后不可达；批量进度由 overlay `Batch N/M` 行单一承担）
  - N4（P3）：批量更新逐项完成不再闪烁进度框（batchActive 期间关闭时机由批量循环统一管理，错误路径同）
  - N5（P3）：批量中途取消只显示汇总文案（单装取消文案不变）
- Spec 036 冒烟反馈修复（第二轮）：
  - 安装互斥补遗（#1）：`doInstall`/`repairInstall` 原静默早退——安装进行中点击更新列表行/徽标弹层/抽屉/版本管理器等二级安装入口无任何反馈；现统一弹「已有安装进行中」提示
  - 批量更新中止回归（#2，P0）：Spec 036 N2 重写引入——`startInstallConfirmed` 为 void 返回（undefined），批量适配器 `if (!started)` 误把成功启动当失败计，且 `active` 仍 true 致下一项预检 break，批量在首个项后即中止（成功0 失败1 跳过0）；`startInstallConfirmed`/`startRepairConfirmed` 现返回 `started` 布尔。集成契约测试锁定该回归
  - 安装失败错误信息增强（#3）：错误 toast 存留 3s→10s 并内嵌「复制错误」一键复制完整错误文本（clipboard 不可用静默降级）；覆盖事件失败（handleInstallError）与启动失败（startInstallConfirmed/startRepairConfirmed）两条路径
- Spec 036 冒烟反馈修复（第三轮 #4）：
  - 后台安装进展查看：安装转入后台后 toolbar 显示进度指示器（`installing && !showProgress` 时），实时展示百分比，点击重新打开 InstallProgressDialog 恢复完整进度/暂停/继续/取消控件——此前后台化后进度完全黑盒，仅结果 toast 可见
  - e2e 守卫用例：`backgrounded install shows a toolbar progress indicator that reopens the dialog`（指示器出现/实时百分比更新/点击重开/重开后指示器隐藏全链路）
- Spec 036 冒烟反馈修复（第三轮 #5）：
  - 老旧 xdev 副本检测/替换失效修复（双重根因）：①stale 判定从「CompareSemver 旧于才替换」改为**镜像一致性语义**（探测版本 ≠ 当前版本即替换）——旧分支 1.2.2-alpha 副本因版本号断层（1.2.2 > 0.3.3）被误判「更新」而永不替换（实测 E:\XDev\xdev.exe 停留 7/10 版本）；②`EnsureSelfInstalled` 新增孤儿清理——删除双二进制时代残留的 `xdev-gui.exe` 副本（单二进制后无任何调用方）。探测失败/自指/同版本跳过等安全边界全部保留
  - 守卫测试：`TestReplaceStaleCLICopy_HigherVersionStillReplaced`（版本号断层场景，旧逻辑红新逻辑绿）+ `TestRemoveObsoleteGUICopy` ×3（清理/无残留 no-op/仅精确文件名）
- Spec 036 冒烟反馈修复（第三轮 #6）：
  - current/xdev junction 长期指向旧版修复：`InstallSelf` 此前仅在根副本**缺失**时被调用（`EnsureSelfInstalled` 的 cliMissing 分支），导致 `install/xdev/` 永远停留在 1.2.2-alpha 目录（内含 0.2.0 二进制）且 `current/xdev` junction 从不更新——PATH 解析 `xdev` 命中 0.2.0 旧版。现 `EnsureSelfInstalled` 无条件执行 `ensureCurrentSelfInstall`（InstallSelf 幂等：当前版本已安装则 no-op，新版本创建目录并切换 junction）
  - 单二进制收口补遗：`InstallSelf` 不再向版本目录复制 `xdev-gui.exe`（双二进制残留逻辑，连同 `installGuiBinary` 函数删除）；守卫测试 `TestInstallSelf_NoGuiBinaryInVersionDir` + `TestInstallSelf_JunctionSwitchesFromLegacyDir`（junction 从旧版目录切换）
  - 并行安装立项（用户反馈批量更新为串行）：调研结论——后端 dlMgr 已支持并发下载 + 独立 taskID 事件流；主要障碍在前端 useInstall 单会话状态机 + InstallProgressDialog 单项 UI。**Spec 037 已立项**（`.scratch/037-parallel-installs/spec.md`），不在本轮实施

## [0.3.3-rc.2] - 2026-08-23

### Changed
- Single-binary model: `xdev`（GUI + TUI + CLI 一个可执行文件，Spec 030 双二进制合并）——无参数启动 GUI、`xdev tui` 进入 TUI、其余参数走 CLI；`wails.json` 输出名 `xdev`；`build.ps1`/`build.sh`/`scripts/build.bat` 单产物构建；`xdev gui` 改为进程内启动（不再 exec 独立 xdev-gui）；EnsureSelfInstalled 只维护单副本；GUI 自更新目标为自身
- Fixed: black console window flash (~1s) on GUI startup — stale-copy probe (`self version`) exec now hides the child console (`util.HideWindow`)
- Removed orphan `cmd/xmdevenvmt` entry (documented build/ops commands updated to root `go build .` / `go run .`)
- Self-update GitHub token resolution reuses `remote.EffectiveGitHubToken` (was a weaker `os.Getenv` subset)
- Sniff channel hardening: GitHub `per_page` 100 + floor-based pagination early-stop; npm version discovery via streaming JSON (no 16 MiB body cap) + npmmirror fallback (12s primary timeout); startup sniff window 60s→120s
- Auto sniffing skips tools the user ignored for updates (ignored_tools.json, kind=update)
- Rust rolling-installer manifests (`install_args {{version}}`) can now derive archive URLs for sniffed versions; manifest merge upserts missing archives for previously-merged versions

## [0.3.3-rc.1] - 2026-08-22

### Added
- Spec 034: startup self-update check with configurable policy — `core.self_update_mode` (`prompt` default / `auto` / `notify`) and `core.self_update_stale` (replace stale local CLI copy on startup); 24h rate limit, version-ignore memory (`self_update_ignore.json`), non-TTY graceful downgrade, GUI three-choice dialog (Update / Ignore & Continue / Quit) and CLI interactive prompt on TUI/GUI entry points only
- `update.ApplyTo` — apply update to a specified target binary (GUI updates the CLI next to itself instead of the running GUI shell)

## [0.3.3] - 2026-08-22

### Changed
- Spec 033: tool metadata unified into the single source of truth `internal/toolmeta` — five duplicated maps/functions removed across `internal/env` and `internal/gui`:
  - tool→binary-name maps (`scanRules` / `getBinNamesForTool` / `findBinaryInDir` inline map) merged into one table (`ToolMeta.BinNames`)
  - `--version` flag tables (env 24 keys vs gui 36 keys) merged into `ToolMeta.VersionArg`; gradle now consistently uses `-version`
  - version extraction (`extractVersionFromOutput` variants) merged into `toolmeta.ExtractVersion*` (prefix union: env `java ` / `Python ` + gui `yarn ` / `cmake version ` etc.)
  - common install paths (`getCommonInstallPaths` env 12-tool subset vs gui superset) merged into `toolmeta.CommonInstallPaths`
  - "inside xdev dir" judgment: `env.isInsideXdevDir` removed, now delegates to `hostscan.InsideDir`
- Env local scan now covers the union of both sides (23 → 41 scan targets) — a correctness improvement, not a regression
- Version bumped to 0.3.3; local tag `v0.3.3` created (not pushed)

## [0.3.2] - 2026-08-20

### Changed
- README rewritten for 0.3.2: current CLI, 51 catalog tools, GitHub Releases download links, retired AI/task/lockfile commands removed
- Specs 010–028 metadata aligned to the governance validator (English states, SemVer, Approval History)

### Fixed
- Ignore-list tests no longer write into the real `$XMDEV_HOME/cache/ignored_tools.json` (Go reappearing after `go test`)
- Sniff-discovered versions (all-zero SHA-256) no longer fail install with "hash tampering" against the signed manifest
- Sniff completion badge clears after the notification panel is opened
- Install confirmation no longer waits on a full category scan / catalog reload (was >3s)
- Yarn/pnpm install methods now detect xdev-managed Node (`corepack` / `npm` on E:) even when PATH still points at a stale D: copy; enable/install stays available for the xdev Node
- Available versions (OpenCode sniff list) sort by release date then semver, newest first — sniffed versions are no longer stuck at the bottom of YAML-append order
- Installed pipx no longer keeps an Install button; yarn/pnpm only keep Enable/Install when a local copy exists but xdev Node has not enabled them yet
- Package-manager install/enable/update now lives in the tool detail drawer, matching language/IDE/build tools; catalog rows only open detail and favorite
- `UpdatePackageManager` no longer treats yarn/pnpm as standalone catalog tools (was `InstallTool("pnpm")`); HasUpdate is not set for sub-tools
- Local-scan ready signal fires after disk cache load; host scan starts immediately instead of waiting 15s
- Local PATH detection uses the App config shim dir (no extra `config.Load("")`)
- Windows shims recopy when size matches but mtime differs (upgrade no longer skipped)
- Host-scan module (`internal/hostscan`) skips xdev shim copies on PATH and returns every PATH instance (not just `LookPath` first hit); LookPath walks past a leading shim-dir hit instead of giving up
- Windows shims prefer a hard link to xdev.exe (fallback copy)
- GUI install wrappers share one `InstallSpec` (`tool` / `package-manager` / `sub-tool`); App stays a Wails adapter over catalog, install-session, host-scan, prefs
- AppContent is a desktop shell: package-manager / sniff / window chrome / notifications live in composables
- Leftover `one-api/` working-tree copy is not part of the product (`THIRD_PARTY.md`)
- Independent package managers (uv) overlay the xdev current install even when PATH still points at a D: leftover; both instances are listed
- Updating a sniff-discovered package-manager version confirms in the frontend (no hidden native dialog / "cancelled by user")
- Install paths copy on click; the separate Copy button is gone
- Package-manager instance rows with more than one copy offer Switch to xdev / Switch to local
- Config tests write `config.yaml` with `filepath.Join` so explicit YAML loads on Unix
- `ValidateSubPath` resolves Darwin `/var` → `/private/var` for not-yet-created children (shim install no longer false-escapes)
- GUI tests that used Windows-only paths (`C:\XDev`, `/bin/true`) now pass on macOS/Linux

## [0.3.1] - 2026-08-18

### Fixed
- npm sniff no longer fails on large packuments (opencode-ai): request abbreviated registry metadata
- Archive URL templates now cover tag-only GitHub downloads (uv / ninja / zed), so sniffed versions get installable URLs
- Yarn/pnpm can be installed via `npm install -g` as well as `corepack enable`
- Unignore by name+kind drops all matching rows; local-scan results refresh after unignore

## [0.3.0] - 2026-08-18

### Added
- Three-round security hardening: isolate/remote command allowlist, proxy URL validation, profile env-var whitelist, Lua `load()` sandbox, SSH target sanitization
- Lua plugin loader now accepts nested `PLUGIN.available` / `PLUGIN.archive` templates
- Windows PATH cleanup removes leftover `TestInitConfig_NoDeadlock` entries (case-insensitive)

### Changed
- `klauspost/compress` 1.17.11 → 1.18.7
- Frontend `nanoid` 3.3.16 → 3.3.18

### Security
- SSRF redirect hops re-validated; DNS resolution fail-closed
- Git clone rejects `http://` / `git://` for third-party manifests
- SSH password passed via `SSHPASS` instead of process arguments

## [0.2.0] - 2026-07-25

### Added
- CI integration tests gate: `go test -tags=integration ./...` on three platforms (Spec 002)
- CI Go version consistency check: `scripts/ci/check-go-version.sh` (Spec 001)
- Release signing hard gate: "Verify signing secrets" step fails if keys missing (Spec 003)
- Build provenance: GitCommit and BuildDate injected via ldflags (Spec 004)
- SHA256SUMS checksum files for release artifacts (M2)
- macOS AMD64 GUI DMG (`xdev-gui-darwin-amd64-v0.2.0.dmg`) (M1)
- GitHub Actions upgraded to v7 (checkout/setup-go/setup-node/upload-artifact) for Node 24
- Go toolchain upgraded to 1.25 (go.mod + CI)
- `internal/version/version.go`: added `GitCommit` variable for ldflags injection

### Changed
- `search` command no-match now returns exit code 0 with message instead of exit code 1 (Spec 007)
- Frontend dependencies: vite 5→6, vitest 2→3, @vue/test-utils downgraded to fix brace-expansion vulnerability
- Go dependencies: golang.org/x/crypto v0.33→v0.54, x/net v0.35→v0.57, x/sys, x/text, x/sync upgraded
- Release workflow: incremental build (skip existing assets), `overwrite_files: false`
- Release workflow: tags trigger (`on.push.tags: ['v*']`)

### Fixed
- Dependabot vulnerabilities: 26→0 (golang.org/x/crypto, x/net, esbuild, postcss, brace-expansion)
- Integration tests cross-platform: `SysProcAttr` type mismatch on non-Windows runners
- `TestFindAsset_NoMatch` false positive on Linux (GOOS/GOARCH substring fallback)
- `TestProbeRangeLengthLimitAndHEADFallback` flaky server-side byte counter (OS-dependent buffering)
- Windows drive-letter tests gated behind `//go:build windows`
- `TestHostArch` platform expectations aligned with Linux/Darwin x86_64 mapping
- ZipSlip absolute-path assertion hardened for cross-platform extractors
- Windows GUI process helpers split behind build tags
- `EnvPair`, `UpdateShimDirInUserPath`, `DetachConsole` Windows API isolation

### Security
- Ed25519 signing key pair configured in GitHub Secrets
- Release artifacts now signed (manifest signatures + public key injection via ldflags)
- All release jobs verify signing secrets before proceeding

## [0.1.0] - 2026-07-24

### Added
- Initial GitHub Actions release with 12 multi-platform artifacts
- Linux: tar.gz (amd64/arm64), deb (amd64/arm64), rpm (amd64/arm64)
- Windows: zip (amd64)
- macOS: tar.gz (amd64/arm64), DMG (arm64 CLI + arm64 GUI)
- AppImage: x86_64 GUI variant
- Incremental release builds (skip existing assets)
- Governance CI with constitution/spec/approvals/links validation
- Spec 005: Host state snapshot implementation (`internal/testkit/hostsnapshot`)
- Governance stabilization Tasks 1-10 completed and archived