# 代理工具集成与治理

> **文档类型**：长期参考文档
> **状态**：Living
> **权威范围**：仓库级 OpenCode / Spec Kit 集成的退役范围、保留的治理材料及历史引用解释

## 当前使用方式

仓库级 OpenCode / Spec Kit 集成已于 2026-11-19 退役。仓库不再提供 `/speckit.*` 命令、集成安装状态或对应的 PowerShell 生成脚本。此变更仅涉及仓库内的开发工具集成。

活跃实现问题和机器本地工作规格按 [Issue tracker](issue-tracker.md) 组织；领域术语和架构决策按 [Domain Docs](domain.md) 查阅。已跟踪的 `specs/`、Constitution、审批和审查记录继续承担其原有治理职责。

## 保留的治理材料

- [Constitution](../../.specify/memory/constitution.md)、`.specify/approvals/`、[规格索引](../../specs/README.md)、[ADR 索引](../adr/README.md) 和安全治理材料继续保留。
- `.specify/templates/` 的 13 份模板（含 overrides）及 `.specify/workflows/` 的三份定义继续保留。模板或 workflow 中的旧命令名称描述其原有生成流程，不表示本仓库仍安装这些命令；需要模板时直接读取相应文件。
- [文档链接校验](../../scripts/docs/validate-links.ps1)、[Constitution 校验](../../scripts/docs/validate-constitution.ps1)、[Spec 校验](../../scripts/docs/validate-specs.ps1)、[审批校验](../../scripts/docs/validate-approvals.ps1)、[治理校验](../../scripts/docs/validate-governance.ps1)、[治理 CI](../../.github/workflows/governance.yaml) 和 [Git hook](../../scripts/git-hooks/pre-commit.ps1) 继续保留。它们独立于已退役的集成脚本。

工具退役不修改既有审批条款、状态转换规则或规范权威关系；受保护内容的后续变更仍须通过现有治理门禁。

## 已退役文件

以下路径记录原安装位置，供检索历史引用和原始内容使用。

| 原路径 | 退役内容 |
|---|---|
| `.opencode/commands/speckit.analyze.md` | 一致性分析命令 |
| `.opencode/commands/speckit.checklist.md` | 清单生成命令 |
| `.opencode/commands/speckit.clarify.md` | 规格澄清命令 |
| `.opencode/commands/speckit.constitution.md` | Constitution 编辑命令 |
| `.opencode/commands/speckit.converge.md` | 收敛审查命令 |
| `.opencode/commands/speckit.implement.md` | 实现命令 |
| `.opencode/commands/speckit.plan.md` | 计划生成命令 |
| `.opencode/commands/speckit.specify.md` | 规格生成命令 |
| `.opencode/commands/speckit.tasks.md` | 任务生成命令 |
| `.opencode/commands/speckit.taskstoissues.md` | 任务转 issue 命令 |
| `.specify/init-options.json` | 初始化选项 |
| `.specify/integration.json` | 集成安装状态 |
| `.specify/integrations/opencode.manifest.json` | OpenCode 安装清单 |
| `.specify/integrations/speckit.manifest.json` | Spec Kit 安装清单 |
| `.specify/scripts/powershell/check-prerequisites.ps1` | 集成前置检查 |
| `.specify/scripts/powershell/common.ps1` | 集成路径、状态和模板解析函数 |
| `.specify/scripts/powershell/create-new-feature.ps1` | 规格生成脚本 |
| `.specify/scripts/powershell/setup-plan.ps1` | 计划生成脚本 |
| `.specify/scripts/powershell/setup-tasks.ps1` | 任务模板解析脚本 |

## 历史引用与原始内容

[文档体系设计记录](../history/superpowers/specs/2026-07-15-spec-documentation-system-design.md) 和 [模板覆盖实施记录](../history/superpowers/plans/2026-07-16-speckit-template-overrides.md) 保留当时的路径、命令和验证步骤。它们用于解释当时的实现，不作为当前可执行操作指南。上表是这些旧路径的退役索引。

原始文件可从清理前基线 `f011031c23065754c0497a3e5d65264d0d160da0` 查阅，例如：

```powershell
git show f011031c23065754c0497a3e5d65264d0d160da0:.specify/scripts/powershell/common.ps1
```

当前治理检查仍通过 `scripts/docs/` 执行，不需运行已退役的命令或恢复其安装状态。
