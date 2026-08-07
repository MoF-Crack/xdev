# Spec 026: 附属工具动态安装

> **状态**：待批准
> **类型**：功能增强
> **日期**：2026-08-05
> **前置**：Spec 025 已完成（包管理器自动安装 + 按形态区分下载）
> **遵循**：SDD + LOOP（本文档获批后 → plan → task → checklist → loop → 全部获批才实施）

## 一、目标

### 1.1 需求 1：yarn/pnpm 动态启用提示

当前 Yarn/PNPM 在「包管理器」列表中显示静态 `installHint` 文案"需先安装 Node.js，然后运行 corepack enable yarn"——无论 Node 是否已安装都显示同样提示。

用户反馈："完全可以优化为检测到 node 已安装，就直接显示安装按钮，检测到 node 未安装再提示需要安装 node，因为本身就有已安装应用扫描机制，完全可以扫描结果复用。"

**目标**：
- Node 已安装 → yarn/pnpm 显示「启用」按钮，点击后 xdev 后台执行 `corepack enable yarn`（或 pnpm）
- Node 未安装 → 显示提示"需先安装 Node.js"（保持现有文案）
- 复用 `GetPackageManagers` 已有的 Node 检测结果，无需新增扫描

### 1.2 需求 2：pipx 动态安装选项

当前 pipx 显示静态 `installHint` 文案"通过 pip install pipx 或 uv tool install pipx 安装"——无论 pip/uv 是否存在都显示两个选项。

用户反馈："检测到两个同时存在时才在用户点击安装按钮后作为选项提供给用户，单一存在时默认，都不存在时才提醒用户依赖这俩包管理器。"

**目标**：
- pip 和 uv 都存在 → 点「安装」弹出选项对话框（pip install pipx / uv tool install pipx 二选一）
- 仅 pip 存在 → 直接执行 `pip install pipx`
- 仅 uv 存在 → 直接执行 `uv tool install pipx`
- 都不存在 → 显示提示"需先安装 pip 或 uv"

## 二、现状（已核实）

### 2.1 pmDef 附属工具定义

`internal/gui/app_local_detect.go:730-737` 的 `packageManagerDefs()`：

| name | toolName | detectCmd | installHint | CanInstall |
|------|----------|-----------|-------------|------------|
| yarn | "node" | "yarn" | "需先安装 Node.js，然后运行 corepack enable yarn" | false（detectCmd != toolName） |
| pnpm | "node" | "pnpm" | "需先安装 Node.js，然后运行 corepack enable pnpm" | false |
| pipx | "" | "pipx" | "通过 pip install pipx 或 uv tool install pipx 安装" | false（toolName=""） |

`detectPackageManager` (line 432) 的 `canInstall` 逻辑：`(toolName != "" && detectCmd == toolName) || installCommand != ""`——附属工具两个条件都不满足。

### 2.2 检测机制现状

`GetPackageManagers()` 并发检测所有 pmDef，每个 pmDef 的 `detectCmd` 通过 `exec.LookPath` 检查是否在 PATH。结果包含 `Installed bool` / `Version string` / `LocalPath string`。

**关键**：Node 的检测结果（`npm` 或 `node` 的 Installed/Version/LocalPath）已在同一次 `GetPackageManagers` 调用中获得——yarn/pnpm 的检测可以复用同批次 npm 的结果。pip 和 uv 的检测结果同理（同批次 `pip` 和 `uv` 的 pmDef）。

### 2.3 corepack enable 执行环境

`corepack` 是 Node.js ≥ 16.10 自带的包管理器管理工具。`corepack enable yarn` 在 Node 安装目录下创建 yarn shim。执行环境要求：
- 需要 `node` 在 PATH（corepack 是 node 的附属命令）
- 不需要管理员权限（写入 node 安装目录的 bin/，用户级安装的 node 无需提权）
- 命令：`corepack enable yarn`（或 `corepack enable pnpm`）

## 三、功能需求

### F1: pmDef 扩展 — 附属工具安装机制

`pmDef` 新增字段支持附属工具的动态安装：

```go
type pmDef struct {
    // ... 现有字段 ...
    // Spec 026: 附属工具安装机制。
    // enableCommand 非空时，该工具是"可启用附属工具"（如 yarn/pnpm 通过
    // corepack enable 启用）。CanInstall 在父工具已安装时为 true。
    enableCommand string // 启用命令（如 "corepack enable yarn"），空 = 非附属工具
    parentTool    string // 父工具名（如 "node"），用于检测父工具是否已安装
    // installMethods 列出可选安装方式（如 pipx 可通过 pip 或 uv 安装）。
    // 非空时，安装流程根据可用方法动态选择/弹选项。
    installMethods []installMethod // 空 = 单一安装方式或不可安装
}

type installMethod struct {
    name    string // 方法标识（如 "pip" / "uv" / "corepack"）
    command string // 执行命令（如 "pip install pipx" / "uv tool install pipx"）
    // detectCmd 用于检测该方法的前置工具是否可用（如 "pip" / "uv"）
    detectCmd string
}
```

### F2: detectPackageManager 动态 CanInstall + InstallHint

`detectPackageManager` 扩展：
- 附属工具（`enableCommand != ""`）：检查 `parentTool` 是否已安装（在同批次检测结果中查找）→ 已安装则 `CanInstall=true`，`InstallHint` 动态改为"Node 已安装，点击启用"；未安装则保持现有提示
- 多方法工具（`installMethods != ""`）：检查每个 method 的 `detectCmd` 是否可用 → 至少一个可用则 `CanInstall=true`；多个可用则 `InstallHint` 改为"检测到 pip 和 uv，点击安装选择方式"；单一可用则"检测到 pip，点击安装"；都不可用则保持现有提示

`PackageManagerInfo` 新增字段：
- `EnableCommand string` json — 启用命令（前端用于确认对话框显示）
- `InstallMethods []InstallMethodInfo` json — 可用安装方法列表（前端用于选项对话框）

### F3: InstallPackageManager 扩展 — enable + method 路径

`InstallPackageManager(toolName)` 扩展：
- `enableCommand != ""` → 走 enable 路径：`exec.Command("corepack", "enable", subtoolName)`，在 Node 环境（PATH 含 node）中执行
- `installMethods` 非空 + 指定 method → 走 method 路径：执行对应 method 的 command
- 新增 `InstallSubTool(toolName, method string) error` API：method 参数指定安装方式（"pip" / "uv" / "" 表示自动选择/启用）

### F4: 前端动态提示 + 选项对话框

- yarn/pnpm：`CanInstall=true` 时显示「启用」按钮（非「安装」），点击 → 确认对话框（显示 enableCommand）→ 调 `InstallSubTool(toolName, "")`
- pipx：`CanInstall=true` 且 `InstallMethods` 长度 > 1 → 点「安装」弹出选项对话框（每个 method 一个选项）→ 用户选 → 调 `InstallSubTool(toolName, selectedMethod)`
- pipx：`CanInstall=true` 且 `InstallMethods` 长度 == 1 → 直接调 `InstallSubTool(toolName, onlyMethod)`
- i18n 新增 key

## 四、范围边界

**做：**
- pmDef 扩展（enableCommand / parentTool / installMethods）
- detectPackageManager 动态 CanInstall + InstallHint
- InstallSubTool API（enable + method 路径）
- 前端动态提示 + 选项对话框
- yarn/pnpm/pipx 三个附属工具的 pmDef 更新

**不做：**
- 不改 npm/npx（它们 toolName="node" 且 detectCmd==toolName，CanInstall 已为 true）
- 不改 uvx（toolName="uv"，CanInstall 已为 true）
- 不改 brew/scoop/choco（Spec 025 已处理）
- 不新增 manifest 文件
- 不改 ToolInfo / Archive.Form 等 Spec 025 已完成的 API

## 五、验证

1. `go test ./internal/gui/ -short -count=1 -run "PmDef|SubTool|Enable"` 通过
2. Node 已安装时：yarn/pnpm CanInstall=true，InstallHint="Node 已安装，点击启用"
3. Node 未安装时：yarn/pnpm CanInstall=false，InstallHint="需先安装 Node.js"
4. pip+uv 都在：pipx CanInstall=true，InstallMethods=[pip,uv]，点安装弹选项
5. 仅 pip：pipx CanInstall=true，InstallMethods=[pip]，直接安装
6. 都不在：pipx CanInstall=false，InstallHint="需先安装 pip 或 uv"
7. 前端 `npx vite build` + vitest
8. e2e：附属工具安装新用例
9. `wails build` 产出 xdev-gui.exe

## 六、风险

| 风险 | 缓解 |
|------|------|
| corepack 不在 PATH（Node 版本过旧） | 检测 corepack 可用性，不可用时提示"Node 版本过旧，需 ≥16.10" |
| pip/uv 版本过旧不支持 install 命令 | 命令执行失败时返回明确错误 |
| 同批次检测结果竞态 | GetPackageManagers 先并发检测所有 pmDef，收齐结果后再计算附属工具的 CanInstall（两阶段） |
| InstallSubTool method 参数注入 | method 硬编码在 pmDef 的 installMethods，前端只传 method name 选中，后端查 pmDef 确认 |

## 七、审批请求

请审批本 spec。获批后依次产出 plan / task / checklist / loop，每份单独获批才进入下一阶段，全部获批后开始实施。

## 批准记录 (Approval History)

> **批准ID**: `2026-08-08-spec-026-draft-001`
