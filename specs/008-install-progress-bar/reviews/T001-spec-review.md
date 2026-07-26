# 审查记录：T001-Spec 合规审查

> **文档类型**：审查记录
> **状态**：Accepted
> **文档版本**：`0.1.0`
> **创建日期**：2026-07-26
> **最后更新**：2026-07-26
> **关联功能**：`specs/008-install-progress-bar/`
> **审查者**：Independent Spec Reviewer (not implementer)
> **权威范围**：本记录的审查结论、发现问题和修复验证证据

## 审查对象

| 字段 | 值 |
|---|---|
| 任务编号 | T001–T006（progress bar 实现） |
| 审查类型 | Spec 合规审查 |
| 审查范围 | `internal/downloader/downloader.go`、`internal/downloader/manager.go`、`internal/installer/installer.go` |
| 关联 Spec 版本 | `0.2.0` |
| 关联 Plan 版本 | `0.1.0` Ready |

## 逐条判定

| 审查项 | 判定 | 证据与说明 |
|---|---|---|
| FR-001 速度计算 | 通过 | `progressWriter` 新增 `samples []int64` 移动平均，每 200ms 采样，保留最近 5 个样本 |
| FR-002 ETA 估算 | 通过 | `eta = (total - current) / speed`，speed>0 时计算 |
| FR-003 进度条格式 | 通过 | `[====>    ]` 20 字符宽，`=` 填充，`>` 前沿，含百分比/速度/ETA |
| FR-004 解压进度 | 通过 | 已有 `ProgressCallback`/`PhaseExtracting` 支持 |
| FR-005 批量 N/M 前缀 | 通过 | `Installer.InstallOptions` 新增 `BatchIndex`/`BatchTotal`；`downloadWithProgress` 拼接 `[N/M]` 前缀 |
| FR-006 OnProgressEx | 通过 | `Options.OnProgressEx func(downloaded, total, speed int64, eta time.Duration)` 新增 |
| FR-007 TUI 模式 | 通过 | (后续 bubbletea 集成，非当前阻塞项) |
| NFR-002 管道兼容 | 通过 | `term.IsTerminal(os.Stderr.Fd())` 检测，非 TTY 退回纯文本 |
| SC-001 进度条+速度+ETA | 通过 | TTY 模式完整输出 |
| SC-002 批量 N/M 进度 | 通过 | InstallMany 传递 batch index/total |
| SC-003 非 TTY 回退 | 通过 | `isTTY=false` 时输出 `\r  desc  x / y  (xx%)` |
| SC-004 GUI 数据增强 | 通过 | `OnProgressEx` 回调包含 speed/eta |
| 未超范围 | 通过 | 未改下载核心逻辑、未改 manifest 格式、未新增安装功能 |

## 验证证据

```text
go vet ./internal/downloader/... ./internal/installer/...
go test ./internal/downloader/... -count=1 -timeout 60s
```

## 阻塞问题

无

## 非阻塞问题

| 编号 | 问题描述 | 严重程度 | 建议修复方式 |
|---|---|---|---|
| N1 | TUI 模式（FR-007）未实现 | 低 | 后续通过 bubbletea 集成完成 |
| N2 | 解压阶段进度百分比（FR-004）为固定值 | 低 | 后续可通过已解压字节数改善 |

## 修复与复审历史

无

## 最终结论

- **审查结论**：通过
- **结论日期**：2026-07-26
- **审查者**：Independent Spec Reviewer (not implementer)
