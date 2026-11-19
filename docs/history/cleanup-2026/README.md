# 2026 年历史文档整理索引

> **文档类型**：历史索引与状态映射
> **状态**：Snapshot
> **核对日期**：2026-11-19
> **核对基线**：`f011031c23065754c0497a3e5d65264d0d160da0`
> **工作分支**：`chore/cleanup-history-and-integrations`

本索引记录四份旧文档的归档位置、历史引用兼容方式和后续事项入口。归档正文保留原始字节，仅前置归档说明；原文中的旧元数据、版本、命令、完成标记和估算仍是当时记录。它们不能替代当前源码、正式 Spec 或本次核对结果，也不直接授权实施新需求。

## 归档位置

| 原始入口 | 原状态 | 完整归档 | 本次处理 |
|---|---|---|---|
| [开发进度与缺陷分析](../../progress-and-defects-analysis.md) | Snapshot | [2026-07-15 分析报告](../reports/2026-07-15-progress-and-defects-analysis.md) | 保留快照正文；旧入口保留迁移指引 |
| [后续工作计划](../../pending-work-plan.md) | Frozen（待拆分） | [2026-07-15 冻结计划](../implementation-plans/2026-07-15-pending-work-plan.md) | 保留全部五项方案；在下表映射 Spec 007 和遗留差异 |
| [Backlog](../../backlog.md) | Frozen | [2026-07-01 Backlog](../2026-07-01-backlog.md) | 保留六批候选需求、附录和历史完成声明；旧入口继续承担候选需求追溯 |
| [旧 unsigned 安全记录](../../../build/manifest-changes-unsigned.md) | 历史 unsigned 记录 | [2026-07-14 manifest 变更记录](../reports/2026-07-14-manifest-changes-unsigned.md) | 保留 Rust 与 Java 两部分安全分析；旧入口保持可追溯及提交资格 |

[Constitution](../../../.specify/memory/constitution.md) 仍引用分析报告，并在“旧计划和 Backlog”一节指名旧计划与 Backlog。[Spec 007](../../../specs/007-ui-download-integration-acceptance/spec.md) 仍依赖旧计划的精确范围，并要求 SHA 变更保留在旧 unsigned 记录的入口。上述四个源路径因此保留短指引及主要章节锚点；本次不修改 Constitution、Spec、Plan、Tasks 或批准记录。

## B5 五项后续工作映射

[原计划五项总览](../implementation-plans/2026-07-15-pending-work-plan.md#总览)与 [Spec 007 验收清单](../../../specs/007-ui-download-integration-acceptance/tasks.md#验收清单)逐项对应。Spec 007 的历史 Tasks 将前四项列为完成、vet 列为 deferred；下面保留当前证据与历史声明的差异，归档不自动关闭需求。

| 原计划事项 | 正式需求 | 当前核对结论 | 后续入口 |
|---|---|---|---|
| 1. 填 SHA / Windows full SHA audit | FR-001、FR-002；User Story 1 | **保留待核验**：旧占位 hash 字面值已无匹配，但仍有空 SHA 和同 URL 不同非空 SHA；历史“全部正确填写”不能作为当前完成证明 | [Spec 007](../../../specs/007-ui-download-integration-acceptance/spec.md#requirements-mandatory)、[原计划精确范围](../implementation-plans/2026-07-15-pending-work-plan.md#精确范围) |
| 2. `.yaml.sig` 重签 | FR-003；User Story 2 | **部分已验证，完整覆盖待完成**：已有嵌入签名可验签；完整覆盖守卫因 51 个可签工具中 15 个缺签名而跳过，另 1 个工具无哈希豁免 | [签名覆盖守卫](../../../manifests_integrity_guard_test.go)、[签名操作流程](../../security/procedures/signing-guide.md)、[旧变更记录入口](../../../build/manifest-changes-unsigned.md) |
| 3. 镜像技术债文档化 | FR-005；User Story 4 | **历史记录已存在，当前差异保留**：四工具记录完整存在；其中镜像与 CI 降级结论和当前配置/代码存在偏差，不能据此宣称镜像问题消失 | [四工具技术债记录](../unfixable-tools.md)、[Spec 009 镜像选择](../../../specs/009-mirror-smart-selection/spec.md)、[当前 CI gate](../../../internal/linkaudit/ci_gate.go) |
| 4. TRANSIENT 错误重试 | FR-004；User Story 3 | **固定重试已实现，可配置验收保留**：最多追加 3 次重试，后续轮次固定等待 2 秒，取消守卫通过；原 Spec 的可配置次数/间隔、30 秒验收示例和 known-transient 备选机制未因此获完整验证 | [Audit 实现](../../../internal/linkaudit/audit.go)、[Request 参数](../../../internal/linkaudit/types.go)、[Spec 007 场景](../../../specs/007-ui-download-integration-acceptance/spec.md#user-story-3-transient-错误自动重试机制生效-priority-p3) |
| 5. `toolsHashFullCheck` vet 告警 | FR-006；User Story 5 | **当前源码与验证已完成**：原第 96、103 行已使用 `fmt.Print`，本次 `go vet ./...` 返回 0；历史 Tasks 的 deferred 标记原样保留，未擅自勾选 | [当前工具源码](../../../toolsHashFullCheck/main.go)、[Spec 007 原验收项](../../../specs/007-ui-download-integration-acceptance/tasks.md#验收清单) |

### SHA 与签名证据边界

本次只做本地源码及数据核对，没有执行原计划约 29GB 的联网 full audit，也没有修改 manifest、生成密钥或重签。

- [Android Build Tools](../../../manifests/android-build-tools.yaml)、[Command-line Tools](../../../manifests/android-cmdline-tools.yaml)、[NDK](../../../manifests/android-ndk.yaml)、[Platform](../../../manifests/android-platform.yaml)的当前 SHA 字段分别为 6、9、12、8 条，空值分别为 2、6、8、3 条，合计 35 条中 19 条为空；旧计划的固定占位 hash 在当前全部 manifest 中无匹配。这只能证明旧字面值不再存在。
- 原冲突范围中的 [Kotlin](../../../manifests/kotlin.yaml) 2.0.21、1.9.24、1.8.22，[Erlang](../../../manifests/erlang.yaml) 27.1.2、26.2.5、25.3.2，以及 [Gradle](../../../manifests/gradle.yaml) 7.6.4，合计 7 个 URL 仍各有多个不同的非空 SHA。其他历史冲突的当前值即使一致，也不能代替实际下载证明。
- [历史实施记录](../spec-007-deferred-items.md)称当时 38 份 manifest 的 SHA 与签名已经完成；当前工具集合已变化。已有签名的密码学验证通过，不等于签名图已经覆盖当前全部 manifest 和哈希。
- `TestManifestSignatureCoverage` 输出 `15/51` 缺签名并明确 `SKIP`。该项继续保留，由现有正式签名/发布流程处理；本次不修改门禁或批准文件。

### 镜像与重试的保留差异

[四工具技术债记录](../unfixable-tools.md)已覆盖 Haskell、Kotlin、Ninja、UV，但当前 [Haskell](../../../manifests/haskell.yaml)、[Kotlin](../../../manifests/kotlin.yaml)、[Ninja](../../../manifests/ninja.yaml)、[UV](../../../manifests/uv.yaml)配置同时存在空镜像和已配置镜像条目。本次没有联网证明镜像健康，原记录中的“不需要进一步操作”仅作为历史结论保留。

该记录声称“official 失败但有镜像时降级为 warning”；当前 [GateDecisionFor](../../../internal/linkaudit/ci_gate.go)对官方源的 `URL_DEAD` 和 `TRANSIENT_ERROR` 仍返回阻断，镜像失败才降级为 warning。[下载源政策](../../security/policy/download-source-policy.md)本身标记 Deprecated，且未包含 Spec 007 指定的四工具专节。应沿现有 Spec 007/009 入口核对验收，不由本次归档新增或实施需求。

当前重试位于 linkaudit 的 `Audit`，首个重试立即执行，后两轮等待 2 秒，使用取消上下文终止循环。[取消回归守卫](../../../internal/linkaudit/audit_retry_cancel_b5_test.go)本次通过。此结果不能扩展为安装下载器全部重试场景、16 条历史 URL 的当前网络健康，或可配置重试已验收。

## Backlog 候选需求入口

[冻结 Backlog](../2026-07-01-backlog.md)的六批候选改进和全部附录均保留。涉及已移除 AI、项目、任务或服务功能的旧建议，应先参照 [Spec 012](../../../specs/012-remove-ai-project-task-service/spec.md)及当前产品源码判断适用性；不能直接恢复旧功能或把旧清单当作现行任务。

尚未确认的候选需求仍从 [Backlog 入口](../../backlog.md)追溯到原文，再按现有正式规格流程处理。本次没有为候选项新建需求、改变优先级或宣布完成。

## 备份与验证

四源在修改前已备份到 lead 指定的工程外归档目录，并生成 `manifestsha256.json` 与 `SHA256SUMS`。下列 SHA-256 同时对应原始备份和每份新归档头之后的完整正文；源位置短指引不使用这些旧正文哈希。

| 原始文档 | 原文 SHA-256 |
|---|---|
| [分析报告原文](../reports/2026-07-15-progress-and-defects-analysis.md) | `84dab2b17f17e28b60f522dd014b944724572a6b209fa4ea0dae08e247c3992e` |
| [冻结计划原文](../implementation-plans/2026-07-15-pending-work-plan.md) | `5bb287566387ee6f238b30d0829386b1e4790e1d2b55d45551ddf7c1e9877d2b` |
| [Backlog 原文](../2026-07-01-backlog.md) | `4fae62b95b3443e71333fa2b5ea6fe8d035569eb6dfd8b86c9a1c0f71f3061af` |
| [旧 unsigned 记录原文](../reports/2026-07-14-manifest-changes-unsigned.md) | `c03070b73c799919e93eb00dc54adf9aa4e80d93a67b04ad13b1da1489d65f6d` |

2026-11-19 本地验证：`go vet ./...` 返回 0；签名孤儿检查、已有嵌入签名验签、瞬时重试取消守卫通过；完整签名覆盖检查明确跳过并保留缺失事实。文档链接与归档正文保留结果以本次交付的验证输出为准。
