# Spec 020: 缓存归档保留策略

> **文档类型**：Feature Spec
> **状态**：Approved
> **文档版本**：1.0.0
> **创建日期**：2026-08-01
> **最后更新**：2026-08-20
> **所有者**：xinyan <zuijiuhjd@foxmail.com>
> **权威范围**：缓存归档保留策略的目标、范围和验收
> **替代文档**：无
> **Constitution**：`1.0.0`
## 问题

下载的安装包归档（如 `llvm-22.1.8.tar.xz` 822 MB、`java-25.zip` 205 MB）目前安装成功后**立即删除**。用户希望自行决定保留策略：
- 立即删除（当前行为）
- 保留 7 天 / 30 天
- 永久保留

保留的目的是避免重复下载（换版本、修复重装时可直接用缓存）。

## 现状

| 组件 | 现状 |
|------|------|
| `app_install.go` | 安装成功后 `toolName-*` glob 匹配 `.zip/.tar.gz/.tgz/.sha256` 并 `os.Remove`（无条件立即删） |
| `config.go` `CoreConfig` | 无保留策略字段 |
| `CacheSettings.vue` | 仅展示分类占用 + 手动清理按钮，无保留策略设置 |
| 缓存文件 | `cache/<tool>-<version>.<ext>`（含 `.part`/`.sha256` 伴生文件） |

## 修复

### F1: 配置 — 归档保留策略

`internal/config/config.go` `CoreConfig` 新增：

```go
// ArchiveRetention 下载归档的保留策略：
// "immediate" 安装成功立即删除 | "7d" 保留 7 天 | "30d" 保留 30 天 | "forever" 永久保留
ArchiveRetention string `yaml:"archive_retention"`
```

新增方法（参照 `GetPathMode`/`SetPathMode` 模式）：
```go
func (c *Config) GetArchiveRetention() string // 默认 "immediate"
func (c *Config) SetArchiveRetention(v string) error // 校验 immediate|7d|30d|forever
```

### F2: 安装成功后按策略处理

`internal/gui/app_install.go` 安装完成清理段（约 L321-335）改为调用新 helper：

```go
// applyArchiveRetention 按配置策略处理安装成功后的缓存归档。
// immediate → 删除；7d/30d/forever → 保留（7d/30d 由启动清理兜底）。
func (a *App) applyArchiveRetention(toolName string)
```

- `immediate`：现有删除逻辑不变
- `7d` / `30d` / `forever`：**不删除**，仅保留日志

### F3: 启动/定期清理过期归档

新增 `internal/gui/app_cache_retention.go`（或并入 app_config.go）：

```go
// CleanupExpiredArchives 删除超过保留期限的归档（7d/30d 策略）。
// 依据文件 ModTime；forever/immediate 跳过。返回删除数量。
func (a *App) CleanupExpiredArchives() int
```

- 在 `app.go` `startup()`（或 `domReady` 延迟任务）中调用一次，作为兜底清理
- 匹配 `cache/` 下的归档文件（`.zip/.tar.gz/.tgz/.tar.xz/.7z/.exe/.msi/.dmg/.jar/.sh` 等，可复用 `app_size_breakdown.go` 的 `cacheArchiveSuffixes` 思路），同时删除伴生的 `.sha256`/`.part` 文件
- 删除后调用 `InvalidateSizeBreakdown()` 刷新明细缓存

### F4: GUI 绑定

```go
// GetArchiveRetention 返回当前保留策略（"immediate"|"7d"|"30d"|"forever"）。
func (a *App) GetArchiveRetention() string

// SetArchiveRetention 更新保留策略并保存配置。
func (a *App) SetArchiveRetention(policy string) error
```

### F5: 前端 — 设置页下拉

`CacheSettings.vue` 新增「归档保留策略」下拉（FormSection 内）：
- 选项：立即删除 / 保留 7 天 / 保留 30 天 / 永久保留
- 通过 props `retention` + `@update:retention` 事件联动
- `SettingsPage.vue` 透传；`AppContent.vue` 新增 `archiveRetention` ref，加载时 `GetArchiveRetention()`，变更时 `SetArchiveRetention()` + 成功提示
- i18n zh/en：
  - `settings.archive_retention`: 归档保留策略 / Archive Retention
  - `settings.retention_immediate`: 立即删除 / Delete after install
  - `settings.retention_7d`: 保留 7 天 / Keep 7 days
  - `settings.retention_30d`: 保留 30 天 / Keep 30 days
  - `settings.retention_forever`: 永久保留 / Keep forever
  - `settings.retention_saved`: 保留策略已保存 / Retention policy saved

## 验证

1. `go test ./internal/gui/ -short`（新增：Get/SetArchiveRetention 校验、CleanupExpiredArchives 按 mtime 删除、immediate 安装后删除/保留策略不删）
2. 前端 `vite build` + CacheSettings 相关测试（如有）
3. 手动：
   - 设置 → 归档保留策略 = 保留 30 天 → 安装某工具 → 缓存归档**保留**
   - 手动改旧文件 mtime 超过 30 天 → 重启 → 归档被清理
   - 保留策略 = 立即删除 → 安装后归档删除（回归）
4. `wails build`


## 批准记录 (Approval History)

| 阶段 | 状态 | 批准者 | 日期 | 文档版本 | 批准范围 |
|---|---|---|---|---|---|
| Spec 批准 | Approved | `xinyan <zuijiuhjd@foxmail.com>` | 2026-08-08 | `1.0.0` | `2026-08-20-spec-020-approved-001` 缓存归档保留策略的目标、范围和验收 |
