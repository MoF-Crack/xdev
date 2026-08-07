# 验收标准：上游模型发现 + 模型参数配置

> **文档类型**：Acceptance Criteria
> **状态**：Draft
> **创建日期**：2026-07-27
> **关联 Spec**：`specs/010-ai-assistant-enhancement/spec.md` v0.7.0
> **关联方案**：综合 sub2api/new-api/one-api/cc-switch 调研的上游模型发现实现方案
> **权威范围**：定义上游模型发现与模型参数配置功能的逐模块验收标准，确保实施后可对照验证

---

## 模块清单

| 模块 | 标识 | 验收标准数 |
|---|---|---|
| M1 数据结构定义 | ModelInfo / ModelListResult | AC-M1-001..002 |
| M2 ProtocolAdapter 接口扩展 | BuildListModelsRequest / ParseListModelsResponse | AC-M2-001..002 |
| M3 OpenAI Chat 适配器 | ListModels 请求/响应解析 | AC-M3-001..005 |
| M4 OpenAI Responses 适配器 | ListModels 请求/响应解析 | AC-M4-001..003 |
| M5 Anthropic 适配器 | ListModels 请求/响应解析 | AC-M5-001..005 |
| M6 internalProvider.ListModels | HTTP 调用 + ETag 缓存 + 降级 | AC-M6-001..012 |
| M7 ProviderConfig 模型持久化 | Models 字段 + 向后兼容 | AC-M7-001..003 |
| M8 能力解析分层 | 静态表 + 用户覆盖 + 保守默认 | AC-M8-001..005 |
| M9 CLI 命令 | list-models / --fetch-models | AC-M9-001..008 |
| M10 GUI binding | AIListProviderModels | AC-M10-001..004 |
| M11 前端模型选择交互 | 下拉 + 获取按钮 + 降级输入 | AC-M11-001..009 |
| M12 Wails binding 重生成 | App.js 含新方法 | AC-M12-001..002 |
| M13 测试覆盖 | httptest mock 三协议 | AC-M13-001..006 |
| M14 兼容性 | 向后兼容不破坏现有 | AC-M14-001..005 |

---

## M1 数据结构定义

### AC-M1-001 ModelInfo 结构完整

**Given** `internal/ai/provider.go` 定义了 `ModelInfo` 结构
**When** 检查结构字段
**Then** 包含以下字段，且 JSON tag 正确：
- `ID string` — `json:"id"`，模型标识（如 "gpt-4o"）
- `DisplayName string` — `json:"display_name"`，显示名（上游无则用 ID）
- `OwnedBy string` — `json:"owned_by"`，所属组织（如 "openai"）
- `Created int64` — `json:"created"`，Unix 时间戳（上游无则 0）

### AC-M1-002 ModelListResult 结构完整

**Given** `internal/ai/provider.go` 定义了 `ModelListResult` 结构
**When** 检查结构字段
**Then** 包含以下字段：
- `Models []ModelInfo` — 模型列表
- `FromCache bool` — `json:"from_cache"`，是否命中缓存（304）
- `ETag string` — `json:"-"`，不序列化，内存缓存用
- `FetchedAt time.Time` — `json:"fetched_at"`，获取时间

---

## M2 ProtocolAdapter 接口扩展

### AC-M2-001 接口新增两个方法

**Given** `internal/ai/provider.go` 的 `ProtocolAdapter` 接口
**When** 检查接口定义
**Then** 在现有 `BuildRequest` / `ParseResponse` 之外，新增：
- `BuildListModelsRequest(baseURL, apiKey string) (*http.Request, error)` — 构建模型列表 HTTP 请求
- `ParseListModelsResponse(body []byte) ([]ModelInfo, error)` — 解析模型列表响应

### AC-M2-002 三个适配器都实现新方法

**Given** `anthropic_proto.go` / `openai_chat_proto.go` / `openai_responses_proto.go`
**When** 编译项目
**Then** 三个适配器都实现了 `BuildListModelsRequest` 和 `ParseListModelsResponse`，`go build ./internal/ai/...` 通过

---

## M3 OpenAI Chat 适配器 ListModels

### AC-M3-001 请求构建正确

**Given** `OpenAIChatAdapter.BuildListModelsRequest("https://api.deepseek.com/v1", "sk-xxx")`
**When** 检查返回的 `*http.Request`
**Then**：
- Method = `GET`
- URL = `https://api.deepseek.com/v1/models`（joinURL 去重 /v1，无 `/v1/v1/models`）
- Header `Authorization` = `Bearer sk-xxx`

### AC-M3-002 请求构建处理 base_url 无 /v1

**Given** `OpenAIChatAdapter.BuildListModelsRequest("https://api.openai.com/v1", "sk-xxx")`
**When** 检查 URL
**Then** URL = `https://api.openai.com/v1/models`（不重复 /v1）

### AC-M3-003 请求构建处理 base_url 无 /v1 后缀

**Given** `OpenAIChatAdapter.BuildListModelsRequest("https://gateway.example.com", "sk-xxx")`
**When** 检查 URL
**Then** URL = `https://gateway.example.com/v1/models`

### AC-M3-004 响应解析正确

**Given** 响应 body = `{"object":"list","data":[{"id":"gpt-4o","object":"model","created":1234,"owned_by":"openai"},{"id":"gpt-4o-mini","object":"model","created":1235,"owned_by":"openai"}]}`
**When** 调用 `ParseListModelsResponse(body)`
**Then** 返回 `[]ModelInfo`，长度 2：
- `[0].ID="gpt-4o"`, `[0].OwnedBy="openai"`, `[0].Created=1234`, `[0].DisplayName="gpt-4o"`
- `[1].ID="gpt-4o-mini"`, `[1].OwnedBy="openai"`, `[1].Created=1235`

### AC-M3-005 响应解析失败明确报错

**Given** 响应 body = `<html>Not Found</html>`（非 JSON）
**When** 调用 `ParseListModelsResponse(body)`
**Then** 返回 error，信息含 "openai models: decode"

---

## M4 OpenAI Responses 适配器 ListModels

### AC-M4-001 请求构建正确

**Given** `OpenAIResponsesAdapter.BuildListModelsRequest("https://api.openai.com/v1", "sk-xxx")`
**When** 检查 `*http.Request`
**Then**：
- Method = `GET`
- URL = `https://api.openai.com/v1/models`
- Header `Authorization` = `Bearer sk-xxx`

### AC-M4-002 响应解析正确

**Given** 响应 body 与 AC-M3-004 相同（OpenAI /v1/models 格式统一）
**When** 调用 `ParseListModelsResponse(body)`
**Then** 返回 `[]ModelInfo`，结果与 AC-M3-004 一致

### AC-M4-003 解析失败明确报错

**Given** 响应 body 非 JSON
**When** 调用 `ParseListModelsResponse(body)`
**Then** 返回 error，信息含 "openai responses models: decode" 或 "models: decode"

---

## M5 Anthropic 适配器 ListModels

### AC-M5-001 请求构建正确

**Given** `AnthropicAdapter.BuildListModelsRequest("https://api.anthropic.com", "sk-ant-xxx")`
**When** 检查 `*http.Request`
**Then**：
- Method = `GET`
- URL = `https://api.anthropic.com/v1/models`
- Header `x-api-key` = `sk-ant-xxx`
- Header `anthropic-version` = `2023-06-01`

### AC-M5-002 请求构建处理 base_url 含 /v1

**Given** `AnthropicAdapter.BuildListModelsRequest("https://proxy.example.com/v1", "sk-ant-xxx")`
**When** 检查 URL
**Then** URL = `https://proxy.example.com/v1/models`（不重复 /v1）

### AC-M5-003 响应解析正确

**Given** 响应 body = `{"data":[{"type":"model","id":"claude-sonnet-4-5","display_name":"Claude Sonnet 4.5"},{"type":"model","id":"claude-opus-4.1","display_name":"Claude Opus 4.1"}]}`
**When** 调用 `ParseListModelsResponse(body)`
**Then** 返回 `[]ModelInfo`，长度 2：
- `[0].ID="claude-sonnet-4-5"`, `[0].DisplayName="Claude Sonnet 4.5"`
- `[1].ID="claude-opus-4.1"`, `[1].DisplayName="Claude Opus 4.1"`

### AC-M5-004 响应解析处理空 display_name

**Given** 响应 body = `{"data":[{"type":"model","id":"claude-sonnet-4-5"}]}`（无 display_name 字段）
**When** 调用 `ParseListModelsResponse(body)`
**Then** 返回 `[]ModelInfo`，长度 1，`[0].DisplayName="claude-sonnet-4-5"`（用 ID 兜底）

### AC-M5-005 解析失败明确报错

**Given** 响应 body 非 JSON
**When** 调用 `ParseListModelsResponse(body)`
**Then** 返回 error，信息含 "anthropic models: decode"

---

## M6 internalProvider.ListModels

### AC-M6-001 内存缓存命中（TTL 内）

**Given** 同一 base_url 的模型列表已在 1h 内获取过（内存缓存有效）
**When** 再次调用 `provider.ListModels(ctx)`
**Then** 直接返回缓存结果，`FromCache=true`，不发 HTTP 请求

### AC-M6-002 内存缓存过期后重新获取

**Given** 缓存条目超过 1h TTL
**When** 调用 `provider.ListModels(ctx)`
**Then** 重新发 HTTP 请求获取，更新缓存，`FromCache=false`

### AC-M6-003 ETag 条件请求 304 命中

**Given** 缓存中有 ETag，上游支持 ETag
**When** 调用 `provider.ListModels(ctx)`，上游返回 304
**Then** 使用缓存的模型列表，`FromCache=true`，不发完整请求体

### AC-M6-004 ETag 条件请求 200 更新

**Given** 缓存中有 ETag
**When** 调用 `provider.ListModels(ctx)`，上游返回 200 + 新 ETag
**Then** 解析新响应，更新缓存（含新 ETag），`FromCache=false`

### AC-M6-005 正常获取模型列表

**Given** provider 配置了 base_url + APIKeyRef + protocol，上游可访问
**When** 调用 `provider.ListModels(ctx)`
**Then**：
- 通过 keystore.Get(APIKeyRef) 取 API key
- 调对应协议适配器的 BuildListModelsRequest
- 用 p.client.Do（带 timeout）发请求
- 调 ParseListModelsResponse 解析
- 返回 `ModelListResult{Models, FromCache:false, FetchedAt:now}`

### AC-M6-006 API Key 无效时明确报错

**Given** API key 无效（上游返回 401）
**When** 调用 `provider.ListModels(ctx)`
**Then** 返回 error，信息含 "list models: HTTP 401"，不崩溃

### AC-M6-007 网络超时报错

**Given** 上游不可达（连接超时）
**When** 调用 `provider.ListModels(ctx)`（ctx 带 15s timeout）
**Then** 返回 error，信息含 "list models:" + 超时/连接错误

### AC-M6-008 上游不支持 /v1/models（404）

**Given** 上游端点不提供模型枚举（返回 404）
**When** 调用 `provider.ListModels(ctx)`
**Then** 返回 error，信息含 "HTTP 404"

### AC-M6-009 非 JSON 响应报错

**Given** 上游返回 HTML 错误页
**When** 调用 `provider.ListModels(ctx)`
**Then** 返回 error，信息含 "decode"（来自适配器 ParseListModelsResponse）

### AC-M6-010 空模型列表

**Given** 上游返回 `{"data":[]}`（空列表）
**When** 调用 `provider.ListModels(ctx)`
**Then** 返回 `ModelListResult{Models: [], FromCache:false}`，不报错

### AC-M6-011 云端未信任 provider 拒绝获取

**Given** provider Trusted=false 且 base_url 非 localhost
**When** 调用 `provider.ListModels(ctx)`
**Then** 返回 error "provider not trusted"（与 Chat 一致的信任拦截）

### AC-M6-012 本地 provider 无需信任

**Given** provider base_url = `http://localhost:11434/v1`，Trusted=false
**When** 调用 `provider.ListModels(ctx)`
**Then** 正常获取模型列表（本地 provider 免信任）

---

## M7 ProviderConfig 模型持久化

### AC-M7-001 新增 Models 字段

**Given** `internal/config/config.go` 的 `ProviderConfig` 结构
**When** 检查字段
**Then** 包含 `Models []string` 字段，yaml tag = `models,omitempty`

### AC-M7-002 向后兼容

**Given** 旧 config.yaml 无 `models` 字段
**When** 加载配置
**Then** `ProviderConfig.Models` 为 nil/空，不报错，不影响其他字段

### AC-M7-03 模型列表持久化（可选）

**Given** 用户通过 GUI/CLI 获取了模型列表
**When** 保存配置
**Then** `ProviderConfig.Models` 可存储模型 ID 列表到 config.yaml（可选功能，若实现则 omitempty 不影响旧配置）

---

## M8 能力解析分层

### AC-M8-001 用户显式配置优先

**Given** ProviderConfig.Capabilities.ContextWindow = 128000（用户显式配置）
**When** 调用 `ResolveModelCapabilities("gpt-4o", &caps)`
**Then** 保留用户配置的 ContextWindow=128000，不被静态表覆盖

### AC-M8-002 静态表匹配

**Given** ProviderConfig.Capabilities 为零值，model = "gpt-4o"
**When** 调用 `ResolveModelCapabilities("gpt-4o", &caps)`
**Then** 从 `ModelCapabilityTable["gpt-4o"]` 填充能力（ContextWindow=128000, Vision=true）

### AC-M8-003 静态表未匹配保守默认

**Given** ProviderConfig.Capabilities 为零值，model = "unknown-model-xyz"
**When** 调用 `ResolveModelCapabilities("unknown-model-xyz", &caps)`
**Then**：
- Reasoning=false, Vision=false, ContextWindow=8192
- 日志记录 "model not in capability table, using conservative defaults"

### AC-M8-004 推理模型能力正确

**Given** model = "deepseek-reasoner"
**When** 调用 `ResolveModelCapabilities`
**Then** Reasoning=true（静态表已配）

### AC-M8-005 用户覆盖后不被静态表重置

**Given** 用户设 Vision=true，model 未在静态表
**When** 调用 `ResolveModelCapabilities`
**Then** Vision 保持 true（用户显式配置不被保守默认覆盖）

---

## M9 CLI 命令

### AC-M9-001 list-models 命令存在

**Given** 项目编译后的 xdev 二进制
**When** 执行 `xdev ai config provider list-models --help`
**Then** 显示帮助信息，说明用法 `list-models <provider-name>`

### AC-M9-002 list-models 正常输出

**Given** 已配置 provider "deepseek"（base_url + apikey），上游可访问
**When** 执行 `xdev ai config provider list-models deepseek`
**Then**：
- 输出模型列表表格（ID / OwnedBy）
- 显示获取耗时与 from-cache 状态
- 退出码 0

### AC-M9-003 list-models 获取失败

**Given** 已配置 provider "my-gateway"，但上游不可达
**When** 执行 `xdev ai config provider list-models my-gateway`
**Then**：
- 输出错误信息 "✘ Failed to fetch models: ..."
- 提示 "You can still set model manually: xdev ai config model set my-gateway --model <id>"
- 退出码 0（不因获取失败而报错退出）

### AC-M9-004 list-models 未配置 provider

**Given** 未配置任何 provider
**When** 执行 `xdev ai config provider list-models deepseek`
**Then** 显示 provider 引导（与现有 ai 子命令一致的引导）

### AC-M9-005 provider add --fetch-models 自动获取

**Given** 执行 `xdev ai config provider add deepseek --apikey sk-xxx --fetch-models`
**When** 上游可访问
**Then**：
- 添加 provider 成功
- 自动获取模型列表，输出 "Fetched N models: model1, model2, ..."
- Active model 设为默认（builtin default 或第一个获取的）

### AC-M9-006 --fetch-models 获取失败仍添加

**Given** 执行 `xdev ai config provider add custom my-gw --base-url <url> --protocol openai-chat --model gpt-4o --apikey sk-xxx --fetch-models`
**When** 上游不可达
**Then**：
- provider 仍添加成功（--fetch-models 是 best-effort）
- 输出警告 "Failed to fetch models: <error>, model set to gpt-4o (manual)"
- 退出码 0

### AC-M9-007 缓存命中显示

**Given** 1h 内已获取过 deepseek 的模型列表
**When** 再次执行 `xdev ai config provider list-models deepseek`
**Then** 输出含 "from cache: true" 或类似标记

### AC-M9-008 model set 命令不依赖 ListModels

**Given** 已配置 provider，但未获取过模型列表
**When** 执行 `xdev ai config model set deepseek --model deepseek-chat`
**Then** 直接设置模型，不触发 ListModels，不报错

---

## M10 GUI binding

### AC-M10-001 AIListProviderModels binding 存在

**Given** `internal/gui/app_project.go`
**When** 检查方法
**Then** 存在 `func (a *App) AIListProviderModels(providerName string) (*ai.ModelListResult, error)`

### AC-M10-002 正常返回模型列表

**Given** 已配置 provider，上游可访问
**When** 前端调用 `AIListProviderModels("deepseek")`
**Then** 返回 `ModelListResult{Models: [...], FromCache: bool, FetchedAt: time}`，前端可解析 JSON

### AC-M10-003 provider 不存在报错

**Given** 未配置 provider "nonexistent"
**When** 调用 `AIListProviderModels("nonexistent")`
**Then** 返回 error "provider not found"

### AC-M10-004 获取超时不阻塞 GUI

**Given** 上游响应慢
**When** 调用 `AIListProviderModels`
**Then** 带 15s timeout，超时返回 error，不阻塞 GUI 主线程

---

## M11 前端模型选择交互

### AC-M11-001 有模型列表时显示下拉

**Given** `modelOptions` 非空（已获取过模型列表）
**When** 渲染 provider 配置表单的 model 字段
**Then** 显示 `<n-select>` 下拉（filterable 可搜索），选项来自 modelOptions

### AC-M11-002 无模型列表时显示输入框

**Given** `modelOptions` 为空（未获取或获取失败）
**When** 渲染 model 字段
**Then** 显示 `<n-input>` 输入框（手动输入模型 ID），placeholder 如 "gpt-4o"

### AC-M11-003 "获取模型"按钮存在

**Given** provider 配置表单
**When** 检查 model 字段旁
**Then** 存在 "获取模型" 按钮（data-action="fetch-models"），带 loading 状态

### AC-M11-004 获取成功更新下拉

**Given** 用户点击"获取模型"，上游返回 3 个模型
**When** 获取完成
**Then**：
- modelOptions 更新为 3 个选项
- model 字段从输入框切换为下拉
- 若当前 model 为空，自动选第一个模型
- 显示成功提示

### AC-M11-005 获取失败降级

**Given** 用户点击"获取模型"，上游返回 401
**When** 获取失败
**Then**：
- 显示错误提示（n-alert warning）"API key 无效"或对应错误
- modelOptions 保持空
- model 字段保持输入框
- 不崩溃，用户可手动输入

### AC-M11-006 缓存命中提示

**Given** 1h 内已获取过，再次点击"获取模型"
**When** 返回 FromCache=true
**Then** 显示提示 "models from cache"（info 级别）

### AC-M11-007 选择模型后能力联动

**Given** 用户从下拉选择 model = "gpt-4o"
**When** 模型选择变化
**Then**：
- 调用后端获取该模型的能力（或前端用静态表查）
- reasoning_effort 下拉：若模型 Reasoning=false 则禁用；若 true 则启用
- context_window 自动填充静态表值（若用户未手动改过）

### AC-M11-008 内置渠道预设模型

**Given** 用户选择内置渠道 "deepseek"（builtin）
**When** 切换到该 provider
**Then** model 字段预填 "deepseek-chat"（BuiltinProviders 默认），用户可点"获取模型"刷新为完整列表

### AC-M11-009 获取按钮 loading 状态

**Given** 点击"获取模型"发起请求
**When** 请求进行中
**Then** 按钮显示 loading 状态，禁用重复点击

---

## M12 Wails binding 重生成

### AC-M12-001 App.js 含 AIListProviderModels

**Given** 执行 `wails generate module` 或 `wails build`
**When** 检查 `frontend/wailsjs/go/gui/App.js`
**Then** 包含 `AIListProviderModels` 方法的导出

### AC-M12-002 App.d.ts 含类型定义

**Given** 同上
**When** 检查 `frontend/wailsjs/go/gui/App.d.ts` 或 `models.ts`
**Then** 包含 `ModelListResult` / `ModelInfo` 的 TypeScript 类型定义

---

## M13 测试覆盖

### AC-M13-001 OpenAI Chat ListModels 测试

**Given** httptest server 返回标准 OpenAI /v1/models 响应
**When** 调用 `OpenAIChatAdapter.BuildListModelsRequest` + `ParseListModelsResponse`
**Then**：
- 请求 URL 正确（无 /v1 重复）
- Authorization header 正确
- 解析出正确的模型列表
- 测试不访问真实网络

### AC-M13-002 Anthropic ListModels 测试

**Given** httptest server 返回 Anthropic /v1/models 响应
**When** 调用 `AnthropicAdapter.BuildListModelsRequest` + `ParseListModelsResponse`
**Then**：
- 请求 URL 正确
- x-api-key + anthropic-version header 正确
- 解析出正确模型列表（含 display_name）

### AC-M13-003 internalProvider.ListModels 端到端测试

**Given** httptest mock provider，模拟 /v1/models 响应
**When** 调用 `provider.ListModels(ctx)`
**Then**：
- 正确获取模型列表
- FromCache=false（首次）
- 第二次调用 FromCache=true（缓存命中）

### AC-M13-004 ETag 304 测试

**Given** httptest server 支持 ETag（首次 200 + ETag，第二次 If-None-Match 返回 304）
**When** 连续调用两次 `provider.ListModels(ctx)`
**Then**：
- 第一次 FromCache=false，获取 ETag
- 第二次 FromCache=true（304 命中）

### AC-M13-005 错误场景测试

**Given** httptest server 返回 401 / 404 / 超时 / 非 JSON
**When** 调用 `provider.ListModels(ctx)`
**Then** 每种错误场景返回明确 error，不 panic

### AC-M13-006 不访问真实网络

**Given** 所有 ListModels 相关测试
**When** 运行 `go test ./internal/ai/...`
**Then** 不发起任何真实网络请求（全部用 httptest mock 或 t.Skip 标记集成测试）

---

## M14 兼容性

### AC-M14-001 Provider 接口签名升级兼容

**Given** `Provider.ListModels` 签名从 `([]string, error)` 升级为 `(ModelListResult, error)`
**When** 检查 mockProvider（`assistant_test.go`）
**Then** mockProvider 已适配新签名，现有测试通过

### AC-M14-002 ProtocolAdapter 新方法不破坏现有

**Given** ProtocolAdapter 新增两个方法
**When** 编译所有实现 ProtocolAdapter 的代码
**Then** 三个适配器都实现了新方法，`go build ./...` 通过

### AC-M14-003 config 向后兼容

**Given** 旧 config.yaml 无 `models` 字段
**When** 加载配置
**Then** 不报错，ProviderConfig.Models 为 nil

### AC-M14-004 现有 provider 配置流程不破坏

**Given** 现有 `xdev ai config provider add` / `list` / `remove` / `switch` / `test` 命令
**When** 执行
**Then** 行为不变（list-models 是新增命令，不影响现有）

### AC-M14-005 GUI 现有功能不破坏

**Given** 现有 AI 面板的 provider 列表、skill 卡片、结构化结果展示
**When** 改造 model 字段为下拉
**Then** 其他功能不受影响，`go build ./...` + `npm run build` + `wails build` 全通过

---

## 整体验收门禁

| 门禁 | 标准 | 验证方式 |
|---|---|---|
| 编译 | `go build ./...` 通过 | 命令执行 |
| 静态检查 | `go vet ./...` 通过 | 命令执行 |
| 单元测试 | `go test ./internal/ai/... ./internal/config/... ./internal/cli/... ./internal/gui/...` 通过，不访问真实网络 | 命令执行 |
| 前端构建 | `npm run build` 通过 | 命令执行 |
| Wails 构建 | `wails build` 通过，App.js 含 AIListProviderModels | 命令执行 + 文件检查 |
| GUI 启动 | xdev-gui.exe 启动不崩溃 | 进程验证 |
| CLI 实测 | `xdev ai config provider list-models --help` 显示帮助 | 命令执行 |
| 向后兼容 | 旧 config.yaml 加载不报错 | 测试验证 |
| 定位约束 | 无通用 chat、无开放 skill 注册、5 内置 skill 不变 | 代码检查 |

---

## 已知限制（验收时明确记录，不视为不合格）

| 限制 | 说明 | 理由 |
|---|---|---|
| 内存缓存非持久化 | 进程退出后模型列表缓存丢失，下次重新获取 | 轻量设计，1h TTL 够用；可选持久化到 config.Models |
| 静态能力表非自动更新 | ModelCapabilityTable 硬编码，新模型需版本发布时更新 | xdev 非元数据中心，硬编码可接受 |
| ETag 支持依赖上游 | 部分上游不支持 ETag，缓存退化为纯 TTL | 降级后仍有 TTL 缓存，体验可接受 |
| 模型列表格式因上游而异 | 不同 provider 的 /v1/models 响应字段可能有差异 | 三协议适配器各自解析，未覆盖的格式降级手动输入 |
| 获取超时 15s | 慢上游可能超时 | 可配 timeout，降级手动输入 |

---

## 验收流程

1. **编译验证**：go build / vet / test 全绿
2. **前端验证**：npm build / wails build 全绿，App.js 含新 binding
3. **CLI 实测**：编译 xdev，执行 list-models / provider add --fetch-models / model set 命令族
4. **GUI 实测**：启动 xdev-gui，打开 AI 面板，配置 provider，点击"获取模型"，验证下拉切换与降级
5. **兼容性验证**：用旧 config.yaml 加载，确认不报错
6. **对照本文件**：逐条核对 AC-M*-*** 是否满足，记录 ✅/⚠️/❌