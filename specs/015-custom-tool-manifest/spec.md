# Spec 015: 用户自定义工具——自动版本发现 + 动态 manifest 生成

> **文档类型**：Feature Spec
> **状态**：Approved
> **文档版本**：1.0.0
> **创建日期**：2026-08-08
> **最后更新**：2026-08-20
> **所有者**：xinyan <zuijiuhjd@foxmail.com>
> **权威范围**：用户自定义工具自动发现与动态 manifest 的目标、范围和验收
> **替代文档**：无

## 背景与动机

xdev 的工具列表完全由内置 manifest YAML 驱动。用户无法添加自定义工具（如仓颉、内部工具链、私有 SDK），必须手动编写 YAML。

用户要求：只需提供工具名和下载源地址，xdev 自动发现版本、生成 manifest、后续自动同步。**不需要用户手动维护版本列表。**

## 范围

### 功能

用户在设置页"自定义工具"区域：
1. 输入工具名 + 下载源 URL（GitHub releases / HTML 页面 / npm registry）
2. xdev 自动嗅探该源，发现所有可用版本
3. 自动生成完整 manifest YAML（含版本列表、archive URL、shim 名推断）
4. 写入 `$XMDEV_HOME/manifests/<tool>.yaml`
5. 工具立即出现在工具目录页，可安装/切换
6. 后续 sniff 自动发现新版本并更新 manifest（与内置工具一致）

### 不在范围

- 不修改内置 manifest
- 不实现自定义 sniff 协议（复用现有 GitHub/HTML/JSON sniffer）

## 实现设计

### 用户输入

用户只需提供：

| 字段 | 说明 | 示例 |
|---|---|---|
| 工具名 | 仅 `[a-z0-9-]` | `cangjie` |
| 显示名 | 可选 | `Cangjie` |
| 下载源 URL | GitHub `owner/repo` 或完整 HTML 页面 URL | `cangjie-lang/cangjie` 或 `https://cangjie-lang.cn/download` |
| 源类型 | 自动推断或用户选择：github / html / npm | `github` |
| 版本过滤正则 | 可选，默认 `v?(\d+\.\d+\.\d+)$` | `cangjie-sdk-.+-(\d+\.\d+\.\d+)` |
| shim 名 | 可选，默认用工具名 | `cjc` |
| 环境变量名 | 可选，默认 `<TOOLNAME>_HOME` | `CANGJIE_HOME` |

### 自动 manifest 生成流程

```
用户提交 →
  1. 根据源类型选择 sniffer（github/html/npm）
  2. 调用 remote.DiscoverVersions() 发现版本列表
  3. 对每个版本，从发现的 archive URL 推断 format（zip/tar.gz）
  4. 生成 manifest YAML：
     - name, display_name, shims, env_vars
     - versions[] 含 version/archive/url/format/sha256(空,走TOFU)
     - remote 块（供后续 sniff 使用）
     - latest = 发现的最新版本
  5. 写入 $XMDEV_HOME/manifests/<tool>.yaml
  6. 重新加载 catalog，工具出现在列表中
```

### 后端

**新增 GUI binding 方法**：

- `AddCustomTool(name, displayName, sourceURL, sourceType, pattern, shim, envVar string) *CustomToolResult`
  - 校验工具名（`[a-z0-9-]`，防路径穿越）
  - 调用 sniffer 发现版本
  - 生成 manifest YAML 并写入 on-disk
  - 返回结果（成功/失败 + 发现的版本数）

- `RemoveCustomTool(name string) error`
  - 删除 on-disk manifest 文件
  - 重新加载 catalog

- `ListCustomTools() []CustomToolInfo`
  - 列出 on-disk manifest 中非内置的工具

- `PreviewCustomToolVersions(sourceURL, sourceType, pattern string) []string`
  - 预览嗅探结果，让用户确认源地址有效后再提交

**manifest.go**：
- 新增 `GenerateCustomManifest(name, displayName, shim, envVar string, versions []DiscoveredVersion) ([]byte, error)`
- 自动推断 archive format（从 URL 后缀：.zip→zip, .tar.gz→tar.gz）
- 自动生成三个平台条目（如果源只提供一个平台的 URL，其他平台留空或用相同 URL）

**config.go**：
- 新增 `CustomTools []CustomToolConfig` 字段，记录用户添加的自定义工具元数据（名称、源 URL、源类型），用于列表展示和删除

### 前端

**SettingsPage.vue 新增"自定义工具"区块**：
- 在镜像配置下方新增 SectionCard
- 添加表单：工具名、显示名、源 URL、源类型（下拉：GitHub/HTML/npm）、版本正则（可选）、shim 名（可选）、环境变量名（可选）
- "预览版本"按钮 → 调用 `PreviewCustomToolVersions`，显示发现的版本列表
- "添加"按钮 → 调用 `AddCustomTool`，成功后刷新工具列表
- 已添加的自定义工具列表，每项可删除

### 安全

- 工具名校验：`^[a-z0-9-]+$`，拒绝 `..`、`/`、`\` 等路径字符
- 源 URL 校验：必须是 HTTPS 或已知安全域名
- 生成的 manifest 文件路径固定为 `$XMDEV_HOME/manifests/<name>.yaml`，不可路径穿越
- SHA256 走 TOFU 机制（spec 014 已实现）

## 验收标准

1. `go build ./...` 通过
2. `go test ./...` 通过
3. `npm run build` + `wails build` 通过
4. 用户输入工具名 + GitHub `owner/repo` → 自动发现版本 → manifest 生成 → 工具出现在列表中
5. 用户输入工具名 + HTML 页面 URL → 自动发现版本 → manifest 生成 → 工具出现在列表中
6. 预览版本功能正常工作
7. 删除自定义工具后从列表消失
8. 自定义工具可安装（走 TOFU）
9. 后续 sniff 自动更新自定义工具的版本和 latest
10. 工具名校验防止路径穿越
11. 自定义工具配置持久化到 config.yaml

## 测试用例与预期结果

| 用例 | 操作 | 预期结果 |
|---|---|---|
| T1 | 添加自定义工具，源为 GitHub `oven-sh/bun` | 自动发现版本列表，生成 manifest，工具出现在列表中 |
| T2 | 添加自定义工具，源为 HTML `https://cangjie-lang.cn/download` | 自动发现版本，生成 manifest |
| T3 | 预览版本功能 | 返回版本列表，不写入 manifest |
| T4 | 工具名含 `../` | 拒绝，返回错误 |
| T5 | 工具名含大写字母 | 拒绝，仅允许小写 |
| T6 | 删除自定义工具 | on-disk manifest 删除，工具从列表消失 |
| T7 | 添加后运行 sniff | sniff 发现新版本并更新 manifest latest |
| T8 | 安装自定义工具的版本 | TOFU 机制生效，计算 SHA256 并缓存 |

## 性能约束

| 用例 | 性能指标 |
|---|---|
| P1 | `PreviewCustomToolVersions` 对 GitHub 源的响应时间 < 5s |
| P2 | `AddCustomTool` 含版本发现 + manifest 生成 + 写入 < 10s |
| P3 | `ListCustomTools` 响应时间 < 50ms |

## 回归安全审计

| 用例 | 审计维度 | 预期结果 |
|---|---|---|
| S1 | 路径穿越 | 工具名 `../etc/passwd` 被拒绝 |
| S2 | 内置工具覆盖 | 自定义工具名与内置同名时，on-disk 覆盖 embedded（预期行为，不报错） |
| S3 | 恶意源 URL | 非 HTTPS 源被拒绝 |
| S4 | manifest 注入 | 生成的 YAML 中用户输入被正确转义 |
| S5 | 现有工具不受影响 | 添加/删除自定义工具不影响内置工具的 manifest |

## 风险

- **源 URL 无响应**：sniff 超时或 404 → 返回错误，不生成 manifest
- **版本发现结果为空**：返回错误，不生成空 manifest
- **archive URL 推断失败**：URL 无明确后缀时默认用 zip，用户安装时可能解压失败
- **GitHub API 限流**：预览/添加时可能被限流，需重试或提示用户

## 批准记录 (Approval History)

| 阶段 | 状态 | 批准者 | 日期 | 文档版本 | 批准范围 |
|---|---|---|---|---|---|
| Spec 批准 | Approved | `xinyan <zuijiuhjd@foxmail.com>` | 2026-08-08 | `1.0.0` | `2026-08-20-spec-015-approved-001` 用户自定义工具自动发现与动态 manifest 的目标、范围和验收 |
