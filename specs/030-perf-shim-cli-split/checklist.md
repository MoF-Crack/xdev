# Spec 030 Checklist

- [x] `go list -deps .` 无 `github.com/wailsapp/wails`
- [x] `go test ./internal/cli ./internal/shim ./internal/discovery ./internal/update ./internal/plugin -count=1`
- [x] `go test ./internal/env ./internal/gui -count=1`
- [x] `go test ./... -short -count=1`
- [x] interval OnStartup 500ms 内返回（测试）
- [x] SniffAll catalog 加载次数 = 1（测试）
- [x] asset 名 windows/amd64 → `xdev-windows-amd64-v0.3.2.zip`
- [x] `xdev gui` 无 GUI 二进制时错误信息含 `xdev-gui`
