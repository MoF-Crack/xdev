# 仅有 Spec 无 Plan 的测试功能

> **文档类型**：Feature Spec
> **状态**：Draft
> **文档版本**：0.1.0
> **创建日期**：2026-07-18
> **最后更新**：2026-07-18
> **所有者**：xinyan <zuijiuhjd@foxmail.com>
> **权威范围**：测试固件验证用途——合法中间态（Spec 已创建但尚未编写 Plan）
> **替代文档**：无

## 目标

此 Spec 目录不包含 plan.md，模拟 Spec 已创建但尚未进入 Plan 阶段的合法中间态。validate-specs.ps1 应对此返回 PASS（仅 Spec 无 Plan 是允许的），validate-governance.ps1 应报告 spec-plan-consistency 为 pending 但非 fatal。

## 范围与非目标

### 范围

- 验证 validate-specs.ps1 对仅含 spec.md 的目录返回 PASS

### 非目标

- 不测试 Plan 相关检查

## 用户场景

1. 功能已 specify，但尚未 clarify/plan
2. 此状态是 SDD 生命周期中的合法阶段
3. Spec 仍然可以且应该通过元数据完整性检查

## 功能需求

### FR-001：允许仅有 Spec 的状态

活跃 Spec 目录可以只包含 spec.md，不要求必须存在 plan.md。

## 非功能需求

### NFR-001：可区分性

验证器应能区分"仅有 Spec"（合法）和"Spec 缺失"（非法）。

## 边界条件

- 目录中存在 spec.md
- 目录中不存在 plan.md
- 此状态为合法中间态

## 假设与依赖

无。

## 成功标准

### SC-001

validate-specs.ps1 对此目录返回 PASS（0 退出码）。

## 风险分类

| 风险 | 分类 |
|---|---|
| 无 | 低 |

## Constitution、兼容性、跨平台和安全影响

- **Constitution 影响**：无
- **兼容性影响**：无
- **跨平台影响**：无
- **安全影响**：无

## Approval History

| 阶段 | 状态 | 批准者 | 日期 | 文档版本 | 批准范围 |
|---|---|---|---|---|---|
| Spec 创建 | Pending | xinyan <zuijiuhjd@foxmail.com> | 2026-07-18 | 0.1.0 | 测试固件用途 |

## 变更历史

| 版本 | 日期 | 变更类型 | 说明 | 批准者 |
|---|---|---|---|---|
| 0.1.0 | 2026-07-18 | Initial | 创建仅有 Spec 的测试固件 | xinyan <zuijiuhjd@foxmail.com> |
