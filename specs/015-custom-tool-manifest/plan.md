# Plan 015: 用户自定义工具——自动版本发现 + 动态 manifest 生成

> **状态**：待用户批准
> **依据**：`specs/015-custom-tool-manifest/spec.md`

## 执行策略

两阶段串行：后端 binding + manifest 生成逻辑先完成，前端 UI 后完成。

```
Phase 1: 后端（config 结构体 + manifest 生成 + GUI binding + sniffer 集成）
    ↓
Phase 2: 前端（自定义工具表单 + 预览 + 列表 + i18n）
    ↓
Phase 3: 构建验证 + 测试 + 性能 + 安全审计
```

## Phase 1: 后端

### 1.1 config.go — 新增 CustomToolConfig 结构体

```go
type CustomToolConfig struct {
    Name        string `yaml:"name"`
    DisplayName string `yaml:"display_name,omitempty"`
    SourceURL   string `yaml:"source_url"`
    SourceType  string `yaml:"source_type"` // github|html|npm
    Pattern     string `yaml:"pattern,omitempty"`
    Shim        string `yaml:"shim,omitempty"`
    EnvVar      string `yaml:"env_var,omitempty"`
}
```

Config struct 新增 `CustomTools []CustomToolConfig` 字段。

### 1.2 manifest.go — 新增 GenerateCustomManifest

```go
func GenerateCustomManifest(name, displayName, shim, envVar string, versions []DiscoveredVersion) ([]byte, error)
```

- 自动推断 archive format（URL 后缀 .zip→zip, .tar.gz→tar.gz, .tgz→tar.gz）
- 生成 versions 列表，sha256 留空
- 生成 remote 块（供后续 sniff）
- latest = 发现的最新版本
- env_var 默认 `<NAME>_HOME`，shim 默认用工具名

### 1.3 GUI binding — 新增 4 个方法

在 `internal/gui/app_custom_tools.go`（新建文件）：

- `PreviewCustomToolVersions(sourceURL, sourceType, pattern string) []string`
  - 根据 sourceType 构造 RemoteSource，调用 remote.DiscoverVersions
  - 返回版本列表，不写 manifest

- `AddCustomTool(name, displayName, sourceURL, sourceType, pattern, shim, envVar string) *CustomToolResult`
  - 校验工具名 `^[a-z0-9-]+$`
  - 调用 sniffer 发现版本
  - 调用 GenerateCustomManifest 生成 YAML
  - 写入 `$XMDEV_HOME/manifests/<name>.yaml`
  - 保存 CustomToolConfig 到 config.yaml
  - 重新加载 catalog
  - 返回结果

- `RemoveCustomTool(name string) error`
  - 删除 on-disk manifest 文件
  - 从 config.yaml 的 custom_tools 中移除
  - 重新加载 catalog

- `ListCustomTools() []CustomToolInfo`
  - 从 config.yaml 读取 custom_tools 列表

### 1.4 验证
- `go build ./...` 通过
- `go test ./internal/gui/... ./internal/manifest/... ./internal/config/...` 通过

### 1.5 测试用例与预期结果

| 用例 | 操作 | 预期结果 |
|---|---|---|
| T1.5.1 | `GenerateCustomManifest("cangjie", "Cangjie", "cjc", "CANGJIE_HOME", versions)` | 返回有效 YAML，含 name/display_name/shims/env_vars/versions/remote/latest |
| T1.5.2 | `GenerateCustomManifest` 传入 URL 后缀 `.zip` | format 字段为 "zip" |
| T1.5.3 | `GenerateCustomManifest` 传入 URL 后缀 `.tar.gz` | format 字段为 "tar.gz" |
| T1.5.4 | `AddCustomTool` 工具名含 `../` | 返回错误，不写文件 |
| T1.5.5 | `AddCustomTool` 工具名含大写字母 | 返回错误 |
| T1.5.6 | `AddCustomTool` 有效工具名 + 有效源 | manifest 写入成功，config.yaml 保存，返回发现的版本数 |
| T1.5.7 | `RemoveCustomTool` 已存在的自定义工具 | manifest 文件删除，config.yaml 更新 |
| T1.5.8 | `RemoveCustomTool` 不存在的工具 | 返回错误 |
| T1.5.9 | `ListCustomTools` 有 2 个自定义工具 | 返回 2 条记录 |
| T1.5.10 | `PreviewCustomToolVersions` 有效 GitHub 源 | 返回版本列表，不写 manifest |

## Phase 2: 前端

### 2.1 SettingsPage.vue — 新增自定义工具区块

在镜像配置 SectionCard 下方新增：
- SectionCard 标题"自定义工具"
- 添加表单：工具名、显示名、源 URL、源类型下拉(GitHub/HTML/npm)、版本正则(可选)、shim 名(可选)、环境变量名(可选)
- "预览版本"按钮 → 调用 PreviewCustomToolVersions，显示版本列表
- "添加"按钮 → 调用 AddCustomTool
- 已添加的自定义工具列表（从 ListCustomTools 加载），每项可删除

### 2.2 AppContent.vue — 新增 state 和函数

- `customTools` ref、`customToolSubmitting` ref、`customToolPreview` ref
- `loadCustomTools()`、`submitAddCustomTool()`、`removeCustomTool()`、`previewCustomToolVersions()`
- onMenuSelect 中 settings 分支加载自定义工具

### 2.3 i18n

en.ts / zh.ts 新增 `custom_tools.*` 相关 key。

### 2.4 验证
- `npm run build` 通过
- `npm run test:unit -- --run` 通过

### 2.5 测试用例与预期结果

| 用例 | 操作 | 预期结果 |
|---|---|---|
| T2.5.1 | 填写表单并点击"预览版本" | 调用 PreviewCustomToolVersions，显示版本列表 |
| T2.5.2 | 点击"添加"按钮 | 调用 AddCustomTool，成功后工具列表刷新 |
| T2.5.3 | 点击删除按钮 | 调用 RemoveCustomTool，列表更新 |
| T2.5.4 | 工具名为空时提交 | 添加按钮 disabled |
| T2.5.5 | 源 URL 为空时提交 | 添加按钮 disabled |

## Phase 3: 构建验证 + 测试 + 性能 + 安全审计

### 构建验证

| 用例 | 操作 | 预期结果 |
|---|---|---|
| T3.1 | `go build ./...` | 编译通过 |
| T3.2 | `go test ./...` | 全部通过 |
| T3.3 | `npm run build` | 前端构建通过 |
| T3.4 | `wails build` | GUI 构建通过 |
| T3.5 | GUI 中添加自定义工具 | 工具出现在工具目录页 |
| T3.6 | 安装自定义工具的版本 | TOFU 机制生效 |
| T3.7 | 对自定义工具运行 sniff | 新版本被发现并更新 manifest latest |

### 功能测试

| 用例 | 操作 | 预期结果 |
|---|---|---|
| T3.8 | 添加自定义工具，源为 GitHub `oven-sh/bun` | 自动发现版本，生成 manifest，工具出现在列表中 |
| T3.9 | 添加自定义工具，源为 HTML 页面 | 自动发现版本，生成 manifest |
| T3.10 | 预览版本功能 | 返回版本列表，不写入 manifest |
| T3.11 | 删除自定义工具 | on-disk manifest 删除，工具从列表消失 |

### 性能约束测试

| 用例 | 操作 | 预期结果 | 性能指标 |
|---|---|---|---|
| T3.P1 | `PreviewCustomToolVersions` 对 GitHub 源 | 返回版本列表 | 响应时间 < 5s |
| T3.P2 | `AddCustomTool` 含版本发现 + manifest 生成 + 写入 | 成功添加 | 总耗时 < 10s |
| T3.P3 | `ListCustomTools` 读取 10 个自定义工具 | 返回列表 | 响应时间 < 50ms |
| T3.P4 | `GenerateCustomManifest` 对 50 个发现版本生成 YAML | 返回有效 YAML | 耗时 < 30ms |

### 回归安全审计

| 用例 | 审计维度 | 操作 | 预期结果 |
|---|---|---|---|
| T3.S1 | 路径穿越 | 工具名 `../etc/passwd` | 被拒绝，返回错误，不创建文件 |
| T3.S2 | 路径穿越 | 工具名 `a/b` | 被拒绝（含 `/` 字符） |
| T3.S3 | 内置工具覆盖 | 自定义工具名 `go`（与内置同名） | on-disk 覆盖 embedded（预期行为），不报错，但提示用户"将覆盖内置工具" |
| T3.S4 | 恶意源 URL | 非 HTTPS 源 `http://evil.com/releases` | 被拒绝 |
| T3.S5 | manifest 注入 | 工具名含 YAML 特殊字符 `: \n` | 被工具名正则 `^[a-z0-9-]+$` 拦截 |
| T3.S6 | 现有工具不受影响 | 添加/删除自定义工具后检查内置工具列表 | 内置工具 manifest 无变化 |
| T3.S7 | config.yaml 安全 | 添加自定义工具后检查 config.yaml 完整性 | config.yaml 格式正确，无注入 |
| T3.S8 | TOFU 机制不变 | 安装自定义工具版本（sha256 为空） | TOFU 机制生效，计算 SHA256 并缓存 |
| T3.S9 | sniff 安全 | 对自定义工具运行 sniff | 仅更新 on-disk manifest，不影响 embedded |
| T3.S10 | 删除安全 | 删除自定义工具后检查 | on-disk manifest 文件删除，config.yaml 中记录删除，catalog 重新加载后工具消失 |

## 批准记录 (Approval History)

> **批准ID**: `2026-08-08-plan-015-001`
