# Tasks 016: UI 打磨 + 更新忽略 + 本地扫描结果与忽略清单

> **状态**：待用户批准  
> **依据**：`specs/016-ui-polish-scan-ignore/spec.md` v1.1 + `plan.md` + `checklist.md`

## 任务总览

| ID | 任务 | 依赖 | 验收要点 | 覆盖用例 |
|----|------|------|----------|----------|
| T001 | 忽略清单后端 CRUD + 持久化 + 校验 + 日志 | — | 单测通过；JSON 落盘；L1–L8 | T-F05/17/18/19, T-S01–S07 |
| T002 | GetAvailableUpdates / ScanLocal 过滤 | T001 | 忽略项不出现；L9–L10 | T-F01/02/07, T-S08 |
| T003 | 全局按钮居中 + `.xdev-btn` | — | 侧栏左对齐、操作钮统一 | T-F14/15 |
| T004 | 仪表盘嗅探/刷新 + 快捷操作同行 | T003 | 同一行 flex | T-F10/11 |
| T005 | 环境变量表行高与列可见性 | — | 状态/操作可见，行高 44–56px | T-F12/13, T-S14 |
| T006 | UpdateBadge 忽略 + 计数中文化 | T002 | 忽略后徽标减 1 | T-F01–04/16 |
| T007 | 扫描结果列表 UI + 迁移/忽略 | T002 | 列表可操作 | T-F06–09, L11–L12 |
| T008 | 忽略清单管理 UI | T001 | 可取消忽略 | T-F04/08 |
| T009 | i18n + usage-manual 短节 | T006–T008 | 中英文齐全 | 文档 |
| T010 | 构建 + 功能/安全/日志验收 | 全部 | checklist 勾选 | T-F* / T-S* / L* / P* |

---

### T001: 忽略清单后端

**Files:**
- Create: `internal/gui/app_ignore.go`
- Create: `internal/gui/app_ignore_test.go`
- Modify: `internal/gui/app.go`（loadIgnored / 字段）

**Steps:**
- [ ] 定义 `IgnoredToolInfo` + 内存列表 + mutex
- [ ] `loadIgnored` / `saveIgnored` 使用固定文件名 `ignored_tools.json`
- [ ] 实现校验：name/kind/path（spec 输入校验表）
- [ ] 硬上限 2000 条
- [ ] 实现 `GetIgnoredTools` / `IgnoreTool` / `UnignoreTool`
- [ ] 实现操作日志 L1–L8
- [ ] 测试：幂等、Unignore、坏 name/kind/NUL、损坏 JSON、硬上限

**Verify:** `go test -vet=off ./internal/gui/ -run Ignore -count=1`

---

### T002: 过滤集成

**Files:**
- Modify: `internal/gui/app_updates.go`
- Modify: `internal/gui/app_local_detect.go`

**Steps:**
- [ ] GetAvailableUpdates 跳过 kind=update（L9）
- [ ] ScanLocalEnvironments 跳过 kind=local-scan（L10；path 规则见 spec）
- [ ] 测试 FilterUpdates / FilterScan

**Verify:** `go test -vet=off ./internal/gui/ -count=1`

---

### T003: 全局按钮风格

**Files:**
- Modify: `frontend/src/styles/theme.css`
- Modify: `frontend/src/layouts/AppSidebar.vue`
- Modify: `frontend/src/pages/DashboardPage.vue`

**Steps:**
- [ ] 全局 `button { text-align: center; }`
- [ ] `.xdev-btn` / primary / secondary
- [ ] 侧栏 `text-align: left`
- [ ] 仪表盘操作钮使用 class

**Verify:** 视觉 T-F14/15 + `npm run build`

---

### T004: 仪表盘按钮同行

**Files:**
- Modify: `frontend/src/pages/DashboardPage.vue`

**Steps:**
- [ ] 可用更新区 flex row：嗅探 + 刷新
- [ ] 快捷操作区 flex row：扫描 + 检查更新

**Verify:** T-F10/11

---

### T005: 环境变量表密度

**Files:**
- Modify: `frontend/src/pages/EnvironmentVariablesPage.vue`

**Steps:**
- [ ] padding 垂直 8px
- [ ] 值列 ellipsis + title
- [ ] 列宽保证状态/操作可见（≥900px）

**Verify:** T-F12/13；T-S14 编辑/保护变量行为不变

---

### T006: UpdateBadge 忽略

**Files:**
- Modify: `frontend/src/components/UpdateBadge.vue`
- Modify: `frontend/src/AppContent.vue`
- Modify: i18n zh/en

**Steps:**
- [ ] 每项「忽略」+ emit
- [ ] IgnoreTool + loadUpdates
- [ ] 计数中文化 `updates.count`

**Verify:** T-F01–04/16；重启仍忽略

---

### T007: 本地扫描结果列表

**Files:**
- Modify: `frontend/src/pages/DashboardPage.vue`
- Modify: `frontend/src/AppContent.vue`

**Steps:**
- [ ] scanResults 状态
- [ ] 表：工具/版本/路径/迁移/忽略
- [ ] 迁移现有 API；忽略 local-scan
- [ ] 可选 L11/L12 日志或 toast

**Verify:** T-F06–09

---

### T008: 忽略清单管理

**Files:**
- Modify: DashboardPage（Section）
- Modify: AppContent.vue

**Steps:**
- [ ] GetIgnoredTools 列表
- [ ] UnignoreTool + 刷新 updates/scanResults

**Verify:** T-F04/08

---

### T009: i18n + 文档

**Files:**
- `frontend/src/i18n/locales/zh.ts` / `en.ts`
- `docs/usage-manual.md`

**Steps:**
- [ ] plan 中全部 i18n keys
- [ ] 文档：忽略清单路径、扫描结果迁移/忽略、取消忽略

**Verify:** 无裸 key；文档可检索

---

### T010: 构建与交叉验收

```powershell
$env:GOROOT = "E:\XDev\install\go\1.26.5\go"
$env:PATH = "E:\XDev\install\go\1.26.5\go\bin;" + ($env:PATH -replace "E:\\XDev\\shims;?", "")
go test -vet=off ./internal/gui/ -count=1
cd frontend; npm run build
# root
wails build
```

- [ ] 对照 `checklist.md` 逐项勾选
- [ ] 功能：至少 T-F01/03/04/06/07/10/12/17
- [ ] 安全：至少 T-S01/03/06/09/11
- [ ] 日志：L3/L6/L2 或 L8 抽检
- [ ] 性能 P1–P4 可选粗测

---

## 完成定义

- [ ] Spec 全部 FR（G1–G6）有对应任务产出
- [ ] 功能测试矩阵关键项有自动化或手工记录
- [ ] 安全审计关键项通过
- [ ] 操作日志符合 L 表
- [ ] `checklist.md` 用户验收通过
