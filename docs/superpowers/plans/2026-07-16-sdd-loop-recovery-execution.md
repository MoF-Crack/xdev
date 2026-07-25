# XDev SDD/LOOP 恢复执行清单

> **用途**：上下文压缩、会话中断或执行者切换后的唯一恢复入口
> **状态**：Current recovery index（旧执行清单已被当前事实取代）
> **记录日期**：2026-07-16
> **执行基线**：`93069f89082bcb685cb833c69bae64e157bf4b20`
> **当前更新**：2026-07-25 (Phase 2)

## 2026-07-25 当前恢复状态（Phase 2）

本节是当前恢复入口。后续第 1-5 节保留为历史执行基线，不能覆盖本节
或 Git 事实。

- Constitution 已正式批准为 `1.0.0`，Ratified 日期为 `2026-07-16`。
- **所有 7 份 Spec 已 Accepted**（001-007），Spec 005 SC-004 deferred。
- **远端 CI + Governance 全绿**，integration tests 三平台通过。
- **GitHub Release v0.1.0 + v0.2.0 已发布**。
  - v0.2.0：15 个产物（含签名 + SHA256SUMS + integration tests gate）。
  - Release 页面: https://github.com/MoF-Crack/xdev/releases/tag/v0.2.0
- Release workflow 支持增量构建 + 签名硬门禁 + SHA256SUMS。
- Dependabot 漏洞从 26 个降到 0 个（全部修复）。
- C 盘缓存已迁移到 E 盘（npm cache + go-build + .agents via symlink）。
- 版本号已对齐到 `0.2.0`（`internal/version/version.go` + `frontend/package.json`）。
- `docs/progress-and-defects-analysis.md`、`docs/pending-work-plan.md` 和
  `docs/roadmap.md` 的历史正文是 Snapshot/Frozen 材料，不是恢复事实来源。

本文件已是最新状态。下一步工作见末尾"下一步计划"节。

## 1. 当前事实

- Constitution 为 `.specify/memory/constitution.md` `1.0.0-rc.3`，仍是候选版，`Ratified: Pending`。
- 用户已经明确同意 `rc.3` 当前候选内容；批准尚未写入结构化批准记录和 Constitution `Approval History`。
- 提交 `059da207f3cbd028a7e1f95537914238fdf00ddb` 中存在未经事前批准修订 Constitution 的历史事实；后续批准不能追溯合法化该操作。
- approval gate 设计提交为 `a8b271b`，实施计划提交为 `93069f8`。
- 工作树中 `scripts/docs/` 是未提交草稿；现场测试结果为 5/21 通过、16/21 失败，不能作为门禁。
- `scripts/docs/validate-approvals-trace.ps1` 是损坏的临时调试文件，必须删除。
- `specs/`、治理目录、模板覆盖、链接验证和 governance CI 尚未建立。

## 2. 强制执行顺序

以下阶段必须串行执行。前一阶段未满足退出条件时，不得进入后一阶段。

### P0-A：修复批准验证器

目标文件：

```text
scripts/docs/validate-approvals.ps1
scripts/docs/tests/validate-approvals.tests.ps1
```

必须完成：

1. 删除 `scripts/docs/validate-approvals-trace.ps1`。
2. 删除测试中的 `[DBG]` 和其他临时输出。
3. 使用 PowerShell 5.1 可解析、可重复的 UTF-8 处理方式。
4. 覆盖并验证以下受保护资产：Constitution、ADR、安全例外、Spec、Plan、验收、归档。
5. 正确识别：
   - `| 版本 | ... |`；
   - `| 状态 | ... |`；
   - `> **版本**：...`；
   - `> **状态**：...`；
   - `**Version**: ...`；
   - `**Ratified**: Pending`。
6. 无批准、批准 ID 不匹配、路径/版本/状态/批准者不匹配、空授权原文、旧记录复用均必须失败。
7. 合法的同步 JSON 与文档 `Approval History` 写入必须通过。

退出条件：

```text
Passed: 21
Failed: 0
```

并且两个 PowerShell 脚本均通过语法解析，空差异输出：

```text
APPROVAL_GATE_PASSED: 0 protected change(s) validated
```

### P0-B：接入版本化本地门禁

创建：

```text
scripts/git-hooks/pre-commit
scripts/git-hooks/pre-commit.ps1
scripts/install-git-hooks.ps1
```

退出条件：

- hook 测试证明未批准治理变更被阻断；
- 合法同步批准写入通过；
- `core.hooksPath` 显式配置为 `scripts/git-hooks`；
- hook 只调用 `scripts/docs/validate-approvals.ps1 -Staged`，不存在第二套规则。

### P0-C：写入纠正事实与 `rc.3` 批准

创建或修改：

```text
docs/governance-incidents/2026-07-16-unauthorized-constitution-rc3.md
.specify/approvals/2026-07-16-constitution-rc3-001.json
.specify/memory/constitution.md
```

必须满足：

- 事件记录明确 `059da20` 的未经授权操作事实；
- 明确后续批准不追溯授权、不覆盖、不消除该事实；
- JSON 保存用户原始授权文本；
- Constitution `Approval History` 引用相同 approval ID；
- Constitution 继续为 `1.0.0-rc.3` 候选版；
- `Ratified: Pending` 和正式批准日期待定保持不变。

### P0-D：审查、验收与提交门禁

必须依次执行：

1. Spec 合规独立审查；
2. 工程质量独立审查；
3. 修复所有 Critical/Important 问题并复审；
4. 展示测试、暂存白名单、私钥隔离和 Constitution 状态证据；
5. 获得用户对最终纠正差异的明确验收批准；
6. 仅在批准后创建 `governance: enforce recorded approvals` 提交。

禁止把设计批准、计划批准或“继续执行”解释为最终验收批准。

### P1：治理骨架

P0 提交完成后，执行：

```text
docs/superpowers/plans/2026-07-15-governance-scaffolding.md
```

P1 建立 `docs/adr/`、`docs/security/`、`docs/audits/`、`docs/history/`、`specs/` 和首份 manifest 签名安全例外。P1 必须单独取得执行批准和最终验收批准。

### P2A + P2B + P3

P1 完成后可并行：

- P2A：中文 Spec Kit 模板覆盖；
- P2B：ADR、安全例外、归档和审查记录模板；
- P3：Markdown 路径与锚点验证。

### P4A → P4B

- P4A：旧文档元数据治理，不移动文件；
- P4B：通过链接验证后执行历史迁移。

两者不得并行。

### P5 + P6

- P5：Spec、Constitution、治理验证脚本与报告模式 governance CI；
- P6：七个已知缺口的独立 Feature Spec，只写 Spec，不实现功能。

### P7：Constitution 正式批准

只有 P4B、P5、P6 完成，事实冲突关闭或具备有效例外，并获得用户明确批准后，才能将 Constitution 升级为 `1.0.0` 并将治理 CI 切换为阻塞模式。

## 3. 当前禁止事项

- 不 amend、rebase、reset 或重写 `059da20`。
- 不在 P0 完成前执行 P1。
- 不在 P1 完成前创建活跃 Feature Spec 或迁移历史文档。
- 不在测试 21/21 通过前安装 approval gate hook。
- 不把 `rc.3` 候选内容批准描述为 Constitution `1.0.0` 正式批准。
- 不自行写入用户未明确作出的批准。
- ~~不修改 `.github/workflows/ci.yaml`。~~（已完成：CI/Release workflow 已稳定）
- 不执行 `git add .`、`git add -A`、`git reset --hard` 或 `git clean -fd`。
- `keys/priv.key` 永远不得暂存或提交。

## 4. 恢复协议

新会话或上下文压缩后：

1. 先读取本文件顶部"2026-07-25 当前恢复状态"和治理稳定化归档勘误；
2. 运行 `git status --short --branch` 和 `git log --oneline -5`；
3. 运行 approval gate 聚焦测试，记录通过/失败数；
4. 从第一个未满足退出条件的阶段继续；
5. 不重复执行已经具备提交和验证证据的阶段；
6. 发现本文件与 Git 事实冲突时，以 Git、测试和明确用户批准证据为准，并先更新本文件的“当前事实”。

## 5. 进度记录

| 阶段 | 状态 | 证据 |
|---|---|---|
| P0-A 修复批准验证器 | Completed | 21/21 通过；提交 `ba5310d` |
| P0-B 本地门禁 | Completed | `core.hooksPath=scripts/git-hooks`；提交 `ba5310d` |
| P0-C 纠正与批准记录 | Completed | approval JSON、事件记录和 Constitution approval ID 已写入；提交 `ba5310d` |
| P0-D 审查与验收 | Completed | 用户批准后提交 `ba5310d governance: enforce recorded approvals` |
| P1 治理骨架 | Completed | 12 个目录索引 + 1 份安全例外（Draft）；提交 `d0aec02` |
| P2A/P2B/P3 | Completed (violation recorded) | 提交 `2241bdc`；执行未经明确授权，用户接受但记录违规 |
| P4A 旧文档元数据 | Completed | 已完成并作为 P4B 前置输入 |
| P4B 历史迁移 | Completed | 提交 `0c97849` |
| P5/P6 | Completed | 提交 `9a36126`；P6 仅创建 7 份 Draft Spec |
| P7 正式批准 | Completed | 提交 `e91310c`；Constitution `1.0.0` |
| 治理稳定化 Task 1-10 | Completed | 本地验证 + 归档；提交 `640c506` |
| 远端 CI 验证 | Completed | commit `99cc4db`，三平台 build+test 全部 success |
| Spec 005 宿主快照 | Accepted | `internal/testkit/hostsnapshot`；提交 `43193ee` |
| GitHub Release v0.1.0 | Completed | 12 产物在线构建；tag `v0.1.0` @ commit `a360d7f` |
| Release 增量构建 | Completed | `Check existing release assets` + `overwrite_files: false`；commit `a360d7f` |

## 6. 下一步计划

### 短期（可选，按优先级）

1. **Dependabot 漏洞处理**：GitHub 报告 25 个漏洞（8 critical, 5 high, 12 moderate）。
   这是独立议题，不影响 CI/Release，但应在下一个版本前处理。

2. **Node 20 deprecation**：CI 中 `actions/checkout@v4`、`actions/setup-go@v5`、
   `actions/setup-node@v4`、`actions/upload-artifact@v4` 仍使用 Node 20（当前被
   forced to Node 24，仅为警告）。升级到使用 Node 24 的 action 版本可消除警告。

3. **Release 签名**：当前 release job 的 `Sign manifests` step 因无
   `XDEV_SIGNING_PRIVATE_KEY` secret 而跳过。配置签名 secret 后，release 产物
   将自动嵌入公钥并签名 manifest。

4. **GUI 变体全平台覆盖**：当前 AppImage 仅 x86_64，DMG 仅 arm64。
   可增加 linux-arm64 AppImage 和 darwin-amd64 DMG 覆盖。

5. **SHA256SUMS 校验文件**：release 中未包含校验和文件。
   可在 release job 末尾生成 `SHA256SUMS` + `SHA256SUMS.sig` 上传。

### 中期

6. **7 份 Draft Feature Spec 推进**：P6 创建的 7 份 Spec 仍为 Draft，
   需逐份走澄清→批准→计划→实现→验收流程。

7. **Spec 005 SC-004**：宿主快照 ≤5% 开销验证仍 deferred。
   全包 TestMain 覆盖受 import cycle 限制，需设计绕行方案。

8. **版本号策略**：当前 `v0.1.0` 是首个 release。后续应建立
   semver 策略（patch 修 bug / minor 加功能 / major 破坏性变更），
   并在 release notes 中记录变更类型。
