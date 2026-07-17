# 安全例外

> **文档类型**：治理索引
> **状态**：Living
> **权威范围**：临时安全偏离的格式、批准和失效规则

## 合法状态

```text
Draft
Active
Expired
Closed
Revoked
```

## 必需内容

每份安全例外必须记录：

- 偏离的 Constitution 或 Policy 条款；
- 风险和影响范围；
- 禁止扩大的边界；
- 补偿控制和修复计划；
- 责任人、批准者和逐次发布批准记录；
- 目标版本、截止日期和自动失效条件；
- 关闭证据或续期重新评估记录。

## 批准规则

- Agent 只能创建 `Draft`，不能自行激活、续期或关闭例外。
- `Active`、续期和关闭必须由 `xinyan <zuijiuhjd@foxmail.com>` 明确批准。
- 到期例外自动变为 `Expired`，不得继续用于发布。
