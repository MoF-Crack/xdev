# Spec 027 Plan: 包管理器多实例检测 + 迁移

> **状态**：待批准
> **前置**：Spec 027 已获批
> **日期**：2026-08-05

## 总原则

- **不改数据模型语义**：PackageManagerInfo 现有字段（Installed/Version/LocalPath）保持不变，只新增 Instances
- **增量检测**：现有单实例检测逻辑保留，额外收集多实例
- **迁移轻量**：附属工具迁移 = 链接/复制到 xdev 目录，不重装
- **验证门**：每个 Phase 跑测试，C 阶段端到端验证

## Phase 划分

```
A 后端能力（A1 Instances 字段 + A2 多实例检测 + A3 MigratePackageManager API）
   │  （A1→A2→A3 串行，同文件）
   ├─ B 前端多实例列表 + 迁移按钮（依赖 A API 定型）
   └─ C 集成验证
```

## Phase A — 后端能力

### A1：PackageManagerInfo + PMInstanceInfo 类型
**涉及**：`internal/gui/app.go`
- 新增 `type PMInstanceInfo struct { Version, Path string; Managed bool; Source string }`
- `PackageManagerInfo` 新增 `Instances []PMInstanceInfo` json 字段
- **验收**：编译通过

### A2：多实例检测
**涉及**：`internal/gui/app_local_detect.go`
- `detectPackageManager` 扩展：收集所有检测到的实例
  - xdev 管理实例：`currentVersion(toolName)` + `shim.FindInDir` → Managed=true, Source="xdev"
  - 系统实例：`exec.LookPath` → 若路径与 xdev 实例不同 → Managed=false, Source="local"
  - 去重：按规范化路径比较
- `info.Installed`/`Version`/`LocalPath` 保持现有语义（xdev 优先），Instances 列出全部
- **测试**：模拟 xdev+系统双实例 → Instances 长度=2；单实例 → 长度=1；去重
- **验收**：`go test ./internal/gui/ -short -run "Instance"`

### A3：MigratePackageManager API
**涉及**：`internal/gui/app_local_detect.go`
- `func (a *App) MigratePackageManager(toolName string) error`
- 附属工具（toolName 非空，如 pip 的 toolName="python"）：在 xdev 管理的父工具目录创建附属工具的链接
- 独立工具（scoop/brew/choco）：复制目录 + 创建 current 链接（类似 MigrateLocalTool）
- 成功后 `InvalidateToolsCache`
- **测试**：mock 迁移逻辑验证
- **验收**：`go test ./internal/gui/ -short -run "Migrate"`

## Phase B — 前端多实例列表 + 迁移按钮

### B1：前端多实例 + 迁移
**涉及**：`frontend/src/components/ToolDetailDrawer.vue`、`frontend/src/AppContent.vue`、i18n
- package-manager 类工具详情：显示 Instances 列表（每行：版本 + 路径 + 来源徽标 xdev/local）
- 非 xdev 管理实例旁显示「迁移」按钮 → 调 `MigratePackageManager(toolName)`
- i18n：`pm.instance_xdev` / `pm.instance_local` / `pm.migrate` / `pm.migrate_success` / `pm.migrate_failed`（zh/en）
- **验收**：`npx vite build` + vitest

## Phase C — 集成验证
### C1：全量回归 + 构建 + e2e + 手动场景 + 文档

---

**审批请求**：请审批本 plan。

## 批准记录 (Approval History)

> **批准ID**: `2026-08-08-plan-027-001`
