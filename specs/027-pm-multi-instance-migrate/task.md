# Spec 027 Task 清单

> **状态**：待批准
> **前置**：Spec 027 + Plan 已获批
> **日期**：2026-08-05

## Phase A — 后端能力

### T-A1 PMInstanceInfo 类型 + Instances 字段
- **涉及**：`internal/gui/app.go`
- **预期产出**：PMInstanceInfo struct + PackageManagerInfo.Instances 字段
- **验收**：编译通过
- **依赖**：无

### T-A2 多实例检测
- **涉及**：`internal/gui/app_local_detect.go`
- **预期产出**：detectPackageManager 收集 xdev+系统双实例 + 去重
- **验收**：`go test ./internal/gui/ -short -run "Instance"` 通过
- **依赖**：T-A1

### T-A3 MigratePackageManager API
- **涉及**：`internal/gui/app_local_detect.go`
- **预期产出**：MigratePackageManager(toolName) error + 迁移逻辑
- **验收**：`go test ./internal/gui/ -short -run "Migrate"` 通过
- **依赖**：T-A1

## Phase B — 前端

### T-B1 前端多实例列表 + 迁移按钮
- **涉及**：ToolDetailDrawer.vue、AppContent.vue、i18n
- **预期产出**：Instances 列表 + 迁移按钮 + i18n
- **验收**：`npx vite build` + vitest
- **依赖**：T-A2 + T-A3

## Phase C — 集成验证

### T-C1 全量回归与构建
### T-C2 手动场景
### T-C3 文档收尾

---

**审批请求**：请审批本 task 清单。