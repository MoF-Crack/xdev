# 04 — F11: 下载轮询超时补 emit installError

Status: done
Spec: 035（F11，P2）
Files: `internal/gui/app_install.go`
Blocks: 08（F2 批量依赖本项同批发布，否则批量等待挂死仅能手动 Cancel 逃生）

## 改动（最小）

`installWithOptions` 轮询 goroutine 的 maxPolls 耗尽分支（app_install.go:400-403）补 emit，格式与同函数 StatusFailed/Cancelled 分支（:390-395）一致：

```go
// 超时：下载任务耗时过长，清理
logWarn("[InstallTool] download poll timeout for %s@%s", toolName, version)
wailsRuntime.EventsEmit(a.ctx, "installError", map[string]string{
    "taskId":  taskID,
    "tool":    toolName,
    "version": version,
    "error":   "安装等待超时（1 小时）",  // 与同函数 "下载失败"/"下载已取消" 中文风格一致
})
a.dlMgr.Cancel(taskID)
a.dlMgr.RemoveTask(taskID)
```

单装路径（进度框等 Completed 永不来）与批量路径同受益。

## 验收

- [ ] `go build ./...` + `go test ./internal/gui -count=1` 全绿
- [ ] 代码走查：emit 在 Cancel/RemoveTask 之前、taskID 在作用域内、map 键名与既有 emit 一致
