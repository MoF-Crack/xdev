# XDev 安全治理

> **文档类型**：治理索引
> **状态**：Living
> **权威范围**：XDev 安全政策、操作流程和临时例外的分类入口

## 目录

| 目录 | 职责 |
|---|---|
| `policy/` | 长期安全规则和发布约束 |
| `procedures/` | 密钥管理、签名、恢复等操作步骤 |
| `exceptions/` | 有期限、可撤销、需批准的临时安全偏离 |
| `../audits/` | 基于特定代码基线的时间点安全审计 |

## 权威关系

- Ratified Constitution 的安全原则优先于本目录所有文档。
- Policy 定义长期规则，Procedure 说明如何执行，Exception 只允许临时偏离。
- 审计报告不能自动创建、续期或批准安全例外。
- 安全例外不能将风险描述为永久修复。

## 当前迁移状态

现有 `docs/download-source-policy.md`、`docs/signing-guide.md`、`docs/download-cache-recovery.md` 和安全审计将在后续迁移计划中处理。本阶段不移动这些文件。
