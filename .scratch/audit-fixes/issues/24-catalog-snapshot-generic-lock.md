# 24 catalog 快照读 + Generic 加锁 + 双 nil 门

Status: resolved
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

## Comments（Batch 4 实施记录）

- 改动：
  - `internal/gui/app.go` — 新增 `func (a *App) getCat() *manifest.Catalog`（**catMu.RLock**，非 Lock）。
  - 读点收敛：`app_dependencies.go` 的 toolManifest、`app_tool_query.go` 的 GetDebugInfo 与 GetAvailableVersions 改 getCat()；**`app_install.go` 的 lookupToolManifest 与 formFilter 分支的 `g.M` 裸读**由我把关补上（改 getCat() / `g.Manifest()`）——另 `internal/plugins/java/java.go` 的 `p.M.FindVersion` 改 `p.Manifest().FindVersion`（统一访问方式）。
  - nil 门：GetDebugInfo（cat==nil 时留空）、GetAvailableVersions（cat==nil 返回 nil）两处补上（原 :56/:498 已有）。
  - `internal/plugin/generic.go` — Generic 增 `mu sync.RWMutex`；**13 个只读方法**全加 RLock，`SetManifest` 加 Lock；`ArchiveFor` 内部由 `g.Name()`（会再取 RLock）改为直接读 `g.M.Name` 避免写方等待时的嵌套 RLock 死锁；`Manifest()` 注释注明「指针替换受锁保护，但就地改同一个 *manifest.Tool 不在覆盖范围（应走 SetManifest）」。
- 守卫测试：`internal/gui/app_catalog_snapshot_test.go`（CT2 GetDebugInfo nil-cat 不 panic / CT3 GetAvailableVersions 同型 / CT1 替代：并发 reloadCatalog 换指针与读点交错的多轮自洽）/ `internal/gui/app_local_cache_test.go`（票 22）/ `internal/plugin/generic_concurrency_test.go`（CT4：400 轮 SetManifest 写 + 13 读方法 × 200 轮交错）。
- **红线先红后绿**：CT2/CT3 红 —「GetDebugInfo() 且 cat == nil 不应 panic，实际 panic: runtime error: invalid memory address or nil pointer dereference」（两用例均 FAIL）→ 修复后 PASS。探针 `RED-PROBE` 全仓 0 残留。
- **CT1（-race）本机不可执行**（MinGW 8.1.0 与 Go 1.26 race 运行时不兼容，已实测；WSL 未安装）→ 用「并发换指针 + nil 窗口注入 + 多轮自洽」替代；建议在具备 race 的环境补跑 `go test -race ./internal/gui ./internal/plugin`。
- `go vet` copylocks 无告警（Generic 加锁后未出现按值拷贝）✓。
- P2-07 注记：HasUpdate 徽标可能滞后到下次 reloadCatalog（可接受）；单飞共享加载结果的需求在 B5 票 39（P2-07）处理。
