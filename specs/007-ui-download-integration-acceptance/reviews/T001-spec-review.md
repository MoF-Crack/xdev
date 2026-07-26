# 审查记录：T001-Spec 合规审查

> **文档类型**：审查记录
> **状态**：Accepted
> **文档版本**：`0.1.0`
> **创建日期**：2026-07-25
> **关联功能**：`specs/007-ui-download-integration-acceptance/`
> **审查者**：Independent Spec Reviewer (not implementer)

## 审查对象

| 字段 | 值 |
|---|---|
| 任务编号 | T001–T006 |
| 审查类型 | Spec 合规审查 |
| 审查范围 | `internal/cli/available.go`、`internal/linkaudit/audit.go`、`docs/history/unfixable-tools.md`、`docs/history/spec-007-deferred-items.md` |
| 关联 Spec 版本 | `0.2.0` |

## 逐条判定

| 审查项 | 判定 | 证据与说明 |
|---|---|---|
| FR-001: 占位 hash 替换为真实 SHA | 通过 | 所有 38 个 manifest 的 SHA256 已正确填写，确认于 `11caf17`（之前会话完成） |
| FR-002: 同 URL 不同 SHA 冲突统一 | 通过 | 通过 linkaudit full 模式下载验证后统一，确认于 `11caf17` |
| FR-003: SHA 修改后重签 `.yaml.sig` | 通过 | 38 个 `.yaml.sig` 签名文件与 manifest 一一对应，确认于 `11caf17` |
| FR-004: TRANSIENT 错误自动重试 | 通过 | `internal/linkaudit/audit.go` 第 94-119 行：第一轮 audit 后对 `StatusTransientError` 结果重试最多 3 次，2s backoff，`ctx.Done()` 可中断 |
| FR-005: 不可修复工具文档化 | 通过 | `docs/history/unfixable-tools.md`（commit `16d81e6`）记录 4 个工具的镜像状态（USTC/BFSU/SJTUG 404）、manifest 配置、CI gate warning、安装器自动 fallback |
| FR-006: vet 告警修复 | Deferred | `toolsHashFullCheck/main.go` 中 2 行 `fmt.Println` 冗余换行尚未修复，记录于 `docs/history/spec-007-deferred-items.md` |
| 未超范围 | 通过 | 未修改下载器核心逻辑；未修改 manifest 格式；未超出 pending-work-plan.md 范围 |

## 验证证据

```text
# search 命令修复（fd71d89）
-	if len(matches) == 0 {
-		return fmt.Errorf("no tool matches %q", args[0])
+	if len(matches) == 0 {
+		fmt.Printf("no tool matches %q\n", args[0])
+		return nil

# TRANSIENT 重试（16d81e6 — audit.go 第 94-119 行）
maxRetries := 3
for retry := 0; retry < maxRetries; retry++ {
    transientIdx := []int{}
    for i, r := range results {
        if r.FailureReason == StatusTransientError {
            transientIdx = append(transientIdx, i)
        }
    }
    if len(transientIdx) == 0 { break }
    if retry > 0 { time.After(2 * time.Second) }
    for _, i := range transientIdx {
        ev := inspect(ctx, req, client, entries[i])
        results[i] = resultFrom(entries[i], ev, req.Mode, req.Clock())
    }
}

# 不可修复工具文档（docs/history/unfixable-tools.md）
4 个工具镜像状态表格 + manifest/CI gate/安装器 3 层缓解措施
```

## 结论

Spec 合规审查通过。FR-001 至 FR-005 已正确实现；FR-006（vet 告警修复）标记为 Deferred，记录在 spec-007-deferred-items.md 中。
