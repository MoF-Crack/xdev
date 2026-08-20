# Feature Specification: AI 助手功能（xdev 专属窄场景）

> **文档类型**：Feature Spec
> **状态**：Approved
> **文档版本**：0.7.0
> **创建日期**：2026-07-26
> **最后更新**：2026-07-26
> **所有者**：xinyan <zuijiuhjd@foxmail.com>
> **权威范围**：定义 xdev AI 助手能力（Provider 接入层 + xdev 专属窄场景 skill）的目标、范围、需求和验收标准
> **替代文档**：无

**Feature Branch**: `010-ai-assistant-enhancement`
**Created**: 2026-07-26
**Status**: Approved
**Input**: 用户需求"设计 AI 助手功能"+ 修订一"模型由用户决定，提供内置/自定义两种渠道接入"+ 修订二"本地整合进自定义渠道，兼容 Anthropic/OpenAI-Chat/OpenAI-Responses 三种协议"+ 修订三"需配置 model id 及推理/上下文窗口/思考强度等参数"+ 修订四"参考开源 agent 模型配置实现完善"+ 修订五"AI 助手应收窄到 xdev 专属窄场景，不内置通用 agent"+ 修订六"加入根据用户当前项目整体环境进行推荐的 skill"

---

## Why

当前 AI 助手存在两层问题：

### 一、接入层问题（核心）

`internal/ai/assistant.go` 硬编码 Ollama 单一后端：
- 固定 `ollamaURL = "http://localhost:11434"`（`assistant.go:39`），`isLocalOllamaURL` 严格限制只能 localhost（`assistant.go:83`），任何远程 URL 被拒
- `generate` 调 Ollama 专有协议 `/api/generate`（`assistant.go:149`），`IsAvailable` 只 ping `/api/tags`（`assistant.go:115`）

问题：**强制本地大模型 = 完全不顾用户成本**。本地 7B 模型需 8GB+ 显存或牺牲 CPU，多数开发机不具备；用户已有云端 API key（DeepSeek、OpenAI 等）却无法接入，被迫装 Ollama。无 Provider 抽象，换供应商要改 `assistant.go`。无 API Key 管理。降级被绑死 Ollama。

### 二、定位问题（本 spec 关键约束）

xdev 是**开发环境版本管理器**，不是 agent 平台。AI 助手的价值锚点必须是：**"通用 AI 看不到的 xdev 内部状态"**——已装版本、激活状态、PATH 配置、manifest 候选、shim 健康度。离开这些上下文，AI 助手就是套壳的通用聊天，无存在必要。

当前 `SuggestVersions`/`DiagnoseIssue`/`Generate` 三个能力中：
- `SuggestVersions`/`DiagnoseIssue` 有 xdev 专属价值（带环境上下文）
- `Generate` 是通用单轮问答，无 xdev 专属价值（通用 ChatGPT 即可）

本 spec 明确边界：**AI 助手 = 带环境上下文的窄场景助手**，而非可扩展的通用 agent 平台。

不关闭此缺口：AI 助手要么被迫装 Ollama，要么沦为通用聊天壳，无法成为 xdev 差异化卖点。

## 目标

### A. Provider 接入层（核心，用户明确要求）

1. **Provider 抽象**：新增 `Provider` 接口，`Assistant` 不再绑死任何后端，只持有当前激活 Provider
2. **两种接入渠道，用户自选**：
   - **内置渠道（Builtin）**：xdev 预置常见云端 provider 配置（OpenAI、Anthropic、DeepSeek、通义千问、Google Gemini、Ollama Cloud 等），内置默认 `base_url`，用户只需填 API Key
   - **自定义渠道（Custom）**：用户自行配置 `base_url` + `api_key` + `model` + **协议格式**（Anthropic / OpenAI Chat Completions / OpenAI Responses 三选一），兼容自建网关、本地服务（Ollama 本地、llama.cpp、LM Studio）、任何符合三种协议的端点
   - 说明：本地 Ollama 通过自定义渠道配置（base_url 填 localhost，选 openai-chat 协议）；Ollama Cloud 通过内置渠道
3. **API Key 安全存储**：默认系统密钥库（Win Credential Manager / macOS Keychain / Linux Secret Service），无支持时降级 AES-256 加密文件；`config.yaml` 不存明文，只存引用名
4. **多 Provider 共存 + 切换**：用户可配置多个 provider，`xdev ai config provider switch <name>` 或 GUI 下拉切换 active
5. **fallback 链**：active provider 不可用时按配置尝试 fallback provider 链，最终走规则化降级，不静默失活
6. **模型与推理参数配置**：每个 provider 配置携带 model id 及推理参数（temperature、top_p、max_tokens、reasoning_effort、context_window 等）；内置渠道预置每模型默认值与能力声明，自定义渠道由用户填写
7. **能力查询接口**：`Provider` 接口提供能力查询方法（`SupportsReasoning()`、`SupportsVision()`、`ContextWindow()` 等），运行时按能力决定参数注入
8. **双层参数 + extra_body 透传**：全局默认参数 + 模型级覆盖 + `extra_body` 透传协议专属参数（优先级最高）
9. **轻量成本计量**：不维护全局定价表，`ChatResult` 含 token 用量，按用户可选配置的 `pricing` 估算成本；保持零外部 AI 依赖（`internal/ai/` 仅用标准库）

### B. 能力层（收窄到 xdev 专属窄场景）

10. **固定内置 skill，不可扩展**：仅 5 个 skill（suggest、diagnose、health-check、migrate、**recommend**），全部利用 xdev 内部状态；**砍掉** Skill 可扩展机制（无 Go/配置/Lua 三源注册、无 SkillRegistry 开放注册）
11. **单轮调用，无通用 chat**：每个 skill 独立单轮调用，采集 AIContext → 调 provider → 解析结构化输出；**砍掉** `xdev ai chat` 通用多轮会话
12. **AIContext 采集**：一次构建 OS/Arch、已装工具、激活版本、PATH 健康度、manifest 候选、**当前项目环境（.xdev.toml 的 tools/env/tasks，若存在）**，5 个 skill 共享
13. **结构化输出 + 一键执行**：skill 声明输出 schema，解析为 Go 结构体；建议携带可执行命令，GUI/CLI 一键执行（带二次确认）——这是 xdev 专属闭环（建议命令→执行命令）
14. **优雅降级**：每 skill 声明 `Fallback`，所有 provider 不可用时返回规则化建议（基于 xdev 内部数据，不依赖 AI）

## 范围

### 包含

#### Provider 接入层
- 新增 `internal/ai/provider.go`：`Provider` 接口 + `ProviderRegistry` + `Protocol` 枚举（anthropic / openai-chat / openai-responses）
- 新增 `internal/ai/providers/`：协议适配器
  - `builtin.go`：内置渠道配置表（OpenAI、Anthropic、DeepSeek、Qwen、Gemini、Ollama Cloud 等，各含默认 base_url + 协议 + 默认 model + 能力声明）
  - `anthropic_proto.go`：Anthropic Messages API 协议适配（`POST /v1/messages`，`x-api-key` header）
  - `openai_chat_proto.go`：OpenAI Chat Completions 协议适配（`POST /v1/chat/completions`，`Bearer`）—— 兼容 OneAPI、vLLM、OpenRouter、Ollama 本地、DeepSeek、Qwen 等
  - `openai_responses_proto.go`：OpenAI Responses API 协议适配（`POST /v1/responses`，`Bearer`）
- 新增 `internal/ai/keystore.go`：跨平台密钥库抽象
- 重构 `internal/ai/assistant.go`：移除 Ollama 硬编码，持有 `activeProvider` + `fallbackChain`，方法委托给 Provider
- 重构 `internal/config/config.go`：`AIConfig` 扩展为 `AIConfig{ Providers map[string]ProviderConfig, ActiveProvider string, FallbackChain []string, DefaultParams, Pricing, ... }`
- CLI 重构：`xdev ai config provider add/list/remove/switch/test`、`apikey set/get/remove`、`default-params set/show`、`model set`
- GUI：AI 设置面板增加 provider 配置 UI（渠道选择、base_url、协议格式、model、推理参数、key 输入、连接测试、激活切换）

#### 能力层（收窄）
- 新增 `internal/ai/context.go`：`AIContext` 构建器
- 新增 `internal/ai/skills.go`：`skill` 接口 + 5 个内置固定 skill（**不可外部注册**）
- 新增 `internal/ai/schema.go`：结构化输出 schema + JSON 解析容错
- 新增 `internal/ai/fallback.go`：规则化降级策略集合
- 重构现有 3 个能力：`SuggestVersions`→`suggest` skill、`DiagnoseIssue`→`diagnose` skill、`Generate`→废弃（无 xdev 专属价值）；新增 `health-check`、`migrate`、`recommend` skill
- CLI 扩展：`xdev ai run <skill> [args]`（单次执行某 skill）、`xdev ai skills`（列出 5 个内置）
- GUI 扩展：AI 面板显示 4 个 skill 入口、结构化建议卡片（执行按钮）

### 非目标

- **`xdev ai chat` 通用多轮对话**（无 xdev 专属价值，通用 AI 客户端即可）
- **Skill 可扩展机制**（Go/配置/Lua 三源注册、SkillRegistry 开放注册）——4 个内置固定 skill 不可扩展，避免沦为 agent 平台
- **模型分级 main/reasoning**（xdev AI 调用频率低，单 active provider 足够）
- 强制要求用户必须配置某 provider（默认无配置时显示引导，不强制本地）
- 实现 OpenAI function calling 标准
- 流式输出（SSE）首版不做，预留接口
- Gemini 原生协议（首版通过 openai-chat 协议接入其 OpenAI 兼容端点）
- 训练/微调模型
- 修改 manifest/plugin 接口
- 修改下载器/SHA-256 校验逻辑

---

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 用户配置 DeepSeek 内置渠道 (Priority: P0)

用户已有 DeepSeek API key，无需装本地模型，直接配置即用。

**Acceptance Scenarios**:
1. **Given** 用户有 DeepSeek API key，**When** 执行 `xdev ai config provider add deepseek --apikey sk-xxx`，
    **Then** 内置渠道：base_url 自动取 DeepSeek 默认值（`https://api.deepseek.com/v1`），协议自动推断为 openai-chat，key 写入系统密钥库，provider 注册为 `deepseek` 并设为 active
2. **Given** 同上，**When** `xdev ai status`，**Then** 显示 `Active provider: deepseek, Model: deepseek-chat, Protocol: openai-chat, Endpoint: https://api.deepseek.com/v1`
3. **Given** 同上，**When** `xdev ai config provider test deepseek`，**Then** 发最小 ping 请求，返回 OK/失败 + 延迟 + token 用量与成本估算

### User Story 2 - 用户配置自定义渠道（三种协议） (Priority: P0)

用户自建网关或本地服务，通过自定义渠道接入，需声明协议格式。

**Acceptance Scenarios**:
1. **Given** 用户自建 OneAPI 网关，**When** `xdev ai config provider add custom mygateway --base-url https://my-gateway.example.com/v1 --apikey sk-xxx --model gpt-4o --protocol openai-chat`，
    **Then** 注册为 `mygateway` custom provider，按 openai-chat 协议调用
2. **Given** 用户本地跑 Ollama，**When** `xdev ai config provider add custom local-ollama --base-url http://localhost:11434/v1 --model llama3 --protocol openai-chat`，
    **Then** 注册为本地 provider（无需 apikey），通过 Ollama 的 `/v1/chat/completions` 调用
3. **Given** 用户接入 Anthropic 官方但走自定义反代，**When** `xdev ai config provider add custom claude-proxy --base-url https://my-proxy.com --apikey sk-ant-xxx --model claude-sonnet-4-5 --protocol anthropic`，
    **Then** 按 Anthropic Messages API 协议调用
4. **Given** 用户接入 OpenAI Responses API，**When** `xdev ai config provider add custom oai-resp --base-url https://api.openai.com/v1 --apikey sk-xxx --model gpt-4o --protocol openai-responses`，
    **Then** 按 Responses API 协议调用
5. **Given** custom 端点实际协议与配置不符，**When** 调用 AI，**Then** 报错"provider 响应与协议 openai-chat 不兼容"，不崩溃，触发 fallback 或降级

### User Story 3 - 未配置 provider 时的引导 (Priority: P1)

用户无任何配置，首次使用被引导。

**Acceptance Scenarios**:
1. **Given** 用户未配置任何 provider，**When** 首次执行任何 `xdev ai` 子命令，**Then** 显示引导："请配置 AI 渠道：内置（`provider add openai|anthropic|deepseek|qwen|gemini|ollama-cloud --apikey <key>`）或自定义（`provider add custom <name> --base-url <url> --protocol <anthropic|openai-chat|openai-responses> --model <m>`）"
2. **Given** 同上，**When** GUI 打开 AI 面板，**Then** 显示配置引导卡片，含"添加内置渠道"与"添加自定义渠道"两个入口

### User Story 4 - 用户配置模型与推理参数 (Priority: P0)

用户能配置 model id 与推理参数，适配不同任务需求。

**Acceptance Scenarios**:
1. **Given** 用户添加 OpenAI 内置渠道，**When** `xdev ai config provider add openai --apikey sk-xxx --model gpt-4o --reasoning-effort high --max-tokens 4096`，
    **Then** ProviderConfig 记录 model=gpt-4o、reasoning_effort=high、max_tokens=4096，后续调用按此参数发送
2. **Given** 用户用推理模型（o1/o3、DeepSeek-R1、Claude thinking），**When** 配置 `--reasoning-effort high`，
    **Then** 调用时按协议注入对应字段（OpenAI `reasoning.effort`、Anthropic `thinking.budget_tokens`）
3. **Given** 用户跑长上下文任务，**When** `xdev ai config model set openai --context-window 128000`，
    **Then** AIContext 构建时按此窗口裁剪上下文（超长时截断）
4. **Given** 用户用不支持推理的模型（gpt-4o-mini），**When** `--reasoning-effort high`，
    **Then** 能力声明标 `reasoning: false`，记录警告"该模型不支持推理，reasoning_effort 将被忽略"，不注入该字段
5. **Given** GUI AI 设置面板，**When** 用户编辑 provider，**Then** 显示 model id、推理参数滑块/输入、reasoning_effort 下拉（仅能力支持时可调）、能力显示

### User Story 5 - 双层参数与 extra_body 透传 (Priority: P2)

借鉴 Continue 的全局默认+模型级覆盖双层配置与 `extraBodyProperties` 透传。

**Acceptance Scenarios**:
1. **Given** `ai.default_params.temperature=0.3`，provider deepseek 未配 params，**When** 调用，**Then** 用全局默认 temperature=0.3
2. **Given** 同上，provider deepseek 配 `params.temperature=0.7`，**When** 调用，**Then** 模型级 temperature=0.7 覆盖全局
3. **Given** 用户需传协议专属参数（如 OpenAI `logprobs`），**When** `provider add custom x --extra-body '{"logprobs":true}'`，
    **Then** 该参数透传到请求 body，不经 ModelParams 字段映射
4. **Given** `extra_body` 字段与 ModelParams 冲突，**When** 调用，**Then** `extra_body` 优先级最高，日志记录覆盖链

### User Story 6 - AI 版本建议带 xdev 上下文与可执行命令 (Priority: P1)

用户问版本建议，AI 知道当前装的版本、manifest 候选，返回结构化建议可一键执行——这是 xdev 专属闭环。

**Acceptance Scenarios**:
1. **Given** 用户已装 python@3.11.10，manifest latest=3.12.7，active provider=deepseek，**When** `xdev ai run suggest python`，
    **Then** 返回 `Suggestion{Version:3.12.7, Reason:"你当前 3.11.10，3.12 是最新稳定版...", Command:"xdev install python@3.12.7"}`，reason 含 xdev 上下文
2. **Given** 同上，**When** GUI 点击"执行"按钮，**Then** 二次确认后执行 `xdev install python@3.12.7`
3. **Given** 所有 provider 不可用，**When** `xdev ai run suggest python`，**Then** 走规则化降级（manifest latest + 命令），标 `[fallback]`，退出码 0

### User Story 7 - 环境诊断与一键修复 (Priority: P1)

用户遇到"python 命令找不到"，AI 基于 xdev 内部状态诊断并给修复命令。

**Acceptance Scenarios**:
1. **Given** 用户 shim 目录不在 PATH，**When** `xdev ai run diagnose "python command not found"`，
    **Then** AI 诊断："shim 目录 E:\XDev\shims 不在 PATH"，返回 `Action{Type:run, Command:"xdev config set path.inject_to_windows_path true"}`，可一键执行
2. **Given** 同上，**When** GUI 点击"修复"按钮，**Then** 二次确认后执行修复命令
3. **Given** 所有 provider 不可用，**When** `xdev ai run diagnose "..."`，**Then** 走规则化降级（规则化 PATH/init 检查清单），标 `[fallback]`

### User Story 8 - 健康检查 (Priority: P2)

`health-check` skill 基于 xdev 内部状态做精简 doctor。

**Acceptance Scenarios**:
1. **Given** 用户执行 `xdev ai run health-check`，**When** AI 分析 AIContext（已装工具、激活状态、PATH、shim 完整性），
    **Then** 返回结构化健康报告，列出问题项与建议命令
2. **Given** 所有 provider 不可用，**When** `xdev ai run health-check`，**Then** 走纯规则检查（等同精简 `xdev doctor`），标 `[fallback]`

### User Story 9 - 本地工具迁移建议 (Priority: P2)

`migrate` skill 发现本地非 xdev 管理的工具，建议迁移命令。

**Acceptance Scenarios**:
1. **Given** 用户系统有 `C:\Python311\python.exe` 非 xdev 管理，**When** `xdev ai run migrate`，
    **Then** AI 返回 `Suggestion{Tool:python, Path:C:\Python311, Command:"xdev migrate python"}`，可一键执行
2. **Given** 所有 provider 不可用，**When** `xdev ai run migrate`，**Then** 走规则化降级（扫描 PATH + 常见路径匹配 shims 名），标 `[fallback]`

### User Story 9 - 项目环境推荐 (Priority: P1)

`recommend` skill 根据当前项目环境（`.xdev.toml` 或 `.tool-versions`）推荐工具版本组合，解决"新项目该装哪些工具什么版本"或"项目工具版本是否合理/能否升级"问题——这是 xdev 专属能力（通用 AI 看不到 `.xdev.toml` 内容与已装版本对比）。

**Acceptance Scenarios**:
1. **Given** 当前项目无 `.xdev.toml`，但扫描到 `package.json`（`engines.node=20`）与 `go.mod`（`go 1.22`），**When** `xdev ai run recommend`，
    **Then** AI 推荐工具组合：`node@20, go@1.22`，返回 `Recommendation{Tools:[{Name:node, Version:20, Reason:"package.json engines 声明"},{Name:go, Version:1.22, Reason:"go.mod 声明"}], Command:"xdev install --from-local"}`，建议生成 `.xdev.toml`
2. **Given** 当前项目有 `.xdev.toml`（`node=20, python=^3.11`），用户已装 node@20.18.0、python@3.11.10，**When** `xdev ai run recommend --check`，
    **Then** AI 对比项目声明与已装版本，返回 `Recommendation{Status:"satisfied", Gaps:[], Upgrades:[{python, 3.11→3.12, "manifest latest"}]}`，给出升级建议命令
3. **Given** 项目 `.xdev.toml` 声明 `python=^3.11` 但已装 python@3.10.15（不满足约束），**When** `xdev ai run recommend --check`，
    **Then** AI 识别约束冲突，返回 `Recommendation{Status:"conflict", Gaps:[{python, required:^3.11, installed:3.10.15, Command:"xdev install python@3.12"}]}`，一键修复
4. **Given** 当前目录无任何项目配置文件且无可推断的项目标记文件，**When** `xdev ai run recommend`，
    **Then** 返回提示"未发现项目配置（.xdev.toml/.tool-versions）或可推断的项目标记文件（package.json/go.mod/requirements.txt 等），请先 `xdev init` 或在项目根目录运行"
5. **Given** 所有 provider 不可用，**When** `xdev ai run recommend`，**Then** 走规则化降级：解析 `.xdev.toml`/`.tool-versions`/项目标记文件，按声明版本与 manifest latest 比对，输出 gap 列表 + 命令，标 `[fallback]`
6. **Given** GUI 在项目目录打开 AI 面板，**When** 点击"recommend"入口，**Then** 显示项目环境推荐卡片：推荐工具组合、约束冲突、升级建议，每项含执行按钮

### User Story 10 - Provider 不可用自动 fallback (Priority: P1)

激活 provider 不可用时按链降级。

**Acceptance Scenarios**:
1. **Given** active=deepseek，fallback_chain=[local-ollama]，deepseek 超时，**When** 调用 skill，
    **Then** 自动尝试 local-ollama，成功则用其返回（标注 `[fallback:local-ollama]`），失败走规则化降级
2. **Given** 无 fallback 配置，active 不可用，**When** 调用，**Then** 走 skill 自身 Fallback
3. **Given** fallback 中某 provider 报"key 无效"，**When** 继续 fallback，**Then** 跳过该 provider 并记录，不再重试本轮

### User Story 11 - API Key 安全存储 (Priority: P0)

API key 不泄漏到 config.yaml 明文（除非用户显式选择）。

**Acceptance Scenarios**:
1. **Given** 默认配置，**When** `xdev ai config apikey set deepseek sk-xxx`，**Then** key 写入系统密钥库，config.yaml 只存 `api_key_ref: "deepseek"`，不存明文
2. **Given** 系统无密钥库支持，**When** 设置 key，**Then** 降级到 config.yaml AES-256 加密存储（机器绑定密钥），并提示
3. **Given** 用户显式 `--store plaintext`，**When** 设置 key，**Then** 写 config.yaml 明文（仅本地开发用，给警告）
4. **Given** `xdev ai config apikey get deepseek`，**When** 执行，**Then** 脱敏显示（`sk-x...xxxx`），不全文回显

---

## Requirements *(mandatory)*

### 功能需求 (Functional Requirements)

#### Provider 接入层

- **FR-001**：新增 `Provider` 接口：`Name()`、`Chat(ctx, messages) (ChatResult, error)`、`Ping(ctx) error`、`Config() ProviderConfig`、能力查询方法（`SupportsReasoning()`/`SupportsVision()`/`SupportsStreaming()`/`ContextWindow()`/`ListModels(ctx)`）
- **FR-002**：`ProviderConfig` 结构：`Type`（builtin|custom）、`BuiltinName`、`Protocol`（anthropic|openai-chat|openai-responses）、`BaseURL`、`Model`、`APIKeyRef`、`Timeout`、`Params`（模型级推理参数）、`Capabilities`、`ExtraBody`（协议专属参数透传）
- **FR-003**：`ProviderRegistry` 支持注册/查找/枚举/切换 active；内置渠道配置表预置 openai、anthropic、deepseek、qwen、gemini、ollama-cloud（各含默认 base_url + 协议 + 默认 model + 能力声明）
- **FR-004**：`Protocol` 枚举三值，对应三个协议适配器：
  - `anthropic` → Anthropic Messages API（`POST /v1/messages`，`x-api-key` + `anthropic-version` header）
  - `openai-chat` → OpenAI Chat Completions（`POST /v1/chat/completions`，`Bearer`）—— 兼容 OneAPI、vLLM、OpenRouter、Ollama 本地、DeepSeek、Qwen 等
  - `openai-responses` → OpenAI Responses API（`POST /v1/responses`，`Bearer`）
- **FR-005**：内置渠道：`provider add <builtin-name> --apikey <key> [--model <m>] [--temperature <f>] [--top-p <f>] [--max-tokens <n>] [--reasoning-effort <e>] [--context-window <n>]` 自动填充默认 base_url、协议、默认 model 及能力声明
- **FR-006**：自定义渠道：`provider add custom <name> --base-url <url> --protocol <anthropic|openai-chat|openai-responses> --model <m> [--apikey <key>] [--timeout <dur>] [--max-tokens <n>] [--temperature <f>] [--top-p <f>] [--reasoning-effort none|low|medium|high] [--context-window <n>] [--reasoning] [--vision] [--extra-body '<json>']`；protocol、base_url、model 必填；apikey 可选
- **FR-007**：三种协议适配器独立解析响应；`ChatResult` 统一结构（`Content`、`TokensUsed`、`CostEstimate`、`FinishReason`）屏蔽协议差异
- **FR-008**：协议与端点实际响应不匹配时报"provider 响应与协议 <X> 不兼容"，不崩溃；调用方据此 fallback 或降级
- **FR-009**：`Assistant` 重构：持有 `activeProvider` + `fallbackChain`，`generate` 委托 active provider，失败按链 fallback
- **FR-010**：`ModelParams` 结构含 `Temperature`、`TopP`、`MaxTokens`、`ReasoningEffort`（none|low|medium|high）、`ContextWindow`、`Stop`；所有字段可选，未设用协议默认
- **FR-011**：`Capabilities` 结构含 `Reasoning`、`ContextWindow`、`Vision`、`Streaming`；内置渠道按已知模型预置，自定义渠道由用户填或保守默认（reasoning=false, context_window=8192）
- **FR-012**：协议适配器按 `ModelParams` 注入协议对应字段：
  - openai-chat：`temperature`/`top_p`/`max_tokens`/`stop`；reasoning_effort 注入 `reasoning_effort`（仅 model 支持）
  - openai-responses：同上 + `reasoning.effort`（嵌套）
  - anthropic：`temperature`/`top_p`/`max_tokens`；reasoning_effort 映射 `thinking.type=budgeted` + `thinking.budget_tokens`（按等级换算）
- **FR-013**：`ContextWindow` 控制 AIContext 与上下文的 token 裁剪：构建 prompt 前粗估 token（~4 字符/token），超出时按优先级裁剪：最早上下文 > manifest 候选 topN > AIContext 详情，保留 system prompt 与核心用户意图
- **FR-014**：内置模型能力表 `ModelCapabilityTable`（gpt-4o、o1/o3、claude-sonnet-4-5、deepseek-reasoner 等）；用户改 `--model` 时自动匹配，未匹配用保守默认并提示
- **FR-015**：模型不支持某参数时（如 gpt-4o-mini 配 reasoning_effort）记录警告并忽略该字段，不报错；GUI 同步禁用控件
- **FR-016**：双层参数配置：`AIConfig.DefaultParams`（全局默认）+ `ProviderConfig.Params`（模型级覆盖）；调用时合并：模型级非零字段覆盖全局；`ExtraBody`（协议专属参数）优先级最高；冲突时日志记录覆盖链
- **FR-017**：CLI `xdev ai config model set <provider> --model <id> [--temperature <f>] [--reasoning-effort <e>] [--context-window <n>]`：运行时调整激活或指定 provider 的模型与推理参数
- **FR-018**：CLI `xdev ai config default-params set [--temperature <f>] [--top-p <f>] [--max-tokens <n>] [--reasoning-effort <e>] [--context-window <n>]`、`default-params show`
- **FR-019**：`Keystore` 抽象：`Set`/`Get`/`Delete`/`Has`；Windows Credential Manager、macOS Keychain、Linux Secret Service；无原生支持降级 AES-256 加密文件（机器绑定，存 `$XMDEV_HOME/cache/keystore.dat`）
- **FR-020**：`config.yaml` 的 `ai:` 段重构为：`providers: map[name]ProviderConfig`、`active_provider`、`fallback_chain`、`default_params`、`pricing`（可选）、`confirm_actions`、`system_prompt`；API key 不进 config.yaml，只存 `api_key_ref` 引用名
- **FR-021**：CLI `xdev ai config provider add/list/remove/switch/test`、`apikey set/get/remove [--store plaintext|keystore]`
- **FR-022**：未配置任何 provider 时，所有 `xdev ai` 子命令显示引导，列出示例命令
- **FR-023**：GUI AI 设置面板：渠道类型选择、内置渠道下拉、base_url 输入、协议下拉、model 输入、推理参数滑块/输入、key 输入框（密码态）、连接测试、激活切换
- **FR-024**：成本计量轻量化：不维护全局定价表；`ChatResult.TokensUsed` 由 provider 返回；`AIConfig.Pricing map[string]Pricing`（用户可选配每模型 input/output 单价）；`ai run` 完成后按 `TokensUsed × Pricing` 估算并日志记录；未配 pricing 时只记 token

#### 能力层（收窄到 xdev 专属）

- **FR-025**：`AIContext` 含 OS/Arch、XMDevDir、已装工具（name→version→active）、激活版本、PATH 含 shimDir 与否、shimDir、manifest 候选（name→latest+topN）、用户意图（来自 run args）、**当前项目环境（`.xdev.toml` 的 tools/env/tasks，若存在；若无则扫描 `.tool-versions` 及项目标记文件 package.json/go.mod/requirements.txt/Cargo.toml/pyproject.toml 等推断）**；一次构建，5 个 skill 共享
- **FR-026**：`skill` 接口：`Name()`、`Description()`、`BuildPrompt(ctx, args)`、`OutputSchema()`、`Parse(raw)`、`NeedsProvider()`、`Fallback(ctx, args)`
- **FR-027**：**5 个内置固定 skill，不可外部注册**：`suggest`（版本建议）、`diagnose`（环境诊断）、`health-check`（精简 doctor）、`migrate`（本地工具迁移）、`recommend`（项目环境推荐）；**无 SkillRegistry 开放注册、无 Lua/配置扩展源**
- **FR-028**：`Assistant.Run(skill, args)` 调度：构建 AIContext → 取 skill → 若 NeedsProvider 则调 active provider（失败 fallback） → Parse → 失败/不可用走 skill.Fallback
- **FR-029**：`Suggestion` 结构：`Version`/`Reason`/`Compatibility`/`Command`/`Confidence`（suggest/migrate 用）
- **FR-030**：`Action` 结构：`Type`（run/set/use/install）、`Command`/`Tool`/`Version`/`Args`，执行前二次确认（`confirm_actions` 默认 true）
- **FR-030a**：`Recommendation` 结构（recommend skill 用）：`Status`（satisfied|conflict|empty）、`Tools []ToolRec`、`Gaps []GapRec`、`Upgrades []UpgradeRec`、`Command`（一键执行命令）；`ToolRec{Name, Version, Reason}`；`GapRec{Tool, Required, Installed, Command}`；`UpgradeRec{Tool, From, To, Reason, Command}`
- **FR-030b**：`recommend` skill 两种模式：默认模式（无 `.xdev.toml` 时扫描项目标记文件推断推荐工具组合 + 生成 `.xdev.toml`）、`--check` 模式（有 `.xdev.toml` 时对比声明与已装版本，输出 satisfied/conflict/gaps/upgrades）
- **FR-031**：CLI `xdev ai run <skill> [args]`（单次执行某 skill，recommend 支持 `--check`）、`xdev ai skills`（列出 5 个内置）；**无 `xdev ai chat`**
- **FR-032**：GUI AI 面板：5 个 skill 入口卡片、结构化建议展示（含执行按钮）、无通用聊天框
- **FR-033**：降级：`suggest` → manifest latest+命令；`diagnose` → 规则化 PATH/init 检查清单；`health-check` → 纯规则检查（精简 doctor）；`migrate` → 扫描 PATH+常见路径匹配 shims 名；`recommend` → 解析 `.xdev.toml`/`.tool-versions`/项目标记文件，按声明版本与 manifest latest 比对，输出 gap 列表 + 命令
- **FR-034**：现有 `SuggestVersions`/`DiagnoseIssue` API 保留为 deprecated wrapper（内部走新 skill），不破坏 GUI 现有调用；`Generate` API 废弃（无 xdev 专属价值）

### 非功能需求 (Non-Functional Requirements)

- **NFR-001**：性能 — `AIContext.Build` < 50ms（本地数据无网络）；provider 调用遵循各自超时（默认 60s）；token 估算用粗估（~4 字符/token）不阻塞主流程
- **NFR-002**：兼容性 — 现有 `SuggestVersions`/`DiagnoseIssue` API 保留为 deprecated wrapper（内部走 Provider + skill），不破坏 GUI 现有调用
- **NFR-003**：安全 — API Key 默认进系统密钥库，不进 config.yaml 明文；`apikey get` 脱敏显示；AIContext 只发给用户配置的 provider 端点（用户自负信任责任，不强制 localhost）
- **NFR-004**：成本透明 — `provider test` 显示 token 用量与成本估算；`ai run` 记录 token 消耗；`max_tokens` 可配
- **NFR-005**：可扩展 — 新增内置 provider 只需在 `internal/ai/providers/` 加文件实现接口并注册；**skill 不可外部扩展**（仅 4 个内置固定）
- **NFR-006**：可降级 — 任何 skill 在所有 provider 不可用时走 skill.Fallback；provider 失败按 fallback_chain 降级
- **NFR-007**：跨平台 — 密钥库按平台选最优实现；会话/配置 JSON 格式跨平台一致
- **NFR-008**：可测试 — Provider 与 skill 接口允许注入 mock client，单元测试不依赖真实 API
- **NFR-009**：可观测 — 每次 `Assistant.Run` 记录：provider 名、skill 名、token 用量、是否 fallback、耗时；失败记录错误
- **NFR-010**：隐私告知 — 首次配置云端 provider 时提示"AIContext 含环境信息将发送至 <provider 端点>，请确认信任该 provider"，需用户确认
- **NFR-011**：定位约束 — AI 助手仅服务于 xdev 专属窄场景（版本建议、环境诊断、健康检查、迁移建议）；不提供通用聊天、不实现可扩展 skill 框架、不沦为 agent 平台

---

## Success Criteria *(mandatory)*

- **SC-001**：用户配置 DeepSeek/OpenAI/自定义/本地任一渠道后，`xdev ai run suggest python` 返回结构化建议
- **SC-002**：未配置任何 provider 时，`xdev ai` 命令显示引导，不报错不崩溃
- **SC-003**：API Key 默认存系统密钥库，`config.yaml` 不含明文 key
- **SC-004**：`xdev ai config provider switch <name>` 切换 active，后续调用走新 provider
- **SC-005**：active provider 不可用时按 fallback_chain 降级，最终走规则化 Fallback，无静默失败
- **SC-006**：用户可配置 model id 与推理参数，调用按协议注入对应字段；模型不支持某参数时忽略并警告
- **SC-007**：双层参数生效——模型级 Params 覆盖全局 DefaultParams；`extra_body` 透传协议专属参数且优先级最高；冲突时日志记录
- **SC-008**：Provider 能力查询接口生效——`SupportsReasoning()` 等方法驱动参数注入与 UI 控件启用
- **SC-009**：`xdev ai skills` 列出 5 个内置 skill（suggest/diagnose/health-check/migrate/recommend），无外部 skill
- **SC-010**：GUI AI 面板显示 5 个 skill 入口卡片 + 结构化建议 + 一键执行（带确认），无通用聊天框
- **SC-011**：所有 5 个 skill 在 provider 不可用时走规则化 Fallback，返回基于 xdev 内部数据的有效结果
- **SC-012**：`ai run` 完成后日志记录 token 用量；用户配 `pricing` 时记录成本估算
- **SC-013**：AI 助手不偏离 xdev 定位——无通用 chat、无可扩展 skill 框架、无模型分级，仅服务于 xdev 专属窄场景
- **SC-014**：`recommend` skill 在无 `.xdev.toml` 时扫描项目标记文件推断推荐工具组合；有 `.xdev.toml` + `--check` 时对比声明与已装版本输出 satisfied/conflict/gaps/upgrades；无任何项目标记时给出明确提示

---

## API / 契约草案

```go
// internal/ai/provider.go
package ai

type Message struct {
    Role    string `json:"role"`    // system|user|assistant
    Content string `json:"content"`
}

type ProviderConfig struct {
    Type         string        `yaml:"type"`          // builtin|custom
    BuiltinName  string        `yaml:"builtin_name"`  // openai|anthropic|deepseek|qwen|gemini|ollama-cloud (builtin only)
    Protocol     string        `yaml:"protocol"`      // anthropic|openai-chat|openai-responses
    BaseURL      string        `yaml:"base_url"`
    Model        string        `yaml:"model"`
    APIKeyRef    string        `yaml:"api_key_ref"`
    Timeout      string        `yaml:"timeout"`
    Params       ModelParams   `yaml:"params"`        // 模型级参数（覆盖全局默认）
    Capabilities Capabilities  `yaml:"capabilities"`
    ExtraBody    map[string]any `yaml:"extra_body,omitempty"` // 协议专属参数透传，优先级最高
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
    CostEstimate float64 // 按用户配置 Pricing 估算，未配则为 0
    FinishReason string
}

// Provider 接口（能力查询方法驱动参数注入与 UI）
type Provider interface {
    Name() string
    Config() ProviderConfig
    Chat(ctx context.Context, messages []Message) (ChatResult, error)
    Ping(ctx context.Context) error
    SupportsReasoning() bool
    SupportsVision() bool
    SupportsStreaming() bool
    ContextWindow() int
    ListModels(ctx context.Context) ([]string, error) // 可选
}

// 协议适配器
type ProtocolAdapter interface {
    BuildRequest(baseURL, apiKey string, messages []Message, model string, params ModelParams, caps Capabilities, extraBody map[string]any) (*http.Request, error)
    ParseResponse(body []byte) (ChatResult, error)
}

// 内置渠道默认配置表（含每模型能力声明）
var BuiltinProviders = map[string]ProviderConfig{
    "openai":       {Protocol: "openai-responses", BaseURL: "https://api.openai.com/v1", Model: "gpt-4o", Capabilities: Capabilities{ContextWindow: 128000, Vision: true}},
    "anthropic":    {Protocol: "anthropic", BaseURL: "https://api.anthropic.com", Model: "claude-sonnet-4-5", Capabilities: Capabilities{Reasoning: true, ContextWindow: 200000, Vision: true}},
    "deepseek":     {Protocol: "openai-chat", BaseURL: "https://api.deepseek.com/v1", Model: "deepseek-chat", Capabilities: Capabilities{ContextWindow: 65536}},
    "qwen":         {Protocol: "openai-chat", BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Model: "qwen-plus", Capabilities: Capabilities{ContextWindow: 131072}},
    "gemini":       {Protocol: "openai-chat", BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai", Model: "gemini-2.0-flash", Capabilities: Capabilities{ContextWindow: 1000000, Vision: true}},
    "ollama-cloud": {Protocol: "openai-chat", BaseURL: "https://api.ollama.com/v1", Model: "llama3.3", Capabilities: Capabilities{ContextWindow: 32768}},
}

var ModelCapabilityTable = map[string]Capabilities{
    "gpt-4o":            {ContextWindow: 128000, Vision: true},
    "gpt-4o-mini":       {ContextWindow: 128000, Vision: true},
    "o1":                {Reasoning: true, ContextWindow: 200000},
    "o3":                {Reasoning: true, ContextWindow: 200000},
    "claude-sonnet-4-5": {Reasoning: true, ContextWindow: 200000, Vision: true},
    "deepseek-reasoner": {Reasoning: true, ContextWindow: 65536},
    // ... 其余按需补充
}

// internal/ai/keystore.go
type Keystore interface {
    Set(name, value string) error
    Get(name string) (string, error)
    Delete(name string) error
    Has(name string) bool
}

// internal/ai/skills.go — 5 个内置固定 skill，不可外部注册
type AIContext struct {
    OS, Arch           string
    XMDevDir           string
    InstalledTools     []ToolStatus
    ActiveVersions     map[string]string
    PathHasShimDir     bool
    ShimDir            string
    ManifestCandidates  []ManifestCandidate
    UserIntent          string
    ProjectEnv          *ProjectEnv // 当前项目环境，nil 表示无项目配置
}

// ProjectEnv 当前项目环境（从 .xdev.toml/.tool-versions 推断，或扫描项目标记文件）
type ProjectEnv struct {
    HasConfig      bool                 // 是否有 .xdev.toml/.tool-versions
    ConfigPath     string               // 配置文件路径
    Tools          map[string]string    // 声明的工具版本约束（.xdev.toml [tools]）
    Env            map[string]string    // 声明的环境变量
    Tasks          map[string]TaskInfo  // 声明的任务
    InferredFrom   []string             // 推断来源标记文件（package.json/go.mod/requirements.txt 等，无 .xdev.toml 时）
    Dir            string               // 项目根目录
}

type TaskInfo struct {
    Run  string
    Desc string
}

type skill interface { // 小写，包外不可注册
    Name() string
    Description() string
    BuildPrompt(ctx AIContext, args []string) string
    OutputSchema() reflect.Type
    Parse(raw string) (any, error)
    NeedsProvider() bool
    Fallback(ctx AIContext, args []string) (any, error)
}
// 内置实现：suggestSkill, diagnoseSkill, healthCheckSkill, migrateSkill, recommendSkill（包内固定，无 Registry 开放）

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

// Recommendation — recommend skill 输出
type Recommendation struct {
    Status   string      `json:"status"`   // satisfied|conflict|empty
    Tools    []ToolRec   `json:"tools"`    // 推荐工具组合
    Gaps     []GapRec    `json:"gaps"`     // 约束冲突/缺失
    Upgrades []UpgradeRec `json:"upgrades"` // 可升级项
    Command  string      `json:"command"`  // 一键执行命令
}
type ToolRec struct {
    Name    string `json:"name"`
    Version string `json:"version"`
    Reason  string `json:"reason"`
}
type GapRec struct {
    Tool     string `json:"tool"`
    Required string `json:"required"` // .xdev.toml 声明的约束
    Installed string `json:"installed"` // 已装版本，空表示未装
    Command  string `json:"command"`
}
type UpgradeRec struct {
    Tool    string `json:"tool"`
    From    string `json:"from"`
    To      string `json:"to"`
    Reason  string `json:"reason"`
    Command string `json:"command"`
}

// internal/ai/assistant.go (重构后)
type Assistant struct {
    providers *ProviderRegistry
    keystore  Keystore
    skills    map[string]skill // 5 个内置固定，包内初始化，无开放注册
    fallback  []string
}
func (a *Assistant) Run(skill string, args []string) (any, error)
func (a *Assistant) Chat(...) // 废弃 wrapper，内部调 Run 或返回 ErrDeprecated

// config 重构
type AIConfig struct {
    Providers      map[string]ProviderConfig `yaml:"providers"`
    ActiveProvider string                   `yaml:"active_provider"`
    FallbackChain  []string                 `yaml:"fallback_chain"`
    DefaultParams  ModelParams              `yaml:"default_params"`
    Pricing        map[string]Pricing         `yaml:"pricing"`
    ConfirmActions bool                     `yaml:"confirm_actions"`
    SystemPrompt   string                   `yaml:"system_prompt"`
}
```

---

## 风险与对策

| 风险 | 影响 | 对策 |
|------|------|------|
| API Key 泄漏 | 用户财产损失 | 默认系统密钥库；config.yaml 不存明文；`apikey get` 脱敏显示；无密钥库时 AES 加密+机器绑定 |
| 用户误配置协议格式 | 调用失败 | `provider test` 验证协议与端点匹配；解析失败报"与协议 X 不兼容"；走 fallback |
| 模型不支持某推理参数 | 调用报错 | Capabilities 声明 + ModelCapabilityTable 匹配；不支持时忽略并警告；GUI 禁用控件 |
| reasoning_effort 各协议映射不一致 | 行为不一致 | 协议适配器各自映射（OpenAI `reasoning.effort`、Anthropic `thinking.budget_tokens`），文档注明 |
| ContextWindow 估算不准 | 裁剪过度/不足 | 粗估保守裁剪（留 10% 余量）；provider 返回实际 token 后回填校准；用户可手动覆盖 |
| 双层参数 + extra_body 优先级混乱 | 调试困难 | 明确优先级：extra_body > 模型级 Params > 全局 DefaultParams > 协议默认；冲突时日志记录覆盖链 |
| 定价表需用户维护 | 成本估算缺失 | 不维护全局定价表；用户可选配 `pricing`；未配只记 token；`provider test` 用各 provider 公开定价显示估算 |
| 用户误配置恶意端点 | 环境信息外泄 | 首次配置 provider 强制确认信任；AIContext 发送前日志记录目标端点；不强制 localhost |
| Ollama 本地服务被弱化 | 离线用户受影响 | Ollama 本地通过自定义渠道（openai-chat，base_url 填 localhost）完整支持；Ollama Cloud 通过内置渠道；fallback_chain 可配本地兜底 |
| AI 助手定位漂移到通用 agent | xdev 职责膨胀 | 4 个内置固定 skill 不可扩展；无通用 chat；无模型分级；`skill` 接口小写包外不可注册；NFR-011 约束 |
| Skill 输出不稳定 | 结构化解析失败 | JSON 块提取容错 + 字段缺失默认值 + 失败回 Fallback |

---


> **批准ID**: `2026-07-26-spec-010-draft-001`
## 批准记录 (Approval History)

| 阶段 | 状态 | 批准者 | 日期 | 文档版本 | 批准范围 |
|---|---|---|---|---|---|
| Draft 创建 | Draft | `xinyan <zuijiuhjd@foxmail.com>` | 2026-07-26 | `0.1.0` | 初始设计（本地 Ollama 单一后端） |
| Draft 修订一 | Draft | `xinyan <zuijiuhjd@foxmail.com>` | 2026-07-26 | `0.2.0` | 移除 Ollama 强制依赖，新增 Provider 抽象层 |
| Draft 修订二 | Draft | `xinyan <zuijiuhjd@foxmail.com>` | 2026-07-26 | `0.3.0` | 本地整合进自定义渠道，三种协议 |
| Draft 修订三 | Draft | `xinyan <zuijiuhjd@foxmail.com>` | 2026-07-26 | `0.4.0` | 新增 ModelParams/Capabilities/协议字段映射 |
| Draft 修订四 | Draft | `xinyan <zuijiuhjd@foxmail.com>` | 2026-07-26 | `0.5.0` | 参考开源 agent 调研，新增模型分级/能力查询/双层参数/轻量成本 |
| Draft 修订五 | Draft | `xinyan <zuijiuhjd@foxmail.com>` | 2026-07-26 | `0.6.0` | 采纳"AI 助手应收窄到 xdev 专属窄场景"意见，砍掉 chat 多轮/Skill 可扩展/模型分级，明确 AI 助手定位边界 |
| Draft 修订六 | Draft | `xinyan <zuijiuhjd@foxmail.com>` | 2026-07-26 | `0.7.0` | 采纳"加入根据用户当前项目整体环境进行推荐的 skill"意见，新增第 5 个内置 skill `recommend`，扩展 AIContext 采集 ProjectEnv，新增 Recommendation 结构与 --check 模式 |
| Spec 批准 | Approved | `xinyan <zuijiuhjd@foxmail.com>` | 2026-08-20 | `0.7.0` | `2026-08-20-spec-010-approved-001` SDD Loop 收敛后元数据对齐为 Approved |

---

## 变更历史

| 版本 | 日期 | 变更类型 | 说明 | 批准者 |
|---|---|---|---|---|
| 0.1.0 | 2026-07-26 | Initial Draft | 初始设计（本地 Ollama 单一后端） | — |
| 0.2.0 | 2026-07-26 | Major Revision | 移除 Ollama 强制依赖，新增 Provider 抽象层 | — |
| 0.3.0 | 2026-07-26 | Revision | 本地整合进自定义渠道，三种协议（anthropic/openai-chat/openai-responses） | — |
| 0.4.0 | 2026-07-26 | Revision | 新增 ModelParams/Capabilities/协议字段映射/ContextWindow 裁剪 | — |
| 0.5.0 | 2026-07-26 | Revision | 参考开源 agent 调研，新增模型分级/能力查询/双层参数/轻量成本 | — |
| 0.6.0 | 2026-07-26 | Major Revision | 采纳"AI 助手应收窄到 xdev 专属窄场景"意见：砍掉 `xdev ai chat` 通用多轮、Skill 可扩展机制（Go/配置/Lua 三源）、模型分级 main/reasoning；保留 Provider 接入层 + 4 个内置固定 skill（suggest/diagnose/health-check/migrate）+ 结构化建议一键执行；`skill` 接口小写包外不可注册；新增 NFR-011 定位约束、SC-013 定位验收；AI 助手明确为"带环境上下文的窄场景助手"而非"可扩展 agent 平台" | — |
| 0.7.0 | 2026-07-26 | Revision | 采纳"加入根据用户当前项目整体环境进行推荐的 skill"意见：新增第 5 个内置固定 skill `recommend`（项目环境推荐）；扩展 AIContext 采集 ProjectEnv（.xdev.toml 的 tools/env/tasks，或扫描项目标记文件 package.json/go.mod/requirements.txt 等推断）；新增 Recommendation 结构（Status/Tools/Gaps/Upgrades/Command）与 `--check` 模式（对比声明与已装版本）；新增 User Story 9、FR-030a/b、SC-014 | — |