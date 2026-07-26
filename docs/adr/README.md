# Architecture Decision Records (ADR)

> **文档类型**：治理流程
> **状态**：Active
> **创建日期**：2026-07-25

## 用途

ADR 记录项目中重大架构决策的上下文、决策和后果。每个 ADR 是一个独立的 Markdown 文件，记录不可逆的架构选择。

## 何时创建 ADR

在以下情况中创建 ADR：
- 选择框架或库（如选择 Wails 作为 GUI 框架）
- 定义数据流或系统边界（如 manifest 签名验证链）
- 确定跨平台策略（如 Windows 主平台 + 跨平台分层验证）
- 改变已有架构模式（如从单体到模块化）

## 文件命名

```
docs/adr/NNNN-short-title.md
```

例如：`docs/adr/0001-ed25519-manifest-signing.md`

## 模板

```markdown
# ADR-NNNN: [标题]

> **状态**：Proposed | Accepted | Deprecated | Superseded by ADR-XXXX
> **日期**：YYYY-MM-DD

## 背景

[决策的上下文和问题陈述]

## 决策

[做出的选择和理由]

## 后果

[正面和负面影响]
```

## 已有决策

| # | 决策 | 状态 |
|---|---|---|
| 0001 | Ed25519 manifest 签名验证 | ✅ 已创建 |
| 0002 | Wails v2 作为 GUI 框架 | ✅ 已创建 |
| 0003 | Windows 主平台 + 跨平台分层验证 | ✅ 已创建 |
| 0004 | Go cobra CLI 框架 | ✅ 已创建 |
| 0005 | hostsnapshot 套件级测试隔离 | ✅ 已创建 |