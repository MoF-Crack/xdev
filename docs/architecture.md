> **文档类型**：长期参考文档
> **状态**：Living
> **更新范围**：Spec 040，2026-11-17

# XDev 架构文档

## 1. 产品与运行入口

XDev 是使用 Go 的跨平台开发环境版本管理器，提供 Cobra CLI、Bubble Tea TUI 和 Wails v3.1.0 + Vue 3 GUI。Go 最低版本以 go.mod 为准，当前为 1.27.0。

正式分发保持单二进制：`xdev[.exe]` 同时承担 CLI、TUI、GUI 和托管工具 shim。零参数调用先检查 shim 身份；真实工具别名进入工具进程，XDev 自身才进入 GUI。`xdev tui` 与 `xdev gui` 是同一程序的子命令。

构建形态明确分开：

| 构建条件 | 用途 | 前端资源 |
|---|---|---|
| `desktop,production` | Windows/macOS 正式桌面单二进制 | 嵌入真实 dist |
| `desktop,production,gtk3` | Linux 正式桌面单二进制 | 嵌入真实 dist |
| 不带 `desktop` | 开发测试、维护命令、CLI/TUI/shim | 不依赖 dist；GUI 返回明确错误 |
| `desktop,bindings`，Linux 可另加 `gtk3` | 静态生成 Wails 绑定 | 使用空资源 stub，不启动 GUI |

维护构建不是完整桌面发布产物。绑定生成标签不得进入正式发布构建。Wails 3 决策与用户授权来源见 [ADR-0007](adr/0007-wails3-desktop-runtime.md)；历史 ADR 保留当时的背景。

## 2. 目录与模块边界

```text
main.go / main_gui.go       单二进制分派与 GUI 启动器
main_bindings.go            bindings 条件下的无副作用入口
internal/
  cli/                     命令路由、配置上下文、GUI 启动回调注入
  gui/                     App 服务 facade、会话与桌面适配
  installer/               下载、校验、安装、激活、卸载
  manifest/                YAML、Catalog、缓存与 latest 规则
  plugin/                  Generic/Lua/Vfox 与注册表
  plugins/java/            Java 特定归档解析
  hostscan/                PATH 快照扫描与 basename 索引
  discovery/ / remote/     嗅探调度与远程版本发现
  downloader/ / extractor/ 下载及归档解包
  env/ / shim/             环境写入与工具分派
  config/ / project/       全局配置与 .xdev.yaml 项目配置
  isolator/ / orchestrator/隔离环境与远程实例
  signer/ / update/        清单信任与自更新
  util/ / resolver/        路径/进程工具与版本解析
frontend/
  bindings/                正式生成的 Wails v3 TypeScript 绑定
  src/desktop/             事件、窗口与兼容 namespace 适配
  src/composables/         安装会话、批量执行与页面状态
  src/AppContent.vue       桌面布局、导航与交互接线
scripts/build.mjs          本地及 CI 共用构建入口
scripts/release/           更新归档及平台包生成
build.ps1 / build.sh       薄构建入口
```

主要依赖方向为入口 → 表示层 → 业务模块 → 基础设施。下表是关键生产依赖，不是所有 import 的穷举：

| 包 | 主要内部依赖 | 职责 |
|---|---|---|
| cli | config、discovery、env、installer、manifest、plugin、project、pm、orchestrator、shim、signer、tui、update | 命令编排；通过回调进入 GUI，不直接依赖 gui |
| gui | config、installer、manifest、plugin、hostscan、localmigrate、toolmeta、downloader、env、discovery、isolator、orchestrator、update | 绑定 facade 与桌面业务状态 |
| installer | config、downloader、extractor、manifest、plugin、mirror、env、shim、signer、util | 核心安装和激活流程 |
| manifest | util | 清单集合、排序、合并、校验及缓存 |
| plugin | manifest、resolver、util | 工具元数据与工具专有行为 |
| hostscan | 标准库 | PATH 全量扫描，跳过 shim 目录 |

`internal/lockfile` 及旧 service/task 模块不属于当前依赖图。项目配置使用 `.xdev.yaml`；不要以旧绑定列表推断仍有 `GetLockfile`、`RunProjectTask` 或服务管理方法。

## 3. 启动与生命周期

```text
main.init
  → 注册内嵌 manifests 与签名
main
  → 无参数：shim.MaybeExec 优先；未匹配则 runGUI
  → 有参数：注册 SetConfiguredGUILauncher(runGUIWithConfig)
             → Cobra 解析显式 home 与 --no-path-fix
             → CLI / TUI / GUI 子命令
runGUIWithConfig(cfg, noEnvWrite)
  → LoadCatalogCached → cli.LoadRegistry
  → NewAppWithOptions(cfg, catalog, registry, RunOptions)
  → gui.Run
      → application.New + application.NewService(app)
      → 创建隐藏窗口、设置 desktopRuntime
      → ServiceStartup(ctx) → startup → root context
      → WindowRuntimeReady → domReady → 显示窗口、加载/刷新本地缓存
      → ServiceShutdown → shutdown → 取消后台工作、清理下载
```

无参数 GUI 自行加载默认配置；`xdev --xmdevenv-dir <home> --no-path-fix gui` 使用 CLI 已解析的配置及策略，不重新调用默认配置加载覆盖显式 home。WebView 数据目录位于该 home 下。

`desktopRuntime` 集中事件、窗口显示、文件/目录选择和确认框。后台业务不使用框架 context 发事件。异步确认等待通过 buffered one-shot 回调与 context 管理；安装确认需要响应相应会话取消。

## 4. 安装会话与核心工作流

### 4.1 会话协议

`StartInstall(InstallSpec)` 只返回受理身份；成功受理不代表安装完成。InstallSpec 包含 sessionId、tool、version、form、confirmed、force、kind、method。

每个 InstallSession 保存唯一 sessionId、资源 key、状态、phase、progress、控制能力和递增 revision。前端先订阅 `installSession` 再启动，随后用 `GetInstallSession` 补齐快速完成或响应不确定的情况；旧身份及旧 revision 不能污染重试。

| 状态 | 含义 |
|---|---|
| accepted | 已登记，尚未完成 |
| downloading | 下载阶段，暂停能力以 canPause 为准 |
| cancelling | 取消已受理，等待 worker 返回和清理 |
| committing | 已越过提交门，用户取消不再可用 |
| completed | 安装流程返回成功 |
| error | 失败终态 |
| cancelled | 已确认取消的终态 |
| needs-repair | 发现已有残缺安装，需要显式修复 |

`BeforeCommit` 与取消请求共用会话锁：取消先赢则禁止安装树写入/激活，提交先赢则拒绝用户取消。`CancelInstall` 是受理确认，前端必须继续等待终态。资源在 worker 返回前保持占用；终态只发布一次。安装相关查询缓存在终态事件发出前失效。

Legacy `InstallTool*`、`RepairInstall`、`InstallPackageManager`、`InstallSubTool` 保留接口并等待实际会话完成。新前端使用 StartInstall；同一 coordinator 覆盖单装、修复、PM 和批量的预检/确认阶段。批量拥有一个前端 lease，每项拥有独立后端会话。

### 4.2 安装器职责

```text
依赖完整性检查 → ResolveVersion → 按平台及 Form 选择 archive
  → 校验工具/版本/安装路径
  → 检查已有安装完整性
  → 下载、镜像回退、SHA/签名检查
  → 工具安装锁 + BeforeCommit
  → force 替换 / 按 archive、executable、script、npm 安装
  → 完整性检查、PostInstall
  → 释放安装锁 → Activate（自行取得工具锁）
```

Force 不应在下载与验证前删除旧安装。形态选择传到实际归档解析；已有安装及缓存身份必须与所选归档保持一致，相关行为由安装器回归测试验证。安装完成但激活失败可返回带 ActivationError 的结果，由 GUI 发出激活警告。

Kind 为 tool、package-manager、sub-tool。corepack/npm 的资源归属为 node，pip 为 python；不能仅以 UI 中的 yarn/pnpm 别名判断是否冲突。官方脚本和 sub-tool 命令进入提交阶段后不提供用户取消；应用退出的 context 生命周期仍需贯穿实际子进程。

### 4.3 Shim 与 shell hook

Shim 依据可执行文件 basename 查找 `current/<tool>`，必要时通过反向索引查找 npm→node 等映射。Unix 使用 `unix.Exec` 覆盖进程，Windows 转发 stdin/stdout/stderr 和退出码。Windows 优先 hard link、Unix 使用 symlink，复制为兼容回退。

Shell hook 在进入目录时查找最近的 `.xdev.yaml`，输出适合当前 shell 的环境修改。`--no-path-fix` 禁止宿主持久化环境写入；GUI 安装器、环境编辑、PATH 模式、hook、profile 和初始化/迁移副作用遵守传入策略。

## 5. Plugin 契约

```go
type Plugin interface {
    Name() string
    DisplayName() string
    Description() string
    Homepage() string
    Category() string
    Manifest() *manifest.Tool
    ResolveVersion(spec string) (string, error)
    DefaultVersion() string
    ArchiveFor(version string) (*manifest.Archive, error)
    InstallDir(root, version string) (string, error)
    Shims() []string
    EnvVars(installPath string) map[string]string
    PathAppend(installPath string) []string
    PostInstall(ctx context.Context, installPath string) error
    Deactivate(installPath string) error
    RequiredPlugin() []string
}
```

Generic 使用 YAML；Java 覆盖特定归档解析；Lua 与 Vfox 提供扩展行为。插件提供元数据和钩子，安装器拥有下载、校验、解包及激活。InstallDir 的错误必须传播，不能把不安全路径替换成一个伪目录继续安装。

## 6. 配置与 Catalog

配置保留现有 `core`、`path`、`proxy`、`mirror` 等分区及 install/current/shims/cache/downloads/env/manifests 目录布局。显式 `--xmdevenv-dir` 优先于环境变量和默认 home 探测。切换 home 时必须清空派生路径并重新 ResolvePaths，不能只修改根字段。

清单定义 name、versions、archives、latest、shims、env_vars、remote、form_factor 等字段。归档 SHA-256 的全零值是占位符，不能称为已验证；下载策略依据来源、签名和 TOFU 规则处理。

Catalog 加载与缓存共用顶层和 `third-party/` 来源集合，不递归其他子目录。同名磁盘工具按顶层先、third-party 后处理。没有内嵌清单时，缺失或无清单的目录可以回退到开发仓库 manifests；仅含 third-party 清单不触发空目录回退。

存在内嵌清单时执行 embedded-first 合并：内嵌同版本 archive/hash 等字段保留，磁盘新版本追加。磁盘 latest 只有在合并结果中存在、可安装且更高时才能推进；稳定默认不跳预发布，同通道预发布可以推进，无法可靠排序的任意标签不用于自动晋升。原有效默认（含 LTS/首项回退）不能被追加排序意外改变。

缓存指纹包括来源根、相对路径、mtime 和 size，因此冷热缓存具有相同集合及同名优先级。它不逐文件计算内容 hash；保留大小及时间戳的外部修改不在该元数据模型的保证范围内。

## 7. 桌面前端与绑定

前端继续使用 Vue 3 Composition API 与 Naive UI。正式生成目录为 `frontend/bindings`，运行时依赖固定为 `@wailsio/runtime` 3.1.0。`frontend/src/main.js` 装载生成绑定和 desktop bridge 后挂载 Vue。

`frontend/src/desktop/bridge.js` 将 v3 `Events.On` 的 event.data 转为页面已有 payload 形式，并负责订阅释放；窗口动作经同目录 adapter 调用。兼容 `window.go.gui.App` / `window.runtime` namespace 是页面迁移边界，不是自行实现一套 RPC。

当前主要绑定分组如下（完整签名以生成文件为准）：

| 分组 | 代表方法 |
|---|---|
| 查询 | GetCategories、GetToolsByCategory、GetInstalledVersions、GetAvailableVersions、PreviewInstall |
| 会话 | StartInstall、GetInstallSession、CancelInstall、InstallTool、RepairInstall |
| 激活/下载 | UseVersion、UninstallVersion、PauseDownload、ResumeDownload |
| 配置/环境 | GetConfig、SetPathMode、GetEnvVars、SetEnvVar、CleanAllInvalidEnvVars |
| 本地迁移 | ScanLocalEnvironments、MigrateLocalTool、MigrateAllLocalTools |
| 隔离/远程 | GetIsolateStatus、IsolateExec、ListRemoteInstances、CreateRemoteInstance |
| 初始化/profile | IsInitialized、InitConfig、GetDrives、ExportProfile、ImportProfile |

## 8. 安全与发布

- 归档解包进行目标路径约束，拒绝路径穿越及危险成员；失败清理残缺目录。
- 环境写入区分显式编辑与 manifest 提供的值；名称/受保护键和路径值分别校验，不把所有环境接口描述成同一白名单。
- Lua 扩展限制宿主文件、进程及模块加载能力；上下文和执行预算由插件实现控制。
- 内置清单使用 Ed25519 签名和版本/平台 hash 覆盖检查；第三方来源按签名/TOFU 策略处理。任意磁盘同名清单不能替换内嵌同版本可信归档。
- 发布签名目录由一个签名 job 产生，包含 manifests、签名和公钥，全部平台下载相同输入再构建。
- `--release` 构建要求公钥且强制签名覆盖；缺签不得通过 skip。私钥仅用于签名步骤，不能进入发布产物。
- 更新归档成员为根目录 `xdev` 或 `xdev.exe`。打包后由 updater consumer 验证解包，汇总完整平台归档后生成 SHA256SUMS。

## 9. 构建与验证

安装固定生成器：

```sh
# Windows / macOS
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0
# Linux（使用项目已选定的 GTK3 后端）
go install -tags gtk3 github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0
```

Wails v3 配置为 `wails.yaml`，旧版 `wails.json` 不再作为构建依据。该配置提供原生开发所需元数据；本轮未验证直接运行 `wails3 dev`。直接 `wails3 build` 也不保证与项目的公钥注入、签名覆盖和发布打包流程等价。

受支持的本地及发布构建入口统一到 `node scripts/build.mjs`：

```sh
# Linux / macOS
./build.sh
# Windows PowerShell
.\build.ps1
.\build.ps1 -SkipBuild  # 只执行下载源审计，跳过构建
.\build.ps1 -SkipAudit  # 跳过下载源审计，继续构建
```

PowerShell 包装脚本只声明 `-SkipBuild` 和 `-SkipAudit`。Node 入口负责生成绑定、安装前端依赖、构建资源、注入版本/公钥并编译；可通过 GO、NPM、WAILS3 指定真实工具路径。Linux 构建需要 GTK3 与 WebKitGTK 4.1 开发依赖，macOS 需要相应 Xcode Command Line Tools。

```sh
# 维护构建无需前端 dist
go build -o xdev-maintenance .
# 独立生成绑定；Linux 加 gtk3
wails3 generate bindings -f '-tags desktop,bindings' -ts -d frontend/bindings ./internal/gui
```

默认产物为 `build/bin/xdev.exe`（Windows）或 `build/bin/xdev`（Unix），同时生成版本化副本。正式构建默认标签包含 desktop；不可用维护构建替换发布流程。

验证分层包括默认 Go 单测/vet、desktop 标签编译及 GUI 测试、前端 unit/smoke/production build、隔离 HOME 的 integration 测试、可用宿主上的 CLI/shim 和原生窗口验证。新增会话测试需检查真实终态而非只检查受理返回。

截至 2026-11-17，集成负责人报告的实际验证范围：Windows desktop 构建、原生 WebView2 GUI 渲染、生成绑定调用、GetConfig 保持临时 home 和正常退出均通过；Linux 的 manifest/plugin/gui/installer 全包测试及 `desktop,production,gtk3` 构建通过。macOS 尚未实机运行；Linux 构建通过也不等于已完成原生窗口交互验证。具体命令与完整验收结果由 Spec 040 验收记录维护。
