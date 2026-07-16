# 下载缓存空间回收方案

> **创建日期**：2026-07-15
> **状态**：待实施
> **关联**：`docs/superpowers/plans/2026-07-11-xdev-ui-download-sources.md` Task 17 后续

---

## 背景

xdev 的 full SHA audit 需要下载约 29GB 的工具归档文件。当前系统缺少完善的下载后空间回收机制，导致磁盘占用持续增长。本文档定义三类下载缓存场景的回收策略。

### 当前缓存现状

| 缓存类型 | 目录 | 命名规则 | 当前清理机制 | audit 后大小 |
|---|---|---|---|---|
| linkaudit full mode | `%TEMP%/xdev-linkaudit/` | `SHA256(URL).archive` | 无自动清理 | ~29GB |
| 运行时安装 archive | `<xmDevDir>/cache/` | `<tool>-<version>.<format>` | 仅 GUI 手动 `CleanCache` | 持续增长 |
| toolsHashFullCheck | `./downloads/` | 原始文件名 | `cleanup=true` 删成功条目 | ~29GB |

### 已确认的问题

1. linkaudit `Audit()` 函数无 cleanup 逻辑（`internal/linkaudit/audit.go`）
2. 运行时 installer 安装后 archive 永久保留，无 LRU 淘汰或大小限制（`internal/installer/installer.go:354-402`）
3. `toolsHashFullCheck` cleanup 只删成功条目，失败条目和 `.part` 残留（`pipeline.go:416-438`）
4. GUI `CleanCache` 仅支持全删或按类型删，无按版本/工具单独清理
5. 配置无 `cache_max_size` 或 `cache_retention_days` 字段（`internal/config/config.go:106-117`）

---

## 场景 1：linkaudit full audit 后自动清理

### 设计

#### Request 新增字段

```go
// internal/linkaudit/types.go
type Request struct {
    // ...existing fields...
    CleanupOnSuccess bool  // audit 全部完成后自动清理缓存
}
```

#### CLI flag

```
-cleanup  (默认 true)
```

`cmd/linkaudit/main.go` 新增 `-cleanup` flag，默认 `true`。用户可 `-cleanup=false` 保留缓存供调试。

#### 清理逻辑

在 `internal/linkaudit/audit.go` 的 `Audit()` 末尾：

```
if req.CleanupOnSuccess {
    1. 遍历 CacheDir 中所有 *.archive 文件
    2. 对每个文件，查找对应的 Result：
       - 如果 Result.FailureReason == "PASS" → 删除文件
       - 如果 Result.FailureReason == "HASH_MISMATCH" → 保留（供调试）
       - 如果 Result.FailureReason == "TRANSIENT_ERROR" → 保留（供重试）
       - 如果 Result.FailureReason == "URL_DEAD" → 删除（文件可能是错误页面）
    3. 删除所有 *.part 临时文件
    4. 统计清理的文件数和总大小，写入报告
}
```

#### 清理统计输出

在 JSON 报告末尾增加 `cleanup` 字段：

```json
{
  "cleanup": {
    "enabled": true,
    "filesDeleted": 150,
    "filesRetained": 3,
    "bytesFreed": 29845309440,
    "bytesRetained": 845678901
  }
}
```

Markdown 报告末尾增加摘要行：

```
## Cache Cleanup
- Enabled: true
- Files deleted: 150 (29.8 GB freed)
- Files retained: 3 (845.7 MB, for debugging)
```

### 预期结果

- full audit 全部 PASS 后 ~29GB 自动回收
- HASH_MISMATCH 文件保留供分析
- TRANSIENT_ERROR 文件保留供重试
- CLI 默认清理（`-cleanup=true`），调试时 `-cleanup=false`
- CI gate 运行后不留残留

### 第二方案：CLI 独立清理命令

如果方案 A 因 audit 逻辑耦合过复杂：

- 新增 `linkaudit -cleanup-cache -cache <dir>` 子命令
- 删除指定目录中所有 `*.archive` 和 `*.part`
- 不依赖 audit 结果，用户可在任何时候运行
- 输出清理统计

### 兜底方案：OS 层面清理

- Windows `%TEMP%` 在磁盘清理时自动清除
- 在 `docs/download-source-policy.md` 注明缓存位置和手动清理方法
- 无代码改动

### 改动文件

| 文件 | 改动 |
|---|---|
| `internal/linkaudit/types.go` | Request 加 `CleanupOnSuccess bool` |
| `internal/linkaudit/audit.go` | Audit 末尾加 cleanup 逻辑 + 报告统计 |
| `internal/linkaudit/cleanup.go`（新） | `CleanupCache(dir, results) (CleanupStats, error)` |
| `cmd/linkaudit/main.go` | 加 `-cleanup` flag（默认 true） |
| `internal/linkaudit/cleanup_test.go`（新） | 测试 PASS 删除、MISMATCH 保留、TRANSIENT 保留 |
| `internal/linkaudit/report.go` | JSON/Markdown 报告加 cleanup 统计字段 |

---

## 场景 2：运行时安装 archive 的 LRU 淘汰

### 设计

#### 配置项

在 `internal/config/config.go` 的 `CoreConfig` 新增：

```go
type CoreConfig struct {
    // ...existing fields...
    CacheMaxSizeMB    int64  `yaml:"cache_max_size_mb"    json:"cacheMaxSizeMb"`    // 0 = 不限制
    CacheRetentionDays int   `yaml:"cache_retention_days" json:"cacheRetentionDays"` // 0 = 不限制
}
```

- `CacheMaxSizeMB`：CacheDir 总大小上限（MB）。超过时按 LRU 删除最旧文件。默认 `0`（不限制，由用户决定）。
- `CacheRetentionDays`：archive 保留天数。超过此天数未访问的文件自动删除。默认 `0`（不限制，由用户决定）。

两个值均为 `0` 时不自动清理，保持当前行为（向后兼容）。

#### PruneCache 实现

新增 `internal/cache/prune.go`：

```go
type PruneResult struct {
    FilesDeleted int
    BytesFreed   int64
    BytesRetained int64
    Errors       []string
}

func PruneCache(cacheDir string, maxSizeMB int64, retentionDays int) (PruneResult, error)
```

逻辑：

1. 遍历 CacheDir，收集所有 archive 文件（`.zip`/`.tar.gz`/`.tgz`/`.7z`/`.exe`/`.tar.xz`）和对应 `.sha256` sidecar
2. 读取每个文件的 `ModTime`（Windows 下 `atime` 不可靠，使用 `ModTime` 作为最后修改时间代理）
3. 如果 `retentionDays > 0`：删除 `ModTime` 早于 `now - retentionDays*24h` 的文件
4. 如果 `maxSizeMB > 0`：按 `ModTime` 从旧到新排序，依次删除直到总大小 < maxSizeMB
5. 每删一个 archive 同时删对应 `.sha256` sidecar
6. 不删除 `.part`（活跃下载的临时文件）
7. 不删除非 archive 文件（如 `tool_scan_cache.json`）

#### 触发时机

在 `internal/installer/installer.go` 安装成功后调用：

```go
// 安装成功后
if i.cfg.Core.CacheMaxSizeMB > 0 || i.cfg.Core.CacheRetentionDays > 0 {
    result, err := cache.PruneCache(i.cfg.Core.CacheDir, i.cfg.Core.CacheMaxSizeMB, i.cfg.Core.CacheRetentionDays)
    if err != nil {
        // PruneCache 失败不阻断安装，仅记日志
        log.Printf("WARN: cache prune failed: %v", err)
    } else if result.FilesDeleted > 0 {
        log.Printf("cache prune: deleted %d files, freed %d bytes", result.FilesDeleted, result.BytesFreed)
    }
}
```

#### GUI 集成

`frontend/src/components/settings/CacheSettings.vue` 增加：
- 缓存上限输入（MB，0=不限制）
- 保留天数输入（天，0=不限制）
- 当前缓存总量显示
- 说明文字："设置为 0 则不自动清理。安装工具后自动按 LRU 淘汰旧缓存。"

新增后端 API：
- `GetCacheLimits() → { maxSizeMB, retentionDays }`
- `SetCacheLimits(maxSizeMB, retentionDays) → error`

### 预期结果

- 用户配置上限后，安装完成自动淘汰旧缓存
- 不配置（0）时保持当前行为
- `ModTime` 作为 LRU 依据（Windows `atime` 不可靠）
- PruneCache 失败不阻断安装
- `.sha256` sidecar 随 archive 一起清理

### 第二方案：仅手动清理 + 安装后提示

不增加自动清理，改善手动体验：
- 安装成功后 toast："Archive cached (2.3GB). Clean cache in Settings."
- CacheSettings 显示各 archive 大小和最后修改时间
- 支持按工具/版本单独删除

### 兜底方案：安装后立即删除 archive

- installer 安装成功后删除 CacheDir 中的 archive
- 无缓存命中能力（每次安装都要重新下载）
- 配置项 `cache_archives bool`（默认 `false` = 安装后删除）

### 改动文件

| 文件 | 改动 |
|---|---|
| `internal/config/config.go` | CoreConfig 加 `CacheMaxSizeMB` + `CacheRetentionDays` |
| `internal/cache/prune.go`（新） | `PruneCache()` + `PruneResult` |
| `internal/cache/prune_test.go`（新） | 测试大小淘汰、天数淘汰、sidecar 清理、不删 .part |
| `internal/installer/installer.go` | 安装成功后调用 `PruneCache` |
| `internal/gui/app_config.go` | `GetCacheLimits` / `SetCacheLimits` API |
| `frontend/src/components/settings/CacheSettings.vue` | 配置 UI |
| `frontend/src/i18n/locales/{en,zh}.ts` | 新增 i18n 文案 |

---

## 场景 3：toolsHashFullCheck cleanup 改进

### 设计

#### 修复 vet 告警

`toolsHashFullCheck/main.go:96` 和 `:103` 的 `fmt.Println` 参数末尾含 `\n`，`go vet` 报 `redundant newline`。

修改：
```go
// 第 96 行
// 前: fmt.Println("🔄 Resetting all failed entries to pending...\n")
// 后: fmt.Println("🔄 Resetting all failed entries to pending...")

// 第 103 行
// 前: fmt.Println("🔧 Finalize mode: writing hashed entries to manifests and cleaning up...\n")
// 后: fmt.Println("🔧 Finalize mode: writing hashed entries to manifests and cleaning up...")
```

#### cleanup 范围扩展

当前 `pipeline.go:416-438` 的 cleanup 只删成功条目。改进为：

1. `cleanup=true`（默认）时：
   - 删成功条目的 archive 文件
   - 删所有 `.part` 临时文件
   
2. 新增 `-cleanup-all` flag：
   - 删全部文件（成功 + 失败 + 临时）
   - 用于完全清空 `cacheDir`

3. 运行结束时输出清理统计：
   ```
   Cache cleanup: deleted 150 files (29.8 GB freed), retained 3 files (845.7 MB, for debugging)
   ```

#### 清理逻辑

```go
func (p *Pipeline) cleanupCache(successOnly bool) {
    entries := p.progress.GetAll()
    for _, entry := range entries {
        if successOnly && entry.Status != "success" {
            continue  // 只删成功的
        }
        if entry.CachePath != "" {
            os.Remove(entry.CachePath)
            os.Remove(entry.CachePath + ".sha256")
        }
    }
    // 删 .part
    filepath.Walk(p.cacheDir, func(path string, info os.FileInfo, err error) error {
        if strings.HasSuffix(path, ".part") {
            os.Remove(path)
        }
        return nil
    })
}
```

### 预期结果

- `go vet ./...` 全量通过（无 toolsHashFullCheck 告警）
- 正常运行后自动清理成功条目 + `.part`
- 失败条目保留供调试
- `-cleanup-all` 一键清空

### 第二方案：与 linkaudit 共享缓存目录

- `toolsHashFullCheck` 的 `cacheDir` 改为 `%TEMP%/xdev-linkaudit/`
- 命名规则统一为 `SHA256(URL).archive`
- 完成后调用 linkaudit 的清理逻辑

### 兜底方案：独立清理脚本

新增 `scripts/clean-downloads.ps1`：
- 接受目录参数（默认 `./downloads` 和 `%TEMP%/xdev-linkaudit/`）
- 删除 `*.archive`、`*.part`、`*.zip`、`*.tar.gz`、`*.7z`
- 输出清理统计

### 改动文件

| 文件 | 改动 |
|---|---|
| `toolsHashFullCheck/main.go` | 修复 vet + 新增 `-cleanup-all` flag |
| `toolsHashFullCheck/pipeline.go` | 扩展 cleanup 逻辑 + 统计输出 |

---

## 统一执行计划

### 依赖关系

```
[3] toolsHashFullCheck vet修复 + cleanup改进 ──→ 独立
[1] linkaudit 自动清理                    ──→ 独立于 [3]
[2] 运行时 LRU 淘汰                       ──→ 独立于 [1][3]，代码量最大
```

三者互不依赖，可并行实施。

### 建议执行顺序

1. **[3]** toolsHashFullCheck vet 修复 + cleanup 改进（2 行 vet + cleanup 逻辑，~30 分钟）
2. **[1]** linkaudit 自动清理（types + audit + CLI + 测试，~1 小时）
3. **[2]** 运行时 LRU 淘汰（config + prune + installer + GUI + 测试，~2 小时）

### 验证

- `go test ./internal/cache ./internal/linkaudit ./internal/installer ./internal/config -count=1`
- `go vet ./...`（含 toolsHashFullCheck）
- `go run ./cmd/linkaudit -cleanup -mode probe`（验证 cleanup flag 工作）
- `go run ./toolsHashFullCheck -help`（验证 -cleanup-all flag）
- 前端 `npm run test:unit` + `npm run test:e2e`（验证 CacheSettings UI）
