# Spec 023 LOOP 实施循环

> **状态**：待审批
> **前置**：Spec 023 + Plan + Task + Checklist 已获批
> **日期**：2026-08-02
> **作用**：定义分批实施 → 自动验证 → 审查 → 失败回退的循环机制，保证 16 项 task 可靠收敛。

## 一、批次划分（Batch）

按依赖关系分组，同一 Batch 内可并行（分派不同 subagent 时注意写冲突），批次间串行。

| Batch | Tasks | 内容 | 依赖 | 并行性 |
|-------|-------|------|------|--------|
| B1 | T-A1, T-A2, T-A3, T-A4 | manifest/remote/extractor/installer 平台能力 | 无 | 文件不重叠可并行 |
| B2 | T-A5, T-A6 | 注册泛化 + 形态模型 | A1 结构定义 | A6 依赖 A1 类型；串行或子序 |
| B3 | T-B1, T-B2 | 依赖检查（后端 + 前端） | B1 依赖 A1/A6；B2 依赖 B1 | 串行（B2 前端依赖 B1 API） |
| B4 | T-C1, T-C2 | 启动 + 形态 UI | C1 依赖 A6；C2 依赖 C1 | 串行 |
| B5 | T-D1..D7 | 首批 8 个 manifest | 依赖 A2/A3/A4/A6 | 每项独立可并行（文件独立） |
| B6 | T-E1 | 集成验证 | 依赖 B1-B5 | 单批 |

## 二、每批 LOOP 流程（Implement → Verify → Review → 收敛）

```
┌─ LOOP 入口（Batch N）
│
├─ L1 Implement：分派 subagent 实施（fixer/designer，见「分派策略」）
│     · 小步执行：每文件改完即编译检查
│     · 明确边界：只做本 Batch 内容，不越界重构
│     · 超时护栏：单任务 >20 分钟无进展 → 中断并检查半成品（参考历史挂起教训）
│
├─ L2 Verify（自动验证门，必须全绿才进 Review）
│     · go build ./...（编译）
│     · 目标测试：go test <涉及包> -short（该 Batch 全部新增/关联测试）
│     · 前端（B3/B4/B6）：npx vite build + 相关 vitest
│     · L2 失败 → 回退到 L1 修复（修复循环，见四）
│
├─ L3 Review（人工/独立审查门）
│     · 必审 Batch：B1（平台能力地基）、B2（形态模型）、B5（manifest 正确性）
│     · 审查人：oracle（架构/安全）对 Go 改动；obsever/人工对前端
│     · 审查项：行为一致性、向后兼容、测试覆盖、无越界改动
│     · L3 发现问题 → 回退到 L1 修复
│
└─ L4 收敛：Batch 全部通过 → 更新 checklist 勾选 + 进入下一 Batch
        直至 B6 完成 → 全量收敛（见五）
```

## 三、分派策略

| Batch | 主实施 | 理由 |
|-------|--------|------|
| B1 | fix-1（后端） | manifest/remote/extractor/installer 均为后端 Go |
| B2 | fix-1（后端 A5/A6 Go 部分） | register + manifest + integrity + ToolInfo |
| B3 | fix-1（B1 后端）+ des-1（B2 前端，依赖 B1 完成后） | 前后端分离 |
| B4 | fix-1（C1 后端）+ des-1（C2 前端） | 同上 |
| B5 | fix-1（manifest 批量）+ 我（URL/SHA 实测，沿 Spec 022 流程） | manifest 与网络实测分离 |
| B6 | 我（全量验证 + 手动场景） | 集成收尾 |

**防挂起护栏**（历史教训）：
- 每个 subagent 任务给出明确"若模糊则最小合理决策并继续，禁止停滞"
- 只跑目标测试，不跑全量（B6 才全量）
- 我实时监控：任务 >20 分钟无结果 → 查询状态；若卡死 → 中断并检查半成品文件（参考 mingw.yaml 重复 key 事故），修复后继续或换策略
- manifest 批量编辑用脚本+校验（重复 key 扫描），避免手工破坏

## 四、修复循环（L2/L3 失败回退）

```
L2/L3 发现失败
  ├─ 记录问题（文件/测试/错误）
  ├─ 判断：
  │    ├─ 半成品（subagent 中断遗留）→ 我检查并修复残留（如重复 key/坏缩进）→ 重跑验证
  │    ├─ 单点缺陷 → 回退 L1 修复该点 → 重跑 L2（≤3 次）
  │    └─ 设计缺陷（>3 次或跨多点）→ 停止，报用户讨论（架构问题不硬修）
  └─ 修复后重跑：L2 全绿 → L3 → 收敛
```

**收敛定义（单 Batch）**：目标测试全通过 + build 通过 + 审查无阻塞项。

## 五、全量收敛（LOOP 出口）

B1-B6 全部通过后：

1. **全量回归**：`go test ./... -short` 无 FAIL（26+ 包）
2. **前端**：`npx vite build` + 全量相关 vitest
3. **构建**：`wails build` 产出 xdev-gui.exe
4. **手动场景**（B6）：
   - vscode 安装 → 详情页 GUI+CLI 徽标 → 「启动」拉起
   - claude-code → CLI/TUI 徽标 + 复制命令；npm 路径依赖提示（node 缺失/满足切换）
   - codex → 三形态徽标 + GUI 外部说明
   - JetBrains exe 静默安装到 install dir
5. **文档收尾**：spec/plan/task/checklist 全部标记完成；checklist 执行记录表填满
6. **出口条件**：上述 1-5 全绿；任何一项不满足 → 回退对应 Batch 修复循环

## 六、风险与护栏汇总

| 风险 | 护栏 |
|------|------|
| subagent 卡死/挂起 | 20 分钟超时查询 + 中断 + 半成品检查修复 |
| manifest 批量编辑破坏 | 脚本化编辑 + 重复 key 扫描 + 每批 manifest 解析测试 |
| 前后端接口不同步 | B3/B4 先后端（fix-1 完成并测试）再前端（des-1 依赖 API 定型） |
| 网络实测（URL/SHA）耗时 | 沿 Spec 022 分批实测流程，单批 ≤3 文件，临时文件即算即清 |
| 静默参数未文档化（Cursor/Zed） | manifest 注释降级方案（zip/官方脚本优先）+ 实测验证 |
| Codex GUI 归属不清 | 实施期决策：xdev 管理 CLI/TUI，GUI 外部化并在 manifest 注释 |

## 七、审批请求

请审批本 LOOP 循环设计。获批后开始 B1 实施（按循环推进，每批汇报验证结果）。
