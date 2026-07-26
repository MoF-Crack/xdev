# ADR-0004: Cobra CLI 框架

> **状态**：Accepted
> **日期**：2026-07-25

## 背景

XDev 需要支持大量子命令（install/use/search/doctor/audit 等），以及 shim 调度
（根据 `os.Args[0]` basename 分发到不同工具）。候选 CLI 框架包括：
- 标准库 `flag`：功能简单，不支持子命令
- urfave/cli：轻量但子命令支持有限
- spf13/cobra：成熟的子命令框架，社区活跃

## 决策

采用 spf13/cobra 作为 CLI 框架：
- root 命令在 `internal/cli/root.go` 中定义
- 每个子命令独立文件（`available.go`、`search.go`、`keygen.go` 等）
- shim 调度在 `root.go` 的 `PersistentPreRun` 中通过 `shim.MaybeExec` 实现
- 全局 flag（`--no-color`、`--no-path-fix`、`--xmdevenv-dir`）统一定义

## 后果

- 正面：子命令组织清晰，易于扩展
- 正面：shim 调度与 CLI 框架无缝集成
- 正面：自动生成 help 和 completion
- 负面：cobra 引入间接依赖（pflag、inconshreveable/mousetrap 等）

---

## Approval History

| approval_id | decision | date |
|---|---|---|
| 2026-07-26-adr-0004-001 | Accepted | 2026-07-26 |
