# Spec 025: 安装流程增强 — 包管理器自动安装 + 按形态区分下载

> **状态**：待批准
> **类型**：功能增强
> **日期**：2026-08-04
> **前置**：Spec 024 已完成（开发工具扁平分类 + manifest 并行加载）
> **遵循**：SDD + LOOP（本文档获批后 → plan → task → checklist → loop → 全部获批才实施）

## 一、目标

### 1.1 需求 1：包管理器集成自动安装

当前 Homebrew/Scoop/Chocolatey 三项在 xdev 包管理器列表中只显示"访问 https://xxx 安装"文案（`app_local_detect.go:420-422` 的 `installHint`），`CanInstall=false`（`toolName=""`），用户必须手动打开浏览器去外部网站安装——这让 xdev 退化成了"收藏夹 + 探测器"，没有发挥集成价值。

用户反馈："比如 scoop 的 readme 里面就有安装命令，完全可以在用户确认安装之后直接后台执行。如果需要手动访问安装，那集成到 xdev 里面的意义是什么？"

**目标**：让 xdev 能直接后台执行包管理器的官方安装命令，用户点「安装」按钮即可完成安装，无需打开浏览器。

### 1.2 需求 2：下载按形态区分

当前 manifest 的 archives 按 **os/arch** 区分（windows/darwin/linux × amd64/arm64），不按形态（GUI/CLI/TUI）区分。形态徽标只用于展示 + 决定"启动"还是"复制命令"按钮。

用户反馈："既然区分了 GUI TUI CLI 那为什么下载的时候不区分？"

**目标**：对「多形态独立下载」的工具（如 OpenCode 的 Desktop App 与 CLI 是独立 archive），安装时让用户选择形态 → 筛选对应 archive。单包多形态工具（如 VS Code 的 code.exe 既是 GUI 又是 CLI）不显示选择器。

## 二、现状（已核实）

### 2.1 包管理器安装现状

`internal/gui/app_local_detect.go:407-439` 的 `GetPackageManagers()` 硬编码了 13 个包管理器定义（`pmDef`）。其中：

| 包管理器 | toolName | CanInstall | installHint |
|----------|----------|------------|-------------|
| brew | "" | false | 访问 https://brew.sh 安装 |
| scoop | "" | false | 访问 https://scoop.sh 安装 |
| choco | "" | false | 访问 https://chocolatey.org 安装 |
| npm/node/pip/cargo 等 | 有值 | true | 随父工具自动安装 |

`InstallPackageManager(toolName)` (line 697) 只能安装 `toolName != ""` 的（通过 xdev manifest 管线）；brew/scoop/choco 的 `toolName=""` 所以不可安装。

### 2.2 包管理器官方安装命令（webfetch 核实，2026-08-04）

| PM | 官方安装命令 | 权限 | 执行环境 | 安装目录 |
|----|-------------|------|---------|---------|
| **Scoop** | `irm get.scoop.sh \| iex` | **非管理员**（管理员需 `-RunAsAdmin` 参数） | PowerShell 5.1+，ExecutionPolicy 需 RemoteSigned/Unrestricted/Bypass | `C:\Users\<user>\scoop` |
| **Homebrew** | `/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"` | **无需 sudo**（脚本自提示，交互式确认） | bash（macOS Terminal / Linux shell / WSL） | macOS: `/opt/homebrew`（Apple Silicon）/ `/usr/local`（Intel）；Linux: `/home/linuxbrew/.linuxbrew` |
| **Chocolatey** | `Set-ExecutionPolicy Bypass -Scope Process -Force; iex ((New-Object System.Net.WebClient).DownloadString('https://community.chocolatey.org/install.ps1'))` | **需管理员** PowerShell | PowerShell v3+，.NET 4.8，ExecutionPolicy Bypass | `C:\ProgramData\Chocolatey` |

**安全模型决策（用户批准：混合策略）**：
- Scoop/Homebrew：直接执行官方命令（脚本广受信任，Scoop 明确非管理员，Homebrew 脚本自带交互式确认）
- Chocolatey：需管理员权限，xdev 检测到非管理员时提示用户以管理员重启 xdev 或手动执行

### 2.3 形态区分现状

manifest `Archive` 结构（`internal/manifest/manifest.go:205-219`）字段：`OS / Arch / Format / URL / MirrorURL / SHA256 / Size / Strip / InstallArgs / InstallKind / RequiresProxy`——**无 form_factor 字段**。

安装流程：`InstallWithProgress` → `installByKind` → 按 os/arch 匹配 `ArchiveForCurrent` → 下载/解压/激活。形态（GUI/CLI/TUI）不参与 archive 选择。

前端：`useInstall` composable + `InstallProgressDialog`——用户点安装 → 直接走后端 `InstallTool`，无形态选择步骤。

**多形态独立下载的工具示例**：
- OpenCode：`form_factor: [gui, cli, tui]`，Desktop App（GUI）与 CLI 是独立下载——当前 manifest 把它们放在同一个 version 的 archives 里，但没区分哪个 archive 对应哪个形态
- VS Code：`form_factor: [gui, cli]`，但 GUI 和 CLI 是同一个安装包（`code.exe` 既是 GUI 入口也是 CLI）——单包多形态，无需选择器

## 三、功能需求

### F1: 包管理器 script install 机制

#### F1a: 新增 `install_kind: "remote-script"`

当前 `InstallKind` 有 `archive` / `executable` / `script`三种。新增第四种 `remote-script`：

- 语义：下载远程脚本到临时文件，执行后清理（不保留 archive）
- 与 `script` 的区别：`script` 的 URL 指向 xdev 下载并保留的安装脚本（如 rustup-init.sh）；`remote-script` 的 URL 指向官方安装脚本（如 get.scoop.sh），xdev 只负责执行不保留
- SHA256：可选（官方安装脚本可能动态变化，留空时 TOFU）

#### F1b: 包管理器 manifest

为 brew/scoop/choco 新增 manifest（`manifests/scoop.yaml` / `brew.yaml` / `choco.yaml`），或扩展 `pmDef` 加 `installCommand` 字段。

**方案选择**：扩展 `pmDef` 加 `installCommand` + `requiresAdmin` + `executionEnv` 字段，而非新建 manifest——因为包管理器安装不走 xdev 的 download→extract→activate 管线，而是直接执行远程命令。

```go
type pmDef struct {
    // ... 现有字段 ...
    installCommand string   // 官方安装命令（空 = 不可自动安装，保持现有行为）
    requiresAdmin  bool     // 是否需要管理员权限
    executionEnv   string   // "powershell" | "bash" — 执行环境
    platform       string   // "windows" | "darwin" | "linux" | "any" — 限定平台
}
```

- scoop：`installCommand="irm get.scoop.sh | iex"`, `requiresAdmin=false`, `executionEnv="powershell"`, `platform="windows"`
- brew：`installCommand='/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"'`, `requiresAdmin=false`, `executionEnv="bash"`, `platform="any"`（macOS/Linux/WSL）
- choco：`installCommand="Set-ExecutionPolicy Bypass -Scope Process -Force; iex ((New-Object System.Net.WebClient).DownloadString('https://community.chocolatey.org/install.ps1'))"`, `requiresAdmin=true`, `executionEnv="powershell"`, `platform="windows"`

#### F1c: `InstallPackageManager` 扩展

- 检测 `installCommand != ""` → 走新的 script install 路径
- Windows + powershell：`exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", installCommand)`
- macOS/Linux + bash：`exec.Command("bash", "-c", installCommand)`
- `requiresAdmin=true` 且当前非管理员：返回明确错误"Chocolatey 需要管理员权限，请以管理员重启 xdev 或手动执行安装命令"
- 后台执行 + 实时输出捕获（logs 到 GUI log）
- 安装完成后重新检测 `GetPackageManagers` 确认成功

#### F1d: 前端安装按钮

- `CanInstall=true` 时（installCommand != ""）显示「安装」按钮
- 点击 → 确认对话框（显示安装命令 + 安全提示）→ 调 `InstallPackageManager`
- `requiresAdmin=true` 时按钮文案改为「以管理员安装」并附 tooltip

### F2: archive 级别 form_factor 标注 + 安装时形态选择

#### F2a: Archive 结构扩展

`manifest.Archive` 新增可选字段：

```go
type Archive struct {
    // ... 现有字段 ...
    Form string `yaml:"form,omitempty"` // gui | cli | tui — 该 archive 对应的形态（空 = 不区分，所有形态共用）
}
```

- 缺省（`Form=""`）：该 archive 适用于所有形态（向后兼容现有 50 个 manifest）
- `Form="gui"`：该 archive 只含 GUI 组件
- `Form="cli"`：该 archive 只含 CLI 组件

#### F2b: 多形态检测

新增 helper：`func (t *Tool) HasFormSpecificArchives() bool`——检查该 tool 的所有 version 的所有 archive 是否有 `Form != ""` 的条目。

- 有 → 该 tool 是"多形态独立下载"，安装时需显示形态选择器
- 无 → 该 tool 是"单包多形态"或"单形态"，安装时不显示选择器（现有行为）

#### F2c: 安装时形态选择（仅多包工具）

后端 `InstallWithProgress` / `InstallTool` 新增可选 `formFilter string` 参数：

- `formFilter=""`：现有行为（按 os/arch 匹配，忽略 Form）
- `formFilter="gui"`：只匹配 `Form=="gui"` 或 `Form==""` 的 archive（Form=="" 兜底兼容）
- `formFilter="cli"`：只匹配 `Form=="cli"` 或 `Form==""` 的 archive

`ArchiveForCurrent` 扩展：新增 `formFilter` 参数，匹配优先级：`os+arch+form` > `os+any+form` > `os+arch+""` > 现有降级。

#### F2d: 前端形态选择器

`InstallProgressDialog` 或安装前新增形态选择步骤：

- `GetToolDetail` 返回新增 `hasFormSpecificArchives bool` 字段
- `hasFormSpecificArchives=true` 时，点「安装」先弹出形态选择器（复选框：GUI / CLI / TUI，来自 `formFactor`）
- 用户选形态 → 调 `InstallToolWithForm(toolName, version, formFilter)`
- `hasFormSpecificArchives=false` 时：现有行为，直接安装

### F3: OpenCode manifest 标注 Form（示范）

为 OpenCode 的 archives 标注 `form` 字段作为 F2 的首个用例：

- Desktop App archive：`form: gui`
- CLI archive：`form: cli`
- 其他单包多形态工具（VS Code 等）不动

## 四、范围边界

**做：**
- 扩展 `pmDef` 加 `installCommand`/`requiresAdmin`/`executionEnv`/`platform`
- `InstallPackageManager` 支持 script install 路径
- 前端包管理器「安装」按钮 + 确认对话框
- `Archive.Form` 字段 + `HasFormSpecificArchives()` helper
- `InstallToolWithForm` API + `ArchiveForCurrent` formFilter
- 前端形态选择器（仅多包工具）
- OpenCode manifest 标注 Form

**不做：**
- 不为 brew/scoop/choco 新建 manifest 文件（走 pmDef 扩展，不走 download→extract→activate 管线）
- 不改现有 50 个 manifest 的 archive 结构（Form 缺省空，向后兼容）
- 不改单包多形态工具（VS Code/JetBrains 等）的安装流程
- 不实现 Chocolatey 的自动提权（只检测 + 提示，不自动 UAC 提升）
- 不实现安装后的 PATH 自动刷新（提示用户重启 shell/xdev）
- 不改 `ToolInfo` / `LaunchTool` / `CheckToolDependencies` 等 Spec 023 已完成的 API

## 五、验证

1. `go test ./internal/gui/ ./internal/installer/ ./internal/manifest/ -short -count=1` 通过
2. 包管理器安装：
   - Scoop（Windows 非管理员）：点「安装」→ 确认 → 后台执行 `irm get.scoop.sh | iex` → 安装成功 → `GetPackageManagers` 显示 scoop 已安装
   - Homebrew（macOS/Linux）：点「安装」→ 确认 → 后台执行 curl|bash → 安装成功
   - Chocolatey（Windows 非管理员）：点「以管理员安装」→ 提示"需要管理员权限"
3. 形态选择：
   - OpenCode（`hasFormSpecificArchives=true`）：点「安装」→ 弹出形态选择器（GUI/CLI/TUI）→ 选 CLI → 只下载 CLI archive
   - VS Code（`hasFormSpecificArchives=false`）：点「安装」→ 直接安装（无选择器）
4. 前端 `npx vite build` + vitest
5. e2e：包管理器安装 + 形态选择器新用例
6. `wails build` 产出 xdev-gui.exe

## 六、风险

| 风险 | 缓解 |
|------|------|
| 执行远程脚本的安全风险 | 确认对话框显示完整命令 + 安全提示；用户明确确认才执行 |
| Chocolatey 需管理员权限 | 检测非管理员时返回明确错误 + 提示，不自动提权 |
| PowerShell ExecutionPolicy 限制 | 命令带 `-ExecutionPolicy Bypass -Scope Process`，不永久改系统策略 |
| 安装后 PATH 未刷新 | 安装完成后提示用户重启 xdev；后端重新检测 `GetPackageManagers` 确认 |
| Homebrew 脚本交互式确认 | bash 命令通过 stdin 传入确认（或脚本自带 `NONINTERACTIVE=1` 环境变量跳过） |
| Archive.Form 向后兼容 | 缺省空 = 所有形态共用；`HasFormSpecificArchives` 只在有 Form!="" 时才显示选择器 |
| 形态选择器 UX 复杂度 | 只对多包工具显示；单包工具无感知 |
| installCommand 硬编码在 pmDef | 可接受（与 installHint 同源，都是硬编码）；未来可抽到配置 |

## 七、审批请求

请审批本 spec。获批后依次产出：

1. **plan**（Phase 划分：A 包管理器 script install + B 形态区分下载 + C 集成验证）
2. **task**（细化到可执行单元）
3. **checklist**（逐项验收标准）
4. **loop**（实施循环与防挂起护栏）

每份文档单独获批才进入下一阶段，全部获批后开始实施。

## 批准记录 (Approval History)

> **批准ID**: `2026-08-08-spec-025-draft-001`
