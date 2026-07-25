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
- **状态**：Deferred — 需要重资源
- **描述**：替换 32 条 Android manifest 占位 hash，解决 5 组同 URL 不同 SHA 冲突
- **依赖**：无
- **阻塞原因**：29GB 下载量不适合在常规开发会话中执行
- **后续方案**：在专用 CI job 或长时间运行的环境中执行 `toolsHashFullCheck`

### #2 `.yaml.sig` 重签
- **状态**：Deferred — 依赖 #1
- **描述**：SHA audit 修改 manifest 后签名失效，需用 `XDEV_SIGNING_PRIVATE_KEY` 重新签名
- **依赖**：#1 完成后才能执行
- **后续方案**：`xdev sign-manifests --manifests-dir manifests --priv-key keys/priv.key`

### #3 不可修复工具文档化
- **状态**：Deferred — 待实施
- **描述**：Haskell、Kotlin、Ninja、UV 无可用镜像，需创建文档记录
- **依赖**：无
- **后续方案**：在 `docs/history/` 下创建 `unfixable-tools.md` 记录每个工具的不可修复原因和替代方案

### #4 TRANSIENT 错误重试
- **状态**：Deferred — 待实施
- **描述**：16 条 TRANSIENT 下载错误需重试验证或建立 CI 豁免机制
- **依赖**：无
- **后续方案**：在 `internal/linkaudit/ci_gate.go` 中增加 `knownTransient` 豁免列表，CI gate 对已记录的 TRANSIENT URL 降级为 warn 而非 block