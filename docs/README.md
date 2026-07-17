# XDev 文档索引

> **文档类型**：长期参考文档
> **状态**：Living
> **权威范围**：XDev 文档分类、入口和职责边界

## 文档分层

| 层级 | 目录 | 职责 |
|---|---|---|
| 治理约束 | `.specify/memory/constitution.md`、`docs/adr/`、`docs/security/exceptions/` | 定义必须遵守的全局规则、架构决策和临时安全偏离 |
| 活跃规格 | `specs/<编号-功能名>/` | 定义当前功能的目标、计划、任务、审查和验收 |
| 长期参考 | `docs/` | 解释当前系统的架构、命令、操作、测试和路线图 |
| 历史记录 | `specs/archive/`、`docs/history/`、`docs/audits/` | 保存已完成规格、旧计划和时间点审计 |

## 权威边界

- Constitution 正式批准后是最高工程治理约束。
- 用户批准的活跃 `spec.md` 是对应功能的任务级需求权威源。
- 长期参考文档必须随代码事实更新，但不能覆盖 Constitution 或已批准 Spec。
- Backlog、Roadmap、历史计划和审计不能直接驱动实现。

## 主要入口

- 架构：`docs/architecture.md`
- 技术参考：`docs/technical-reference.md`
- 操作指南：`docs/operations.md`
- 测试策略：`docs/test-plan.md`
- ADR：`docs/adr/`
- 安全治理：`docs/security/`
- 时间点审计：`docs/audits/`
- 历史实施记录：`docs/history/`
- 活跃功能规格：`specs/`

## 语言规则

Constitution、ADR、安全例外和 Feature Spec 使用简体中文。函数名、类型名、CLI 命令、路径、配置键、日志原文和 Git 提交信息保留英文。

## 当前状态

文档体系处于候选治理实施阶段。现有文件将在后续计划中分批添加元数据和迁移；本索引不代表迁移已经完成。
