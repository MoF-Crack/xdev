# Spec 031: GUI 启动与 Host-scan 性能

> **文档类型**：Feature Spec
> **状态**：Approved
> **文档版本**：1.0.0
> **创建日期**：2026-08-21
> **最后更新**：2026-08-21
> **所有者**：xinyan <zuijiuhjd@foxmail.com>
> **权威范围**：PATH 一次 ReadDir 索引；包管理器列表不再对每个实例同步 `--version`（yarn/pnpm 启用探测除外）；去掉仪表盘体积预取；Naive 停止全量 `app.use`，重页面懒加载。
> **Constitution**：`1.0.0`

## 一、目标

1. **hostscan.Scan**：每个 PATH 目录一次 `ReadDir`，在内存匹配 Names/PATHEXT；Windows 不再为同一扩展写大小写两份候选。
2. **FirstOnPATH**：一次 Scan 全部 names，取 PATH 序第一个命中。
3. **GetPackageManagers / collectPMInstances**：列表不 `exec --version`；实例 Version 可空/`unknown`。yarn/pnpm 仍用短超时探测 corepack「未启用」（功能正确性）。
4. **AppContent onMounted**：去掉 `prefetchSizeBreakdowns`；体积只在用户打开抽屉时加载。
5. **Naive**：删除 `app.use(naive)`；`App.vue` 显式引入 Config/Message/Dialog Provider；重页面 `defineAsyncComponent`。

## 二、非目标

- 恢复 hook/lockfile
- 虚拟列表（分类 N≈40）
- 新 Wails 绑定（不增 GetBootstrapState）

## 三、验收

- 现有 hostscan 测试全绿 + 新：单目录多名字只需一次 ReadDir（可用 hook/计数）
- PM 列表路径：`probePMVersion` 不对 npm/pip/uv 等独立 PM 的 PATH 实例调用
- vitest 全绿；`vite build` 通过；初始 JS 不再是单一 1.6MB naive 全量 chunk（允许 vendor split）
- `go test ./internal/hostscan ./internal/gui -count=1` 全绿

## 批准记录 (Approval History)

| 阶段 | 状态 | 批准者 | 日期 | 文档版本 | 批准范围 |
|---|---|---|---|---|---|
| Spec 批准 | Approved | `xinyan <zuijiuhjd@foxmail.com>` | 2026-08-22 | `1.0.0` | `2026-08-22-spec-031-approved-001` GUI 启动与 Host-scan 性能（提交时补录治理批准记录） |
