# P6 实施计划：已知缺口 Feature Spec 化

> **文档类型**：实施计划
> **关联路线图**：`docs/superpowers/plans/2026-07-15-spec-documentation-system-roadmap.md` P6（第 50 行、第 161-173 行）
> **关联设计规格**：`docs/superpowers/specs/2026-07-15-spec-documentation-system-design.md` `1.0.0-rc.1` 第 10 节
> **前置子项目**：P2A Spec Kit 模板覆盖、P5 治理自动化
> **状态**：Planned
> **文档版本**：`1.0.0`
> **创建日期**：2026-07-16
> **所有者**：`xinyan <zuijiuhjd@foxmail.com>`
> **批准范围**：仅授权按本计划在 P6 路径白名单内创建 7 份 Draft Feature Spec；不授权任何代码实现。
> **延期授权声明**：本计划不授权执行 P7 Constitution 正式批准、任何 Spec 的审查批准、或 P6 范围外的任何文件操作。

---

## 1. 目标

创建 **7 份独立的 Feature Spec（spec.md）** ，将 Constitution `1.0.0-rc.3` 中识别、并经路线图扩展的 **7 个当前已知执行缺口**固化为正式 Spec 文档。每份 Spec 自包含、可独立审查，初始状态均为 `Draft`。

**实施后**：
- `specs/` 目录下新增 7 个独立功能目录，每个包含一份 `spec.md`；
- 7 个缺口拥有从 Constitution 约束逐条追溯的正式需求文档；
- P7 Constitution 正式批准获得可逐项关闭的缺口清单；
- 不产生任何代码改动、CI 配置修改或发布流程变更。

## 2. 架构概述

### 2.1 缺口与 Constitution 的映射关系

Constitution `1.0.0-rc.3` 第 293-302 行"当前已知执行缺口"原列 4 项。路线图结合 Constitution 其他原则和外部历史计划，扩展为 7 项完整缺口清单：

| Spec 编号 | Spec 名称 | Constitution 来源 | 缺口描述 |
|---|---|---|---|
| `001-ci-go-version-consistency` | CI Go 工具链版本一致性 | 原则 VII，第 92 行 | CI 的 Go 版本 `1.23` 与 `go.mod` 的 `go 1.23` 当前一致（已于 rc.3 同步），但缺少自动化同步验证，未来 patch 可能漂移 |
| `002-integration-tests-ci-gate` | integration tests CI 门禁 | 缺口第 297 行 | 默认 CI 未执行带 `integration` build tag 的集成测试 |
| `003-release-signing-hard-gate` | 发布签名硬门禁 | 缺口第 298 行 | 发布作业在签名密钥或公钥缺失时可能继续执行，尚未形成签名硬门禁 |
| `004-triple-platform-release` | 三平台发布产物同源 | 原则 V，第 65-73 行 | 三平台发布产物需从同一源码版本构建并验证，当前缺少该门禁 |
| `005-test-host-state-snapshot` | 测试宿主状态快照 | 缺口第 299 行，原则 II | 测试隔离依赖 `--no-path-fix` 等参数，缺少统一的宿主状态快照检测 |
| `006-dual-review-auto-verification` | 双重审查和批准记录自动验证 | 缺口第 300 行，原则 IV | 双重审查和批准历史尚未由模板或自动检查强制 |
| `007-ui-download-integration-acceptance` | UI/下载源计划剩余集成验收 | 第 255 行、`docs/pending-work-plan.md` | 旧 UI/下载源实施计划的剩余集成验收和最终验证事项 |

### 2.2 每份 Spec 的内容结构

每份 Spec 严格使用 `.specify/templates/overrides/spec-template.md`（P2A 中文覆盖模板）定义的结构，至少包含：

1. **元数据块**：文档类型、状态 `Draft`、版本 `0.1.0`、创建/更新日期、所有者、权威范围
2. **Why**：该缺口为何必须关闭（引用 Constitution 对应条款）
3. **目标**：该 Spec 的核心可验证目标
4. **范围（包含/非目标）**：本次 Spec 覆盖的内容和明确排除的内容
5. **User Scenarios & Testing**：至少 3 个独立可测的用户场景（优先级 P1-P3），每个含 Given/When/Then 验收场景
6. **Requirements**：
   - 功能需求（FR-001 ~ FR-0XX，至少 3 条）
   - 非功能需求（NFR-001 ~ NFR-005：性能、安全、兼容性、跨平台、可维护性）
7. **Success Criteria**：可度量成果（SC-001 ~ SC-0XX）
8. **假设与依赖**
9. **风险分类**：安全、兼容性、跨平台、持久化、签名/发布
10. **Constitution 影响评估**：逐条对照 rc.3 的 7 条不可协商原则
11. **批准记录 (Approval History)**：空白（状态 `Draft`，所有阶段 `Pending`）
12. **变更历史**：Initial Draft 记录

### 2.3 与 Plan/Tasks 的关系

本计划仅创建 `spec.md`。每份 Spec 后续将独立进入完整的 SDD 生命周期：`clarify → plan → Constitution 合规审查 → 用户批准 Plan → tasks → analyze → implement → 双重独立审查 LOOP → converge → 用户批准验收 → 用户批准归档`。P6 不进入 `plan` 或 `implement` 阶段。

## 3. 全局约束

以下约束必须贯穿所有 Task：

| # | 约束 | 来源 |
|---|---|---|
| C1 | **只写 Spec，不实现代码**：不修改 `.go`、`.ps1`、`.yaml`、`.github/` 等任何代码或 CI 配置 | 路线图第 173 行、第 182 行 |
| C2 | **初始状态统一为 `Draft`**：7 份 Spec 的状态、批准记录和变更历史均标记为 Draft/Pending，不包含任何 `Approved` 或 `Active` 记录 | 路线图第 187-200 行 |
| C3 | **每份 Spec 自包含**：任意一份 Spec 可独立阅读和审查，不依赖其他 Spec 的上下文 | Constitution 第 III 原则 |
| C4 | **简体中文**：Spec 正文、标题、注释使用简体中文；代码标识符、路径、命令、配置键、日志原文和 Git 提交信息保留英文 | 路线图第 15 行 |
| C5 | **Constitution 候选上下文**：`.specify/memory/constitution.md` 当前为 `1.0.0-rc.3`（候选版），Spec 中对 Constitution 的引用须表述为"候选"或"rc.3"而非"已批准" | 路线图第 13 行 |
| C6 | **活跃目录规则**：使用 `specs/<三位编号-功能名>/`，编号从 `001` 递增，功能名用 kebab-case | `specs/README.md` 第 9-15 行 |
| C7 | **P6 不审批 Spec**：P6 只创建 Draft，不执行 Spec 合规审查、用户批准或 `Draft → Approved` 状态跃迁。这些属于 P7 及之后的独立工作 | 路线图第 51 行、第 187-200 行 |
| C8 | **批准者记录为待定**：批准者字段不得由 Agent 预填 `xinyan <zuijiuhjd@foxmail.com>`，应留空或写"待定" | Constitution 第 179-185 行 |
| C9 | **不修改已有文件**：本计划只创建 7 个 `spec.md` 新文件，不修改 `constitution.md`、路线图、模板或任何已有代码与文档 | 路线图第 175-186 行 |
| C10 | **禁止路径操作**：不使用 `git add .`、`git add -A`、`git reset --hard`、`git clean -fd` | 路线图第 22 行 |

## 4. 文件结构

### 4.1 目标交付物（P6 路径白名单）

```text
specs/
├── README.md                                   # 已存在（P1），不修改
├── archive/                                    # 已存在（P1），不修改
├── 001-ci-go-version-consistency/
│   └── spec.md                                 # 新建 — CI Go 工具链版本一致性
├── 002-integration-tests-ci-gate/
│   └── spec.md                                 # 新建 — integration tests CI 门禁
├── 003-release-signing-hard-gate/
│   └── spec.md                                 # 新建 — 发布签名硬门禁
├── 004-triple-platform-release/
│   └── spec.md                                 # 新建 — 三平台发布产物同源
├── 005-test-host-state-snapshot/
│   └── spec.md                                 # 新建 — 测试宿主状态快照
├── 006-dual-review-auto-verification/
│   └── spec.md                                 # 新建 — 双重审查和批准记录自动验证
└── 007-ui-download-integration-acceptance/
    └── spec.md                                 # 新建 — UI/下载源计划剩余集成验收
```

### 4.2 不修改的文件

```text
.specify/memory/constitution.md                 # Constitution rc.3 — 不改
.specify/templates/overrides/spec-template.md   # 中文模板 — 不改
specs/README.md                                 # 活跃规格索引 — 不改
docs/superpowers/plans/2026-07-15-spec-documentation-system-roadmap.md  # 路线图 — 不改
docs/pending-work-plan.md                       # 待处理工作 — 不改
.github/workflows/ci.yaml                       # CI 配置 — 不改
go.mod / go.sum                                 # Go 依赖 — 不改
内部 Go 源码、PowerShell 脚本、Wails 前端源码     # 全部 — 不改
```

---

## 5. 逐任务分解

---

### Task 1：前置验证

**目标**：确认上游 P2A 和 P5 交付物存在、目标 `specs/` 目录可安全创建、模板路径可用。

**文件范围**：只读检查，不写入任何文件。

#### 步骤

- [ ] **Step 1：验证 P2A 交付物（中文 Spec 模板）**

确认文件存在且非空：

```powershell
$p2aTarget = '.specify/templates/overrides/spec-template.md'
if (-not (Test-Path -LiteralPath $p2aTarget -PathType Leaf)) {
    throw "P2A 中文 Spec 模板缺失：$p2aTarget。请先完成 P2A。"
}
$size = (Get-Item -LiteralPath $p2aTarget).Length
if ($size -lt 500) {
    throw "P2A Spec 模板内容不足：$p2aTarget ($size bytes)"
}
Write-Output "OK: P2A Spec 模板存在 ($size bytes)"
```

预期：文件存在，且大小 >500 字节。

- [ ] **Step 2：验证 P5 交付物（治理自动化）**

确认治理验证脚本存在：

```powershell
$scripts = @(
    'scripts/docs/validate-specs.ps1',
    'scripts/docs/validate-constitution.ps1',
    'scripts/docs/validate-governance.ps1'
)
$missingScripts = @($scripts | Where-Object { -not (Test-Path -LiteralPath $_ -PathType Leaf) })
if ($missingScripts.Count -gt 0) {
    Write-Output "WARNING: 以下 P5 治理脚本缺失，但不阻塞 P6：$($missingScripts -join ', ')"
    Write-Output "建议在 Spec 创建后、P7 批准前完成 P5 脚本。"
} else {
    Write-Output 'OK: P5 治理脚本全部存在'
}
```

预期：缺失不阻塞 P6（P5 为并行依赖），但应在 P7 前完成。

- [ ] **Step 3：验证 Constitution 和路线图可读**

确认关键参考文件存在且可读取：

```powershell
$refs = @(
    '.specify/memory/constitution.md',
    'docs/superpowers/plans/2026-07-15-spec-documentation-system-roadmap.md',
    'specs/README.md',
    '.specify/templates/overrides/spec-template.md',
    'docs/pending-work-plan.md'
)
foreach ($ref in $refs) {
    if (-not (Test-Path -LiteralPath $ref -PathType Leaf)) {
        throw "缺失参考文件：$ref"
    }
    $content = Get-Content -LiteralPath $ref -Raw
    if ([string]::IsNullOrWhiteSpace($content)) {
        throw "参考文件为空：$ref"
    }
}
Write-Output 'OK: 全部参考文件可读取'
```

预期：无异常。

- [ ] **Step 4：检查目标目录状态**

确认 7 个目标 Spec 目录均不存在（或为空目录可安全创建）：

```powershell
$targetDirs = @(
    'specs/001-ci-go-version-consistency',
    'specs/002-integration-tests-ci-gate',
    'specs/003-release-signing-hard-gate',
    'specs/004-triple-platform-release',
    'specs/005-test-host-state-snapshot',
    'specs/006-dual-review-auto-verification',
    'specs/007-ui-download-integration-acceptance'
)
$existingDirs = @($targetDirs | Where-Object { Test-Path -LiteralPath $_ -PathType Container })
if ($existingDirs.Count -gt 0) {
    Write-Output "WARNING: 以下 Spec 目录已存在："
    $existingDirs | ForEach-Object { Write-Output "  $_" }
    Write-Output "请手动决策：覆盖、跳过或重命名。"
    throw '目标目录已存在，无法自动创建'
}
Write-Output 'OK: 全部 7 个 Spec 目录不存在，可安全创建'
```

预期：无已存在目录。

- [ ] **Step 5：记录实施基线**

```powershell
$baseCommit = git rev-parse HEAD
$baseStatus = git status --short
Write-Output "P6 基线 Commit: $baseCommit"
Write-Output '工作区状态:'
$baseStatus
```

预期：记录完整基线。若工作区存在与 `specs/` 目录重叠的未提交修改，记录但继续。

**门禁**：P2A Spec 模板存在且非空；7 个目标目录均不存在；Constitution 和路线图可读；基线已记录。

---

### Task 2：创建 Spec 001 + 002

**目标**：创建第 1、2 份 Draft Feature Spec。

**文件范围**：
- `specs/001-ci-go-version-consistency/spec.md` — 新建
- `specs/002-integration-tests-ci-gate/spec.md` — 新建

#### Task 2.1：创建 Spec 001 — CI Go 工具链版本一致性

**内容要求**：

| 章节 | 要求 |
|---|---|
| **Why** | Constitution 原则 VII 规定"CI 的 Go 工具链版本必须与 go.mod 的 go 指令一致；不一致属于可复现性缺陷"。当前 CI 硬编码 `go-version: "1.23"`，与 `go.mod` 的 `go 1.23` 于 rc.3 修订时同步一致，但无自动验证机制确保未来 patch 更新时两侧保持同步。若 CI 版本滞后，可能导致 CI 构建行为与本地开发不一致；若 CI 版本超前，可能引入 `go.mod` 未声明的语言特性。 |
| **目标** | 建立自动化机制，确保 CI 的 Go 工具链版本始终与 `go.mod` 的 `go` 指令一致，漂移时立即检测并阻塞 CI。 |
| **范围** | 包含：CI workflow 中 Go 版本解析、`go.mod` `go` 指令提取、版本比对逻辑、不一致时的失败输出。非目标：Go 版本升级本身、修改 `go.sum` 或 vendor 策略、其他语言工具链的版本一致性。 |
| **User Scenarios** (≥3) | 1. (P1) CI 检测到 go.mod 版本变更时自动同步；2. (P2) 本地开发验证 CI 版本一致性；3. (P3) 版本漂移时 CI 明确失败并给出修复指引 |
| **FR** (≥3) | FR-001：CI 必须从 `go.mod` 提取 `go` 指令值作为权威 Go 版本；FR-002：CI 必须将当前使用的 Go 版本与权威版本比对；FR-003：不一致时 CI 必须明确失败并输出差异 |
| **SC** (≥2) | SC-001：`go.mod` 修改 `go` 指令后，CI 在不手动更新 workflow 时检测到不一致；SC-002：一致时正常通过，零误报 |

#### Task 2.2：创建 Spec 002 — integration tests CI 门禁

**内容要求**：

| 章节 | 要求 |
|---|---|
| **Why** | Constitution "当前已知执行缺口"第 297 行明确："默认 CI 未执行带 `integration` build tag 的集成测试"。当前 `go test ./...` 执行常规测试，但带 `integration` tag 的测试（涉及跨模块集成、文件系统操作和下载流程）不在 CI 中运行。长期跳过集成测试可能导致集成路径在无人察觉的情况下损坏。 |
| **目标** | 在 CI 中建立 integration tests 门禁，确保每次 push/PR 执行带 `integration` build tag 的测试套件，失败时阻塞合并。 |
| **范围** | 包含：CI job 或 step 执行 `go test -tags=integration ./...`、超时和资源限制、失败报告。非目标：修改现有测试代码、增加新集成测试用例、将集成测试作为 release gate（由 Spec 003 覆盖）。 |
| **User Scenarios** (≥3) | 1. (P1) Push 时 CI 自动运行 integration tests；2. (P2) integration test 失败时 PR 阻塞；3. (P3) 本地可复现 CI integration test 结果 |
| **FR** (≥3) | FR-001：CI 必须包含专用 job/step 执行 `go test -tags=integration`；FR-002：任一 integration test 失败时 CI 必须标记为失败；FR-003：integration test 超时必须触发明确超时错误 |
| **SC** (≥2) | SC-001：含 `integration` tag 的测试在每次 CI 运行中被执行；SC-002：integration test 失败可追溯至具体测试用例 |

---

### Task 3：创建 Spec 003 + 004

**目标**：创建第 3、4 份 Draft Feature Spec。

**文件范围**：
- `specs/003-release-signing-hard-gate/spec.md` — 新建
- `specs/004-triple-platform-release/spec.md` — 新建

#### Task 3.1：创建 Spec 003 — 发布签名硬门禁

**内容要求**：

| 章节 | 要求 |
|---|---|
| **Why** | Constitution 缺口第 298 行："发布作业在签名密钥或公钥缺失时可能继续执行，尚未形成签名硬门禁"。当前 `.github/workflows/ci.yaml` 仅在 secret 存在时签名或注入公钥，缺失 secret 时发布流程可能继续并产出未签名或签名验证被跳过的产物。Constitution 原则 I 和"分层 Manifest 信任模型"要求"正式发布必须启用 manifest 签名验证"、"信任判断必须发生在执行和提取之前"。此缺口同时关联 `docs/security/exceptions/manifest-signature-verification.md` 安全例外。 |
| **目标** | 建立发布签名硬门禁：签名私钥或根公钥缺失时，CI 必须明确失败并阻止发布产物生成，不得静默降级为未签名发布。 |
| **范围** | 包含：CI release job 的签名私钥检查、公钥注入检查、缺失时的硬失败、失败日志必须包含缺失变量名。非目标：签名算法变更、manifest 格式变更、对已发布历史产物的追溯签名。 |
| **User Scenarios** (≥3) | 1. (P1) 发布时签名私钥缺失导致 CI 失败；2. (P2) 发布时公钥缺失导致 CI 失败；3. (P3) 签名验证链完整时发布成功 |
| **FR** (≥3) | FR-001：CI release job 必须在签名私钥缺失时立即失败；FR-002：CI release job 必须在根公钥未注入时立即失败；FR-003：失败日志必须明确标识缺失的 secret 变量名 |
| **SC** (≥2) | SC-001：删除签名 secret 后触发发布流程，CI 必须返回非零退出码；SC-002：签名正常时发布不受影响 |

#### Task 3.2：创建 Spec 004 — 三平台发布产物同源

**内容要求**：

| 章节 | 要求 |
|---|---|
| **Why** | Constitution 原则 V 要求"正式发布前必须通过 Windows、Linux、macOS CI。缺少任一平台证据时，只能声明已验证的平台，不能声明'跨平台完成'"。当前 CI 包含三平台构建 job，但缺少统一的同源验证门禁：三平台产物是否从同一 Git commit 构建、是否使用了相同的依赖版本。若平台间不一致，可能导致"跨平台"声明不成立。 |
| **目标** | 建立三平台发布产物同源验证门禁：确保同一发布版本的 Windows、Linux、macOS 产物均从同一 `HEAD` 构建，使用相同的 `go.sum`/`package-lock.json`，不一致时阻止发布。 |
| **范围** | 包含：Git commit 一致性验证、依赖版本一致性检查、CI 中三平台构建的同步约束。非目标：三平台功能等价性测试（属于各功能 Spec）、交叉编译工具链的版本管理。 |
| **User Scenarios** (≥3) | 1. (P1) 发布时自动验证三平台产物来自同一 commit；2. (P2) 任缺平台证据时发布失败；3. (P3) 依赖版本不一致时发布失败 |
| **FR** (≥3) | FR-001：CI release job 必须记录并比对三平台构建使用的 Git commit；FR-002：任一平台构建失败时，发布流程必须阻塞；FR-003：三平台产物必须使用相同的 `go.sum` 和 `package-lock.json` |
| **SC** (≥2) | SC-001：三平台产物 commit 之一不匹配时发布被阻止；SC-002：三平台一致时正常发布 |

---

### Task 4：创建 Spec 005 + 006

**目标**：创建第 5、6 份 Draft Feature Spec。

**文件范围**：
- `specs/005-test-host-state-snapshot/spec.md` — 新建
- `specs/006-dual-review-auto-verification/spec.md` — 新建

#### Task 4.1：创建 Spec 005 — 测试宿主状态快照

**内容要求**：

| 章节 | 要求 |
|---|---|
| **Why** | Constitution 原则 II 和缺口第 299 行："测试隔离依赖 `--no-path-fix` 等参数，尚缺少统一的宿主状态快照检测"。测试必须使用临时目录、内存替身、沙箱或明确的无持久化写入模式；测试结束后的回滚不能替代隔离。当前缺少统一的自动化检测机制，无法证明测试确实未修改真实用户注册表、环境变量、Shell 配置和全局工具目录。 |
| **目标** | 建立统一的测试宿主状态快照机制：在测试前捕获关键宿主状态基线，测试后比对差异，发现任何持久化修改时测试立即失败并标记为阻塞性缺陷。 |
| **范围** | 包含：宿主状态快照的定义（注册表、环境变量、文件系统路径、全局配置）、快照采集和比对逻辑、CI 集成。非目标：替换现有测试隔离策略（`--no-path-fix` 等参数仍可用）、对第三方测试框架的侵入式修改。 |
| **User Scenarios** (≥3) | 1. (P1) 自动化测试运行前后宿主状态无差异；2. (P2) 发现宿主状态变更时测试失败并输出差异；3. (P3) CI 中强制执行宿主状态隔离检查 |
| **FR** (≥3) | FR-001：测试框架必须支持测试前后的宿主状态快照采集；FR-002：快照差异检测必须覆盖注册表、环境变量、指定文件系统路径；FR-003：发现差异时测试必须返回失败并输出具体变更内容 |
| **SC** (≥2) | SC-001：已知修改宿主状态的测试被快照检测捕获并失败；SC-002：完全隔离的测试零误报通过 |

#### Task 4.2：创建 Spec 006 — 双重审查和批准记录自动验证

**内容要求**：

| 章节 | 要求 |
|---|---|
| **Why** | Constitution 原则 IV 和缺口第 300 行："双重审查和批准历史尚未由模板或自动检查强制"。Constitution 要求每个实现任务必须依次通过 Spec 合规审查和工程质量审查两个独立门禁，审查者不得是任务实现者，批准记录必须版本化。当前审查流程依赖人工约定，缺少自动化检查确保：(a) 每个任务存在两类审查记录；(b) 审查者与实现者不重复；(c) 审查记录路径和格式符合约定。 |
| **目标** | 建立自动化验证机制，检查 `specs/<编号-功能名>/reviews/` 下的审查记录完整性、独立性和格式合规性，缺失或不合规时发出阻塞信号。 |
| **范围** | 包含：审查记录目录约定、审查者独立性验证规则、CI/PowerShell 验证脚本、模板检查集成。非目标：审查内容质量的自动判断（由人工审查者负责）、取代审查流程的自动化审批。 |
| **User Scenarios** (≥3) | 1. (P1) CI 自动检查每个 Verified 任务有对应的两类审查记录；2. (P2) 审查者与实现者重复时检测并报告；3. (P3) 审查记录缺失时 PR 阻塞 |
| **FR** (≥3) | FR-001：验证脚本必须检查 `reviews/` 目录下每个任务是否存在 Spec 审查和 Quality 审查记录；FR-002：必须校验审查者身份与 `tasks.md` 中声明的实现者不重复；FR-003：缺失或违规时 CI 必须以非零退出码报告 |
| **SC** (≥2) | SC-001：缺少任一审查记录时 CI 失败；SC-002：审查者=实现者时 CI 检测并失败 |

---

### Task 5：创建 Spec 007 — UI/下载源计划剩余集成验收

**目标**：创建第 7 份 Draft Feature Spec。

**文件范围**：
- `specs/007-ui-download-integration-acceptance/spec.md` — 新建

**内容要求**：

| 章节 | 要求 |
|---|---|
| **Why** | Constitution 第 255 行将 `docs/history/implementation-plans/2026-07-11-xdev-ui-download-sources.md` 冻结为历史实施记录，明确"未完成的集成验收和最终验证提炼为新的正式 feature spec"。同时 `docs/pending-work-plan.md` 列出了该计划 Task 13-17 的 5 项后续工作：Windows full SHA audit、`.yaml.sig` 重签、不可修复工具文档化、TRANSIENT 错误重试、`toolsHashFullCheck` vet 告警修复。这些事项缺少正式 Spec 驱动的跟踪和验收标准。 |
| **目标** | 将 UI/下载源历史计划中未完成的集成验收事项固化为正式 Feature Spec，明确验收标准、优先级和完成条件，使其可独立追踪和审查。 |
| **范围** | 包含：5 项后续工作的需求定义和验收场景。非目标：直接实施这些工作（由后续 Plan/Tasks 负责）、新增超出 `docs/pending-work-plan.md` 范围的需求。 |
| **User Scenarios** (≥3) | 1. (P1) Windows full SHA audit 完成并替换占位 hash；2. (P2) `.yaml.sig` 基于审计结果重签；3. (P3) TRANSIENT 错误自动重试机制生效 |
| **FR** (≥3) | FR-001：Android manifest 中的占位 hash 必须全部替换为实际 SHA-256 值；FR-002：TRANSIENT 错误下载必须自动重试（最多 N 次）；FR-003：不可修复工具必须在文档中明确记录原因和替代方案 |
| **SC** (≥2) | SC-001：全部 SHA audit 完成后 0 个占位 hash 残留；SC-002：`.yaml.sig` 重签后所有 manifest 签名验证通过 |

---

### Task 6：验证

**目标**：确认 7 份 Spec 全部存在、内容完整、格式合规、无占位符。

**文件范围**：只读检查，不写入任何文件。

#### 步骤

- [ ] **Step 1：验证文件存在且非空**

```powershell
$specs = @(
    'specs/001-ci-go-version-consistency/spec.md',
    'specs/002-integration-tests-ci-gate/spec.md',
    'specs/003-release-signing-hard-gate/spec.md',
    'specs/004-triple-platform-release/spec.md',
    'specs/005-test-host-state-snapshot/spec.md',
    'specs/006-dual-review-auto-verification/spec.md',
    'specs/007-ui-download-integration-acceptance/spec.md'
)
foreach ($s in $specs) {
    if (-not (Test-Path -LiteralPath $s -PathType Leaf)) {
        throw "缺失 Spec 文件：$s"
    }
    $size = (Get-Item -LiteralPath $s).Length
    if ($size -lt 1000) {
        throw "Spec 文件内容不足：$s ($size bytes)"
    }
    Write-Output "OK: $s ($size bytes)"
}
```

预期：7 个文件全部存在且 >1000 字节。

- [ ] **Step 2：扫描占位符**

```powershell
$placeholderIssues = @()
foreach ($s in $specs) {
    $content = Get-Content -LiteralPath $s -Raw
    if ($content -match '\b(TODO|TBD|PLACEHOLDER|NEEDS CLARIFICATION)\b') {
        $placeholderIssues += "$s：包含占位符"
    }
}
if ($placeholderIssues.Count -gt 0) {
    $placeholderIssues
    throw 'Spec 文件包含禁止的占位符'
}
Write-Output 'OK: 无占位符'
```

预期：无占位符。

- [ ] **Step 3：验证关键章节存在**

每份 Spec 必须包含以下章节标记：

```powershell
$requiredSections = @(
    '## Why',
    '## Requirements',
    '### 功能需求',
    '## Success Criteria',
    '## 批准记录',
    '## 变更历史'
)
foreach ($s in $specs) {
    $content = Get-Content -LiteralPath $s -Raw
    foreach ($section in $requiredSections) {
        if ($content -notmatch [regex]::Escape($section)) {
            throw "$s 缺少章节：$section"
        }
    }
}
Write-Output 'OK: 关键章节全部存在'
```

预期：无缺失章节。

- [ ] **Step 4：验证状态和批准记录**

```powershell
foreach ($s in $specs) {
    $content = Get-Content -LiteralPath $s -Raw
    if ($content -notmatch '\*\*状态\*\*：Draft') {
        throw "$s 状态不是 Draft"
    }
    if ($content -match '批准.*Approved' -and $content -notmatch 'Pending') {
        throw "$s 包含非 Draft 的批准记录"
    }
}
Write-Output 'OK: 全部 Spec 状态为 Draft，无伪造批准'
```

预期：全部 Draft，无 Approved 状态。

- [ ] **Step 5：验证需求计数**

```powershell
foreach ($s in $specs) {
    $content = Get-Content -LiteralPath $s -Raw
    # 提取 FR- 行
    $frPattern = '\*\*FR-\d+\*\*'
    $frMatch = [regex]::Matches($content, $frPattern)
    $frCount = $frMatch.Count
    
    if ($frCount -lt 3) {
        throw "$s 功能需求不足：$frCount 条（需要 ≥3）"
    }
    Write-Output "$s : $frCount FR(s)"
}
```

预期：每份 Spec 至少 3 条 FR。

- [ ] **Step 6：验证未意外修改其他文件**

```powershell
git diff --name-only HEAD
# 应仅包含 7 个 spec.md 路径，不得包含：
#   .github/workflows/ci.yaml
#   go.mod / go.sum
#   .specify/memory/constitution.md
#   或任何 .go / .ps1 / .ts / .tsx 源码文件
```

预期：仅 7 个 `specs/XXX/spec.md` 出现在 diff 中。

**门禁**：7 个文件存在且非空；零占位符；关键章节完整；全部 Draft；每份 ≥3 条 FR；无意外修改。

---

## 6. 验收标准

| # | 标准 | 验证方式 |
|---|---|---|
| AC-001 | `specs/` 目录下新增 7 个独立子目录，每个包含一个 `spec.md` | Task 6 Step 1 |
| AC-002 | 每个 `spec.md` 状态为 `Draft`，批准记录全部 `Pending`，无 `Approved` 记录 | Task 6 Step 4 |
| AC-003 | 每个 Spec 包含 Why、Requirements（≥3 条 FR）、Success Criteria、批准记录、变更历史章节 | Task 6 Step 3 |
| AC-004 | 每个 Spec 的功能需求至少 3 条（FR-001 ~ FR-0XX） | Task 6 Step 5 |
| AC-005 | 全部 Spec 无 TODO、TBD、PLACEHOLDER、NEEDS CLARIFICATION 占位符 | Task 6 Step 2 |
| AC-006 | 每个 Spec 的 Why 章节明确引用 Constitution rc.3 的对应条款或缺口行号 | 手动审查 |
| AC-007 | 每个 Spec 的 Constitution 影响评估表格完整（7 条原则逐条判定） | 手动审查 |
| AC-008 | 仓库中除 7 个 `spec.md` 外无其他文件被修改或新增 | Task 6 Step 6 |
| AC-009 | 未修改任何 `.go`、`.ps1`、`.yaml`、`.github/` 文件 | Task 6 Step 6 |
| AC-010 | 每份 Spec 自包含：可独立阅读而不依赖其他 Spec 的内容 | 手动审查 |

---

## 7. 明确延后与不授权事项

以下操作**不在本计划授权范围内**，不得在 P6 执行期间顺手完成：

- **进入 `plan` 阶段**：不为任何 Spec 创建 `plan.md`、`tasks.md` 或 `research.md`；
- **实现代码**：不修改 `.go`、`.ps1`、`.yaml`、`.github/workflows/`、Wails 前端源码或任何生产代码；
- **修改 CI 配置**：不改动 `.github/workflows/ci.yaml` 中的 Go 版本、integration tag、签名逻辑或发布流程；
- **批准 Spec**：不执行 `Draft → Approved` 状态跃迁，不填写批准记录中的批准者、日期或批准范围；
- **执行 Spec 合规审查或工程质量审查**：P6 只创建 Draft，审查属于后续 Plan/Implement 阶段的独立工作；
- **修改 Constitution**：不修改 `.specify/memory/constitution.md`；
- **关闭安全例外**：不修改 `docs/security/exceptions/manifest-signature-verification.md` 的状态；
- **修改 `docs/pending-work-plan.md`**：该文件由 P4A/P4B 治理，不在 P6 范围内；
- **切换治理 CI 为阻塞模式**：属于 P7 Constitution 正式批准后的操作；
- **创建 `plan.md`、`tasks.md` 或 `checklist.md`**：每份 Spec 后续的 Plan/Tasks 属于独立实施计划；
- **使用 `git add .`、`git add -A`、`git reset --hard`、`git clean -fd`**。

如需执行上述任何操作，必须由用户在新的独立子项目实施计划中另行批准。

---

## 8. 每份 Spec 的内容指引（供实施参考）

### 8.1 Spec 001 — CI Go 工具链版本一致性

| 维度 | 内容 |
|---|---|
| Constitution 引用 | 原则 VII，第 92 行："CI 的 Go 工具链版本必须与 go.mod 的 go 指令一致" |
| 当前事实 | CI 硬编码 `go-version: "1.23"`，`go.mod` 为 `go 1.23`，手动同步；无自动化 |
| 核心 FR | 版本提取、比对、漂移失败 |
| 关键 Scenario | go.mod 升级 go 指令后 CI 未同步 → CI 失败 |
| 风险 | 低（当前一致），但属于可复现性缺陷 |

### 8.2 Spec 002 — integration tests CI 门禁

| 维度 | 内容 |
|---|---|
| Constitution 引用 | 缺口第 297 行 |
| 当前事实 | `go test ./...` 默认跳过 `integration` tag；CI 未单独执行集成测试 |
| 核心 FR | CI 新增 integration job、失败阻塞、超时处理 |
| 关键 Scenario | 集成测试失败 → PR 阻塞 |
| 风险 | 中（长期跳过可能导致集成路径静默损坏） |

### 8.3 Spec 003 — 发布签名硬门禁

| 维度 | 内容 |
|---|---|
| Constitution 引用 | 原则 I、缺口第 298 行、安全例外 `manifest-signature-verification.md` |
| 当前事实 | CI 仅在 secret 存在时签名，缺失时不阻塞 |
| 核心 FR | 私钥缺失失败、公钥缺失失败、失败日志包含变量名 |
| 关键 Scenario | 删除 `XDEV_SIGNING_PRIVATE_KEY` secret → 发布失败 |
| 风险 | 高（安全边界），已登记安全例外 |

### 8.4 Spec 004 — 三平台发布产物同源

| 维度 | 内容 |
|---|---|
| Constitution 引用 | 原则 V，第 65-73 行 |
| 当前事实 | CI 有三平台构建 job，但无同源验证 |
| 核心 FR | commit 一致性验证、依赖版本一致性、任缺平台证据发布失败 |
| 关键 Scenario | Linux 构建 commit 与 Windows 不一致 → 发布失败 |
| 风险 | 中（"跨平台"声明可能不成立） |

### 8.5 Spec 005 — 测试宿主状态快照

| 维度 | 内容 |
|---|---|
| Constitution 引用 | 原则 II、缺口第 299 行 |
| 当前事实 | 依赖 `--no-path-fix` 参数，无统一自动化检测 |
| 核心 FR | 快照采集、差异检测、覆盖注册表/环境变量/文件路径、CI 集成 |
| 关键 Scenario | 测试修改注册表 → 快照检测失败 |
| 风险 | 中（宿主污染可能导致难以排查的问题） |

### 8.6 Spec 006 — 双重审查和批准记录自动验证

| 维度 | 内容 |
|---|---|
| Constitution 引用 | 原则 IV、缺口第 300 行 |
| 当前事实 | 审查依赖人工约定，无自动化检查 |
| 核心 FR | 审查记录存在性检查、审查者独立性验证、CI 阻塞 |
| 关键 Scenario | 缺失 Quality 审查记录 → CI 失败 |
| 风险 | 低（治理类），但直接影响 LOOP 合规性 |

### 8.7 Spec 007 — UI/下载源计划剩余集成验收

| 维度 | 内容 |
|---|---|
| Constitution 引用 | 第 255 行、`docs/pending-work-plan.md` |
| 当前事实 | 5 项后续工作未完成，缺少正式 Spec |
| 核心 FR | SHA audit 完成、yaml.sig 重签、TRANSIENT 重试、不可修复工具文档化、vet 修复 |
| 关键 Scenario | 占位 hash 全部替换后 SHA audit 通过 |
| 风险 | 低（无需紧急修复），但属于历史债务 |

---

## 9. 实施注意事项

1. **P5 治理脚本的消费**：P5 的 `validate-specs.ps1` 在 Spec 创建后可用于自动检查章节完整性和占位符，但 P6 不依赖 P5 完全完成（P5 和 P6 可并行）。
2. **7 份 Spec 的并行创建**：7 份 Spec 互不依赖，可按 Task 2-5 任意顺序并行创建。
3. **每份 Spec 的 FR 编号独立**：从 FR-001 开始，各 Spec 独立编号，不跨 Spec 连续。
4. **Constitution 影响评估**：每份 Spec 必须包含完整的 7 条原则影响评估表（Compliant / Not Applicable / Needs Review）。
5. **批准记录格式**：使用 `specs/README.md` 第 34-50 行定义的生命周期和 `spec-template.md` 定义的批准记录表格格式。
6. **审查独立性**：P6 创建的 7 份 Draft Spec 未进入 Plan/Implement/审查流程，因此不涉及审查者与实现者冲突的问题。该约束在后续各 Spec 的 Plan 阶段才生效。

---

## 10. 批准记录 (Approval History)

| 阶段 | 状态 | 批准者 | 日期 | 文档版本 | 批准范围 |
|---|---|---|---|---|---|
| P6 实施计划批准 | Pending | — | — | `1.0.0` | 授权按本计划在 P6 路径白名单内创建 7 份 Draft Feature Spec；不授权代码实现 |

---

## 11. 变更历史

| 版本 | 日期 | 变更类型 | 说明 | 批准者 |
|---|---|---|---|---|
| `1.0.0` | 2026-07-16 | Initial | 依据路线图 P6、Constitution rc.3 和中文 Spec 模板编写 P6 实施计划 | — |
