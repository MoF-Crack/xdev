# 05 — F5: PhaseInstalling percent=-1 TUI 渲染修复

Status: done
Spec: 035（F5，P2）
Files: `internal/tui/install.go`、`internal/tui/app.go`、`internal/tui/tui_test.go`
Blocks: none

## 改动（最小）

1. `progressState.store`（install.go:47-54）：`percent < 0` 时**只更新 phase**（percent/speed/eta 全保持——对齐 GUI「整体保持只换文案」语义；PhaseInstalling 常见时序在下载完 percent=100 后，显示 100% + "Installing" 与 GUI Percent:100 一致）。
2. `app.go` InstallProgressMsg 处理（:234-254）：`if msg.Percent >= 0 { m.installState.Percent = msg.Percent }`——防御层（源头被 1 挡住后真实链路不可达，防未来绕过 store 直赋值）。
3. `viewInstallOverlay`（install.go:137/146）：渲染局部 `percent := s.Percent; if percent < 0 { percent = 0 }`（纯 clamp，进度条与百分数都用局部变量）。

## 验收

- [ ] `go test ./internal/tui -count=1` 全绿，含新守卫：发 `InstallProgressMsg{Percent: -1}`（先发一个 Percent:50）→ `installState.Percent` 仍为 50；`View()` 输出不含 `-1%`
- [ ] `git diff` 确认三处改动无相互冗余冲突（store 分流为主，app.go/render 为防御层，注释已注明）
