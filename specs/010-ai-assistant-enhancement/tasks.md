# Tasks: AI 助手功能（xdev 专属窄场景）

> **文档类型**：Task List
> **状态**：Ready
> **创建日期**：2026-07-26
> **关联 Spec**：`specs/010-ai-assistant-enhancement/spec.md` `0.7.0`
> **关联 Plan**：`specs/010-ai-assistant-enhancement/plan.md` `0.1.0`

## 任务列表

> 任务按依赖顺序编排：接入层（T001-T012）→ 能力层（T013-T022）→ 集成与 UI（T023-T028）→ 审查（T029）
> 标注 ⚡ 的任务可并行（无写冲突）。

| # | 任务 | 依赖 | 状态 | 审查 | 提交 |
|---|---|---|---|---|---|
| T001 | 新增 `internal/ai/provider.go`：`Provider` 接口（含能力查询方法）、`ProviderConfig`、`ModelParams`、`Capabilities`、`Pricing`、`ChatResult`、`Message` 结构 + `ProviderRegistry`（Register/Get/All/Active/SetActive） | — | Ready | — | — |
| T002 | 新增 `internal/ai/providers/builtin.go`：`BuiltinProviders` 配置表（openai/anthropic/deepseek/qwen/gemini/ollama-cloud，各含默认 base_url + 协议 + 默认 model + 能力声明）+ `ModelCapabilityTable`（gpt-4o/o1/o3/claude-sonnet-4-5/deepseek-reasoner 等） | T001 | Ready | — | — |
| T003 | 新增 `internal/ai/providers/anthropic_proto.go`：`ProtocolAdapter` 实现，`POST /v1/messages`，`x-api-key` + `anthropic-version` header，reasoning_effort 映射 `thinking.type=budgeted` + `thinking.budget_tokens`（按等级换算 token 预算），ParseResponse 统一 ChatResult | T001 | Ready | — | — |
| T004 | 新增 `internal/ai/providers/openai_chat_proto.go`：`ProtocolAdapter` 实现，`POST /v1/chat/completions`，`Bearer`，注入 temperature/top_p/max_tokens/stop/reasoning_effort（仅 model 支持），ParseResponse 统一 ChatResult | T001 | Ready | — | — |
| T005 | 新增 `internal/ai/providers/openai_responses_proto.go`：`ProtocolAdapter` 实现，`POST /v1/responses`，`Bearer`，reasoning.effort 嵌套字段，ParseResponse 统一 ChatResult | T001 | Ready | — | — |
| T006 | 新增 `internal/ai/providers/custom.go` + `builtin_provider.go`：`custom` provider 实现（按用户选定 Protocol 委托给三个协议适配器之一）+ `builtin` provider 实现（按 BuiltinProviders 配置 + 协议适配器）；协议与端点响应不匹配报"与协议 X 不兼容" | T002-T005 | Ready | — | — |
| T007 | 新增 `internal/ai/keystore.go`：`Keystore` 接口 + 4 实现：`credentialManagerKeystore`(Win, 调 cmdkey)、`keychainKeystore`(mac, 调 security)、`secretServiceKeystore`(Linux, 调 secret-tool 或 D-Bus)、`encryptedFileKeystore`(fallback, AES-256-GCM + 机器绑定密钥, 存 `$XMDEV_HOME/cache/keystore.dat`) | T001 | Ready | — | — |
| T008 | 重构 `internal/ai/assistant.go`：移除 Ollama 硬编码（`ollamaURL`/`isLocalOllamaURL`/`generate` Ollama 专有逻辑），持有 `*ProviderRegistry` + `Keystore` + `skills map[string]skill` + `fallback []string`；`Chat`/`generate` 委托 active provider，失败按 fallback_chain；保留 `SuggestVersions`/`DiagnoseIssue` 为 deprecated wrapper（内部调 `Run`）；`Generate` 返回 `ErrDeprecated` | T001,T006,T007 | Ready | — | — |
| T009 | 重构 `internal/config/config.go`：`AIConfig` 扩展为 `Providers map[string]ProviderConfig` + `ActiveProvider` + `FallbackChain` + `DefaultParams` + `Pricing` + `ConfirmActions` + `SystemPrompt`；新增旧字段迁移（`ollama_url`/`model`/`timeout` → `providers.ollama` custom + `active_provider: ollama`），备份原 config.yaml；`Save` 兼容新结构 | T001,T007 | Ready | — | — |
| T010 | 实现双层参数合并 + extra_body 透传 + 能力查询驱动：`Assistant.resolveParams`（DefaultParams ← Provider.Params ← extra_body，优先级递增，冲突日志记录覆盖链）；`Provider.Chat` 内部按 `SupportsReasoning()` 等决定字段注入，模型不支持某参数时忽略并警告 | T008,T002 | Ready | — | — |
| T011 | 实现 ContextWindow token 裁剪：`AIContext.Build` 后按 provider `ContextWindow()` 粗估 token（~4 字符/token，留 10% 余量），超限时按优先级裁剪：最早上下文 > manifest 候选 topN > AIContext 详情，保留 system prompt + 核心意图 | T008,T010 | Ready | — | — |
| T012 | 实现轻量成本计量：`ChatResult.CostEstimate` = `TokensUsed × Pricing`（按 input/output 拆分，用户配置 `ai.pricing`）；未配 pricing 时只记 token 不计成本；`provider test` 显示 token 用量与成本估算 | T008,T009 | Ready | — | — |
| T013 | 新增 `internal/ai/context.go`：`AIContext` + `ProjectEnv` + `ToolStatus` + `ManifestCandidate` + `TaskInfo` 结构；`BuildAIContext(cfg, registry, catalog, projDir)` 采集 OS/Arch、已装工具、激活版本、PATH 含 shimDir、manifest 候选（latest + topN）；`ProjectEnv` 通过 `project.FindConfig` + 扫描项目标记文件（package.json engines/go.mod/requirements.txt/Cargo.toml/pyproject.toml）填充 | T009 | Ready | — | — |
| T014 | 新增 `internal/ai/schema.go`：结构化输出 schema 定义 + JSON 解析容错（提取 ```json 代码块、字段缺失默认值、解析失败返回 error 供调用方走 Fallback） | — | Ready | — | — |
| T015 | 新增 `internal/ai/skills.go`：`skill` 接口（小写，包外不可注册）+ 5 个内置固定 skill 注册初始化；`Assistant.Run(skill, args)` 调度：构建 AIContext → 取 skill → 若 NeedsProvider 调 active provider（失败 fallback）→ Parse → 失败/不可用走 skill.Fallback | T008,T013,T014 | Ready | — | — |
| T016 | 实现 `suggest` skill：`BuildPrompt`（注入已装版本 + manifest 候选）、`OutputSchema`=`Suggestion`、`Parse`、`Fallback`（manifest latest + 命令） | T015 | Ready | — | — |
| T017 | 实现 `diagnose` skill：`BuildPrompt`（注入 PATH 健康度 + shim 完整性 + 已装工具状态）、`OutputSchema`=`Action`、`Parse`、`Fallback`（规则化 PATH/init 检查清单） | T015 | Ready | — | — |
| T018 | 实现 `health-check` skill：`BuildPrompt`（注入完整 AIContext）、`OutputSchema`=`[]Action`（问题项 + 建议命令）、`Parse`、`Fallback`（纯规则检查，精简 `xdev doctor`） | T015 | Ready | — | — |
| T019 | 实现 `migrate` skill：`BuildPrompt`（注入本地扫描结果）、`OutputSchema`=`Suggestion`（Tool/Path/Command）、`Parse`、`Fallback`（扫描 PATH + 常见路径匹配 shims 名） | T015 | Ready | — | — |
| T020 | 实现 `recommend` skill：默认模式（无 `.xdev.toml` 时扫描项目标记文件推断推荐工具组合 + 生成 `.xdev.toml` 命令）+ `--check` 模式（有 `.xdev.toml` 时对比声明与已装版本输出 satisfied/conflict/gaps/upgrades）；`OutputSchema`=`Recommendation`、`Fallback`（解析 `.xdev.toml`/`.tool-versions`/标记文件，按声明版本与 manifest latest 比对，输出 gap 列表 + 命令） | T015,T013 | Ready | — | — |
| T021 | 新增 `internal/ai/fallback.go`：集中 5 个 skill 的规则化降级策略（suggest→manifest latest、diagnose→PATH 检查、health-check→精简 doctor、migrate→PATH 扫描、recommend→.xdev.toml 解析比对），返回结果标注 `[fallback]` | T016-T020 | Ready | — | — |
| T022 | 新增 `internal/ai/assistant_test.go` 扩展：mock provider client（实现 `Provider` 接口）+ httptest server；覆盖 5 skill 的 Run 路径、fallback chain、降级、协议适配器、参数注入、ContextWindow 裁剪、双层参数优先级、API Key 脱敏；不依赖真实 API | T001-T021 | Ready | — | — |
| T023 | 重构 `internal/cli/ai_cmd.go`：新增 `xdev ai config provider add/list/remove/switch/test`（内置渠道 `add <name> --apikey`、自定义渠道 `add custom <name> --base-url --protocol --model [--apikey] [--temperature] [--reasoning-effort] [--context-window] [--extra-body]`）、`apikey set/get/remove [--store plaintext|keystore]`（get 脱敏）、`default-params set/show`、`model set <provider> --model [--temperature] ...`、`xdev ai run <skill> [args]`（recommend 支持 `--check`）、`xdev ai skills`；未配置 provider 时显示引导；**移除 `xdev ai chat`** | T008,T009,T015 | Ready | — | — |
| T024 | 重构 `internal/cli/register.go` 等：`newAiCmd` 接入新 `Assistant`（用新 `AIConfig` 构造，传 `Keystore`），替换旧 Ollama 直连 | T008,T023 | Ready | — | — |
| T025 | 重构 `internal/gui/app_project.go`：`GetAIStatus`/`AISuggest`/`AIDiagnose` 内部走新 `Assistant.Run`；`AIChat` 返回废弃提示；新增 bindings：`AIListProviders`/`AIAddProvider`/`AIRemoveProvider`/`AISwitchProvider`/`AITestProvider`/`AIListSkills`/`AIRunSkill`/`AIExecuteAction`（带 confirm_actions 二次确认） | T008,T015 | Ready | — | — |
| T026 | 改造 `frontend/src/pages/AIAssistantPage.vue`：AI 设置面板（渠道类型选择、内置渠道下拉、base_url、协议下拉、model、推理参数滑块/输入、reasoning_effort 下拉仅能力支持时可调、key 输入密码态、连接测试、激活切换）；AI 面板显示 5 个 skill 入口卡片 + 结构化建议卡片（含执行按钮）；**移除通用聊天框**；provider 切换下拉 | T025 | Ready | — | — |
| T027 | 更新 `README.md`：`xdev ai` 命令总览更新（移除 chat，新增 provider/apikey/default-params/model/run/skills），AI 助手定位说明（xdev 专属窄场景，非通用 agent） | T023 | Ready | — | — |
| T028 | 更新 `build.ps1`/`build.sh`：若密钥库需要平台特定构建标签则补充（如 Linux Secret Service 需 `glib` cgo），确保三平台编译通过 | T007 | Ready | — | — |
| T029 | 双重独立审查（Spec 合规 + 工程质量）：Spec 合规审查覆盖 FR-001..034、SC-001..014、NFR-001..011；工程质量审查覆盖模块边界、接口、错误处理、测试隔离、向后兼容、定位约束（无 chat/无可扩展 skill/无模型分级） | T001-T028 | Ready | — | — |

## 验收清单

> 映射 spec.md 0.7.0 的 FR-001..034 与 SC-001..014，及 Constitution 合规。

### Provider 接入层
- [ ] FR-001: Provider 接口含能力查询方法（SupportsReasoning/Vision/Streaming/ContextWindow/ListModels）
- [ ] FR-002: ProviderConfig 结构含 Type/BuiltinName/Protocol/BaseURL/Model/APIKeyRef/Timeout/Params/Capabilities/ExtraBody
- [ ] FR-003: ProviderRegistry 注册/查找/枚举/切换 active；内置渠道配置表预置 6 个
- [ ] FR-004: Protocol 三值对应三个协议适配器（anthropic/openai-chat/openai-responses）
- [ ] FR-005: 内置渠道 `provider add <name> --apikey` 自动填充默认 base_url/协议/model/能力
- [ ] FR-006: 自定义渠道 `provider add custom <name> --base-url --protocol --model [--apikey] ...` 完整参数
- [ ] FR-007: 三协议适配器独立解析，ChatResult 统一结构屏蔽差异
- [ ] FR-008: 协议与端点不匹配报"与协议 X 不兼容"，不崩溃
- [ ] FR-009: Assistant 持有 activeProvider + fallbackChain，generate 委托 + 失败 fallback
- [ ] FR-010: ModelParams 含 Temperature/TopP/MaxTokens/ReasoningEffort/ContextWindow/Stop
- [ ] FR-011: Capabilities 含 Reasoning/ContextWindow/Vision/Streaming；内置预置，自定义保守默认
- [ ] FR-012: 协议适配器按 ModelParams 注入对应字段（openai-chat/openai-responses/anthropic 三种映射）
- [ ] FR-013: ContextWindow 控制 token 裁剪（粗估 + 优先级裁剪，保留 system prompt + 核心意图）
- [ ] FR-014: ModelCapabilityTable 匹配，未匹配保守默认并提示
- [ ] FR-015: 模型不支持某参数时忽略并警告，GUI 禁用控件
- [ ] FR-016: 双层参数（DefaultParams + Provider.Params + extra_body）优先级链，冲突日志记录
- [ ] FR-017: CLI `model set <provider>` 运行时调整模型与推理参数
- [ ] FR-018: CLI `default-params set/show`
- [ ] FR-019: Keystore 抽象 + 4 平台实现 + fallback AES-256 加密文件
- [ ] FR-020: config.yaml ai: 段重构，API key 不进明文只存 api_key_ref
- [ ] FR-021: CLI provider add/list/remove/switch/test + apikey set/get/remove
- [ ] FR-022: 未配置 provider 时所有 ai 子命令显示引导
- [ ] FR-023: GUI AI 设置面板完整 UI
- [ ] FR-024: 轻量成本计量（ChatResult.TokensUsed + 可选 Pricing，未配只记 token）

### 能力层（收窄到 xdev 专属）
- [ ] FR-025: AIContext 含 ProjectEnv（.xdev.toml 或扫描项目标记文件推断）
- [ ] FR-026: skill 接口完整
- [ ] FR-027: 5 个内置固定 skill 不可外部注册（接口小写，无开放 Registry）
- [ ] FR-028: Assistant.Run 调度（构建 ctx → 取 skill → provider → Parse → 失败走 Fallback）
- [ ] FR-029: Suggestion 结构
- [ ] FR-030: Action 结构 + 二次确认（confirm_actions 默认 true）
- [ ] FR-030a: Recommendation 结构（Status/Tools/Gaps/Upgrades/Command）
- [ ] FR-030b: recommend 两种模式（默认推断 + --check 校验）
- [ ] FR-031: CLI `ai run <skill> [args]`（recommend 支持 --check）+ `ai skills`；无 ai chat
- [ ] FR-032: GUI AI 面板 5 个 skill 入口卡片 + 结构化建议 + 执行按钮，无通用聊天框
- [ ] FR-033: 5 skill 降级策略实现
- [ ] FR-034: 现有 API 保留 deprecated wrapper，Generate 废弃

### Success Criteria
- [ ] SC-001: 配置任一渠道后 `ai run suggest python` 返回结构化建议
- [ ] SC-002: 未配置 provider 时 `xdev ai` 显示引导，不报错
- [ ] SC-003: API Key 默认密钥库，config.yaml 不含明文
- [ ] SC-004: `provider switch` 切换 active，后续走新 provider
- [ ] SC-005: active 不可用按 fallback_chain 降级，最终走规则化 Fallback
- [ ] SC-006: 可配置 model id 与推理参数，按协议注入；不支持时忽略警告
- [ ] SC-007: 双层参数生效，extra_body 优先级最高，冲突日志记录
- [ ] SC-008: Provider 能力查询接口驱动参数注入与 UI 控件
- [ ] SC-009: `ai skills` 列出 5 个内置，无外部 skill
- [ ] SC-010: GUI 5 个 skill 入口卡片 + 结构化建议 + 一键执行，无通用聊天框
- [ ] SC-011: 5 skill 在 provider 不可用时走规则化 Fallback 返回有效结果
- [ ] SC-012: `ai run` 记录 token 用量；配 pricing 时记录成本估算
- [ ] SC-013: 无通用 chat、无可扩展 skill 框架、无模型分级，仅服务 xdev 专属窄场景
- [ ] SC-014: recommend 在无/有 `.xdev.toml` 两种情况都正确工作，无标记文件时明确提示

### Constitution / 工程质量
- [ ] go vet 通过
- [ ] go test ./internal/ai/... 通过（mock provider，不依赖真实 API）
- [ ] go test ./internal/config/... 通过（含旧字段迁移测试）
- [ ] go build 三平台编译通过（Windows/Linux/macOS）
- [ ] NFR-011 定位约束：无 `xdev ai chat`、无 skill 可扩展接口暴露、无模型分级字段
- [ ] NFR-002 向后兼容：GUI 现有 bindings 行为不变
- [ ] Constitution I-VII 全 Compliant（见 plan.md 逐条检查）

## 依赖关系图

```text
T001(Provider接口) ──┬── T002(builtin表) ──┐
                      ├── T003(anthropic) ──┤
                      ├── T004(openai-chat)─┤── T006(provider实现)──┐
                      ├── T005(responses) ──┤                         │
                      └── T007(keystore) ────┼─────────────────────┬──┤
                                            │                     │  │
                      T009(config重构) ──── T001 ── T007            │  │
                                            │                     │  │
                      T008(assistant重构)─ T001,T006,T007 ─────────┴──┤
                                            │                        │
                      T010(双层参数) ──── T008,T002                   │
                      T011(ctx裁剪) ──── T008,T010                   │
                      T012(成本计量) ──── T008,T009                   │
                                            │                        │
                      T013(context.go) ── T009                        │
                      T014(schema.go) ── 无                            │
                      T015(skills.go) ── T008,T013,T014 ──┬───────────┤
                      T016-T020(5 skill) ── T015         │           │
                      T021(fallback.go) ── T016-T020     │           │
                      T022(tests) ── T001-T021           │           │
                                            │            │           │
                      T023(cli重构) ── T008,T009,T015 ──┤           │
                      T024(register) ── T008,T023       │           │
                      T025(gui bindings) ── T008,T015 ──┤           │
                      T026(gui frontend) ── T025 ────────────────────┤
                      T027(README) ── T023                           │
                      T028(build脚本) ── T007                        │
                      T029(双重审查) ── T001-T028 ────────────────────┘
```