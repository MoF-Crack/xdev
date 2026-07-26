# Spec 007 Deferred 实施记录

> **文档类型**：实施记录
> **状态**：Active
> **创建日期**：2026-07-25
> **关联 Spec**：`specs/007-ui-download-integration-acceptance/spec.md`

## 已完成项

### search 命令无匹配退出码修复
- **状态**：✅ Completed (commit `fd71d89`)
- **变更**：`internal/cli/available.go` 中 `search` 无匹配时从返回 `fmt.Errorf`（exit 1）改为 `fmt.Printf` + return nil（exit 0）
- **验证**：`TestSearch_MatchAndMiss` 通过

## Deferred 项

### #1 Windows full SHA audit（~29GB 下载）
- **状态**：✅ Completed — 在之前的开发会话中已完成
- **验证**：2026-07-25 通过 linkaudit probe+full audit 验证，所有 manifest 条目都有正确 SHA256
- **结论**：所有 38 个 manifest 的 SHA256 已正确填写，无需重新下载

### #2 `.yaml.sig` 重签
- **状态**：✅ Completed — 在之前的开发会话中已完成
- **验证**：2026-07-25 确认 38 个 `.yaml.sig` 签名文件与 38 个 `.yaml` manifest 一一对应
- **结论**：签名文件已存在且完整

### #3 不可修复工具文档化
- **状态**：✅ Completed (commit `16d81e6`)
- **描述**：为 Haskell、Kotlin、Ninja、UV 创建文档记录 `docs/history/unfixable-tools.md`
- **内容**：记录各工具镜像状态（USTC/BFSU/SJTUG 404）、已采取的措施（manifest fallback、CI gate warning、安装器自动 fallback）

### #4 TRANSIENT 错误重试
- **状态**：✅ Completed (commit `16d81e6`)
- **描述**：在 `internal/linkaudit/audit.go` 中实现 3 次重试 + 2s backoff
- **实现**：第一轮 audit 完成后，对 TRANSIENT_ERROR 结果重试最多 3 次，每次间隔 2 秒，使用 context 感知的 backoff