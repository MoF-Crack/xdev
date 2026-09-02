# 03 — F8+F10: runXdevCommand 自身优先 + ResolveKind 导出收口

Status: done
Spec: 035（F8/F10，P2）
Files: `internal/gui/app_updates.go`、`internal/installer/installer.go`、`internal/gui/app_install.go`、`internal/installer/kind_routing_test.go`
Blocks: none

## 改动（最小）

1. **F8**：`runXdevCommand`（app_updates.go:310-316）改 `os.Executable()` 优先，失败回退 `exec.LookPath("xdev")`。
2. **F10**：`resolveKind` 导出为 `ResolveKind`（installer.go:982），加 nil-guard（`if a == nil { return manifest.InstallKindArchive }`）；installer 内 5 处调用点（:472/485/1002/1088/1377）机械同步改名；`app_install.go:318` 的 `arch.EffectiveKind() != manifest.InstallKindArchive` 改为 `installer.ResolveKind(arch) != manifest.InstallKindArchive`。
3. 测试：kind_routing_test.go 补 `ResolveKind(nil)` 不 panic 断言（一句）。

已核实：gui→installer 依赖已存在无环；生产代码 runXdevCommand 零调用（仅测试）。

## 验收

- [ ] `go build ./...` + `go vet ./internal/gui ./internal/installer` 通过
- [ ] `go test ./internal/gui ./internal/installer -count=1` 全绿
- [ ] `git diff` 确认 app_install.go 仅一处判定行变更
