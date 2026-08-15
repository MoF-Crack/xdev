# Spec 028: 遗留非阻塞项清理（HANDOFF 复核修正版）

> **状态**：已批准（2026-08-09，用户批准后进入实施）
> **类型**：缺陷修复 + 卫生清理
> **日期**：2026-08-09
> **前置**：Spec 024-027 已完成（HANDOFF.md 交接）
> **遵循**：SDD + LOOP
> **触发**：按交接文档对「已知非阻塞项」规划解决方案

## 〇、复核结论（重要：推翻 HANDOFF 误诊）

本次规划前对 HANDOFF.md 三条「已知非阻塞项」做了逐项实测复核，结论如下：

| HANDOFF 原记录 | 实测复核结论 |
|---|---|
| ① manifest/plugin/plugins/java 测试偶发 Go 1.26.5 runtime panic（GC mark worker / runCleanups，环境问题非代码） | **误诊。是确定性死递归代码 bug**：`ArchiveForCurrent ↔ ArchiveForCurrentWithForm("")` 互相调用 → `fatal error: stack overflow`。goroutine dump 中的 GC worker/runCleanups 是任何崩溃转储都包含的常规 goroutine，不是根因。`go test ./internal/manifest/ -run TestArchiveForCurrentPicksExactOS -count=1` 100% 复现。**且生产路径受影响**（installer.go:402 / app_install.go:167 / generic.go:114 / java.go:57），任何走默认 archive 解析的安装都会崩溃 |
| ② 13 个 manifest 中文注释 GBK mojibake（PowerShell Set-Content 破坏，YAML 解析正常） | 属实，且比记录更严重：注释**行被合并**，`name:` 字段被吞入注释（11 个文件，靠 filename fallback 兜底才没炸）；`vscode.yaml` 的 **`gui_binary: Code.exe` 字段被吞入注释** → VS Code GUI 启动路径会报「未声明有效的 gui_binary」（真实功能损失）。13 个文件均未签名（无 .sig），改写无需重签；git/origin/main 均无干净原文（本地新增文件）→ 只能人工重写 |
| ③ 前端 vitest pre-existing 失败（AppShell/ToolCatalogPage/EnvironmentVariablesPage，3 文件） | 记录**过时**。实测（2026-08-09）全量 vitest：**7 个文件 18 个测试失败 / 95 通过**。ToolCatalogPage 现已全绿（10/10）。失败分三类：localStorage 未 polyfill（3 文件）、AppShell 缺 `t` prop（8 测试）、组件重构后测试契约过时（EnvVars 4 + FormSelectorDialog 2 + InstallProgressDialog 3） |

## 一、目标

1. **修复 Go 死递归**：恢复 `ArchiveForCurrent` 的 legacy 语义，默认安装路径（archive 解析）不再崩溃。
2. **清理 13 个 manifest**：重写 mojibake 注释为干净中文（UTF-8 无 BOM），恢复被吞的 `name:` / `gui_binary:` 字段，功能字段逐字节不动；并加编码守卫测试防止复发。
3. **修复前端 vitest 全量失败**：`npx vitest run` 全绿；补充 EnvVarEditorDialog 契约测试。
4. **收尾**：清理 `audit.test.config.mjs` 残留、修正 `.gitattributes` mojibake 行、更新 HANDOFF.md、提交 handoff-backup 恢复点。

## 二、现状（复核证据）

### 2.1 死递归（internal/manifest/manifest.go:593-612）

```go
func (v *Version) ArchiveForCurrent(osName, arch string) (*Archive, bool) {
    return v.ArchiveForCurrentWithForm(osName, arch, "")   // L596
}
func (v *Version) ArchiveForCurrentWithForm(osName, arch, formFilter string) (*Archive, bool) {
    if formFilter == "" {
        return v.ArchiveForCurrent(osName, arch)           // L612 → 回到 L596，∞
    }
    // 6-pass form-aware 选择（此分支正常）
}
```

Spec 025 收敛前（commit 673433c）的 legacy 语义为 3-pass：exact os+arch → os+any → any-os（忽略 Form）。当前版本被错误的「去重」改写打断。

调用链证据：
- `internal/installer/installer.go:402` `p.ArchiveFor(ver)` → `generic.go:114` → 死递归
- `internal/gui/app_install.go:167` 默认回退路径 → 死递归（formFilter 非空路径 L159 正常，故 Spec 025 形态选择器演示未暴露此 bug）
- `internal/plugins/java/java.go:57` → 死递归

受影响测试包：internal/manifest、internal/plugin、internal/plugins/java、internal/installer（实测 FAIL）。

### 2.2 manifest 损坏清单（13 个，均 UTF-8 合法、yaml.v3 可解析）

| 文件 | 损坏行 | 被吞字段 | 恢复方式 |
|---|---|---|---|
| claude-code.yaml | L1-3 全部注释 | `name: claude-code` | 重写（内容推断：npm 平台包安装策略） |
| codex.yaml | L1-7 全部注释 | `name: codex` | 重写 |
| copilot-cli.yaml | L1-5 全部注释 | `name: copilot-cli` | 重写 |
| opencode.yaml | L1-4 全部注释 | `name: opencode` | 重写 |
| vscode.yaml | L1 + L9 | `name: vscode` + **`gui_binary: Code.exe`** | 重写 + 恢复字段（功能修复） |
| cursor.yaml | L15（L2-14 干净原文的损坏重复块） | `name: cursor` | 删除 L15 重复块，恢复 name 字段 |
| zed.yaml | L2 | `name: zed` | 重写（L1 干净） |
| clion/goland/pycharm-community/rubymine/webstorm/jetbrains-idea-community.yaml | L1-5 注释头 | 各 `name:` | 重写（6 文件同模板） |

- 编码事实：13 个文件全部是合法 UTF-8（mojibake 已烘焙为码点）；12 个带 BOM（cursor 无 BOM）。仓库内干净 manifest 的 BOM 本就混杂（go.yaml 有 BOM、node.yaml 无），故 BOM 不作为全局强制项，仅 13 个文件统一为无 BOM。
- 签名事实：13 个文件在 manifests/ 下均**无** .sig 文件 → 改写不触碰签名体系。有 .sig 的 38 个 manifest 一律不动。
- 恢复可行性：`銆?`（字节丢失成 `?`）等表明损坏有损，字节级逆向不可行 → 必须依据 cursor.yaml 的干净原文 + 语义推断重写。
- 兜底机制已生效：loader 在 `Name==""` 时回填 filename stem（manifest.go:396/468/1082），因此被吞的 `name:` 当前无功能影响，但恢复后消除隐性依赖。

### 2.3 前端 vitest 失败清单（2026-08-09 实测）

| 文件 | 失败数 | 根因 | 修复方向 |
|---|---|---|---|
| test/App.test.js | 收集失败 | `localStorage` undefined（`getItem`） | setup.js polyfill localStorage |
| test/SetupWizard.spec.js | 收集失败 | 同上 | 同上 |
| test/i18n.spec.js | 1 | `localStorage.clear` undefined | 同上 |
| test/AppShell.spec.js | 8/8 | `$props.t is not a function`（AppShell 新增 required `t` prop） | 测试补 `t: k=>k` |
| test/pages/EnvironmentVariablesPage.spec.js | 4/8 | 契约过时：scope-all 已移除、行内编辑表单已迁至 EnvVarEditorDialog、状态文本只在 title/aria-label 属性 | 重写测试 + 新建 EnvVarEditorDialog.spec.js |
| test/components/FormSelectorDialog.spec.js | 2/3 | NCheckbox stub 过时（Spec 026 已改 NRadioGroup 单选） | 更新 stub 与断言 |
| test/components/InstallProgressDialog.spec.js | 3/4 | 组件已 i18n 化（`installing.*` key），测试断言英文原文；focus 目标变化 | 更新断言 |
| test/pages/ToolCatalogPage.spec.js | 0（10/10 通过） | —（HANDOFF 记录过时） | 无需修改，仅从失败清单剔除 |

附带卫生项：仓库根 `audit.test.config.mjs`（未跟踪残留）；`.gitattributes` 中 `UNI_ENV_寮€鍙戞枃妗?md` mojibake 文件名（导致 -whitespace 豁免静默失效）。

## 三、功能需求

### F1：修复 ArchiveForCurrent 死递归（P0）

提取私有 helper 承载 legacy 3-pass 选择逻辑；`ArchiveForCurrent` 与 `ArchiveForCurrentWithForm("")` 都调用 helper，不再互相委托：

```go
func (v *Version) ArchiveForCurrent(osName, arch string) (*Archive, bool) {
    return v.archiveFormAgnostic(osName, arch)
}
func (v *Version) ArchiveForCurrentWithForm(osName, arch, formFilter string) (*Archive, bool) {
    if formFilter == "" { return v.archiveFormAgnostic(osName, arch) } // 不再回调
    // 既有 6-pass 保持不变
}
// archiveFormAgnostic：exact os+arch → os+any → any-os（忽略 Form，即 673433c 的 legacy 语义）
```

语义不变性：非空 formFilter 的 6-pass 逻辑逐行不动；空 formFilter 恢复 legacy 行为（Form 字段完全忽略，与函数 doc「behaves identically to ArchiveForCurrent」一致）。

### F2：manifest mojibake 清理

- 13 个文件的重写原则：
  1. 功能性字段（display_name/description/category/shims/form_factor/gui_binary/depends/latest/lts/versions/remote 及其所有子字段）**逐字节不动**；
  2. 注释重写为干净中文，内容 = cursor.yaml 干净原文（同模板直接复用）+ 语义推断；
  3. 恢复被吞的 `name:` 字段（与 filename stem 一致）；**vscode.yaml 恢复 `gui_binary: Code.exe` 真字段**（功能修复，对齐 app_launch.go 的 GUI 启动契约）；
  4. cursor.yaml 仅删除 L15 损坏重复块；
  5. 13 个文件统一 UTF-8 无 BOM（\n 换行，符合 .gitattributes `*.yaml text eol=lf`）。
- 加守卫测试（防复发）：扫描 manifests/*.yaml，断言 (a) 合法 UTF-8；(b) 不含 mojibake 标记字符集（鈥/鍘/瀹/淇/锛/銆/鏈/鏃/绾/鍣/甯/鐩/閿/垎 等 GBK 误读映射 + U+FFFD）；(c) 每个工具解析后 `Name != ""` 且 `Name == filename stem`；(d) form_factor 含 gui 且 shims 非空的 manifest 必须有 gui_binary 字段（vscode 回归断言）。
- 不改有 .sig 签名的 38 个 manifest；不改签名机制。

### F3：前端 vitest 修复

- setup.js 增加 localStorage polyfill（内存 Map 实现，vi.stubGlobal 或 defineProperty）；
- AppShell.spec.js：mountShell 统一注入 `t: (key) => key`；
- EnvironmentVariablesPage.spec.js 按新契约重写（详见 plan 测试用例 TC-C1~C6）；
- 新建 test/components/EnvVarEditorDialog.spec.js（对话框契约：编辑锁定、浏览填充、多路径增删、save 载荷、Escape 关闭、焦点陷阱）；
- FormSelectorDialog.spec.js：NRadioGroup/NRadio stub + 单选断言；
- InstallProgressDialog.spec.js：i18n key 断言 + focus 行为按现实现校准；
- ToolCatalogPage.spec.js 不动（已绿）。

### F4：卫生收尾

- 删除 `audit.test.config.mjs`（或加入 .gitignore——默认删除）；
- 修正 `.gitattributes` 的 mojibake 文件名行；
- 更新 HANDOFF.md：改写三条非阻塞项为修正后结论 + 记录 Spec 028 规划产出；
- handoff-backup 分支按 AGENTS.md 提交恢复点。

## 四、范围边界

**做：**
- internal/manifest/manifest.go 死递归修复 + 回归测试（manifest/plugin/java/installer 四包）
- 13 个 manifest 注释重写 + 字段恢复 + 编码守卫测试
- 前端 7 个失败文件修复 + EnvVarEditorDialog 新测试
- 卫生项（audit.test.config.mjs、.gitattributes、HANDOFF.md、backup 提交）

**不做：**
- 不重签/改签任何 manifest；不动签名机制与 38 个已签名 manifest
- 不重构 ArchiveForCurrentWithForm 的 6-pass 逻辑（保持 Spec 025 行为）
- 不改前端生产组件行为（除 vscode manifest 字段恢复外无生产代码变更；如 Phase C 发现组件侧真 bug，先记录再单独评估，不放本 spec 暗改）
- 不升级/降级 Go 工具链（死递归修复后 1.26.5 即可全绿，无需规避）
- 不做 vitest 配置层面的大改（不换环境、不加 threshold 门禁——门禁作为后续 spec 提议，记入「遗留非阻塞项」）

## 五、验证

1. `go test ./... -short -count=1` 全绿（含此前崩溃的 4 个包）
2. `go test ./internal/manifest/ -run TestArchiveForCurrent -count=3` 稳定通过（回归：死递归不再出现）
3. manifest 重写前后语义 diff 为空（解析 Tool 结构对比，忽略注释）
4. `npx vitest run` 全绿（113+ 测试 0 失败）
5. `npx vite build` 通过
6. `npx playwright test e2e/app.spec.js` 18/18
7. `wails build` 通过
8. 手动/单测验证：vscode manifest 解析后 GUIBinary == "Code.exe"；GUI 启动契约测试通过

## 六、风险

| 风险 | 缓解 |
|---|---|
| 重写注释时误动功能性字段 | 重写前后解析对比测试（struct diff 为空）作为硬门；每个文件单独提交 |
| mojibake 原文推断失真（有损损坏） | 注释仅作文档，失真不影响功能；以 cursor.yaml 干净原文为准，推断处标注「（推断）」 |
| 恢复 vscode gui_binary 改变 GUI 启动行为 | app_launch.go 有既有契约测试（app_launch_test.go），恢复字段后跑该测试确认无回归 |
| 前端测试重写时「迁就实现」掩盖真 bug | Phase C 每个失败逐一分类：明显契约过时→改测试；可疑行为→先查 git 历史/组件意图再定，需在 checklist 记录结论 |
| localStorage polyfill 掩盖真实环境问题 | polyfill 仅限测试 setup；生产代码路径不变 |

## 七、审批请求

请审批本 spec。获批后依次产出 plan / task / checklist，全部获批后开始实施。

## 批准记录 (Approval History)

> **批准ID**: `2026-08-09-spec-028-approved-001`
