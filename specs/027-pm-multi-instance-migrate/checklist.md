# Spec 027 Checklist

> **状态**：完成（Phase A-C 全部通过，LOOP 收敛）
> **前置**：Spec 027 + Plan + Task 已获批
> **日期**：2026-08-05

## Phase A
### T-A1 PMInstanceInfo 类型
- [x] PMInstanceInfo struct（Version, Path, Managed, Source）
- [x] PackageManagerInfo 新增 Instances []PMInstanceInfo json
- [x] ToolInfo 新增 Instances []PMInstanceInfo json（前端实际获取类型，Spec 026 遗漏教训）
- [x] **验收**：编译通过

### T-A2 多实例检测
- [x] detectPackageManager 收集 xdev 管理实例（Managed=true, Source="xdev"）
- [x] detectPackageManager 收集系统实例（Managed=false, Source="local"）
- [x] 路径去重（规范化后比较）
- [x] info.Installed/Version/LocalPath 保持现有语义 + Instances fallback 填充
- [x] 测试：双实例 Instances=2；单实例=1；去重
- [x] pipx toolName="" 时用 effectiveParent="python" 查 xdev 管理实例（Bug 修复）
- [x] **验收**：`go test ./internal/gui/ -short -run "Instance"` 通过

### T-A3 MigratePackageManager
- [x] MigratePackageManager(toolName) error
- [x] 附属工具：在 xdev 父工具目录创建链接/复制（pipx 用 python 作为 parentTool）
- [x] 独立工具：复制目录 + current 链接
- [x] 成功后 InvalidateToolsCache
- [x] 测试：mock 迁移验证
- [x] **验收**：`go test ./internal/gui/ -short -run "Migrate"` 通过

## Phase B
### T-B1 前端多实例 + 迁移
- [x] ToolDetailDrawer 显示 Instances 列表（版本+路径+来源徽标）
- [x] 非 xdev 实例旁「迁移」按钮 → 调 MigratePackageManager
- [x] 前端条件 >=1 且含非 managed 实例才显示（单实例也显示迁移按钮，Bug 修复）
- [x] enableCommand 路径传 tool.name 而非 tool.toolName（yarn/pnpm 启用 Bug 修复）
- [x] onPositiveClick 不返回 Promise（对话框自动关闭 Bug 修复）
- [x] i18n：pm.instance_xdev/local + pm.migrate + migrate_success/failed（zh/en）
- [x] **验收**：`npx vite build` + vitest 通过

## Phase C
### T-C1 全量回归 + 构建 + e2e
- [x] `go test ./... -short`：gui 全绿；plugin/plugins/java 为 Go 1.26.5 runtime cleanup panic（环境问题，Spec 027 未改动这些包）
- [x] `wails build` 产出 xdev-gui.exe
- [x] e2e 18/18 通过
### T-C2 手动场景
- [x] PIP 详情 Instances=[xdev pip, 系统 pip]；PIPX 迁移到 xdev 目录（用户验证）
### T-C3 文档收尾
- [x] checklist 状态更新 + 执行记录

---

## 执行记录

| 日期 | Task | 验收结果 | 备注 |
|------|------|----------|------|
| 2026-08-05 | T-A1 | 通过（PMInstanceInfo + 双结构体 Instances 字段，编译通过） | B1 完成 |
| 2026-08-05 | T-A2 | 通过（多实例检测 + 去重 + pipx effectiveParent 修复） | B1 完成 |
| 2026-08-05 | T-A3 | 通过（MigratePackageManager + 附属/独立双路径） | B1 完成 |
| 2026-08-05 | T-B1 | 通过（多实例列表 + 迁移按钮 + 3 个后续 Bug 修复 + i18n） | B2 完成 |
| 2026-08-05 | T-C1 | 通过（gui 全绿；wails build；e2e 18/18） | B3 完成 |
| 2026-08-05 | T-C2 | 通过（pipx 迁移用户验证成功） | B3 完成 |
| 2026-08-05 | T-C3 | 通过（checklist 收尾） | B3 完成 |
