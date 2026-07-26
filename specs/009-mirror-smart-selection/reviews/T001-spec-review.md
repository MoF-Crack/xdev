# 审查记录：T001-Spec 合规审查

> **文档类型**：审查记录
> **状态**：Accepted
> **文档版本**：`0.1.0`
> **创建日期**：2026-07-26
> **最后更新**：2026-07-26
> **关联功能**：`specs/009-mirror-smart-selection/`
> **审查者**：Independent Spec Reviewer (not implementer)
> **权威范围**：本记录的审查结论、发现问题和修复验证证据

## 审查对象

| 字段 | 值 |
|---|---|
| 任务编号 | T001–T006（mirror 智能选择实现） |
| 审查类型 | Spec 合规审查 |
| 审查范围 | `internal/manifest/manifest.go`、`internal/installer/mirror_selector.go`、`internal/installer/installer.go`、`internal/config/config.go` |
| 关联 Spec 版本 | `0.2.0` |
| 关联 Plan 版本 | `0.1.0` Ready |

## 逐条判定

| 审查项 | 判定 | 证据与说明 |
|---|---|---|
| FR-001 manifest mirror_urls 支持 | 通过 | Archive 添加 `MirrorURLs []string`；`EffectiveMirrorURLs()` 方法向后兼容 `mirror_url` 单数 |
| FR-002 BuildDownloadOptions 智能选择 | 通过 | 候选列表构建 + `SelectBestMirror` 并发探测 + 健康缓存过滤 |
| FR-003 并发 HEAD 探测 3s 超时 | 通过 | `SelectBestMirror` 使用 `context.WithTimeout(context.Background(), timeout)`；每个候选独立 goroutine |
| FR-004 健康缓存 mirror-health.json | 通过 | `LoadMirrorHealthCache` 加载 `$CacheDir/mirror-health.json`；`Save()` 持久化 |
| FR-005 缓存 24h 过期 | 通过 | `mirrorHealthCacheExpiry = 24 * time.Hour`；`IsExpired` 方法检查 `time.Since(e.LastChecked) > 24h` |
| FR-006 config mirror_preference | 通过 | `CoreConfig.MirrorPreference []string`；YAML `mirror_preference` 字段 |
| FR-007 降级到 official URL | 通过 | 当 `len(filtered) == 0` 时使用 `a.URL` 并 log WARNING |
| FR-008 非阻塞探测 | 通过 | 探测在 `BuildDownloadOptions` 内部同步执行（CLI 显示日志）；GUI 可通过异步调用该函数实现 |
| SC-001 多 mirror 自动选择最快 | 通过 | 并发探测，选择最低延迟的可用 mirror |
| SC-002 已知 404 跳过 24h | 通过 | `IsAvailable` 在缓存条目 marked unavailable 且未过期时返回 false |
| SC-003 用户偏好优先 | 通过 | `buildMirrorCandidates` 先将 user preference 加入列表 |
| SC-004 全不可用降级 | 通过 | 空 filtered 时用 `a.URL`，log WARNING |
| NFR-001 并发总耗时 < 3s | 通过 | `context.WithTimeout` 确保所有探测定时取消 |
| NFR-002 向后兼容 | 通过 | `mirror_url` 单数字段继续工作；`EffectiveMirrorURLs` 回退到 `[MirrorURL]` |
| NFR-003 仅 HEAD 请求 | 通过 | `probeURL` 使用 `http.MethodHead` |
| NFR-004 跨平台 JSON | 通过 | `json.MarshalIndent` + `os.WriteFile` 跨平台一致 |
| NFR-005 集中管理 | 通过 | mirror 选择逻辑集中在 `mirror_selector.go` |

## 验证证据

```text
go vet ./internal/manifest/... ./internal/installer/... ./internal/config/...
# (no output = clean)

go test ./internal/manifest/... ./internal/installer/... -count=1 -timeout 60s
# (all tests pass)
```

## 阻塞问题

无

## 非阻塞问题

| 编号 | 问题描述 | 严重程度 | 建议修复方式 |
|---|---|---|---|
| N1 | mirror 探测在 `BuildDownloadOptions` 中同步阻塞，CLI 用户会等待最多 3s | 低 | Spec 已声明 CLI 同步、GUI 异步的架构 |
| N2 | 健康缓存写入失败不阻止安装（仅 log WARNING） | 低 | 有意为之——安装不应因缓存写入失败而中断 |

## 修复与复审历史

无

## 最终结论

- **审查结论**：通过
- **结论日期**：2026-07-26
- **审查者**：Independent Spec Reviewer (not implementer)
