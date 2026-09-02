# Spec 037 — 批量更新并行安装（冒烟反馈 #6-1 立项）

Status: planned
Date: 2026-08-29

## 背景

用户反馈：批量更新当前为严格串行（完成一项再启动下一项），希望多线程并行。

## 现状约束（调研结论）

1. **后端**：`dlMgr`（downloader/manager.go）有 mutex 支持并发下载；每项 installWithOptions 生成独立 taskID（tool@version），事件流天然可区分。但 `AcquireToolLock`（Spec 032）在 Activate/Uninstall 上有工具级锁——**同工具**多版本并行激活会冲突（批量更新场景每工具只有一项，无冲突）；不同工具的解压/激活互不干扰（独立目录）。
2. **前端**：`useInstall` 是**单会话状态机**（active/taskId/progress/error 全单份）——并行 N 项需要 N 份会话实例（事件订阅按 taskId 路由已具备：belongsToCurrentTask 按 taskId 判定）。`InstallProgressDialog` 单工具单进度条设计 → 需改为多项进度列表。后台指示器单百分比 → 需聚合（如「3 项进行中」）。
3. **FR-018**：unverified 版本逐项确认——并行场景下确认阶段与下载阶段可分离（先全部确认，再并行下载），或保持逐项确认但确认后即启动（不等前项完成）。

## 方案方向（待细化）

### 阶段 1：并行下载 + 串行安装激活（低风险）
- 批量确认后，N 项全部调 `InstallTool`（后端各自 goroutine 下载，dlMgr 并发）
- 前端 useInstall 多实例（每项一个实例，按 taskId 路由事件）
- InstallProgressDialog 改多项列表（工具名/版本/各自进度条/独立取消）
- 解压/激活阶段后端按完成顺序串行处理（或每项独立 goroutine——解压独立目录无竞争，激活加工具锁已互斥）

### 阶段 2（可选）：确认合并
- N 个逐项 unverified 确认 → 1 个批量确认框（列明全部项）——需与 FR-018 语义对齐讨论

## 影响面

- `frontend/src/composables/useInstall.js`（多实例化或会话池）
- `frontend/src/composables/useBatchUpdates.js`（循环改并行启动 + Promise.all 等待）
- `frontend/src/components/InstallProgressDialog.vue`（多项 UI）
- `frontend/src/AppContent.vue`（后台指示器聚合）
- 后端基本不动（已支持并发）

## 非目标

- 单装路径不变（仍单会话）
- 同工具多版本并行激活（工具锁语义保持）

## 状态

已立项待实施方案细化 + 审计循环（沿用 Spec 035/036 方法论）。**不在冒烟反馈修复轮实施**。
