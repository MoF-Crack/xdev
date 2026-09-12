# 24 catalog 快照读 + Generic 加锁 + 双 nil 门

Status: open
Type: task
批次: B4
对应修复项: P1-16（B-03=F-09）（方案 §5 主题 C，复核修订版）

## 方案

1. 新增快照读方法 `getCat()`（catMu.RLock + 返回指针；catalog 不可变）；替换 4 处无锁读点：app_dependencies.go:56、app_install.go:498、app_tool_query.go:173、:404。
2. **nil-guard 同时补 :173 与 :404 两处**（:56/:498 已有）——:173 在 GetDebugInfo 内（a.cat.Tools()），恰是 GUI 排障时 cat 最可能为 nil 的入口；v1 只点名 :404 会留 panic。
3. Generic.SetManifest 加 RWMutex：13 个读 M 方法（Name/DisplayName/Description/Homepage/Category/Manifest/Shims/DefaultVersion/ResolveVersion/ArchiveFor/InstallDir/EnvVars/RequiredPlugin）RLock；go vet copylocks 检查。
4. P2-07 关联注记：GetLatestVersionForTool 改查 getCat() 后 HasUpdate 徽标可能滞后到下次 reloadCatalog（可接受，PR 描述标注）；单飞须让等待者复用同一次加载结果。

## 守卫测试

`go test -race ./internal/gui ./internal/plugin` 全绿；GetDebugInfo 在 cat==nil 时不 panic。
