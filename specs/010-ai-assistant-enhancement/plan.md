# Implementation Plan: AI 助手功能（xdev 专属窄场景）

> **文档类型**：Implementation Plan
> **状态**：Ready
> **文档版本**：0.1.0
> **创建日期**：2026-07-26
> **最后更新**：2026-07-26
> **所有者**：xinyan <zuijiuhjd@foxmail.com>
> **关联 Spec**：`specs/010-ai-assistant-enhancement/spec.md` `0.7.0` (Approved)
> **权威范围**：定义所选技术方案、模块边界、接口、数据流和验证方案
> **替代文档**：无

**Branch**: `010-ai-assistant-enhancement` | **Date**: 2026-07-26 | **Spec**: `specs/010-ai-assistant-enhancement/spec.md` `0.7.0`

**Input**: Feature specification from `/specs/010-ai-assistant-enhancement/spec.md`

---

## Summary

重构 `internal/ai/`：移除 Ollama 硬编码依赖，建立 **Provider 抽象层**（用户自选内置/自定义两种渠道，兼容 anthropic/openai-chat/openai-responses 三种协议，API Key 安全存储，模型与推理参数配置，能力查询，双层参数 + extra_body 透传，fallback 链，轻量成本计量）；将 AI 助手收窄为 **xdev 专属窄场景**——5 个内置固定 skill（suggest/diagnose/health-check/migrate/recommend），全部利用 xdev 内部状态（已装版本、激活状态、PATH、manifest 候选、`.xdev.toml` 项目环境），结构化建议 + 一键执行，规则化降级，**无通用 chat、无 Skill 可扩展、无模型分级**。保持零外部 AI 依赖（仅用标准库）。

---

## Technical Context

**Language/Version**: Go 1.25.0（与 `go.mod` 一致）
**Primary Dependencies**: 标准库 `net/http`、`encoding/json`、`crypto/aes`、`os/user`、`runtime`、`reflect`；复用现有 `internal/util`、`internal/manifest`、`internal/project`、`internal/config`
**Storage**:
- `config.yaml` 的 `ai:` 段重构（providers/active_provider/fallback_chain/default_params/pricing/confirm_actions/system_prompt），API Key 不进 config.yaml，只存 `api_key_ref`
- 系统密钥库（Win Credential Manager / macOS Keychain / Linux Secret Service），无支持时降级 `$XMDEV_HOME/cache/keystore.dat`（AES-256 加密，机器绑定）
**Testing**: `go test` 单元测试（mock provider client）+ 现有套件回归；不依赖真实 API
**Target Platform**: Windows 10+ 主平台；Linux/macOS 编译检查 + 密钥库按平台选实现
**Project Type**: CLI/desktop tool（Wails GUI）
**Performance Goals**: `AIContext.Build` < 50ms；provider 调用遵循各自超时（默认 60s）；token 估算用粗估（~4 字符/token）不阻塞主流程
**Constraints**: 不引入外部 AI SDK；`skill` 接口小写包外不可注册；无通用 chat；无模型分级；现有 `SuggestVersions`/`DiagnoseIssue` API 保留为 deprecated wrapper 不破坏 GUI
**Scale/Scope**: `internal/ai/` 包级重构 + `internal/config` AIConfig 扩展 + CLI `ai` 子命令重构 + GUI AI 面板改造

---

## 技术方案

### 模块边界与接口

| 模块 | 职责 | 输入接口 | 输出接口 | 依赖模块 |
|---|---|---|---|---|
| `internal/ai/provider.go` | Provider 抽象 + ProviderRegistry + Protocol 枚举 | `Provider` 接口 | `ChatResult`、`Provider` | 标准库 |
| `internal/ai/providers/*.go` | 协议适配器 + 内置渠道配置表 | `ProtocolAdapter` 接口 | `*http.Request`、`ChatResult` | `provider.go`、标准库 |
| `internal/ai/keystore.go` | API Key 安全存储 | `Keystore` 接口 | `(string, error)` | 平台原生库 / AES |
| `internal/ai/context.go` | AIContext 构建（环境 + 项目环境快照） | `*config.Config`、`*plugin.Registry`、`*manifest.Catalog` | `AIContext` | `config`、`manifest`、`project` |
| `internal/ai/skills.go` | 5 个内置固定 skill + `skill` 接口（小写） | `AIContext`、`args` | `any`（Suggestion/Action/Recommendation） | `context.go`、`schema.go` |
| `internal/ai/schema.go` | 结构化输出 schema + JSON 解析容错 | raw `string`、`reflect.Type` | `(any, error)` | 标准库 `encoding/json` |
| `internal/ai/fallback.go` | 规则化降级策略集合 | `AIContext`、`args` | `(any, error)` | `manifest`、`project` |
| `internal/ai/assistant.go` | 调度层（重构，移除 Ollama 硬编码） | `Run(skill, args)` | `(any, error)` | 上述全部 |
| `internal/config/config.go` | AIConfig 重构 | YAML | `AIConfig` | `provider.go` 的 `ProviderConfig` |
| `internal/cli/ai_cmd.go` | `xdev ai` 子命令重构 | CLI flags | 调用 `Assistant` | `assistant.go`、`config` |
| `internal/gui/app_project.go` | GUI AI 面板改造 | Wails bindings | 调用 `Assistant` | `assistant.go` |

### 建议 API（实现时锁定）

```go
// internal/ai/provider.go
type Message struct {
    Role    string `json:"role"`    // system|user|assistant
    Content string `json:"content"`
}

type ProviderConfig struct {
    Type         string         `yaml:"type"`          // builtin|custom
    BuiltinName  string         `yaml:"builtin_name"`
    Protocol     string         `yaml:"protocol"`      // anthropic|openai-chat|openai-responses
    BaseURL      string         `yaml:"base_url"`
    Model        string         `yaml:"model"`
    APIKeyRef    string         `yaml:"api_key_ref"`
    Timeout      string         `yaml:"timeout"`
    Params       ModelParams    `yaml:"params"`
    Capabilities Capabilities   `yaml:"capabilities"`
    ExtraBody    map[string]any `yaml:"extra_body,omitempty"`
}

type ModelParams struct {
    Temperature     float64  `yaml:"temperature,omitempty"`
    TopP            float64  `yaml:"top_p,omitempty"`
    MaxTokens       int      `yaml:"max_tokens,omitempty"`
    ReasoningEffort string   `yaml:"reasoning_effort,omitempty"` // none|low|medium|high
    ContextWindow   int      `yaml:"context_window,omitempty"`
    Stop            []string `yaml:"stop,omitempty"`
}

type Capabilities struct {
    Reasoning     bool `yaml:"reasoning"`
    ContextWindow int  `yaml:"context_window"`
    Vision        bool `yaml:"vision"`
    Streaming     bool `yaml:"streaming"`
}

type Pricing struct {
    InputPer1M  float64 `yaml:"input_per_1m"`
    OutputPer1M float64 `yaml:"output_per_1m"`
}

type ChatResult struct {
    Content      string
    TokensUsed   int
    CostEstimate float64
    FinishReason string
}

type Provider interface {
    Name() string
    Config() ProviderConfig
    Chat(ctx context.Context, messages []Message) (ChatResult, error)
    Ping(ctx context.Context) error
    SupportsReasoning() bool
    SupportsVision() bool
    SupportsStreaming() bool
    ContextWindow() int
    ListModels(ctx context.Context) ([]string, error)
}

type ProviderRegistry struct { /* name → Provider */ }
func (r *ProviderRegistry) Register(p Provider)
func (r *ProviderRegistry) Get(name string) (Provider, bool)
func (r *ProviderRegistry) All() []Provider
func (r *ProviderRegistry) Active() Provider
func (r *ProviderRegistry) SetActive(name string) error

// internal/ai/providers/proto.go
type ProtocolAdapter interface {
    BuildRequest(baseURL, apiKey string, messages []Message, model string, params ModelParams, caps Capabilities, extraBody map[string]any) (*http.Request, error)
    ParseResponse(body []byte) (ChatResult, error)
}
var BuiltinProviders = map[string]ProviderConfig{ /* openai/anthropic/deepseek/qwen/gemini/ollama-cloud */ }
var ModelCapabilityTable = map[string]Capabilities{ /* gpt-4o/o1/claude-sonnet-4-5/... */ }

// internal/ai/keystore.go
type Keystore interface {
    Set(name, value string) error
    Get(name string) (string, error)
    Delete(name string) error
    Has(name string) bool
}
// 实现：credentialManagerKeystore(Win) / keychainKeystore(mac) / secretServiceKeystore(Linux) / encryptedFileKeystore(fallback)

// internal/ai/context.go
type AIContext struct {
    OS, Arch            string
    XMDevDir            string
    InstalledTools      []ToolStatus
    ActiveVersions      map[string]string
    PathHasShimDir      bool
    ShimDir             string
    ManifestCandidates  []ManifestCandidate
    UserIntent          string
    ProjectEnv          *ProjectEnv
}
type ProjectEnv struct {
    HasConfig     bool
    ConfigPath    string
    Tools         map[string]string
    Env           map[string]string
    Tasks         map[string]TaskInfo
    InferredFrom  []string
    Dir           string
}
func BuildAIContext(cfg *config.Config, reg *plugin.Registry, cat *manifest.Catalog, projDir string) AIContext

// internal/ai/skills.go — skill 接口小写，包外不可注册
type skill interface {
    Name() string
    Description() string
    BuildPrompt(ctx AIContext, args []string) string
    OutputSchema() reflect.Type
    Parse(raw string) (any, error)
    NeedsProvider() bool
    Fallback(ctx AIContext, args []string) (any, error)
}
// 内置实现（包内固定初始化，无开放 Registry）：
// suggestSkill, diagnoseSkill, healthCheckSkill, migrateSkill, recommendSkill

type Suggestion struct {
    Version       string  `json:"version"`
    Reason        string  `json:"reason"`
    Compatibility string  `json:"compatibility"`
    Command       string  `json:"command"`
    Confidence    float64 `json:"confidence"`
}
type Action struct {
    Type    string   `json:"type"` // run|set|use|install
    Command string   `json:"command"`
    Tool    string   `json:"tool"`
    Version string   `json:"version"`
    Args    []string `json:"args"`
}
type Recommendation struct {
    Status   string       `json:"status"`   // satisfied|conflict|empty
    Tools    []ToolRec    `json:"tools"`
    Gaps     []GapRec     `json:"gaps"`
    Upgrades []UpgradeRec `json:"upgrades"`
    Command  string       `json:"command"`
}

// internal/ai/assistant.go (重构后)
type Assistant struct {
    providers *ProviderRegistry
    keystore  Keystore
    skills    map[string]skill // 5 个内置固定，包内初始化
    fallback  []string
    cfg       *config.AIConfig
}
func NewAssistant(cfg *config.AIConfig, keystore Keystore) *Assistant
func (a *Assistant) Run(ctx context.Context, skill string, args []string) (any, error)
func (a *Assistant) Ping(ctx context.Context, providerName string) error
// 现有 API 保留为 deprecated wrapper：
func (a *Assistant) SuggestVersions(tool, current string) ([]string, error) // 内部调 Run("suggest",...)
func (a *Assistant) DiagnoseIssue(problem string) (string, error)           // 内部调 Run("diagnose",...)
func (a *Assistant) Generate(prompt string) (string, error)                // 返回 ErrDeprecated

// config 重构
type AIConfig struct {
    Providers      map[string]ProviderConfig `yaml:"providers"`
    ActiveProvider string                   `yaml:"active_provider"`
    FallbackChain  []string                 `yaml:"fallback_chain"`
    DefaultParams  ModelParams              `yaml:"default_params"`
    Pricing        map[string]Pricing        `yaml:"pricing"`
    ConfirmActions bool                     `yaml:"confirm_actions"`
    SystemPrompt   string                   `yaml:"system_prompt"`
}
```

### 数据流

```text
xdev ai run <skill> [args]
  → BuildAIContext(cfg, registry, catalog, cwd)
      → 采集 OS/Arch、已装工具、激活版本、PATH 健康度
      → FindProjectConfig(cwd) → ProjectEnv（.xdev.toml 或扫描项目标记文件）
      → manifest 候选（latest + topN versions per tool）
  → Assistant.Run(skill, args)
      → skills[skill].BuildPrompt(ctx, args)  → prompt string
      → ResolveParams: DefaultParams ← Provider.Params ← extra_body（优先级递增）
      → ResolveProvider: active → fallback_chain → Skill.Fallback
      → provider.Chat(ctx, messages)
          → ProtocolAdapter.BuildRequest(baseURL, apiKey, messages, model, params, caps, extraBody)
              → anthropic: POST /v1/messages, x-api-key header, thinking.budget_tokens 映射
              → openai-chat: POST /v1/chat/completions, Bearer, reasoning_effort 字段
              → openai-responses: POST /v1/responses, Bearer, reasoning.effort 嵌套
          → HTTP request → response
          → ProtocolAdapter.ParseResponse(body) → ChatResult{Content, TokensUsed, CostEstimate}
      → skills[skill].Parse(raw) → 结构化输出（Suggestion/Action/Recommendation）
      → 失败/不可用 → skills[skill].Fallback(ctx, args) → 规则化结果（标 [fallback]）
  → 输出结构化结果（CLI 表格 / GUI 卡片）
  → 用户点击"执行" → confirm_actions 二次确认 → 执行 Action.Command
```

### 失败处理

| 失败模式 | 检测方式 | 恢复策略 | 用户可见影响 |
|---|---|---|---|
| 未配置任何 provider | `ProviderRegistry.Active()` 返回 nil | 显示引导文案，列出示例命令 | AI 命令返回引导，退出码 0 |
| active provider 不可用 | `provider.Chat` 返回 error | 按 fallback_chain 尝试下一 provider | 标注 `[fallback:<name>]` |
| 所有 provider 不可用 | fallback_chain 耗尽 | 走 `skill.Fallback` 规则化降级 | 标注 `[fallback]`，返回基于 xdev 数据的结果 |
| 协议与端点响应不匹配 | `ParseResponse` 解析失败 | 报"与协议 X 不兼容"，触发 fallback | 错误日志 + fallback |
| API Key 无效 | provider 返回 401/403 | 跳过该 provider，记录日志，不再重试本轮 | 标注 key 无效 |
| 模型不支持某参数 | Capabilities 匹配 | 记录警告，忽略该字段不注入 | 警告日志，调用继续 |
| JSON 结构化解析失败 | `skill.Parse` 失败 | JSON 块提取容错 + 字段缺失默认值 | 失败回 `skill.Fallback` |
| 密钥库无原生支持 | 平台检测 | 降级 AES-256 加密文件 | 提示用户，行为透明 |
| ContextWindow 超限 | token 粗估超 window | 按优先级裁剪：最早上下文 > manifest topN > AIContext 详情 | 保留 system prompt + 核心意图 |
| 项目无任何配置/标记文件 | `ProjectEnv` 为空 | recommend skill 返回明确提示 | 引导 `xdev init` 或在项目根运行 |

---

## 兼容性与迁移

### 向后兼容性

- 是否改变已有 CLI 参数行为？否 — `xdev ai suggest/diagnose/status/config` 保留，新增 `ai run/skills/provider/apikey/default-params/model`
- 是否改变已有配置文件格式？是 — `ai:` 段重构（旧 `ollama_url`/`model`/`timeout` 三字段迁移为 `providers.ollama` custom + `active_provider: ollama`）
- 是否废弃已有接口？是 — `Generate` API 废弃返回 `ErrDeprecated`；`SuggestVersions`/`DiagnoseIssue` 保留为 deprecated wrapper
- GUI 现有 `GetAIStatus`/`AISuggest`/`AIDiagnose`/`AIChat` bindings 保留，内部走新 `Assistant.Run`

### 数据迁移

- 是否需要数据迁移？是 — 首次加载时若 `ai:` 段含旧字段（`ollama_url`/`model`/`timeout`），自动迁移为 `providers.ollama: {type: custom, protocol: openai-chat, base_url: <ollama_url>, model: <model>}`，`active_provider: ollama`，写回 config.yaml 并备份原文件
- API Key 不自动迁移（旧版无 key 概念，Ollama 本地无需 key）

---

## 安全设计

- 认证/授权方式：不适用（本地工具）
- 敏感数据保护：API Key 默认系统密钥库，不进 config.yaml 明文；`apikey get` 脱敏显示（`sk-x...xxxx`）；无密钥库降级 AES-256 加密文件（机器绑定密钥）；用户显式 `--store plaintext` 才写明文（给警告）
- 信任边界：AIContext 含环境信息（已装工具、PATH、项目配置）只发给用户配置的 provider 端点；首次配置云端 provider 强制确认信任（NFR-010）；不强制 localhost（用户自负信任责任）
- 输入验证：provider URL 协议校验（http/https）；协议格式枚举校验；model id 非空校验；`extra_body` JSON 合法性校验
- 动作执行安全：所有 Action 执行前二次确认（`confirm_actions` 默认 true）；破坏性动作（uninstall）强制确认

---

## 测试隔离

- **单元测试范围**：
  - `Provider` 各实现（mock HTTP server，不依赖真实 API）
  - `ProtocolAdapter` 三个协议适配器（httptest）
  - `Keystore` 各平台实现（mock）
  - `AIContext.Build`（mock config/registry/catalog）
  - 5 个 skill 的 `BuildPrompt`/`Parse`/`Fallback`
  - `schema.go` JSON 解析容错
- **集成测试范围**：`Assistant.Run` 端到端（mock provider），含 fallback 链、降级路径
- **平台测试矩阵**：

| 平台 | 测试方式 | 覆盖范围 |
|---|---|---|
| Windows 10+ | 自动 | 全路径：Provider + 协议 + 密钥库(Credential Manager) + 5 skill + config 迁移 |
| Linux (amd64) | 编译检查 + 单元测试 | 密钥库(Secret Service mock) + 协议适配器 + skill |
| macOS 12+ | 编译检查 + 核心单元 | 密钥库(Keychain mock) + config 解析 |

---

## Constitution 逐条检查

| Constitution 原则（1.0.0） | 检查结果 | 说明 | 引用 |
|---|---|---|---|
| I 安全与信任边界 | Compliant | API Key 密钥库存储；AIContext 发送前确认信任；动作二次确认 | NFR-003/NFR-010/FR-030 |
| II 测试必须隔离宿主状态 | Compliant | mock provider client，不调真实 API；密钥库 mock | NFR-008 |
| III Constitution 与 Spec 权威 | Compliant | Spec 0.7.0 Approved 为任务权威 | spec.md |
| IV 双重独立审查 | Compliant | 实施时执行 Spec 合规 + 工程质量双审查 | 生命周期 |
| V Windows 主平台与跨平台分层 | Compliant | Windows 全量；其他编译+单元 | 测试矩阵 |
| VI 向后兼容 | Compliant | 旧 API deprecated wrapper；config 旧字段自动迁移 | NFR-002/数据迁移 |
| VII 可复现构建 | Compliant | 无新不可复现依赖，仅标准库 | NFR-005 |

### 检查结果汇总

- Compliant：7 项
- Not Applicable：0 项
- Exception Required：0 项
- ADR Required：0 项
- Non-compliant：0 项

---

## ADR 与安全例外判断

### 需要创建的 ADR

无。

### 需要的安全例外

无。

---

## 验证方案

- **Spec 合规验证**：FR-001..034、SC-001..014 均映射到实现任务与测试
- **Constitution 合规验证**：无 Non-compliant
- **兼容性验证**：现有 `go test ./internal/ai/...` 不因改动失败；GUI 现有 bindings 行为不变
- **跨平台验证**：Windows 完整；Linux/macOS 至少编译通过 + 密钥库单元测试
- **定位验证**：确认无 `xdev ai chat`、无 Skill 可扩展接口暴露、无模型分级字段（NFR-011/SC-013）

---

## 项目结构

### 文档 (此功能专属)

```text
specs/010-ai-assistant-enhancement/
├── spec.md             # Feature Spec 0.7.0 (Approved)
├── plan.md             # 本文件 (Ready)
├── tasks.md            # 任务清单
└── reviews/            # 实施阶段审查记录
    ├── T001-spec-review.md
    └── T001-quality-review.md
```

### 源代码 (实现阶段)

```text
internal/ai/
├── provider.go          # Provider 接口 + ProviderRegistry + Protocol 枚举（新）
├── context.go          # AIContext + ProjectEnv 构建（新）
├── skills.go           # skill 接口(小写) + 5 个内置固定 skill（新）
├── schema.go           # 结构化输出 schema + JSON 解析容错（新）
├── fallback.go         # 规则化降级策略集合（新）
├── keystore.go         # Keystore 接口（新）
├── assistant.go        # 重构：移除 Ollama 硬编码，持有 activeProvider + fallbackChain
└── providers/
    ├── builtin.go              # 内置渠道配置表 + ModelCapabilityTable（新）
    ├── anthropic_proto.go       # Anthropic Messages API 适配（新）
    ├── openai_chat_proto.go     # OpenAI Chat Completions 适配（新）
    └── openai_responses_proto.go # OpenAI Responses API 适配（新）
internal/config/config.go  # AIConfig 重构 + 旧字段迁移
internal/cli/ai_cmd.go     # ai 子命令重构 + 新增 provider/apikey/default-params/model/run/skills
internal/gui/app_project.go # GUI AI 面板改造（provider 配置 UI + 5 skill 入口卡片）
frontend/src/pages/AIAssistantPage.vue # 前端 AI 面板改造
```

---

## Complexity Tracking

> 无 Constitution 违规需辩护。

---

## 实施任务草图

详见 `tasks.md`。

---

## 批准记录 (Approval History)

| Stage | Status | Approver | Date | Version | Scope |
|---|---|---|---|---|---|
| Plan 起草 | Ready | `xinyan <zuijiuhjd@foxmail.com>` | 2026-07-26 | `0.1.0` | `2026-07-26-plan-010-001` 实施计划已就绪 |

---

## 变更历史

| 版本 | 日期 | 变更类型 | 说明 | 批准者 |
|---|---|---|---|---|
| 0.1.0 | 2026-07-26 | Initial Draft | 依据 Approved spec.md 0.7.0 生成正式 plan.md | — |