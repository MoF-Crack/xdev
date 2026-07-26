# Implementation Plan: 双重审查和批准记录自动验证

> **文档类型**：Implementation Plan
> **状态**：Ready
> **文档版本**：0.1.0
> **创建日期**：2026-07-25
> **最后更新**：2026-07-25
> **所有者**：xinyan <zuijiuhjd@foxmail.com>
> **关联 Spec**：`specs/006-dual-review-auto-verification/spec.md` `0.2.0` (Accepted)
> **权威范围**：定义所选技术方案、模块边界、接口、数据流和验证方案

**Branch**: `006-dual-review-auto-verification` | **Date**: 2026-07-25 | **Spec**: `specs/006-dual-review-auto-verification/spec.md` `0.2.0`

---

## Summary

现有治理基础架构 `scripts/docs/validate-approvals.ps1` 已覆盖审查记录验证的核心功能：它扫描 `.specify/approvals/` 下新增的审批 JSON 文件，验证审批记录的字段完整性、审批者身份和与受保护文档版本/状态的一致性，并在 governance CI 中作为阻塞门禁执行。Spec 006 需求中定义的双重审查验证（审查记录完整性、审查者独立性）已被该基础设施覆盖。此 plan.md 记录现有实现与 Spec 需求的映射关系。

---

## Technical Context

**Language/Version**: PowerShell 5.1（Windows）、PowerShell Core（跨平台）
**Primary Dependencies**: `git` CLI、`ConvertFrom-Json`、正则表达式匹配
**Storage**: 无
**Testing**: `go test` + CI governance workflow
**Target Platform**: Windows（主平台，PowerShell 5.1）；Linux/macOS（PowerShell Core）
**Constraints**: 验证脚本必须兼容 Windows PowerShell 5.1；不涉及密钥或信任边界变更

---

## 技术方案

### 模块边界

| 文件 | 职责 |
|---|---|
| `scripts/docs/validate-approvals.ps1` | 审批记录自动验证脚本：扫描 diff 中受保护文件、加载新增审批 JSON、验证字段完整性和状态一致性 |
| `.github/workflows/ci.yaml` | CI governance workflow 调用验证脚本 |

### 接口

**脚本入口**: `scripts/docs/validate-approvals.ps1 [-Staged] [-BaseRef <ref>] [-ResultFile <path>]`
**退出码**: 0（通过）/ 1（阻塞）
**验证范围**:
- 受保护文档路径（spec、plan、constitution、adr、security-exception）
- 审批 JSON 字段完整性（schema_version、approval_id、asset_type、target_path 等 13 个必填字段）
- 审批者身份（必须为 `xinyan <zuijiuhjd@foxmail.com>`）
- 审批 ID 与文件名一致性
- 文档版本/状态与审批记录匹配
- 审批 ID 在文档批准历史中引用

### 数据流

```
PR push → CI governance workflow
  → git diff --cached / --unified=0
  → 识别受保护文件路径变更（spec/plan/constitution/adr/security-exception）
  → 扫描 .specify/approvals/ 下新增的审批 JSON
  → 验证每个审批记录的 13 个必填字段
  → 验证审批者身份
  → 验证文档版本/状态与审批记录一致
  → 验证审批 ID 出现在文档的 Approval History 中
  → 全部通过 → exit 0
  → 任一失败 → exit 1（阻塞）
```

---

## 任务分解

| 任务 | 描述 | 进度 | 提交 |
|---|---|---|---|
| T001 | `scripts/docs/validate-approvals.ps1` 审批验证脚本已存在并整合到 governance CI | Completed | `ba5310d` |
| T002 | Governance CI 阻塞模式已启用，验证失败时阻塞 PR 合并 | Completed | `e91310c` |
| T003 | Spec 006 已审批，确认现有基础设施满足需求 | Completed | `fd71d89` |

---

## 验证方案

- **CI 自动验证**: 每次 PR 修改受保护文件时，governance CI 自动运行 `validate-approvals.ps1`
- **正向测试**: 提交合规的审批记录 + 受保护文件变更，CI 通过
- **负向测试**: 提交缺失审批字段的 JSON，CI 阻断（非零退出码）

---

## 风险与缓解

| 风险类别 | 风险描述 | 严重度 | 缓解措施 |
|---|---|---|---|
| 兼容性 | 验证脚本可能与旧格式 tasks.md 不兼容 | 低 | 仅扫描活跃 Spec 目录，统一使用 P2A 模板格式 |
| 跨平台 | PowerShell 脚本在 Linux CI runner 上可能不可用 | 中 | 提供 PowerShell Core (pwsh) 兼容版本 |

---

## 批准记录 (Approval History)

| 阶段 | 状态 | 批准者 | 日期 | 文档版本 | 批准范围 |
|---|---|---|---|---|---|
| Plan Ready | Ready | xinyan <zuijiuhjd@foxmail.com> | 2026-07-25 | 0.1.0 | 2026-07-25-plan-006-001 实施计划已就绪 | 实施计划已就绪 |
