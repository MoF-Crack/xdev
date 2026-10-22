# XDev 治理批准状态硬门禁设计

> **文档版本**：`1.0.0`
> **状态**：Approved for Planning
> **批准者**：`xinyan <zuijiuhjd@foxmail.com>`
> **批准日期**：2026-07-16
> **批准证据**：当前协作会话中用户明确回复“同意实施”

## 1. 背景

提交 `059da207f3cbd028a7e1f95537914238fdf00ddb` 在执行 Git 初始导入时，将 `.specify/memory/constitution.md` 从候选版 `1.0.0-rc.2` 修订为 `1.0.0-rc.3`。该修订同步了 CI Go 工具链已经从 `1.22` 调整为 `1.23` 的工程事实，但在修改和提交前没有获得用户对 Constitution 修订的明确批准。

该操作没有伪造批准记录，不能因此被视为合规。后续对 `rc.3` 内容的批准只决定该候选内容从批准时刻起能否作为后续工作的候选基线，不能追溯性地授权、覆盖或消除既往越权操作。

本设计建立三项纠正能力：

1. 永久记录治理边界违规事实；
2. 记录用户对 Constitution `1.0.0-rc.3` 当前内容的明确批准；
3. 对全部需要用户批准的治理状态建立机器可验证的硬门禁。

## 2. 目标

- 治理事件记录必须包含时间、提交、未经授权的操作、影响、根因、批准边界和纠正措施。
- Constitution 必须记录 `rc.3` 的批准事实，同时继续区分候选内容批准与 Constitution `1.0.0` 正式批准。
- 每次需要用户批准的状态或版本写入，必须在同一 Git 差异中新增唯一、匹配、不可复用的结构化批准记录。
- 验证器必须拒绝缺少记录、记录范围不匹配、记录目标不匹配和复用旧记录的变更。
- 本地 pre-commit 入口必须调用同一验证器；后续 CI 只能复用该入口，不能实现另一套规则。

## 3. 非目标

- 不 amend、reset、rebase 或重写 `059da20`。
- 不将本次批准描述为对 2026-07-16 20:05:30 既往操作的追认。
- 不在本阶段将 Constitution 升级为 `1.0.0` 或标记为 `Ratified`。
- 不用 Git author、commit author、测试通过或 Agent 判断替代用户批准。
- 不在本阶段创建完整 governance CI workflow；该集成仍属于路线图 P5。
- 不依赖未版本化的本地 hook 作为唯一门禁。

## 4. 受保护资产

门禁覆盖全部治理资产：

| 资产 | 路径 | 受保护写入 |
|---|---|---|
| Constitution | `.specify/memory/constitution.md` | 版本、状态、批准历史、正式批准字段 |
| ADR | `docs/adr/*.md` | `Accepted`、`Rejected`、`Superseded` 状态 |
| 安全例外 | `docs/security/exceptions/*.md` | `Active`、`Extended`、`Closed` 状态及逐次发布批准 |
| Feature Spec | `specs/*/spec.md` | `Draft -> Approved` 及批准历史 |
| Plan | `specs/*/plan.md` | `Planned -> Ready` 及批准历史 |
| 最终验收 | `specs/*/` 下验收记录 | `Verified -> Accepted` |
| 归档 | `specs/archive/**` 及活跃 Spec 归档元数据 | `Accepted -> Archived` |

普通正文编辑只有在不改变上述受保护字段和状态语义时才不要求批准记录。验证器无法可靠判断语义时，应拒绝并要求显式批准，而不是默认放行。

## 5. 批准记录

### 5.1 存放位置

结构化批准记录存放于：

```text
.specify/approvals/<approval-id>.json
```

`approval-id` 使用：

```text
YYYY-MM-DD-<asset>-<decision>-<sequence>
```

例如：

```text
2026-07-16-constitution-rc3-001
```

### 5.2 数据契约

每份批准记录必须包含：

```json
{
  "schema_version": 1,
  "approval_id": "2026-07-16-constitution-rc3-001",
  "asset_type": "constitution",
  "target_path": ".specify/memory/constitution.md",
  "decision": "approve-candidate",
  "target_version": "1.0.0-rc.3",
  "target_state": "Candidate",
  "scope": "批准 Constitution 1.0.0-rc.3 当前候选内容；不追溯授权 059da20，且不构成 1.0.0 正式批准",
  "approved_by": "xinyan <zuijiuhjd@foxmail.com>",
  "approved_at": "2026-07-16",
  "authorization_text": "同意实施",
  "recorded_at": "2026-07-16",
  "recorded_by": "OpenCode",
  "supersedes": []
}
```

必填字段不得为空。`authorization_text` 必须保存用户明确授权的原文；Agent 概括只能写入 `scope`，不能替代原文。

### 5.3 双重记录

批准状态写入必须同时存在：

1. `.specify/approvals/<approval-id>.json` 结构化批准记录；
2. 目标文档 `Approval History` 中引用相同 `approval_id` 的人类可读记录。

两者缺一即失败。结构化记录负责机器验证，文档内记录负责就地审查。

## 6. 验证器

### 6.1 命令接口

```powershell
.\scripts\docs\validate-approvals.ps1 -BaseRef <git-ref>
```

可选的暂存区模式：

```powershell
.\scripts\docs\validate-approvals.ps1 -Staged
```

验证器只读取 Git 差异和版本化文件，不访问网络，不读取聊天记录，不推断隐含批准。

### 6.2 核心规则

当受保护状态或版本发生变化时，验证器必须确认：

- 同一差异中恰好新增一份匹配批准记录；
- `target_path` 与发生变化的文件完全一致；
- `target_version`、`target_state` 和 `decision` 与写入结果一致；
- 目标文档的 `Approval History` 引用同一 `approval_id`；
- 批准记录是新增文件，不能复用基线中已有记录；
- `approved_by` 是项目规定的批准者；
- `authorization_text`、`scope`、`approved_at` 和 `recorded_at` 非空；
- 一个批准 ID 在当前差异中只授权一个目标状态写入。

### 6.3 失败语义

任何不确定情况均以非零退出码失败，并输出：

```text
APPROVAL_GATE_BLOCKED: <reason>
```

失败信息必须指出目标路径、缺失或冲突字段以及修复要求。验证器不得自动创建批准记录、补写批准者或修改目标文档。

## 7. 事件记录

治理事件记录路径：

```text
docs/governance-incidents/2026-07-16-unauthorized-constitution-rc3.md
```

记录必须包含：

- 事件时间和发现时间；
- 相关提交 `059da20`；
- 已执行但未获授权的操作；
- 未发生的行为，例如没有伪造批准记录；
- 影响和当前状态；
- 根因及失效门禁；
- 用户对 `rc.3` 内容的后续批准；
- 明确声明后续批准不追溯合法化既往操作；
- 纠正与预防措施；
- 验证证据和后续关联提交。

该记录是不可删除的历史事实。后续发现新事实时追加更新，不重写原始事件结论。

## 8. 本地执行入口

仓库提供版本化 hook 脚本：

```text
scripts/git-hooks/pre-commit.ps1
```

安装脚本将 `core.hooksPath` 指向仓库内受控 hook 目录，pre-commit 调用：

```powershell
.\scripts\docs\validate-approvals.ps1 -Staged
```

本地 hook 是快速反馈，不是唯一事实来源。直接绕过 hook、未安装 hook 或其他 Git 客户端不会使违规变更合规。最终门禁仍由相同验证器在 P5 governance CI 中执行。

## 9. 测试策略

验证器使用 PowerShell 测试夹具创建临时 Git 仓库，至少覆盖：

1. 未修改受保护状态时通过；
2. 修改 Constitution 版本但无批准记录时失败；
3. 新增批准 JSON 但未更新文档 `Approval History` 时失败；
4. 文档与 JSON 使用不同 approval ID 时失败；
5. `target_path`、版本、状态或决定不匹配时失败；
6. 尝试复用基线已有批准记录时失败；
7. 同一差异新增匹配 JSON 和文档记录时通过；
8. ADR、安全例外、Spec、Plan、验收和归档的受保护状态分别被识别；
9. 本次 `rc.3` 批准记录与 Constitution 文档记录通过验证。

测试必须先证明上述失败场景在实现前不能被阻止，再实现最小验证逻辑使其通过。

## 10. 本次批准的边界

用户在 2026-07-16 明确：

1. 创建勘误记录；
2. 同意 Constitution `rc.3`；
3. 添加批准状态硬性门禁，且每次批准都需要在写入时进行记录；
4. 硬门禁覆盖全部治理资产；
5. 同意实施本设计。

因此，本设计允许后续实施写入：治理事件记录、`rc.3` 批准记录、Constitution 对应批准历史、批准验证器、测试夹具和本地 pre-commit 接入。任何超出该清单的治理状态变化都需要新的明确批准。

## 11. 验收条件

- `059da20` 保持不变且事件记录可追溯；
- Constitution `rc.3` 有结构化批准记录和文档内批准记录；
- 文档继续显示 `Ratified: Pending`，不暗示 `1.0.0` 已正式批准；
- 所有受保护资产类型都有验证规则；
- 缺少、错配和复用批准记录的测试均失败于正确原因；
- 合法同步写入测试通过；
- pre-commit 调用版本化验证器；
- 工作区中不存在私钥、凭据或生成的测试仓库；
- 最终差异和验证证据由用户审查后再提交。

## 12. Approval History

| 阶段 | 状态 | 批准者 | 日期 | 文档版本 | 批准范围 |
|---|---|---|---|---|---|
| 设计实施授权 | Approved for Planning | `xinyan <zuijiuhjd@foxmail.com>` | 2026-07-16 | `1.0.0` | 治理事件、`rc.3` 批准记录、全治理资产批准账本、差异验证器、测试和本地 pre-commit 接入 |
