# 43 home 缓存格式错配：写入一行 / 读取要求两行（跨进程缓存从未生效）

Status: resolved
Type: bug
批次: B5（宿主安全复盘的连带发现，非审计方案原有票）
对应修复项: 新发现（方案 §6 之外）——与票 40 的宿主污染复盘同源

## 问题

`internal/config/config.go`：

- 写入端 `writeHomeCache`：内容 = `dir + "\n" + os.Getenv("XMDEV_HOME")`。
- 读取端 `readCacheFile`：`strings.TrimSpace` 后 `if len(lines) < 2 { return "" }`。

**生产环境的正常情形是 `XMDEV_HOME` 未设置**（xdev home 是「被发现」的，不是配置的），此时写出的是**单行**文件（本机实测 `E:\XDev\.home.cache` = 8 字节 `45 3a 5c 58 44 65 76 0a`），被读取端永远拒收。后果：

1. **跨进程缓存从未生效**——每个 xdev / shim / CLI / GUI 进程都重新走全盘解析（`saferWindowsHome` 对 8 个盘符求剩余空间，或 `scanForExistingInstall` 逐盘 `stat`）；
2. **每次调用都重写该缓存文件**（mtime 持续抖动）——这正是票 40 宿主安全快照把 `<home>/.home.cache` 的 mtime 变化反复报成「有测试写真实 home」的真实原因（真实原因是**每次调用 xdev 二进制都在重写**，与测试无关）。

该缺陷来自基线导入（`git log -L` 追溯至 `7fd7c43 feat: import XDev application baseline`），与票 31 的原子写重构无关（票 31 只改写入机制，未改内容格式）。

## 方案

读取端接受单行形式（缺第二行 = 记录时环境变量为空），并对两行做 `TrimSpace`（Windows 写出的 CRLF 也能对上）。语义不变：`XMDEV_HOME` 与记录值不一致 → 作废；记录的目录已不存在 → 作废；空首行 → 作废。

```go
lines := strings.Split(strings.TrimSpace(string(data)), "\n")
if len(lines) == 0 { return "" }
cachedHome := strings.TrimSpace(lines[0])
cachedEnv := ""
if len(lines) >= 2 { cachedEnv = strings.TrimSpace(lines[1]) }
if cachedHome == "" { return "" }
if os.Getenv("XMDEV_HOME") != cachedEnv { return "" }
```

## 验收

- 单行缓存被接受，且 `readHomeCache` 命中后不再触发全盘扫描。
- 两行格式语义不变（env 一致才认、不一致作废）。
- 空首行 / 目录已消失 / 文件不存在一律作废。
- 全部用例只读或只写 `t.TempDir()`：红状态下也不会写到真实 home。

## Comments（Batch 5 实施记录）

- 改动：`internal/config/config.go` 的 `readCacheFile`（接受单行 + 两行 TrimSpace；注释写明票 43 的成因与影响面）。
- 守卫测试：`internal/config/home_cache_test.go` —— `TestReadCacheFile_AcceptsSingleLine`（必红核心）/ `TestReadCacheFile_TwoLineFormatStillWorks`（env 一致、env 不一致、单行但当前有 env 三个子例）/ `TestReadCacheFile_RejectsBadRecords`（空首行、目录消失、文件不存在）/ `TestReadHomeCache_UsesSingleLineCache`（跨进程入口；`LOCALAPPDATA`/`APPDATA` 指向临时目录，避开本机真实缓存）。
  - 为何用例断言 `readHomeCache` 而不是 `ResolveHomeDir`：写缓存发生在 `ResolveHomeDir` 的 **miss 分支**里，而该分支的 `homeCachePath()` 此时已用**解析出的真实 home** 组路径——即红状态下会写真实 home。断言「读命中」与「不会重写」等价，且全程零写入。
- **先红后绿证据**（修复前 `-run TestReadCacheFile|TestReadHomeCache -v`）：
  - 红：`home_cache_test.go:43: readCacheFile(单行) = "", want "C:\Users\…\Temp\TestReadCacheFile_AcceptsSingleLine…" —— 生产写出的单行缓存被判无效` + `home_cache_test.go:122: readHomeCache() = "", want …`；同时 `TwoLineFormatStillWorks` / `RejectsBadRecords` 通过（证明用例没有过度放宽）。
  - 绿：四例全 PASS；`TestReadHomeCache_UsesSingleLineCache` 耗时 **0.51s → 0.01s**（不再退化为盘符扫描，直接量化了「缓存命中省掉全盘解析」）。
- 连带调整（宿主安全快照收严，`internal/testkit/hostsnapshot`）：`.home.cache` 与 `cache/shim-index.v1` 属**派生缓存**，每次合法 xdev/shim 调用都可能重写（本票修复前是「每次必重写」），对其做 mtime 指纹会把正常工具链活动报成污染。故 `Options` 新增 `VolatilePaths`（只记存在性）：将 `config.yaml` + `plugins/`（用户自有状态，全指纹，等长改写由 mtime 兜住）与两个派生缓存分开跟踪，新增守卫 `TestCapture_VolatilePathsIgnoreChurn`（重写不算变化、创建/删除必须发现）。
- 验证：`go build ./...` / `go vet ./internal/config/ ./internal/testkit/...` exit 0；`internal/config`、`internal/testkit/hostsnapshot` 全绿；cli/installer/gui 三包（pinned home + 快照）全绿无 CHANGES；全量 `go test ./... -short -count=1` 全包 ok。
