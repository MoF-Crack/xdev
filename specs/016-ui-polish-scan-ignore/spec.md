# Spec 016: UI 打磨 + 更新忽略 + 本地扫描结果与忽略清单

> **文档类型**：Feature Spec
> **状态**：Approved
> **文档版本**：1.1.0
> **创建日期**：2026-07-31
> **最后更新**：2026-08-20
> **所有者**：xinyan <zuijiuhjd@foxmail.com>
> **权威范围**：UI 打磨、更新忽略与本地扫描结果清单的目标、范围和验收
> **替代文档**：无

## 背景与动机

近期 GUI 已支持三主题、仪表盘默认页、环境变量全量编辑等，但实测仍有：

1. 顶部「N updates」气泡只有安装入口，无法忽略不想处理的更新。
2. 仪表盘「全部嗅探 / 刷新」纵向堆叠，密度差。
3. 原生 `<button>` 文字左对齐、样式不统一，与 Naive UI 按钮割裂。
4. 环境变量表格行高过大、横向溢出，状态/操作列不可见。
5. 「扫描本地环境」仅 toast，没有可操作的结果列表；用户无法迁移或忽略某条本地检测。

## 目标

| ID | 目标 | 可观测成功标准 |
|----|------|----------------|
| G1 | 更新提示可忽略 | UpdateBadge 每项有「忽略」；忽略后徽标计数减少；可从忽略清单恢复 |
| G2 | 嗅探/刷新同行布局 | 仪表盘可用更新区两按钮同一行、对齐 |
| G3 | 按钮文字居中 | 全局操作按钮文字水平居中；侧栏导航可保持左对齐 |
| G4 | 按钮风格统一 | 仪表盘/表格操作按钮与 xdev 主题 token 一致（圆角、边框、hover） |
| G5 | 环境变量表可读 | 行高约 44–56px；状态与操作列默认可见；长值省略号 |
| G6 | 本地扫描结果可操作 | 扫描后展示列表：工具/版本/路径；支持迁移、忽略；忽略持久化且可管理 |

## 范围

### 在范围内

- 前端：UpdateBadge、DashboardPage、theme 全局按钮、EnvironmentVariablesPage、本地扫描结果 UI（仪表盘或专用区）。
- 后端：忽略清单持久化（复用 favorites 的 cache JSON 模式）；`GetAvailableUpdates` 过滤忽略；本地扫描结果返回可操作列表并过滤忽略。
- i18n：中英文文案。
- 文档：usage-manual 简短说明忽略清单与扫描结果。

### 不在范围内

- 重写整个设计系统 / 换组件库。
- 修改嗅探协议、GitHub token 策略（已有 014/015 与既有实现）。
- 自动从系统 PATH 删除冲突目录（仅可提示，不静默改 Machine PATH）。
- 完整迁移流程重做（复用现有 Junction/Move API）。

## 功能需求

### FR-1 更新忽略

1. `UpdateBadge` 每个更新项除「安装」外增加「忽略」。
2. 忽略键：工具名 `tool`（与 `UpdateInfo.tool` 一致）。
3. 忽略后：
   - 不再出现在可用更新列表；
   - 顶部徽标计数减少；
   - 持久化到 `$XMDEV_HOME/cache/ignored_tools.json`（`kind=update`，见数据模型）。
4. 提供「忽略清单」管理入口（仪表盘 Section 或快捷区）：列出已忽略项，可「取消忽略」。
5. 取消忽略后，若 sniff 缓存仍显示有更新，则重新出现在列表。

### FR-2 仪表盘嗅探/刷新布局

1. 可用更新卡片内「全部嗅探」「刷新」同一行 flex，水平间距 8–12px。
2. 「全部嗅探」为主按钮视觉权重；「刷新」为次级。
3. 快捷操作区「扫描本地环境」「检查更新」同样改为同一行（一致性）。

### FR-3 按钮文字居中与风格

1. `theme.css` 全局：`button { text-align: center; }`。
2. 侧栏导航按钮显式 `text-align: left` 覆盖。
3. 仪表盘原生操作按钮统一 class（如 `.xdev-btn`）：
   - 高度一致、圆角 `var(--xdev-radius-md)`、边框 `var(--xdev-border-strong)`；
   - 主操作可用 accent 边框/软底；次操作透明底；
   - hover 使用 `var(--xdev-accent-soft)`。
4. 表格内「安装 / 管理版本」等原生按钮使用同一 class，避免浏览器默认立体灰按钮。

### FR-4 环境变量表格密度

1. 单元格垂直 padding 降至约 8px（目标行高 44–56px）。
2. 变量值列：`max-width` + `ellipsis` + title 悬浮全文。
3. 表格布局保证「状态」「操作」列在常见宽度（≥900px）无需横向滚动即可见；必要时 `table-layout: fixed` + 列宽分配。
4. 仍保留窄屏卡片布局（≤700px）。

### FR-5 本地扫描结果 + 忽略清单

1. 点击「扫描本地环境」后：
   - 按钮有 loading（已有）；
   - 完成后在仪表盘展示 **扫描结果列表**：`name`、`displayName`、`version`、`path`。
2. 每行操作：
   - **迁移**：调用现有 `MigrateLocalTool(name, path)`（默认 junction 语义与现有一致）；
   - **忽略**：写入忽略清单，`kind=local-scan`。
3. 再次扫描时，忽略清单中的工具 **skip 不展示**。
4. **忽略清单管理 UI**：
   - 展示 name、kind（update / local-scan）、path（可选）、ignoredAt；
   - 可移除忽略；
   - 移除后下次扫描/更新列表可再次出现。
5. 持久化路径：`$XMDEV_HOME/cache/ignored_tools.json`，复用 `saveJSONToCache` / `loadJSONFromCache` + mutex（同 favorites）。

## 数据模型

```json
// $XMDEV_HOME/cache/ignored_tools.json
{
  "items": [
    {
      "name": "cangjie",
      "kind": "update",
      "path": "",
      "ignoredAt": "2026-07-31T00:00:00+08:00"
    },
    {
      "name": "java",
      "kind": "local-scan",
      "path": "D:\\Env\\Java\\jdk-17",
      "ignoredAt": "2026-07-31T00:00:00+08:00"
    }
  ]
}
```

- `kind`: 仅允许 `update` | `local-scan`
- 唯一键：`kind=update` 时为 `name`；`kind=local-scan` 时为 `name` + 规范化 `path`（path 空则仅 name）
- 重复忽略幂等；`ignoredAt` 使用本地时区 RFC3339

## 后端 API（Wails bindings）

| 方法 | 行为 |
|------|------|
| `GetIgnoredTools() []IgnoredToolInfo` | 列出忽略项 |
| `IgnoreTool(name, kind, path string) error` | 加入忽略清单 |
| `UnignoreTool(name, kind, path string) error` | 移除 |
| `GetAvailableUpdates()` | 过滤 `kind=update` 忽略项 |
| `ScanLocalEnvironments()` | 过滤 `kind=local-scan` 忽略项（按 name；path 非空则精确匹配） |

### 输入校验（强制）

| 字段 | 规则 |
|------|------|
| `name` | 非空；trim 后长度 1–64；仅允许 `[a-zA-Z0-9._-]`；拒绝 `..`、`/`、`\`、NUL |
| `kind` | 必须是 `update` 或 `local-scan` |
| `path` | 可选；若非空：长度 ≤ 512；不得含 NUL；不得用于写文件路径拼接（仅作匹配键） |

校验失败返回明确 error，不写盘。

## 操作日志规范

所有日志 **不得** 打印 GitHub token、代理密码、完整用户 profile 密钥。路径可记，但截断超长 path（>200 字符时记前 200 + `...`）。

| 操作 | 级别 | 日志格式（示例） | 触发点 |
|------|------|------------------|--------|
| L1 加载忽略清单成功 | DEBUG | `[ignore] loaded n=%d from %s` | loadIgnored |
| L2 加载忽略清单失败（损坏/IO） | WARN | `[ignore] load failed: %v — using empty list` | loadIgnored |
| L3 忽略成功 | INFO | `[ignore] add name=%q kind=%q path=%q` | IgnoreTool |
| L4 忽略幂等（已存在） | DEBUG | `[ignore] already ignored name=%q kind=%q` | IgnoreTool |
| L5 忽略失败（校验/写盘） | ERROR | `[ignore] add failed name=%q kind=%q: %v` | IgnoreTool |
| L6 取消忽略成功 | INFO | `[ignore] remove name=%q kind=%q path=%q` | UnignoreTool |
| L7 取消忽略未找到 | DEBUG | `[ignore] remove miss name=%q kind=%q` | UnignoreTool |
| L8 保存忽略清单失败 | ERROR | `[ignore] save failed: %v` | saveIgnored |
| L9 更新列表过滤 | DEBUG | `[ignore] filtered updates: before=%d after=%d` | GetAvailableUpdates |
| L10 扫描列表过滤 | DEBUG | `[ignore] filtered scan: before=%d after=%d` | ScanLocalEnvironments |
| L11 扫描完成（UI） | INFO 或 toast | 已有 scan toast；可选 `[scan] local tools found n=%d` | scanLocal 成功路径 |
| L12 迁移从扫描结果发起 | INFO | `[scan] migrate request name=%q path=%q` | 前端调用 migrate 前/后由现有 migrate 日志覆盖亦可 |

前端：Ignore/Unignore/Scan 失败用 `message.error`；成功用 `message.success`/`info`（短文案，不堆栈）。

## 功能测试矩阵

| 用例 | 前置 | 操作 | 预期结果 |
|------|------|------|----------|
| **T-F01** | sniff 缓存显示 ≥2 个可用更新 | UpdateBadge 对 tool A 点「忽略」 | A 从列表消失；徽标 count−1；`ignored_tools.json` 含 `name=A,kind=update` |
| **T-F02** | T-F01 后 | 点「刷新」/重新 `GetAvailableUpdates` | A 仍不出现；count 不变 |
| **T-F03** | T-F01 后重启 GUI | 打开 UpdateBadge | A 仍被忽略（持久化） |
| **T-F04** | T-F01 后 | 忽略清单对 A 点「取消忽略」 | A 重新出现（若缓存仍有更新）；json 无 A+update |
| **T-F05** | 对已忽略 A 再点忽略 | IgnoreTool(A, update, "") | 幂等成功；列表不重复项 |
| **T-F06** | 本地存在 ≥1 可扫描工具 | 点「扫描本地环境」 | loading 可见；结果表展示 name/version/path；toast 有反馈 |
| **T-F07** | T-F06 列表含 tool B | 对 B 点「忽略」 | B 从结果结果移除；json 含 `kind=local-scan`；再扫描不出现 B |
| **T-F08** | T-F07 后 | 忽略清单取消 B | 再扫描可再次出现 B |
| **T-F09** | T-F06 列表含 tool C 且路径有效 | 对 C 点「迁移」 | 调用现有 migrate；成功 toast；dashboard 可刷新 |
| **T-F10** | 仪表盘可用更新区 | 目视 | 「全部嗅探」「刷新」同一行，间距 8–12px |
| **T-F11** | 仪表盘快捷操作 | 目视 | 「扫描」「检查更新」同一行 |
| **T-F12** | 视口 ≥900px 环境变量页 | 目视 | 状态列、操作列无需横滚即可见；行高约 44–56px |
| **T-F13** | 环境变量值超长 | 目视 | 值省略；title/悬浮可见全文 |
| **T-F14** | 仪表盘/表格原生按钮 | 目视 | 文字居中；风格与主题 token 一致，非浏览器默认灰立体钮 |
| **T-F15** | 侧栏导航 | 目视 | 文字仍左对齐（不被全局 center 破坏） |
| **T-F16** | UpdateBadge count>0 | 目视 | 文案为中文「N 个更新」类，非硬编码英文 `N updates` |
| **T-F17** | IgnoreTool name 含 `../` 或 `/` | API 调用 | 返回 error；json 不写盘 |
| **T-F18** | IgnoreTool kind=`foo` | API 调用 | 返回 error |
| **T-F19** | 损坏的 ignored_tools.json | 启动 GUI / GetIgnoredTools | WARN 日志；空列表；不崩溃 |
| **T-F20** | 无可用更新且无忽略 | UpdateBadge | empty 文案；count=0 不显示徽标数字 |

## 性能约束

| 用例 | 指标 |
|------|------|
| P1 | `IgnoreTool` / `UnignoreTool`（≤100 项清单）写盘 < 50ms（本地 SSD） |
| P2 | `GetIgnoredTools` < 20ms |
| P3 | `GetAvailableUpdates` 过滤额外开销 < 5ms（相对过滤前） |
| P4 | `ScanLocalEnvironments` 过滤额外开销 < 5ms |
| P5 | 仪表盘扫描结果表渲染 ≤100 行不卡顿（可接受简单列表，不做虚拟滚动） |

## 回归安全审计

| 用例 | 审计维度 | 操作 | 预期结果（安全不变式） |
|------|----------|------|------------------------|
| **T-S01** | 路径穿越（name） | `IgnoreTool("../../../etc/passwd", "update", "")` | 拒绝；不创建异常文件 |
| **T-S02** | 路径穿越（path） | `IgnoreTool("java", "local-scan", "D:\\..\\..\\Windows")` 若含 `..` 规范化后仍仅作字符串匹配，**禁止**将 path 拼到写路径 | 写盘仅 `cache/ignored_tools.json` 固定路径 |
| **T-S03** | 非法 kind | `IgnoreTool("go", "delete-all", "")` | 拒绝 |
| **T-S04** | 空 name | `IgnoreTool("", "update", "")` | 拒绝 |
| **T-S05** | NUL 注入 | name 或 path 含 `\x00` | 拒绝 |
| **T-S06** | 清单文件篡改 | 手工改 json 为非法 JSON | 加载失败 → 空列表 + WARN；不 panic |
| **T-S07** | 清单超大 | 构造 items 超 10_000 条（可选上限） | 建议硬上限 2000 条：超出拒绝新增并 ERROR 日志；或截断加载并 WARN（实现选定一种并在代码注释声明：**采用硬上限 2000，超出 Ignore 返回 error**） |
| **T-S08** | 过滤不可绕过 | 忽略 A 后直接改前端 state 无法写注册表；后端 GetAvailableUpdates 仍过滤 | 权威在后端过滤 |
| **T-S09** | 不触碰 PATH/Machine | Ignore/Unignore 全程 | 不写 HKLM；不修改 User/Machine PATH |
| **T-S10** | 无 secrets 日志 | Ignore 含异常长 path | 日志截断；无 token/密码字段 |
| **T-S11** | XSS/HTML | tool 名含 `<script>` 显示在 UI | 文本插值不执行 HTML（Vue 默认 escape） |
| **T-S12** | 迁移权限 | 从扫描结果迁移 | 仅调用既有 Migrate API；不扩大权限 |
| **T-S13** | 回归：favorites | 忽略功能上线后 | favorites.json 读写行为不变 |
| **T-S14** | 回归：环境变量页 | 改表密度后 | 用户/系统级编辑、保护变量拒绝逻辑不变 |
| **T-S15** | 回归：嗅探 | 布局/按钮改动后 | SniffAll / GetAvailableUpdates 行为不变（仅多过滤） |

## 非功能（汇总）

- 忽略清单读写失败：日志 WARN/ERROR，不崩溃；UI toast。
- 不在日志打印 secrets。
- 中英文 i18n 齐全。
- 回归：`go test ./internal/gui/`；`npm run build`；`wails build`。

## 验收场景（用户可勾选）

见 `checklist.md`。概要：

1. 有 2 个可用更新 → 忽略 1 个 → 徽标变为 1；刷新后仍为 1。
2. 忽略清单取消忽略 → 再次出现（若 sniff 仍有更新）。
3. 仪表盘「全部嗅探」「刷新」同一行。
4. 环境变量表默认可见操作列；行高约 44–56px。
5. 扫描本地 → 列表 → 忽略 → 再扫不出现 → 取消忽略后可再出现。
6. 全局操作按钮文字居中、风格与主题一致。

## 风险与缓解

| 风险 | 缓解 |
|------|------|
| 忽略后用户忘记 | 提供忽略清单入口 |
| 迁移与扫描结果两套 API | 扫描列表优先 `ScanLocalEnvironments`；迁移按钮调用现有 migrate |
| 全局 button 样式影响侧栏 | 侧栏显式 left 覆盖 |
| ignored_tools.json 损坏 | 空列表降级 + WARN |
| 清单无限增长 | 硬上限 2000 条 |

## 相关文档

- 截图：仪表盘更新区纵向按钮、已安装操作列原生按钮、环境变量高行高
- 复用：`internal/gui/app_favorites.go` 持久化模式
- 既有：`docs/usage-manual.md` 嗅探与 GitHub Token 章节
- Plan：`plan.md`；Tasks：`tasks.md`；Checklist：`checklist.md`


## 批准记录 (Approval History)

| 阶段 | 状态 | 批准者 | 日期 | 文档版本 | 批准范围 |
|---|---|---|---|---|---|
| Spec 批准 | Approved | `xinyan <zuijiuhjd@foxmail.com>` | 2026-08-08 | `1.1.0` | `2026-08-20-spec-016-approved-001` UI 打磨、更新忽略与本地扫描结果清单的目标、范围和验收 |
