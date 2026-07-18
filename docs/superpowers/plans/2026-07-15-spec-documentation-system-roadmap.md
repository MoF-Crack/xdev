# XDev 规范文档体系实施路线图

> **面向 Agent 执行者：** 本路线图只定义子项目边界、依赖关系和阶段门禁。每个子项目必须使用独立实施计划执行，不得将多个子项目合并为一次无审查的大规模迁移。

**目标：** 将候选 Constitution 和文档体系设计逐步落实为可使用、可验证、可迁移、可自动检查的 XDev 规范文档系统。

**架构：** 实施划分为治理骨架、Spec Kit 模板、治理模板、文档链接验证、元数据治理、历史迁移、治理自动化、已知缺口 Spec 化和最终批准九个子项目。所有写入范围按文件所有权隔离；迁移类工作必须在验证工具和 Git 基线可用后执行。

**技术栈：** Markdown、Git、GitHub Spec Kit 0.12.15、Windows PowerShell 5.1、GitHub Actions、OpenCode/OMO。

## 全局约束

- Constitution 候选版为 `.specify/memory/constitution.md` `1.0.0-rc.2`，尚未正式批准。
- 文档体系设计为 `docs/superpowers/specs/2026-07-15-spec-documentation-system-design.md` `1.0.0-rc.1`，只授权进入实施计划设计。
- 治理和 Spec 文档使用简体中文；代码标识、命令、路径、配置键、日志原文和 Git 提交信息保留英文。
- 不直接修改 `.specify/templates/` 中的 Spec Kit 核心模板；项目覆盖放在 `.specify/templates/overrides/`。
- 活跃功能目录固定为 `specs/<编号-功能名>/`；归档目录固定为 `specs/archive/<年份>/<编号-功能名>/`。
- 历史材料不可改写；纠偏通过元数据、`errata.md` 或替代关系完成。
- Agent 不得代替用户批准 Constitution、Spec、Plan、最终验收或归档。
- 实现任务必须通过 Spec 合规审查和工程质量审查两个独立门禁。
- 私钥 `keys/priv.key` 永远不得暂存、提交或写入文档。
- 禁止执行 `git add .`、`git add -A`、`git reset --hard` 和 `git clean -fd`。

---

## 执行前置条件

开始任何子项目实施前，必须满足：

1. `docs/history/implementation-plans/git-initial-import-plan.md` 定义的 Git 初始导入已经完成；
2. 当前仓库存在可用 `HEAD`，分支为 `main`；
3. 工作区没有与目标子项目重叠的未提交修改；
4. `git` 解析到 `D:\Env\Tools\Git\cmd\git.exe`；
5. `keys/priv.key` 继续被 `.gitignore` 排除；
6. 每个子项目执行前记录基线 commit，便于独立审查和回滚。

如果初始导入尚未完成，只能继续编写和审查实施计划，不能执行下面的迁移与提交步骤。

## 子项目清单

| 编号 | 子项目 | 主要交付物 | 依赖 |
|---|---|---|---|
| P1 | 治理骨架 | 治理目录、索引、manifest 签名安全例外 | Git 初始导入 |
| P2A | Spec Kit 模板覆盖 | 中文 Spec、Plan、Tasks、Checklist 模板 | P1 |
| P2B | 项目治理模板 | ADR、安全例外、归档、审查记录模板 | P1 |
| P3 | 文档链接验证 | Markdown 路径/锚点验证脚本与测试固件 | P1 |
| P4A | 旧文档元数据治理 | 原位置状态、来源、替代关系，不移动文件 | P1、P2B、P3 |
| P4B | 历史文档迁移 | `.trae` 归档、Roadmap、审计、旧计划迁移 | P4A、P3 |
| P5 | 治理自动化 | Spec、Constitution、治理验证脚本；CI 报告模式 | P2A、P2B、P3 |
| P6 | 已知缺口 Spec 化 | 七个正式 Feature Spec，仅写 Spec | P2A、P5 |
| P7 | Constitution 正式批准 | `1.0.0` 批准、治理 CI 切为阻塞模式 | P4B、P5、P6、用户批准 |

本路线图有意将设计阶段 A 中笼统描述的“各类模板”全部分配给 P2A/P2B；P1 仅负责目录索引和首份安全例外。该调整消除了 P1 与 P2 的文件所有权重叠，不改变总体设计交付范围。

## 依赖图

```text
Git 初始导入
    │
    ▼
   P1 治理骨架
    ├────────────┬────────────┐
    ▼            ▼            ▼
  P2A          P2B           P3
Spec模板      治理模板      链接验证
    │            │            │
    ├──────┬─────┘            │
    │      ▼                  │
    │     P4A ◄───────────────┘
    │   元数据治理
    │      │
    │      ▼
    │     P4B 历史迁移
    │
    ├──────────────► P5 治理自动化 ◄──── P2B + P3
    │                   │
    └──────────────────►P6 缺口 Spec 化
                         │
              P4B + P5 + P6 + 用户
                         │
                         ▼
                    P7 正式批准
```

## 可并行执行范围

P1 完成后：

- P2A、P2B、P3 可并行；
- 三者写入范围互不重叠；
- P4A 必须等待 P2B 和 P3；
- P4B 必须等待 P4A；
- P5 等待 P2A、P2B、P3；
- P6 中七个 Feature Spec 可以按独立目录并行编写，但每份仍需用户单独批准。

P4A 与 P4B 不能并行，因为二者依次修改和移动同一批旧文档。

## 文件所有权

### P1：治理骨架

```text
docs/README.md
docs/adr/README.md
docs/security/README.md
docs/security/policy/README.md
docs/security/procedures/README.md
docs/security/exceptions/README.md
docs/security/exceptions/manifest-signature-verification.md
docs/audits/README.md
docs/history/README.md
docs/history/implementation-plans/README.md
specs/README.md
specs/archive/README.md
```

### P2A：Spec Kit 模板覆盖

```text
.specify/templates/overrides/spec-template.md
.specify/templates/overrides/plan-template.md
.specify/templates/overrides/tasks-template.md
.specify/templates/overrides/checklist-template.md
```

### P2B：项目治理模板

```text
.specify/templates/overrides/adr-template.md
.specify/templates/overrides/security-exception-template.md
.specify/templates/overrides/archive-metadata-template.md
.specify/templates/overrides/review-record-template.md
```

### P3：文档链接验证

```text
scripts/docs/validate-links.ps1
scripts/docs/tests/validate-links.tests.ps1
scripts/docs/tests/fixtures/links/**
```

`validate-links.ps1` 只检查 Markdown 文件路径、相对链接和锚点，不负责软件下载源。软件下载源仍由 `scripts/audit-links.ps1` 负责。

### P4A/P4B：旧文档治理与迁移

只允许修改或移动设计文档迁移矩阵中列出的文件。P4A 添加元数据，P4B 在验证通过后移动；两个子项目串行执行。

### P5：治理自动化

```text
scripts/docs/validate-specs.ps1
scripts/docs/validate-constitution.ps1
scripts/docs/validate-governance.ps1
scripts/docs/tests/fixtures/governance/**
.github/workflows/governance.yaml
```

P5 不修改 `.github/workflows/ci.yaml`。

### P6：已知缺口 Feature Spec

每份 Spec 使用独立 `specs/<编号-功能名>/` 目录：

1. CI Go 工具链版本一致性；
2. integration tests CI 门禁；
3. 发布签名硬门禁；
4. 三平台发布产物同源；
5. 测试宿主状态快照；
6. 双重审查和批准记录自动验证；
7. UI/下载源计划剩余集成验收。

P6 只创建并澄清 Spec，不实现对应代码变更。

## 明确延后事项

以下事项不能在 P1-P6 中顺手实施：

- 将治理 CI 从报告模式切换为阻塞模式；
- 将 Constitution 升级为 `1.0.0`；
- 修改 `.github/workflows/ci.yaml` 中 Go 版本、integration tags 或发布签名逻辑；
- 实现七个已知缺口 Feature Spec；
- 冻结或删除旧文档位置；
- 物理删除缓存、依赖目录或历史构建产物；
- 将安全例外描述为永久修复。

## 每个子项目的统一审查流程

每个子项目必须执行：

1. 记录实施前 commit；
2. 按路径白名单修改；
3. 运行子项目聚焦验证；
4. 生成差异包；
5. 执行 Spec 合规审查；
6. 修复阻塞问题并复审；
7. 执行工程质量审查；
8. 修复阻塞问题并复审；
9. 用户批准该子项目验收；
10. 创建子项目提交。

## 总体完成条件

- 四层文档目录全部存在且职责清晰；
- 项目覆盖模板能被 Spec Kit 或项目治理脚本正确消费；
- 历史规格和旧计划按设计迁移，链接有效；
- 治理验证脚本具备成功与失败测试固件；
- CI governance workflow 以报告模式运行；
- 七个当前执行缺口均有独立 Feature Spec；
- Constitution 的事实冲突和执行缺口均已关闭或具有有效例外；
- 用户明确批准 Constitution `1.0.0` 后，治理 CI 才切换为阻塞模式。

## 子计划文件

按以下命名创建：

```text
docs/superpowers/plans/2026-07-15-governance-scaffolding.md
docs/superpowers/plans/2026-07-15-speckit-template-overrides.md
docs/superpowers/plans/2026-07-15-governance-templates.md
docs/superpowers/plans/2026-07-15-document-link-validation.md
docs/superpowers/plans/2026-07-15-legacy-document-metadata.md
docs/superpowers/plans/2026-07-15-document-migration.md
docs/superpowers/plans/2026-07-15-governance-automation.md
docs/superpowers/plans/2026-07-15-known-gap-spec-program.md
docs/superpowers/plans/2026-07-15-constitution-ratification.md
```

本轮只要求完成路线图和 P1 治理骨架计划。后续子计划应在上游交付物和用户决策稳定后逐份编写。
