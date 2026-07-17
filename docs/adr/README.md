# Architecture Decision Records

> **文档类型**：治理索引
> **状态**：Living
> **权威范围**：XDev 长期架构决策的记录规则

## 用途

ADR 记录具有长期影响的架构决策、候选方案、权衡、兼容性、迁移和回滚计划。

改变 Constitution 中的当前架构强约束时，必须先创建 ADR，并同时修订 Constitution、更新受影响 Spec/Plan、获得 `xinyan <zuijiuhjd@foxmail.com>` 明确批准。

## 文件命名

```text
docs/adr/NNNN-<decision>.md
```

- `NNNN` 使用四位递增编号，从 `0001` 开始。
- `<decision>` 使用小写 kebab-case。
- 被替代的 ADR 不删除，状态改为 `Superseded` 并链接替代 ADR。

## 合法状态

```text
Proposed
Accepted
Rejected
Superseded
```

## 批准规则

- Agent 可以起草 ADR，但不能自行将状态改为 `Accepted`。
- `Accepted` 必须由 `xinyan <zuijiuhjd@foxmail.com>` 明确批准。
- ADR 与 Ratified Constitution 冲突时不生效。
