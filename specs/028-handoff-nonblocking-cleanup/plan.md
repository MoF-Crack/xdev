# Spec 028 Plan: 遗留非阻塞项清理

> **状态**：已批准（2026-08-09）
> **前置**：Spec 028 spec 已获批
> **日期**：2026-08-09

## 总原则

- **先修 P0 死递归**：独立 Phase，可单独收敛、单独提交备份点
- **manifest 只动注释与恢复字段**：功能性字段逐字节不动，diff 门禁兜底
- **前端只修测试不暗改组件**：每个失败先分类（环境缺失 / 测试契约过时 / 真 bug），真 bug 记录后显式处理
- **验证门**：每个 Phase 结束跑对应验证命令，Phase D 全量回归

## Phase 划分

```
A 死递归修复（P0，Go 单文件 + 4 包回归）
   │
   ├─ B manifest 清理（13 文件 + 守卫测试，独立于 A）
   └─ C 前端 vitest 修复（7 文件 + 1 新文件，独立于 A/B）
        └─ D 收尾（卫生项 + HANDOFF + 全量回归 + backup 提交）
```

A/B/C 相互独立可并行；D 依赖 A+B+C。

---

## Phase A — ArchiveForCurrent 死递归修复（P0）

### A1：提取 helper 并断开递归
**涉及**：`internal/manifest/manifest.go`（L593-658 区域）

改动（语义不变）：
1. 新增私有方法 `func (v *Version) archiveFormAgnostic(osName, arch string) (*Archive, bool)`，实现 legacy 3-pass：
   - Pass 1：exact os+arch（**忽略 Form**）
   - Pass 2：os + arch=="any"
   - Pass 3：os=="any"
2. `ArchiveForCurrent` → `return v.archiveFormAgnostic(osName, arch)`
3. `ArchiveForCurrentWithForm`：`if formFilter == "" { return v.archiveFormAgnostic(osName, arch) }`；非空分支的 6-pass 逻辑**一行不动**
4. 同步修正函数 doc（说明空 formFilter 走 legacy 且 Form 被忽略）

**验收**：`go build ./...` 通过

### A2：回归测试
**涉及**：`internal/manifest/manifest_test.go`（新增）、既有 4 包崩溃测试自动复活

新增测试用例（见下 TC-A 表）后：
**验收**：
1. `go test ./internal/manifest/ ./internal/plugin/ ./internal/plugins/java/ ./internal/installer/ -short -count=1` 全绿（原崩溃测试复活）
2. `go test ./... -short -count=1` 全绿

### 测试用例（TC-A）

| ID | 用例 | 步骤 | 预期 |
|---|---|---|---|
| TC-A1 | 死递归回归（直接入口） | 调 `v.ArchiveForCurrent("windows","amd64")`（fixture 含精确匹配） | 返回该 archive，无 panic；测试 1s 内完成（栈溢出会 >1s 且 fatal） |
| TC-A2 | 死递归回归（WithForm 空过滤） | 调 `v.ArchiveForCurrentWithForm(os, arch, "")` | 与 TC-A1 结果指针相同 |
| TC-A3 | legacy 忽略 Form | fixture：os+arch 精确匹配的 archive 带 `Form:"gui"` | `ArchiveForCurrent` 仍选中它（legacy 不看 Form） |
| TC-A4 | legacy 回退顺序 | fixture：无 exact，有 os+any 与 any-os 两条 | 选中 os+any（Pass 2 先于 Pass 3） |
| TC-A5 | WithForm 6-pass 不回归 | fixture：同 os+arch 有 Form=="gui" 与 Form=="" 两条；formFilter="gui" | 选中 Form=="gui"（Pass 1）；formFilter 匹配不到时降级 Form==""（Pass 2） |
| TC-A6 | WithForm 空过滤不进入 6-pass | 同 TC-A2 fixture，archives 全带 Form | 选中 exact os+arch（若按 6-pass Pass1 要求 Form==formFilter=="" 会漏选——断言防回归到「去重」的另一种错误写法） |
| TC-A7 | 生产链 smoke | `plugin.NewGeneric(tool).ArchiveFor("21")`（复用 java 测试 fixture 模式） | 返回 archive 或明确错误，无 panic |

---

## Phase B — 13 个 manifest mojibake 清理

### B1：分组重写

| Task | 文件 | 操作要点 |
|---|---|---|
| T-B1a | claude-code / codex / copilot-cli / opencode（npm 平台包 ×4） | 注释头重写为干净中文（平台包策略：optionalDependencies 指向真实二进制包、platform tgz 布局、remote 归主包、sha256 仅实测 Windows amd64 其余留空 TOFU 兜底）；恢复 `name:` |
| T-B1b | JetBrains ×6（clion/goland/pycharm-community/rubymine/webstorm/jetbrains-idea-community） | 同模板重写（Win exe /S /D={{install}}、mac dmg、linux tar.gz strip 1、自带 JBR 无 depends、gui_binary 单字段、sha256 实测 Windows）；恢复 `name:` |
| T-B1c | vscode | 重写 L1 注释头 + **恢复 `gui_binary: Code.exe` 真字段**（功能修复）；恢复 `name:` |
| T-B1d | zed | 仅重写 L2（版本源/GitHub releases/Inno 静默参数/mac dmg/linux tar.gz strip 1）；恢复 `name:` |
| T-B1e | cursor | 仅删除 L15 损坏重复块（L2-14 已有干净原文）；恢复 `name:` |

统一要求：UTF-8 无 BOM、LF 换行、功能性字段逐字节不动、`name:` 值 = filename stem。

**验收（每个子任务）**：`go test ./internal/manifest/ -run TestManifestEncodingGuard -count=1` 通过（B2 先行落地守卫，再逐文件过闸）

### B2：编码守卫测试（先行落地）
**涉及**：`internal/manifest/manifest_encoding_guard_test.go`（新建）

守卫规则（TC-B1~B4）：
1. 每个 `manifests/*.yaml` 必须合法 UTF-8
2. 不得含 mojibake 标记：`鈥 鍘 瀹 淇 锛 銆 鏈 鏃 绾 鍣 甯 鐩 閿 垎 钖 鍙` 及 U+FFFD
3. 解析后 `Name != ""` 且 `Name == strings.TrimSuffix(filename, ".yaml")`（消除对 loader fallback 的隐性依赖）
4. `form_factor` 含 "gui" 且 `len(shims)>0` 的 manifest 必须声明非空 `gui_binary`（vscode 回归断言）

注意：守卫只覆盖未签名/全部 manifest 的编码与结构合法性，不校验 .sig（签名体系不动）。签名 manifest 也满足规则 1-3，故全量扫描安全。

### B3：语义不变性 diff 门禁
**涉及**：`internal/manifest/manifest_cleanup_diff_test.go`（新建，或并入 B2）

- 对 13 个目标文件：重写前保存解析后的 `*Tool`（yaml 解析 + 规范化），重写后重新解析，对比除 Name/GUIBinary（被恢复字段）外全部字段 `reflect.DeepEqual` 为空 diff。
- 实现方式：测试内置「重写前快照」字面量（而非读 git），避免测试依赖历史；若快照维护成本高，退化为：断言关键字段集合（category/versions 数量/首版本 URL/SHA256）与当前文件一致 + `go test ./...` 全绿。

**验收**：`go test ./internal/manifest/ -short -count=1` 全绿；`go test ./... -short -count=1` 全绿

### 测试用例（TC-B）

| ID | 用例 | 步骤 | 预期 |
|---|---|---|---|
| TC-B1 | 编码守卫：UTF-8 合法 | 逐字节读 manifests/*.yaml，UTF8 严格解码 | 无错误 |
| TC-B2 | 编码守卫：无 mojibake 标记 | 在全部 manifest 内容中搜索标记字符集 | 0 命中（当前 13 个文件命中 → 先红后绿） |
| TC-B3 | 编码守卫：name 显式声明 | 解析每个 manifest | `Name != ""` 且等于 filename stem（当前 13 个依赖 fallback → 先红后绿） |
| TC-B4 | 编码守卫：gui 形态必有 gui_binary | 解析每个 manifest，检查 form_factor/shim 组合 | vscode 当前失败（字段被吞）→ 修复后通过 |
| TC-B5 | 语义不变：功能字段 | 对 13 文件断言 category/display_name/versions[0].archives[0].{url,sha256} 与重写前记录一致 | 全等 |
| TC-B6 | 语义不变：YAML 可解析 | 13 文件全部经 catalog loader 加载 | 无错误、无 WARNING（如 depends/form_factor 校验告警） |
| TC-B7 | vscode GUI 契约 | `manifest.Tool.GUIBinary` 断言 + 跑 `go test ./internal/gui/ -run Launch`（app_launch_test.go 既有契约） | GUIBinary=="Code.exe"；launch 契约测试全绿 |

---

## Phase C — 前端 vitest 修复

### C1：localStorage polyfill（3 文件）
**涉及**：`frontend/test/setup.js`

- 以 `vi.stubGlobal('localStorage', ...)` 或 defineProperty 注入内存实现（getItem/setItem/removeItem/clear/key/length）
- **验收**：`npx vitest run test/App.test.js test/SetupWizard.spec.js test/i18n.spec.js` 全绿

### C2：AppShell 测试补 `t` prop（8 测试）
**涉及**：`frontend/test/AppShell.spec.js`
- `mountShell()` 默认注入 `t: (key) => key`（与既有 props 合并，允许覆盖）
- **验收**：`npx vitest run test/AppShell.spec.js` 8/8 通过

### C3：EnvironmentVariablesPage 测试契约重写（4 失败 + 保 4 通过）
**涉及**：`frontend/test/pages/EnvironmentVariablesPage.spec.js`

| 原失败测试 | 处理 |
|---|---|
| renders the structured not-applicable status | 断言改为 `[data-row] .status-dot` 的 `title`/`aria-label` 属性含 `env.not-applicable`（文本渲染路径已改为属性） |
| supports development/all scopes... | 删除 `scope-all` 断言（scope 切换已移除）；保留 level 过滤 + validate + partial 状态断言 |
| supports edit, add, save, cancel, browse... | 页面侧只断言 `edit`/`delete`/`new`/`validate` 事件；编辑/浏览/保存流程移到 C4 对话框测试 |
| fills the value from browse and locks identity | 同左，迁移至 C4 |

- **验收**：`npx vitest run test/pages/EnvironmentVariablesPage.spec.js` 全绿

### C4：新建 EnvVarEditorDialog.spec.js（契约测试）
**涉及**：`frontend/test/components/EnvVarEditorDialog.spec.js`（新建）

| ID | 用例 | 步骤 | 预期 |
|---|---|---|---|
| TC-C1 | 打开与焦点 | `show:true` 挂载，nextTick | 对话框可见（role=dialog），name 输入获得焦点 |
| TC-C2 | 编辑锁定 | `editing` 传入 | name 输入 disabled，level 选择器不渲染；title 用 `env.edit_title` |
| TC-C3 | 新建模式 | 无 `editing` | name 可编辑，level 选择器可见（user/machine） |
| TC-C4 | browse 填充 | `browseDirectory` mock 返回路径，点 browse | 首条空 path 被填充；再次 browse 追加第二条 |
| TC-C5 | 多路径增删 | 点 add-path；点某条 remove | 路径数 +1 / -1（最后一条删除后自动补空） |
| TC-C6 | save 载荷 | 填 name、两路径、level=machine，点 save | emit save：`{name, level:'machine', value:'a;b', original:null}`；emit update:show false |
| TC-C7 | 编辑 save 载荷 | `editing` 传入后 save | `original` = editing 的 {toolName,name,level}；name 不可变 |
| TC-C8 | machine 提示 | level=machine | 显示 `env.machine_hint` |
| TC-C9 | Escape 关闭 | keydown Escape | emit update:show false；不 emit save |
| TC-C10 | 无效路径标记 | `editing.pathEntries` 含 exists=false 路径 | 对应 path-entry 有 `path-entry--invalid` 类 + 显示 `env.path_missing` |
| TC-C11 | save 禁用 | name 为空 | save 按钮 disabled |

- **验收**：`npx vitest run test/components/EnvVarEditorDialog.spec.js` 全绿

### C5：FormSelectorDialog 测试更新（2 失败）
**涉及**：`frontend/test/components/FormSelectorDialog.spec.js`
- 将 NCheckbox stub 替换为 NRadioGroup/NRadio（checked/value/label、emit update:value）语义 stub；单选断言改为 radio `checked` 属性
- **验收**：`npx vitest run test/components/FormSelectorDialog.spec.js` 全绿（3/3）

### C6：InstallProgressDialog 测试更新（3 失败）
**涉及**：`frontend/test/components/InstallProgressDialog.spec.js`
- phase 断言改为 i18n key（`t('installing_downloading', {percent:42})` 的实际渲染产物）；error 状态断言对齐 `installing.*`/错误展示路径；focus 测试按现实现校准（先读组件 focus 逻辑再改，若发现组件行为异常按「真 bug」流程记录）
- **验收**：`npx vitest run test/components/InstallProgressDialog.spec.js` 全绿（4/4）

### C7：全量前端验证
**验收**：
1. `npx vitest run` 全绿（113+ 测试 0 失败）
2. `npx vite build` 通过
3. `npx playwright test e2e/app.spec.js` 18/18

---

## Phase D — 收尾

### D1：卫生项
- 删除 `audit.test.config.mjs`（未跟踪残留）
- 修正 `.gitattributes` 的 `UNI_ENV_寮€鍙戞枃妗?md` → `UNI_ENV_开发文档.md`（验证：`git check-attr whitespace -- "UNI_ENV_开发文档.md"` 输出 whitespace: unset）

### D2：文档与恢复点
- 更新 `HANDOFF.md`：
  - 「已知非阻塞项」三条改写为修正后结论（死递归=已修 P0；manifest=已清理+守卫；vitest=已全绿），附复核日期与证据命令
  - 追加 Spec 028 完成记录与关键文件索引
- 按 AGENTS.md 提交 handoff-backup：
  - Phase A 收敛（L2 门全绿）提交一次：`Spec 028 A: ArchiveForCurrent 死递归修复 + 4 包回归全绿`
  - 全流程收敛（LOOP 出口）提交一次：`Spec 028 B3: manifest 清理 + vitest 全绿 + e2e 18/18 + wails build`

### D3：全量回归
- `go test ./... -short -count=1`
- `npx vitest run` + `npx vite build` + `npx playwright test e2e/app.spec.js`
- `wails build`

---

## 验证标准汇总（Definition of Done）

1. `go test ./... -short -count=1` 全绿（含原崩溃 4 包）
2. `go test ./internal/manifest/ -run TestArchiveForCurrent -count=3` 稳定通过
3. manifest 编码守卫测试通过；13 文件语义 diff 为空
4. `npx vitest run` 全绿；`npx vite build` 通过；e2e 18/18
5. `wails build` 通过
6. git status 干净（audit.test.config.mjs 已处理）
7. handoff-backup 新增 ≥2 个提交点；HANDOFF.md 已更新

**审批请求**：请审批本 plan。

## 批准记录 (Approval History)

> **批准ID**: `2026-08-09-plan-028-001`
