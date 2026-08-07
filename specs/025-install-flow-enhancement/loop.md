# Spec 025 LOOP 实施循环

> **状态**：待批准
> **前置**：Spec 025 + Plan + Task + Checklist 已获批
> **日期**：2026-08-04
> **作用**：定义分批实施 → 自动验证 → 审查 → 失败回退的循环机制，保证 11 项 task 可靠收敛。

## 一、批次划分（Batch）

按依赖关系分组，同一 Batch 内可并行（分派不同 subagent 时注意写冲突），批次间串行。

| Batch | Tasks | 内容 | 依赖 | 并行性 |
|-------|-------|------|------|--------|
| B1 | T-A1, T-A2, T-B1 | pmDef 扩展（gui）+ InstallPackageManager script（gui）+ Archive.Form（manifest） | 无 | A1→A2 串行（同文件）；B1 与 A 文件不重叠可并行 |
| B2 | T-A3, T-B2, T-B3, T-B5 | 前端安装按钮 + HasFormSpecificArchives + InstallToolWithForm + OpenCode manifest | B1 | A3 依赖 A2；B2/B3 串行（B3 依赖 B1）；B5 依赖 B1；可部分并行 |
| B3 | T-B4 | 前端形态选择器 | B2（B2+B3 API 定型） | 单批 |
| B4 | T-C1, T-C2, T-C3 | 集成验证 + 手动场景 + 文档 | B1-B3 全部 | 串行收尾 |

## 二、每批 LOOP 流程（Implement → Verify → Review → 收敛）

```
┌─ LOOP 入口（Batch N）
│
├─ L1 Implement：分派 subagent 实施（fixer/designer，见「分派策略」）
│     · 小步执行：每文件改完即编译检查
│     · 明确边界：只做本 Batch 内容，不越界重构
│     · 超时护栏：单任务 >15 分钟无进展 → 中断并检查半成品
│     · 若模糊则最小合理决策并继续，禁止停滞
│
├─ L2 Verify（自动验证门，必须全绿才进 Review）
│     · go build ./...（编译）
│     · 目标测试：go test <涉及包> -short
│     · 前端（B2/B3/B4）：npx vite build + 相关 vitest
│     · L2 失败 → 回退到 L1 修复（修复循环，见四）
│
├─ L3 Review（独立审查门）
│     · 必审 Batch：B1（后端能力地基 + 安全敏感的 script install）、B2（API 契约）
│     · 审查人：oracle（Go 改动 + 安全审查 script install 命令构造）
│     · 审查项：行为一致性、向后兼容、安全（命令注入/路径遍历）、测试覆盖、无越界改动
│     · L3 发现问题 → 回退到 L1 修复
│
└─ L4 收敛：Batch 全部通过 → 更新 checklist 勾选 + 进入下一 Batch
        直至 B4 完成 → 全量收敛（见五）
```

## 三、分派策略

| Batch | 主实施 | 理由 |
|-------|--------|------|
| B1 | fix-1（A1+A2 gui 包）+ fix-2（B1 manifest 包） | 文件不重叠可并行；A1→A2 串行由 fix-1 内部顺序保证 |
| B2 | fix-1（A3 前端安装按钮）+ fix-2（B2+B3 后端）+ 我（B5 OpenCode manifest 标注） | 前后端分离；B5 小改我直接做 |
| B3 | designer（B4 形态选择器 UI） | 形态选择器是 UX 组件，需设计判断 |
| B4 | 我（全量验证 + 手动场景 + 文档） | 集成收尾 |

**防挂起护栏**（Spec 024 教训：fixer 返回空产出）：
- 每个 subagent 任务给出明确"若模糊则最小合理决策并继续，禁止停滞"
- 只跑目标测试，不跑全量（B4 才全量）
- 我实时监控：任务 >15 分钟无结果 → 查询状态；若卡死或空产出 → 中断并直接接手实施
- 命令构造安全审查：installCommand 硬编码在 pmDef（非用户输入），无注入风险；但仍需 oracle 审查 exec.Command 参数构造
- manifest 批量编辑用 Node.js 脚本（非 PowerShell，避免 UTF-8 编码破坏——Spec 024 教训）

## 四、修复循环（L2/L3 失败回退）

```
L2/L3 发现失败
  ├─ 记录问题（文件/测试/错误）
  ├─ 判断：
  │    ├─ 半成品（subagent 中断/空产出遗留）→ 我检查并修复残留 → 重跑验证
  │    ├─ 单点缺陷 → 回退 L1 修复该点 → 重跑 L2（≤3 次）
  │    └─ 设计缺陷（>3 次或跨多点）→ 停止，报用户讨论
  └─ 修复后重跑：L2 全绿 → L3 → 收敛
```

**收敛定义（单 Batch）**：目标测试全通过 + build 通过 + 审查无阻塞项。

## 五、全量收敛（LOOP 出口）

B1-B4 全部通过后：

1. **全量回归**：`go test ./... -short` 无 FAIL
2. **前端**：`npx vite build` + 全量相关 vitest
3. **构建**：`wails build` 产出 xdev-gui.exe
4. **e2e**：`npx playwright test e2e/app.spec.js`（含新用例）
5. **手动场景**（B4）：
   - Scoop 安装（Windows 非管理员）→ 确认 → 后台执行 → 成功
   - Chocolatey（非管理员）→ 提示需管理员
   - OpenCode 安装 → 形态选择器 → 选 CLI → 只下载 CLI archive
   - VS Code 安装 → 直接安装（无选择器）
6. **文档收尾**：spec/plan/task/checklist/loop 标记完成；HANDOFF.md 更新
7. **出口条件**：上述 1-6 全绿；任何一项不满足 → 回退对应 Batch 修复循环

## 六、风险与护栏汇总

| 风险 | 护栏 |
|------|------|
| subagent 卡死/空产出 | 15 分钟超时查询 + 中断 + 直接接手（Spec 024 教训） |
| manifest 批量编辑破坏编码 | 用 Node.js 脚本（非 PowerShell，Spec 024 教训） |
| script install 命令注入 | installCommand 硬编码在 pmDef（非用户输入）；oracle 审查 exec.Command 构造 |
| Chocolatey 需管理员 | 检测 + 提示，不自动提权 |
| PowerShell ExecutionPolicy | 命令带 -ExecutionPolicy Bypass -Scope Process |
| Homebrew 交互式确认阻塞 | 设 NONINTERACTIVE=1 环境变量 |
| Archive.Form 向后兼容 | 缺省空 = 所有形态共用；HasFormSpecificArchives 只在 Form!="" 时才显示选择器 |
| 形态选择器 UX 复杂 | 只对多包工具显示；单包工具无感知 |
| 前后端接口不同步 | B1 后端先完成并测试，B2/B3 依赖 API 定型后才动前端 |

## 七、审批请求

请审批本 LOOP 循环设计。获批后开始 B1 实施（按循环推进，每批汇报验证结果）。