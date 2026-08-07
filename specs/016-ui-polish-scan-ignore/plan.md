# Plan 016: UI 打磨 + 更新忽略 + 本地扫描结果与忽略清单

> **状态**：待用户批准（文档补全后交叉审验通过，待报批）  
> **依据**：`specs/016-ui-polish-scan-ignore/spec.md`（v1.1）

## 执行策略

```
Phase A: 后端忽略清单 + 过滤 API（TDD）
    ↓
Phase B: 前端 UI 打磨（按钮/布局/环境变量表）与 UpdateBadge 忽略
    ↓
Phase C: 本地扫描结果列表 + 忽略/迁移接线 + 忽略清单管理 UI
    ↓
Phase D: i18n、文档、构建 + 功能/安全/日志验收
```

A 与 B 中纯 CSS 可并行。C 依赖 A。

## 文件边界

| 文件 | 职责 |
|------|------|
| `internal/gui/app_ignore.go` | 忽略清单 CRUD + JSON 持久化（新建） |
| `internal/gui/app_ignore_test.go` | 忽略清单与过滤单测（新建） |
| `internal/gui/app_updates.go` | GetAvailableUpdates 过滤 kind=update |
| `internal/gui/app_local_detect.go` | ScanLocalEnvironments 过滤 kind=local-scan |
| `internal/gui/app.go` | loadIgnored；mutex/字段 |
| `frontend/src/components/UpdateBadge.vue` | 每项「忽略」；计数中文化 |
| `frontend/src/pages/DashboardPage.vue` | 嗅探/刷新同行；扫描结果表；忽略清单；`.xdev-btn` |
| `frontend/src/pages/EnvironmentVariablesPage.vue` | 行高/列宽/ellipsis |
| `frontend/src/styles/theme.css` | 全局 button 居中 + `.xdev-btn*` |
| `frontend/src/layouts/AppSidebar.vue` | 导航 text-align:left |
| `frontend/src/AppContent.vue` | ignore/scan/unignore/migrate 接线 |
| `frontend/src/i18n/locales/{zh,en}.ts` | 文案 |
| `docs/usage-manual.md` | 忽略清单与扫描结果短节 |

## Phase A: 忽略清单后端

### A.1 数据与存储

- 路径：`$XMDEV_HOME/cache/ignored_tools.json`（固定文件名，禁止用户输入拼路径）
- 复用 `saveJSONToCache` / `loadJSONFromCache`
- 硬上限：**2000** 条；超出 `IgnoreTool` 返回 error（spec T-S07）

### A.2 API 与校验

```go
type IgnoredToolInfo struct {
    Name      string `json:"name"`
    Kind      string `json:"kind"` // "update" | "local-scan"
    Path      string `json:"path,omitempty"`
    IgnoredAt string `json:"ignoredAt"`
}

func (a *App) GetIgnoredTools() []IgnoredToolInfo
func (a *App) IgnoreTool(name, kind, path string) error
func (a *App) UnignoreTool(name, kind, path string) error
```

校验规则（与 spec 输入校验表一致）：

- `name`：trim 后 1–64；`^[a-zA-Z0-9._-]+$`；拒绝 `..` `/` `\` NUL
- `kind`：仅 `update` | `local-scan`
- `path`：可选；≤512；无 NUL；**仅匹配键，不拼写路径**

### A.3 过滤

- `GetAvailableUpdates`：跳过 name 命中且 kind=update
- `ScanLocalEnvironments`：跳过 kind=local-scan；path 非空则 name+path 精确匹配，path 空则仅 name

### A.4 操作日志

实现时必须覆盖 spec 操作日志表 **L1–L10**（至少 INFO 级 L3/L6/L8；DEBUG 可按 log 级别输出）。

### A.5 测试（对应功能/安全用例）

| 测试 | 覆盖 |
|------|------|
| TestIgnoreTool_Idempotent | T-F05 |
| TestUnignoreTool | T-F04 |
| TestIgnoreTool_RejectsBadName | T-F17, T-S01, T-S04, T-S05 |
| TestIgnoreTool_RejectsBadKind | T-F18, T-S03 |
| TestLoadIgnored_CorruptJSON | T-F19, T-S06 |
| TestIgnoreTool_HardCap | T-S07 |
| TestFilterUpdates | T-F01/T-F02 后端侧 |
| TestFilterScan | T-F07 后端侧 |

**Verify:** `go test -vet=off ./internal/gui/ -count=1`

## Phase B: UI 打磨

### B.1 按钮（T-F14, T-F15）

- `theme.css`：`button { text-align: center; }` + `.xdev-btn` / `--primary` / `--secondary`
- `AppSidebar`：`button { text-align: left; }`
- Dashboard / 表格原生操作钮使用 `.xdev-btn`

### B.2 仪表盘布局（T-F10, T-F11）

- `.dashboard-page__update-actions` / 快捷操作：`display: flex; flex-wrap: wrap; gap: 8px;`
- 嗅探主、刷新次

### B.3 环境变量表（T-F12, T-F13）

- padding 垂直 8px；值列 ellipsis + title；列宽保证状态/操作可见

### B.4 UpdateBadge（T-F01, T-F16）

- 每项「忽略」；emit `ignore`
- 文案：`t('updates.count', { n: count })`

## Phase C: 扫描结果 + 忽略清单 UI

### C.1 扫描结果（T-F06–T-F09）

- `scanResults` 状态；表：工具/版本/路径/迁移/忽略
- 忽略：`IgnoreTool(name,'local-scan',path)` + 列表移除
- 迁移：`MigrateLocalTool`；成功 toast + 刷新

### C.2 忽略清单（T-F04, T-F08）

- 仪表盘 Section：GetIgnoredTools；取消忽略 → Unignore + 刷新 updates/scan

### C.3 更新忽略接线

- `@ignore` → IgnoreTool(tool,'update','') → loadUpdates

## Phase D: i18n / 文档 / 验证

### i18n keys

| key | zh | en |
|-----|----|----|
| `updates.ignore` | 忽略 | Ignore |
| `updates.count` | {n} 个更新 | {n} updates |
| `updates.ignored_ok` | 已忽略该更新 | Update ignored |
| `scan.results_title` | 本地扫描结果 | Local scan results |
| `scan.migrate` | 迁移 | Migrate |
| `scan.ignore` | 忽略 | Ignore |
| `scan.empty` | 未发现可接管的本地工具 | No manageable local tools found |
| `ignore.list_title` | 忽略清单 | Ignore list |
| `ignore.unignore` | 取消忽略 | Unignore |
| `ignore.empty` | 暂无忽略项 | No ignored items |
| `ignore.add_failed` | 忽略失败 | Failed to ignore |
| `ignore.remove_failed` | 取消忽略失败 | Failed to unignore |

### 验证命令

```powershell
$env:GOROOT = "E:\XDev\install\go\1.26.5\go"
$env:PATH = "E:\XDev\install\go\1.26.5\go\bin;" + ($env:PATH -replace "E:\\XDev\\shims;?", "")
go test -vet=off ./internal/gui/ -count=1
cd frontend; npm run build
# root
wails build
```

### Phase D 验收映射

| 类别 | 执行 |
|------|------|
| 功能测试 | 手工 T-F01–T-F20（见 spec）；自动化覆盖 A.5 |
| 安全审计 | 手工/单测 T-S01–T-S15 |
| 操作日志 | 触发 Ignore/Unignore/损坏 json，检查 xdev-gui.log 含 L* 约定 |
| 性能 | 可选：P1–P4 本地粗测 |
| Checklist | 用户勾选 `checklist.md` |

## 风险

| 风险 | 处理 |
|------|------|
| Wails 新 binding | wails build 生成 |
| 迁移 API 参数 | 优先 `MigrateLocalTool(name, path)` |
| 全局 button 副作用 | 侧栏 left 覆盖 |

## 不做

- 不自动改 Machine PATH  
- 不重做迁移向导  
- 不改 sniff 协议  


## 批准记录 (Approval History)

> **批准ID**: `2026-08-08-plan-016-001`
