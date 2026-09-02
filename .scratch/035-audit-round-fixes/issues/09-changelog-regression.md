# 09 — 收尾: CHANGELOG + 全量回归门

Status: done
Spec: 035（收尾）
Files: `CHANGELOG.md`
Blocked by: 01-08 全部

## 改动

`CHANGELOG.md` `[Unreleased]` 节（:6）追加 Spec 035 条目：F1 零参数 shim 修复（P0）、F2 一键更新全部重构（P0）、F3/F4/F11（P1/P2）、F5-F10（P2）一行式摘要（对齐既有条目风格）。

## 全量回归门（全部通过才可提交）

- [ ] `go build ./...`
- [ ] `go vet ./internal/tui ./internal/installer ./internal/update ./internal/gui`
- [ ] `go test ./... -short -count=1`（29 包全绿）
- [ ] `cd frontend && npx vitest run`（全绿，含两个新 spec）
- [ ] `cd frontend && npx vite build`
- [ ] F1 手动冒烟（别名零参数分发 + xdev 零参数 GUI）
- [ ] F2 手动冒烟（批量 2 项 + 中途取消）
- [ ] TUI 手动冒烟（更新视图 a/Esc 链路）

## 提交

按 ticket 分组提交或单提交（用户决定）；`HANDOFF.md` 按会话协议第 3 条更新；`handoff-backup` 分支按第 5 条提交恢复点。
