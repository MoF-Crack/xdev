# XDev Feature Specs

> **文档类型**：规范索引
> **状态**：Living
> **权威范围**：活跃 Feature Spec 的目录、生命周期和批准规则

## 活跃规格目录

```text
specs/<编号-功能名>/
```

- 编号使用三位或更多数字，如 `001-`、`002-`；
- 功能名使用小写 kebab-case；
- 活跃目录由 Spec Kit 创建和定位；
- `specs/archive/` 不属于活跃功能目录。

## 标准文件

```text
spec.md
plan.md
tasks.md
checklist.md
research.md
data-model.md
quickstart.md
contracts/
reviews/
```

只有 `spec.md` 是任务级需求权威源。Plan 和 Tasks 不得改变 Spec 语义。

## 生命周期

```text
specify
→ 用户批准 Spec
→ clarify（按需）
→ plan
→ Constitution 合规审查
→ 用户批准 Plan
→ tasks
→ analyze
→ implement
→ 双重独立审查 LOOP
→ converge
→ 用户批准验收
→ 用户批准归档
```

## 禁止事项

- Backlog、Roadmap、旧计划和归档规格不能直接进入 implement。
- Agent 不得代填用户批准。
- 未解决占位符、未批准例外或未接受 ADR 不得进入实施。
- `.specify/feature.json` 不得指向 `specs/archive/`。
