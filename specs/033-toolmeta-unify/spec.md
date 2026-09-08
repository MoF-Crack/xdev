# Spec 033: 工具元数据统一与本地检测收口

> **文档类型**：Feature Spec
> **状态**：Approved
> **文档版本**：1.0.0
> **创建日期**：2026-08-22
> **最后更新**：2026-08-22
> **所有者**：xinyan <zuijiuhjd@foxmail.com>
> **权威范围**：消除 env/gui 之间的工具元数据重复；统一版本提取；统一路径判定
> **Constitution**：`1.0.0`

## 一、目标

消除代码分析发现的 5 类重复，把分散在 `internal/env` 与 `internal/gui` 的工具检测元数据、版本解析、路径判定收口到单一权威来源。**不引入回归**，**以可读性和效率为第一优先级**，不追求高阶语法。

### 1.1 重复现状（代码证据）

| # | 重复项 | 位置 | 行为差异 |
|---|---|---|---|
| R1 | 工具→二进制名映射 | `env/scan_local.go:35` `scanRules`(23条)、`gui/app_local_detect.go:1788` `getBinNamesForTool`(14case)、`gui/app_local_detect.go:1940` `findBinaryInDir` 内联 `binaryNames`(28键) | 覆盖工具集不一致 |
| R2 | `--version` 标志表 | `env/scan_local.go:417` `versionFlags`(24键)、`gui/app_local_detect.go:1840` `versionFlags`(38键) | `gradle` 标志不一致(env `--version`/gui `-version`) |
| R3 | `extractVersionFromOutput` | `env/scan_local.go:469`、`gui/app_local_detect.go:1915` | env 单行+内联前缀；gui 逐行+更长前缀+回退 |
| R4 | `getCommonInstallPaths` | `env/migrate.go:205`(55行12工具)、`gui/app_local_detect.go:2080`(140行超集) | 逐字节前12case相同，gui是超集 |
| R5 | "在xdev目录内"判定 | `hostscan.InsideDir`(规范)、`env/scan_local.go:527` `isInsideXdevDir`(独立重写)、`gui/app_local_detect.go:1669` `isInsideDir`(已委托) | env未委托 |

## 二、方案

### 2.1 新建 `internal/toolmeta` 包（单一权威来源）

深模块：一份表，多处查。

```
internal/toolmeta/
├── meta.go        # ToolMeta 结构 + 表 + 查询函数
├── version.go     # extractVersion / extractVersionFromOutput / isVersionLike / stripWindowsSuffix
└── paths.go       # getCommonInstallPaths / checkEnvVars
```

**ToolMeta 结构**（一份表，替代 R1+R2）：

```go
// ToolMeta 是单个工具的检测元数据。所有本地检测路径统一查这份表。
type ToolMeta struct {
    Name       string     // "go", "java", ...
    BinNames   []string   // 可执行文件名（不含扩展名）："go", {"java","javac"}
    EnvVarKeys []string   // 关联环境变量："GOROOT", {"JAVA_HOME"}
    VersionArg []string   // --version 参数：{"version"} for go, {"--version"} for node
    // InstallHints 可选：非标准安装路径（如 %ProgramFiles%\Go）
}
```

**表**（`var table = []ToolMeta{...}`）：取三份的**并集**（28+ 工具），`VersionArg` 取 gui 版（更全），`gradle` 统一为 `{"-version"}`（gradle 接受 `-version` 和 `--version`，但 gui 已用 `-version` 且测试通过）。

**查询函数**：
- `Lookup(name string) (ToolMeta, bool)` — 按工具名查
- `BinNames(name string) []string` — 返回二进制名（未知则返回 `{name}`）
- `EnvVarKeys(name string) []string` — 返回环境变量键
- `VersionArg(name string) []string` — 返回版本参数（默认 `{"--version"}`）

### 2.2 版本提取收口到 `toolmeta/version.go`（替代 R3）

把 `gui/app_local_detect.go` 的 `extractVersion`、`extractVersionFromOutput`、`isVersionLike`、`stripWindowsSuffix` **移到** `toolmeta/version.go`（导出为 `toolmeta.ExtractVersion` 等）。env 版的 `extractVersionFromOutput` 删除，改为调用 `toolmeta.ExtractVersionFromOutput`。

前缀列表取**并集**（env 的 `java `/`Python ` + gui 的 `yarn `/`gradle ` 等）。`.windows.` 截断统一走 `stripWindowsSuffix`。

### 2.3 路径判定收口（替代 R5）

`env/scan_local.go` 的 `isInsideXdevDir` 删除，改为 `hostscan.InsideDir(path, xdevDir)`。

### 2.4 `getCommonInstallPaths` 收口（替代 R4）

把 `gui/app_local_detect.go:2080` 的 `getCommonInstallPaths` **移到** `toolmeta/paths.go`（导出为 `toolmeta.CommonInstallPaths(name)`）。env/migrate.go 的版本删除，改为调用 `toolmeta.CommonInstallPaths`。

`checkEnvVars` 同理移到 `toolmeta.CheckEnvVars`（内部用 `toolmeta.Lookup` 查 `EnvVarKeys`，不再硬编码 map）。

### 2.5 调用方改造（不改行为）

| 原调用 | 改为 |
|---|---|
| `env/scan_local.go` `scanRules` | 删除；`scanTool` 改为遍历 `toolmeta.Table()` 或按名查 |
| `env/scan_local.go` `getToolVersion` | `versionFlags` 删除，改 `toolmeta.VersionArg(rule.binNames[0])`；`extractVersionFromOutput` 改 `toolmeta.ExtractVersionFromOutput` |
| `env/scan_local.go` `isInsideXdevDir` | 改 `hostscan.InsideDir` |
| `env/scan_local.go` `verifyToolBinaries` | 候选路径逻辑保留（无重复），但 `binNames` 从 `rule.binNames` 改为 `toolmeta.BinNames(name)` |
| `env/migrate.go` `getCommonInstallPaths` | 删除；调用 `toolmeta.CommonInstallPaths` |
| `env/migrate.go` `isVersionLike` | 删除；调用 `toolmeta.IsVersionLike` |
| `gui/app_local_detect.go` `getBinNamesForTool` | 删除；调用 `toolmeta.BinNames` |
| `gui/app_local_detect.go` `findBinaryInDir` 内联 `binaryNames` | 删除 map；改 `toolmeta.BinNames` |
| `gui/app_local_detect.go` `getVersion` `versionFlags` | 删除 map；改 `toolmeta.VersionArg(name)` |
| `gui/app_local_detect.go` `extractVersion`/`extractVersionFromOutput`/`isVersionLike`/`stripWindowsSuffix` | 删除；改 `toolmeta.*` |
| `gui/app_local_detect.go` `getCommonInstallPaths` | 删除；改 `toolmeta.CommonInstallPaths` |
| `gui/app_local_detect.go` `checkEnvVars` | 删除；改 `toolmeta.CheckEnvVars` |

## 三、非目标

- 不合并 `getToolVersion`(env) 与 `getVersion`(gui) 的执行外壳（一个走 `rule.binNames`、一个走 `findBinaryInDir`）；只统一它们**查表的部分**。
- 不改 `ScanLocalTools` 的并发结构。
- 不改 GUI `App` 的方法签名。
- 不引入新 Wails 绑定。

## 四、验收标准

1. `internal/env` 与 `internal/gui` 中不再出现 `versionFlags` map 字面量。
2. 不再出现 `binaryNames`/`getBinNamesForTool`/`scanRules` 三份映射；仅 `toolmeta` 包有表。
3. `extractVersionFromOutput` 仅在 `toolmeta` 定义一次。
4. `isInsideXdevDir` 不再存在；`env` 调用 `hostscan.InsideDir`。
5. `getCommonInstallPaths` 仅在 `toolmeta` 定义一次。
6. `go test ./... -short` 全绿。
7. vitest 27/156 全绿。
8. `go build .` + `wails build -tags gui` 通过。
9. 现有 `TestScanLocalTools*` / `TestPmDefSubToolFields` / `TestIndependentPMOverlaysXdevWhenPATHHasLeftover` 等不回归。
10. `grep -r "versionFlags" internal/` 仅命中 `toolmeta/meta.go`（或注释）。

## 五、风险与缓解

| 风险 | 缓解 |
|---|---|
| `gradle` 标志从 `--version` 改 `-version` | gradle 接受两者；gui 已用 `-version` 且测试通过 |
| 工具集并集后 env 的 `ScanLocalTools` 多扫工具 | `ScanLocalTools` 遍历的是 `scanRules`，改为遍历 `toolmeta.Table()` 后覆盖更全——这是**正确性提升**而非回归 |
| `toolmeta` 被 `env` 和 `gui` 同时依赖 | `toolmeta` 只依赖 `os`/`path/filepath`/`runtime`/`strings`，无内部依赖，无环 |
| 前缀列表并集后解析行为微变 | 取并集只**增加**可识别前缀，不会减少；现有测试覆盖 |

## 批准记录 (Approval History)

| 阶段 | 状态 | 批准者 | 日期 | 文档版本 | 批准范围 |
|---|---|---|---|---|---|
| Spec 批准 | Approved | `xinyan <zuijiuhjd@foxmail.com>` | 2026-08-22 | `1.0.0` | `2026-08-22-spec-033-approved-001` 工具元数据统一与本地检测收口的目标、范围和验收（含 3 项决策：collectEnvVars 收口 / bump 0.3.3 / 本地 tag 不 push） |
