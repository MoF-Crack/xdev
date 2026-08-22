# Spec 034: xdev 启动自检更新与旧版本副本替换

> **文档类型**：Feature Spec
> **状态**：Approved
> **文档版本**：1.0.0
> **创建日期**：2026-08-22
> **所有者**：xinyan
> **权威范围**：启动时更新自检（含三选交互）；启动时替换本地旧版 xdev CLI 副本；可配置默认选项
> **Constitution**：`1.0.0`

## 一、目标

补齐两个启动行为缺口（现状盘点见 1.1），**全部复用既有能力**，不重复造轮子：

1. **启动更新自检**：CLI（TUI/GUI 启动型入口）与 GUI 启动时按配置检查 xdev 新版本；发现新版本时按策略 `prompt`（三选：更新 / 忽略并继续 / 退出）/ `auto`（自动更新）/ `notify`（仅提示）处理，用户可自主配置默认选项。
2. **旧版本副本替换**：启动时检测 `XMDevDir` 根下的 xdev CLI 旧版本副本，自动替换为当前版本（可配置开关）。

### 1.1 现状盘点（代码证据）

| # | 需求 | 现状 | 结论 |
|---|---|---|---|
| G1 | 全局自检更新引擎 | `internal/update`：`Check()`（GitHub Releases latest + semver）、`Apply()`（HTTPS 强制 + SHA256SUMS 校验 + 200MB 上限 + Windows rename/回滚）完整 | ✅ 可复用 |
| G2 | 手动更新命令 | CLI `xdev self update`（`internal/cli/extra.go:47`） | ✅ 可复用（交互层可调它） |
| G3 | GUI 手动检查绑定 | `App.CheckForUpdate()`（`app_local_detect.go:245`）+ 前端 Dashboard/About/Settings 手动按钮 | ✅ 可复用绑定 |
| G4 | 配置项 | `core.self_update`（默认 true；`internal/config/config.go:140`），CLI `xdev config get/set` 可读写（`cli/config.go:158/184`）——**无消费者**（孤岛配置） | ⚠️ 补消费者 |
| G5 | 启动自动检查（CLI/GUI） | 均无。GUI `startup()` 只做 proxy/token/归档清理（`app.go:479`）；`domReady` 只调 `EnsureSelfInstalled`（补缺失不比对版本）/`FixLocalVersions`/嗅探 | ❌ 缺口 |
| G6 | 三选交互 | 无（GUI 手动检查仅 info 提示） | ❌ 缺口 |
| G7 | 本地旧版本副本检测+替换 | `EnsureSelfInstalled`（`app.go:844`）只复制**缺失**副本；不比对版本 | ❌ 缺口 |
| G8 | 版本比较 | `util.CompareSemver` / `ParseSemverParts` | ✅ 可复用 |
| G9 | 副本版本探测手段 | `xdev self version` 命令（`extra.go:143`）输出 `xdev <ver> (...)` | ✅ 可复用 + `toolmeta` 提取 |
| G10 | install/ 多版本自管理 | `installer.InstallSelf` + current junction | ✅ 不动（历史版本刻意保留） |

## 二、方案

### 2.1 配置（用户可自主配置，默认安全）

扩展 `CoreConfig`（`internal/config/config.go`）：

```go
SelfUpdate       bool   `yaml:"self_update"`        // 已有：启动自检总开关（默认 true，补消费者）
SelfUpdateMode   string `yaml:"self_update_mode"`   // 新增：prompt | auto | notify（默认 "prompt"）
SelfUpdateStale  bool   `yaml:"self_update_stale"`  // 新增：启动替换旧版本 CLI 副本（默认 true）
```

- `prompt`：发现更新 → 询问「更新 / 忽略并继续 / 退出」
- `auto`：发现更新 → 自动应用（复用 `update.Apply`），完成提示重启
- `notify`：发现更新 → 仅提示（不询问、不自动）
- CLI `xdev config set core.self_update_mode auto` 等效配置（扩展 `cli/config.go` 的 `coreField/setCoreField`，校验枚举值）

### 2.2 决策层（新，`internal/update/policy.go`，尽量薄）

启动编排统一放 `internal/update`（CLI/GUI 共用），交互层各自实现：

```go
// Action 描述一次启动自检的结果动作
type Action int
const (
    ActionSkip   Action = iota // 开关关闭 / 未到期 / 检查失败（静默）
    ActionAsk                  // mode=prompt 且有更新：交给交互层三选
    ActionUpdate               // mode=auto：直接 Apply
    ActionNotify               // mode=notify 或非 TTY 降级：仅提示
)

type Policy struct {
    Check               func(ctx context.Context, current string) (*CheckResult, error) // 注入点（测试用 fake）
    Now                 func() time.Time
    // ...
}

// CheckPolicy 读取配置 + 忽略记录 + 频率记录，返回动作与 CheckResult。
// 频率保护：cache/self_update_last_check.json 记录上次检查时间，24h 内跳过（ActionSkip）。
// 任何错误（网络/解析）→ ActionSkip + log（启动不阻塞）。
func CheckPolicy(cfg *config.Config, cacheDir string) (Action, *CheckResult)
```

- **不新增网络代码**：注入 `update.Check`（默认），测试注入 fake。
- **频率保护**：`$XMDEV_HOME/cache/self_update_last_check.json`（复用 cache JSON 读写模式），24h 内不重复检查——启动型入口每次都可能触发，必须限频。
- **忽略记忆**：`$XMDEV_HOME/cache/self_update_ignore.json` `{ "version": "0.3.3", "ignoredAt": "..." }`：prompt 模式选「忽略并继续」写入；同版本不再打扰；`LatestVersion` 变化后重新提示。

### 2.3 交互层

**GUI**（前端 + 复用 `App.CheckForUpdate` 绑定）：
- `startup()` 后 2s 异步 goroutine：`CheckPolicy` → ActionAsk 时弹 naive-ui Dialog 三选：
  - **更新**：调 Apply 等价流程（Wails 绑定执行）→ 成功提示重启（复用现有 message）
  - **忽略并继续**：写忽略记录，关闭对话框，继续使用
  - **退出**：`rootCancel()` + `WindowClose()`
- ActionNotify：info 提示（复用现有 `update_available_short` 文案）
- ActionUpdate：静默执行 Apply，成功提示重启
- 前端已有 `checkUpdate()` 手动函数（AppContent.vue:1060 调 `CheckForUpdate`），新增 `UpdateActionDialog` 组件或复用 naive dialog 直接组织，**不新增 Wails 绑定**（`CheckForUpdate` 返回结果已有 latest/current/hasUpdate）。

**CLI**（仅交互型入口，**不全局 PersistentPreRun**）：
- 挂载点：无参 `xdev`（TUI RunE，`root.go:84`）、`xdev tui`、`xdev gui` 的 RunE 开头（`internal/cli/extra.go` / `root.go`）
- 原因：其他子命令（list/install/use/which/... shim 形态）每次调用都做网络检查会造成明显延迟与干扰；仅交互型入口符合「启动 xdev」语义
- TTY 交互：`[U]pdate / [I]gnore / [Q]uit`（stdin 读取；Update → 复用 `self update` 流程；Ignore → 写忽略；Quit → `os.Exit(0)`）
- 非 TTY（管道/CI/`--no-path-fix`）：`ActionAsk` 降级 `ActionNotify`——打印一行提示，不阻塞
- `ActionUpdate`：直接执行 update.Apply，输出沿用 `self update` 文案

### 2.4 旧版本副本替换（扩展 `EnsureSelfInstalled`，不重写）

`internal/gui/app.go` 的 `EnsureSelfInstalled` 扩为「缺失 **或 版本更旧** 都处理」：

- **范围**（安全边界）：仅 `XMDevDir/xdev(.exe)` CLI 副本。xdev-gui 副本维持现有 missing-only 逻辑（理由：GUI 二进制无轻量版本探测接口——不能执行 `self version`，Wails 入口不支持 CLI 参数；且 CLI/GUI 版本联动发布，GUI 缺副本由该逻辑补齐即可）
- **版本探测**：`exec.CommandContext(副本路径, "self", "version")`，1s 超时，`toolmeta.ExtractVersionFromOutput` 提取 → `util.CompareSemver(副本版本, version.Version) < 0` 判定 stale
- **替换**：`util.CopyFile(当前exe → 副本路径)`（`EnsureSelfInstalled` 同款），替换前后校验 `util.FileSHA256` 一致
- **自我保护**：副本路径 == 当前可执行文件路径（`os.Executable()` EvalSymlinks 后）→ 跳过
- **失败降级**：exec 失败/超时/解析失败 → 跳过 + log（不阻塞启动）
- **开关**：`core.self_update_stale=false` 时跳过（guī 侧 GUI startup 与 CLI 交互入口共用同一检测函数，放 `internal/update` 导出 `ReplaceStaleCLICopy(exePath, homeDir string) error`，CLI/GUI 都调，提高复用率）
- install/ 多版本目录不动（历史版本由 `installer.InstallSelf` 语义保留）

## 三、非目标

- 不改 `install/` 多版本管理（历史版本刻意保留；current 由既有 junction 管理）
- 不新增 Wails 绑定（复用 `CheckForUpdate`；自检走内部调用）
- 不做全局 PersistentPreRun 自动检查（一次性命令/shim 不被网络检查拖慢）
- 不做签名链升级（SHA256 校验已强制）；不做计划任务/看门狗；不做自动重启（更新后仅提示用户重启）
- 不处理 PATH 上其他位置的 xdev 副本（只处理 `XMDevDir` 管辖范围，避免误伤）

## 四、验收标准

1. `core.self_update_mode` / `core.self_update_stale` 可 `xdev config set` 配置、校验非法值、持久化 roundtrip。
2. GUI 启动 2s 后按配置执行自检：`self_update=false` → 无网络调用；`true` 时 24h 限频生效。
3. `prompt` 模式：有更新 → 三选对话框（更新/忽略并继续/退出）三个动作分别触发 Apply / 写忽略 / 退出。
4. `auto` 模式：有更新 → 自动应用，成功后提示重启；失败静默 log。
5. `notify` 模式：有更新 → 仅提示，无对话框。
6. 非 TTY 下 prompt 降级 notify，命令行不阻塞、退出码为 0。
7. CLI 挂载点仅 3 个交互入口（无参/tui/gui）；子命令与 shim 形态无检查行为（守卫测试）。
8. `self_update_stale=true` 时：`XMDevDir/xdev.exe` 旧版本副本被替换为当前 exe（hash 一致）；同版本/更新版本跳过；exec 失败跳过；副本==当前 exe 跳过。
9. 忽略记录生效：同版本不再提示；新版本重新提示。
10. `go test ./... -short` 全绿；vitest 全绿；`go build .` + `wails build -tags gui` 通过。

## 五、风险与缓解

| 风险 | 缓解 |
|---|---|
| 启动检查网络开销/延迟 | 24h 限频 + 15s 超时 + 失败静默；仅交互入口触发 |
| shim/一次性命令被拖慢 | 挂载点只选 3 个交互入口；守卫测试防回归 |
| 副本替换误伤 | 只处理 XMDevDir 根 CLI 副本；版本探测超时/失败跳过；替换前后 hash 校验；==当前 exe 跳过 |
| prompt 在 CI/管道阻塞 | 非 TTY 检测降级 notify，不读 stdin |
| 配置枚举非法 | set 校验 `prompt|auto|notify`，非法拒绝并报错 |
| 多进程并发写忽略/频率文件 | 复用 cache JSON 原子写模式（临时文件 + rename） |

## 批准记录 (Approval History)

| 阶段 | 状态 | 批准者 | 日期 | 文档版本 | 批准范围 |
|---|---|---|---|---|---|
| Spec 批准 | Approved | `xinyan <zuijiuhjd@foxmail.com>` | 2026-08-22 | `1.0.0` | `2026-08-22-spec-034-approved-001` 启动自检更新与旧版本副本替换的目标、范围和验收（含 3 项决策按默认执行） |