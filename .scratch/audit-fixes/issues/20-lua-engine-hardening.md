# 20 Lua 引擎三项加固（poisoned VM + latest + loadstring）

Status: open
Type: task
批次: B4
对应修复项: P0-15 = F-04 + F-05 + F-06（方案 §4 P0-15）

## 方案

1. **F-06**：DoString 覆盖块（lua_engine.go:123-136）追加 `loadstring = function() error("loadstring is not allowed in sandboxed plugins") end`。
2. **F-05**：latest 改传空串走 resolver.KindLatest 的 highest(available)；确定性兜底：插件侧先探测是否存在任一可 parse 版本，全部不可解析时沿用旧表尾语义；保留原始 versions 顺序。
3. **F-04**：cachedVM 增 `poisoned bool`（vmMu 保护）：超时分支置位；vmFor 在 `cv.mu.Lock()` 返回后复查 poisoned（互斥锁 happens-before 保证可见；命中则 unlock、循环重新取/建新 VM）；中毒 VM 不并发 Close——从缓存摘除引用让 GC 回收；孤儿 goroutine CallByParam 包 `defer recover()`（转 doneCh 错误）。

死循环插件孤儿 goroutine 仍泄漏至进程退出（已知残留，方案遗留观察项 3）。

## 守卫测试

①loadstring 封禁断言（从 load 扩为 load/loadstring 均封）；②`available() return {"2.0.0","1.0.0"}` → latest=="2.0.0"；③超时插件第二次调用拿到新 VM 且不 panic。
