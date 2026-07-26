# Tasks: 下载源 Mirror 智能选择

> **文档类型**：Task List
> **状态**：Completed
> **创建日期**：2026-07-26
> **关联 Spec**：`specs/009-mirror-smart-selection/spec.md`
> **关联 Plan**：`specs/009-mirror-smart-selection/plan.md`

## 任务列表

| # | 任务 | 负责人 | 状态 | 审查 | 提交 |
|---|---|---|---|---|---|
| T001 | manifest Archive 扩展：添加 `MirrorURLs []string` 字段 + `EffectiveMirrorURLs()` 方法 | xinyan | Completed | Reviewed | 本次 |
| T002 | 新增 `internal/installer/mirror_selector.go`：MirrorHealthCache 实现 | xinyan | Completed | Reviewed | 本次 |
| T003 | 新增 `internal/installer/mirror_selector.go`：SelectBestMirror 并发探测实现 | xinyan | Completed | Reviewed | 本次 |
| T004 | config CoreConfig 添加 `MirrorPreference []string` 字段 | xinyan | Completed | Reviewed | 本次 |
| T005 | `BuildDownloadOptions` 增强：候选列表构建 + 智能选择集成 | xinyan | Completed | Reviewed | 本次 |
| T006 | 双重审查（Spec 合规 + 工程质量） | xinyan | Completed | Reviewed | 本次 |

## 验收清单

- [x] FR-001: manifest 支持 mirror_urls 复数字段，向后兼容 mirror_url 单数
- [x] FR-002: BuildDownloadOptions 从候选 mirror 列表中选择最佳 mirror
- [x] FR-003: 并发 HEAD 探测候选 mirror（超时 3s），选择第一个可用或延迟最低的
- [x] FR-004: mirror 不可用结果缓存到 $XMDEV_HOME/cache/mirror-health.json
- [x] FR-005: 缓存条目 24 小时过期，过期后重新探测
- [x] FR-006: config.yaml 支持 mirror_preference 列表，优先于 manifest 默认顺序
- [x] FR-007: 所有 mirror 不可用时自动降级到 official URL，输出 WARNING
- [x] FR-008: mirror 探测不阻塞 UI（探测在 downloadWithProgress 内部，异步回调机制兼容）
- [x] SC-001: 配置了多 mirror 的工具安装时自动选择最快的可用 mirror
- [x] SC-002: 已知 404 的 mirror 在 24 小时内不再被尝试
- [x] SC-003: 用户配置 mirror_preference 后安装优先使用偏好 mirror
- [x] SC-004: 所有 mirror 不可用时安装仍能成功（降级到 official URL）
- [x] go vet 通过
- [x] go test 通过
