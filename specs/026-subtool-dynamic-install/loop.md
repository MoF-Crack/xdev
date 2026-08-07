# Spec 026 LOOP 实施循环

> **状态**：待批准
> **前置**：Spec 026 + Plan + Task + Checklist 已获批
> **日期**：2026-08-05
> **作用**：定义分批实施 → 自动验证 → 审查 → 失败回退的循环机制，保证 7 项 task 可靠收敛。

## 一、批次划分（Batch）

| Batch | Tasks | 内容 | 依赖 | 并行性 |
|-------|-------|------|------|--------|
| B1 | T-A1, T-A2, T-A3 | pmDef 扩展 + 两阶段检测 + InstallSubTool API | 无 | 串行（同文件 app_local_detect.go） |
| B2 | T-B1 | 前端动态提示 + 选项对话框 | B1（API 定型） | 单批 |
| B3 | T-C1, T-C2, T-C3 | 集成验证 + 手动场景 + 文档 | B1-B2 全部 | 串行收尾 |

## 二、每批 LOOP 流程

```
┌─ LOOP 入口（Batch N）
├─ L1 Implement：我直接实施（Spec 024/025 教训：fixer 返回空产出，直接接手更可靠）
│     · 小步执行：每文件改完即编译检查
│     · 明确边界：只做本 Batch 内容，不越界重构
│     · 若模糊则最小合理决策并继续，禁止停滞
├─ L2 Verify（自动验证门）
│     · go build ./internal/gui/
│     · go test ./internal/gui/ -short -count=1 -run "PmDef|Detect|SubTool|InstallSubTool"
│     · 前端（B2/B3）：npx vite build + vitest
│     · L2 失败 → 回退到 L1 修复（≤3 次）
├─ L3 Review（独立审查门）
│     · 必审 Batch：B1（后端能力 + 安全敏感的 exec 构造）
│     · 审查人：oracle（Go 改动 + 安全审查）
│     · L3 发现问题 → 回退到 L1 修复
└─ L4 收敛：Batch 全部通过 → 更新 checklist + 进入下一 Batch
```

## 三、分派策略

| Batch | 主实施 | 理由 |
|-------|--------|------|
| B1 | 我直接实施 | Spec 024/025 教训：fixer 返回空产出；同文件串行改动 |
| B2 | designer（前端 UX） | 形态选择器/选项对话框需设计判断；若失败我接手 |
| B3 | 我（全量验证 + 手动场景 + 文档） | 集成收尾 |

**防挂起护栏**：
- designer 任务 >15 分钟无结果 → 中断并直接接手（Spec 025 des-1 session error 教训）
- 只跑目标测试，不跑全量（B3 才全量）
- manifest 批量编辑用 Node.js 脚本（非 PowerShell，Spec 024 编码破坏教训）

## 四、修复循环

```
L2/L3 发现失败
  ├─ 半成品（subagent 中断/空产出）→ 我检查并修复残留 → 重跑验证
  ├─ 单点缺陷 → 回退 L1 修复 → 重跑 L2（≤3 次）
  └─ 设计缺陷（>3 次或跨多点）→ 停止，报用户讨论
```

## 五、全量收敛（LOOP 出口）

B1-B3 全部通过后：
1. `go test ./... -short` 无 FAIL
2. `npx vite build` + vitest
3. `wails build` 产出 xdev-gui.exe
4. e2e 含新用例
5. 手动场景：Node 已安装 yarn 启用 / pip+uv 都在 pipx 选项 / 仅 pip pipx 直接 / 都不在提示
6. 文档收尾

## 六、风险与护栏

| 风险 | 护栏 |
|------|------|
| subagent 卡死/空产出 | 15 分钟超时 + 直接接手（Spec 024/025 教训） |
| 同批次检测结果竞态 | 两阶段：先并发检测收齐，再串行计算附属工具 |
| corepack 不在 PATH | 检测可用性，不可用时提示 Node 版本过旧 |
| method 参数注入 | method 硬编码在 pmDef，前端只传 name，后端查 pmDef 确认 |
| Go 1.26.5 GC panic | 单测试验证，全量测试偶发非代码问题 |

## 七、审批请求

请审批本 LOOP。获批后开始 B1 实施。