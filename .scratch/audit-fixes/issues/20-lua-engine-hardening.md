# 20 Lua 引擎三项加固（poisoned VM + latest + loadstring）

Status: resolved
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

## Comments（Batch 4 实施记录）

- 改动：
  - `internal/plugin/lua_engine.go` — F-06：沙箱覆盖块追加 `loadstring`（Lua 5.1 中它是 `load` 的别名，漏封即可绕过沙箱），错误信息扩为 load/loadfile/dofile/loadstring；F-04：`cachedVM` 增 `poisoned bool`（cv.mu 保护），`vmFor` 返回 `(*lua.LState, *cachedVM, error)` 并在 `cv.mu.Lock()` 后**复查 poisoned** → 摘除自己那条缓存引用（不并发 Close，交 GC）→ 循环重建；新增 `callProtected(doneCh, fn)`（孤儿 goroutine `defer recover()` → doneCh 错误）；新增未导出测试缝 `callTimeout`/`callTimeoutOverride`（生产零值 = 原 10/30/60s）。
  - `internal/plugin/vfox_compat.go` — 三处超时分支（callAvailable/callArchive/callEnvVars）在**释放 cv.mu 之前**置 `cv.poisoned = true`，goroutine 体改走 callProtected；超时常量抽出，生产文案字面量不变。
  - `internal/plugin/lua_plugin.go` — F-05：`ResolveVersion` 改传 `latest=""` 走 resolver 的 `KindLatest → highest(available)`；新增 `hasParsableSemver` 探测，**全部不可解析时确定性回退旧表尾语义**；原始 versions 顺序不变（不就地排序）。
- 守卫测试：`internal/plugin/lua_hardening_test.go` — LU1（loadstring + load/loadfile/dofile 四子例）/ LU2 / LU3（升/降/乱序/重复 + exact/range 回归）/ LU4（全非 semver → 表尾）/ LU5（超时后第二次调用拿到新 VM 正常返回）/ LU5b（vmFor poisoned 摘除定向单测）/ LU6（孤儿子 goroutine panic → doneCh 错误，进程不崩）。
- **红线先红后绿（修复前实测，非推测）**：
  - LU2/LU3 红：`ResolveVersion(latest) = "1.0.0", want "2.0.0"`（降序插件取到最旧版本）
  - LU1 红：`available() must fail when loadstring() is invoked — sandbox escape`（load/loadfile/dofile 修复前已封，仅 loadstring 逃逸）
  - **LU5 红（F-04 实锤）**：在旧代码上把 30s 超时临时缩短为 250ms 跑探针——第一次 `available() timed out`；第二次 `v="" err=available(): runtime error: index out of range [-1]`（**与孤儿 goroutine 共用同一 LState 导致栈错乱**）+ `VM still cached=true`；修复后第二次调用拿到新 VM 正常返回且缓存已换新。
- 取舍与残留：LU5 用「注入 250ms 超时 + 有界自旋」而非死循环（第二次调用能在默认超时内返回，孤儿 goroutine 自行结束）；LU6 直接单测 `callProtected`（Lua 层难以构造真正逃逸到 Go 的 panic，该包装器正是三处调用点共用的 goroutine 体）；**死循环插件的孤儿 goroutine 仍存活至进程退出**（已知残留，注释登记）。副带修正：KindLTS 在 semver 列表下现也走 highest()（与 latest 语义对齐）。
