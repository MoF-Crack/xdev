# 04 — 收尾: CHANGELOG + 全量回归门 + HANDOFF

Status: done
Spec: 036（收尾）
Files: `CHANGELOG.md`
Blocked by: 01-03 全部

## 改动

`CHANGELOG.md` `[Unreleased]`（Spec 035 条目之后）追加 Spec 036：N1/N3 TUI errMsg 渲染 + 死代码删除；N2 安装互斥守卫（收敛点 + 批量三层防护）；N4/N5 批量进度框闪烁 + 取消双 toast。

## 全量回归门（全部通过才提交）

- [ ] `go build ./...` + `go vet ./internal/tui`
- [ ] `go test ./... -short -count=1`（29 包全绿）
- [ ] `cd frontend && npx vitest run`（全绿，含新 spec 改动）
- [ ] `cd frontend && npx vite build`
- [ ] `cd frontend && npx playwright test e2e/app.spec.js`（18/18）
- [ ] MS-N2：后台运行中点单项安装 → busy 提示而非静默失败
- [ ] MS-N5：批量取消仅一条文案

## 提交

按 ticket 分组提交或单提交（用户决定）；`HANDOFF.md` 按会话协议第 3 条更新；`handoff-backup` 分支按第 5 条提交恢复点。
