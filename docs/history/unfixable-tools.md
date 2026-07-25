# 不可修复工具镜像状态记录

> **文档类型**：技术债记录
> **状态**：Active
> **创建日期**：2026-07-25
> **关联 Spec**：`specs/007-ui-download-integration-acceptance/spec.md` 项 #3

## 背景

Spec 007 项 #3 要求为 Haskell、Kotlin、Ninja、UV 四个工具创建文档记录，
因为它们的镜像 URL 在 SHA audit 过程中返回 404，无法通过镜像下载验证。

## 镜像状态

| 工具 | 镜像源 | 状态 | 官方源 |
|---|---|---|---|
| Haskell/GHC | USTC/BFSU/SJTUG 镜像均 404 | 镜像不可用 | `downloads.haskell.org` |
| Kotlin | JetBrains CDN 404 | 镜像不可用 | GitHub release |
| Ninja | npmmirror 404, TUNA github-release 可用 | 部分镜像可用 | GitHub release |
| UV | npmmirror 404, TUNA github-release 可用 | 部分镜像可用 | GitHub release |

## 已采取的措施

### Manifest 层面

1. **Haskell**：mirror_url 指向 TUNA（`mirrors.tuna.tsinghua.edu.cn/haskell/`），
   部分版本可用。官方源 `downloads.haskell.org` 作为 fallback。
2. **Kotlin**：mirror_url 指向 TUNA（`mirrors.tuna.tsinghua.edu.cn/jetbrains/kotlin/`），
   部分版本可用。GitHub release 作为 fallback。
3. **Ninja**：mirror_url 指向 TUNA github-release，官方源 GitHub release 可用。
4. **UV**：mirror_url 指向 TUNA github-release，官方源 GitHub release 可用。

### CI gate 层面

`internal/linkaudit/ci_gate.go` 已实现：
- official `URL_DEAD`/`TRANSIENT_ERROR` 在镜像存在时降级为 warning（不阻塞）
- official 无镜像时阻塞（确保至少有一个可用源）

### 安装行为

xdev 安装器在 mirror_url 失败时自动 fallback 到官方 URL，用户不受影响。

## 结论

这四个工具的镜像不可用问题已通过以下方式缓解：
1. Manifest 中配置了 TUNA 镜像（部分可用）
2. CI gate 对镜像失败降级为 warning
3. 安装器自动 fallback 到官方源

**不需要进一步操作**。如果未来镜像恢复，SHA audit 时会自动验证通过。