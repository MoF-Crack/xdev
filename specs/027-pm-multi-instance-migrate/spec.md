# Spec 027: 包管理器多实例检测 + 迁移

> **文档类型**：Feature Spec
> **状态**：Approved
> **文档版本**：1.0.0
> **创建日期**：2026-08-05
> **最后更新**：2026-08-20
> **所有者**：xinyan <zuijiuhjd@foxmail.com>
> **权威范围**：包管理器多实例检测与迁移的目标、范围和验收
> **替代文档**：无
> **Constitution**：`1.0.0`
## 一、目标

### 1.1 需求 1：多实例检测

当前 `detectPackageManager` 对每个包管理器只返回一个检测结果——`exec.LookPath` 找到 PATH 中第一个就返回。用户有两个 Python 环境（`D:\Env\python3` 系统 pip + `E:\XDev\install\python\3.14.4` xdev 管理 pip），但 PIP 详情只显示一个。

用户反馈："不是应该两个分区的都出现在可用列表中吗？毕竟多版本管理本身就是 xdev 设计的目的。"

**目标**：`PackageManagerInfo` 新增 `Instances []PMInstanceInfo` 字段，列出所有检测到的实例（xdev 管理的 + 系统安装的）。前端详情抽屉显示为多版本列表。

### 1.2 需求 2：迁移功能

pipx 当前只在 `D:\Env\python3\Scripts\pipx.exe`（系统安装），无法迁移到 xdev 管理目录。用户期望能"迁移"把系统安装的包管理器纳入 xdev 管理。

用户反馈："pipx 也是同样的问题，只能识别到 D 分区的，不能迁移，也不能切换安装位置。"

**目标**：新增 `MigratePackageManager(toolName string) error` API。对于附属工具（pip/pipx），把系统安装的副本复制/链接到 xdev 管理的 Python 目录；对于独立工具（scoop/brew/choco），迁移逻辑类似 `MigrateLocalTool`。

## 二、现状

### 2.1 检测模型

`detectPackageManager`（app_local_detect.go:528）：
- `exec.LookPath(pm.detectCmd)` 找到 PATH 中第一个 → `info.Installed = true`
- Spec 026 增量：附属工具即使 PATH 找到系统版本也检查 xdev 管理版本并覆盖
- **但只保留一个结果**——xdev 管理的覆盖系统的，而非两个都列出

### 2.2 数据模型

```go
type PackageManagerInfo struct {
    Name, DisplayName, Description string
    Installed bool
    Version   string  // 单一版本
    LocalPath string  // 单一路径
    // ... 无 Instances 字段
}
```

### 2.3 详情抽屉

`ToolDetailDrawer.vue` 对 package-manager 类工具只显示一个"本地环境"版本 + "切换到本地版本"按钮。无多版本列表、无迁移按钮。

### 2.4 迁移机制

普通工具有 `MigrateLocalTool(toolName, version, path string) error`——把系统安装的工具目录复制到 xdev install dir + 创建 current 链接。包管理器没有对应的迁移方法。

## 三、功能需求

### F1: PackageManagerInfo 新增 Instances 字段

```go
type PMInstanceInfo struct {
    Version   string `json:"version"`
    Path      string `json:"path"`
    Managed   bool   `json:"managed"`  // true = xdev 管理的，false = 系统安装的
    Source    string `json:"source"`   // "xdev" 或 "local" 或 "system"
}

type PackageManagerInfo struct {
    // ... 现有字段 ...
    Instances []PMInstanceInfo `json:"instances,omitempty"` // Spec 027
}
```

### F2: 多实例检测

`detectPackageManager` 扩展：
1. 先查 xdev 管理的版本（`currentVersion` + `shim.FindInDir`）→ 若找到，加入 Instances（Managed=true, Source="xdev"）
2. 再查 PATH 中的系统版本（`exec.LookPath`）→ 若找到且与 xdev 管理版本不同路径，加入 Instances（Managed=false, Source="local"）
3. `info.Installed` / `info.Version` / `info.LocalPath` 保持现有逻辑（xdev 管理优先），但 Instances 列出全部

### F3: MigratePackageManager API

`func (a *App) MigratePackageManager(toolName string) error`：
- 查 pmDef：若 toolName 是附属工具（toolName 非空，如 pip 的 toolName="python"），在 xdev 管理的父工具目录中创建附属工具的链接/复制
- 若 toolName 是独立工具（scoop/brew/choco），逻辑类似 `MigrateLocalTool`（复制目录 + 创建 current 链接）
- 迁移成功后 `InvalidateToolsCache`

### F4: 前端详情抽屉多实例 + 迁移按钮

- `ToolDetailDrawer.vue`：package-manager 类工具显示 Instances 列表（每个实例一行：版本 + 路径 + 来源徽标）
- 每个非 xdev 管理的实例旁显示「迁移」按钮 → 调 `MigratePackageManager`
- i18n 新增 key

## 四、范围边界

**做：**
- PackageManagerInfo 新增 Instances 字段 + PMInstanceInfo 类型
- detectPackageManager 多实例检测（xdev 管理 + 系统安装都列出）
- MigratePackageManager API
- 前端详情抽屉多实例列表 + 迁移按钮
- i18n

**不做：**
- 不改 PackageManagerInfo 的现有字段（Installed/Version/LocalPath 保持现有语义）
- 不给包管理器加 manifest（不纳入 GetAvailableVersions/GetInstalledVersions 多版本管线）
- 不改包管理器的安装/卸载流程（Spec 025/026 已完成）
- 不实现包管理器的版本切换（UseVersion）——那是"纳入多版本管理"方案的范围
- 不改普通工具（python/node）的检测/迁移

## 五、验证

1. `go test ./internal/gui/ -short -count=1 -run "Instance|Migrate"` 通过
2. PIP 详情：Instances 列出两个实例（E:\XDev 的 pip 26.1.1 Managed=true + D:\Env 的 pip 26.1.1 Managed=false）
3. PIPX 详情：Instances 列出 D:\Env 的 pipx 1.16.6 Managed=false
4. 点 PIPX 迁移 → pipx 复制到 xdev 管理的 Python 目录 → Instances 更新
5. 前端 `npx vite build` + vitest
6. e2e：多实例列表 + 迁移新用例
7. `wails build`

## 六、风险

| 风险 | 缓解 |
|------|------|
| 同一 pip 被检测两次（PATH 和 xdev 指向同一文件） | 去重：按路径规范化后比较 |
| 迁移附属工具（pip）的语义——pip 跟随 python，"迁移"pip 实际上是创建链接 | 明确：附属工具迁移 = 在 xdev python 目录创建 pip 链接/复制 |
| 系统 pip 版本与 xdev pip 版本不同 | Instances 分别显示各自版本 |
| 迁移后 PATH 仍指向旧位置 | 迁移只复制/链接到 xdev 目录，不修改系统 PATH；xdev shim 目录前置后 xdev 版本优先 |

## 七、审批请求

请审批本 spec。获批后依次产出 plan / task / checklist / loop，全部获批后开始实施。

## 批准记录 (Approval History)

| 阶段 | 状态 | 批准者 | 日期 | 文档版本 | 批准范围 |
|---|---|---|---|---|---|
| Spec 批准 | Approved | `xinyan <zuijiuhjd@foxmail.com>` | 2026-08-08 | `1.0.0` | `2026-08-20-spec-027-approved-001` 包管理器多实例检测与迁移的目标、范围和验收 |
