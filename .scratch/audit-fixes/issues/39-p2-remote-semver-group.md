# 39 P2 远程/semver/杂项组（含两条必红/统一饱和项）

Status: resolved
Type: task
批次: B5
对应修复项: P2-07/08/09/11/12/13/14/23/21/22/30/31（方案 §6）

## 方案要点

1. **P2-07** GetLatestVersionForTool 先查 a.getCat()，miss 单飞 LoadCatalog 共享（等待者复用同一次结果）。
2. **P2-08** C-11 304 分页：cacheEntry 存 nextURL；304 时从缓存恢复下一页继续循环。
3. **P2-09** lookupOnPath：Unix os.Stat（非 Lstat——LookPath 语义跟随符号链接）校验执行位；Windows 按 PATHEXT 展开（hostscan.go:247 先例），裸名末位兜底。
4. **P2-11** retryDirect 包级单例 Transport——**仍须检查 testHookDirectTransport**（remote_test.go:1767/1802 换注入，漏检两测试红）；或 CloseIdleConnections。
5. **P2-12 IsAllZeroHash 严格化**：TrimPrefix "0x" 后 `len(s)==64` 才算占位。**必红**：downloader_test.go:238 `{"0", true}` 同步改 **false**；同测试 :234 `{"0x"+64零}` **保持 true**（TrimPrefix 后恰 64 零，与签发端一致）——测试注释显式说明两行口径。
6. **P2-13 + P2-14 + P2-23 统一**：parseSemver 先剥 `+...`；ParseSemverParts 与 numericValue **统一为 strconv.ParseUint + uint64 饱和**（两项分别实现会在同一比较器产生不同天花板）。
7. **P2-21** linkaudit 标签循环：`retryLoop: select { ... case <-ctx.Done(): break retryLoop }`。
8. **P2-22** hmac_key.go 注释如实声明（推荐仅改注释，不破坏兼容）。
9. **P2-30**【复核新增】ConfigureHTTPClient（remote.go:120）包级单例 Transport 或替换前 CloseIdleConnections。
10. **P2-31**【复核新增】register 双 LuaEngine 合并：一次目录扫描、每 .lua 只 DoFile 一次；vfox 加载失败记日志。

## 守卫测试

必红清单执行「先红后绿+同步更新」；semver 统一后大数字/溢出用例排序正确。

## Comments（Batch 5 实施记录）

- 改动（11 项全部落地）：
  - **P2-07**（`internal/gui/app_updates.go`，**落点偏差已确认**：`GetLatestVersionForTool` 唯一实现在此，不在 `internal/remote`）— 先读内存快照 `a.getCat()`（命中即零 I/O）；miss 时走新增 `loadCatalogShared` 单飞：同一 manifestDir 上并发调用共享同一次 `manifest.LoadCatalog`，等待者复用同一次结果（`f.done` 的 close 提供 happens-before）。**只合并在途加载，不做结果缓存**（结果缓存归 reloadCatalog 的 `a.cat` 快照，避免第二个会过期的 catalog 源）。已知副作用并接受：快照命中路径读到的是上次 reloadCatalog 的 catalog，HasUpdate 徽标最多滞后到下一次 reloadCatalog。
  - **P2-08**（`internal/remote/remote.go`）— `cacheEntry` 增加 `nextURL`；`cachePut` 增第 4 参，写入前做 M-1 同主机校验（**只缓存同主机游标**，防跨主机跳转注入）；304 分支由 `break` 改为「从缓存恢复游标 → `currentURL = nextURL; continue`」，游标为空才 `break`。
  - **P2-09**（`internal/shim/shim_fallthrough.go`）— 候选校验改用 `os.Stat`（跟随符号链接，对齐 `exec.LookPath` 语义）+ `executableCandidate`：Unix 要求执行位（纯函数 `hasExecBit` 便于跨平台单测），Windows 按 PATHEXT 展开（镜像 `hostscan.go:247` 写法，PATHEXT 现读可 `t.Setenv` 注入），**裸名末位兜底保留**；alias 已带扩展名时不重复追加。未触碰票 35 的 `isExcludedPathEntry`/`excludedDirIdentities`。
  - **P2-11**（`internal/remote/remote.go`）— 新增包级单例 `directTransport`（`sync.Once`）+ `directTransportForRetry()`；`testHookDirectTransport` 注入检查**原样保留且优先级最高**（既有两处测试依赖）。取舍：泄漏源是「每次回退都新建 transport、旧对象无人关池」，故选单例而非「每次响应后 CloseIdleConnections」——后者会让直连路径退化为每请求一次 TCP+TLS 握手，而回退恰恰发生在最需要低延迟的坏网场景；`retryDirect` 仍每调用新建 `*http.Client`（client 不持连接池）。
  - **P2-12**（`internal/util/fs.go`）— `IsAllZeroHash` 严格化：`TrimPrefix("0x")` 后**必须 `len(s)==64`** 才算占位。**必红已执行**：`downloader_test.go:238` 的 `{"0", true}` 同步改为 `false`，同测试 `:234` 的 `{"0x"+64零}` 保持 `true`（与签发端口径一致），两行均在测试内注释说明。
  - **P2-13 + P2-14 + P2-23 统一**（`internal/util/semver.go`）— `parseSemver` 先剥 `+build` 元数据；`numericValue` 与 `ParseSemverParts` **统一为 `strconv.ParseUint` + `math.MaxUint64` 饱和**（此前两项各自实现会在同一比较器产生不同天花板，形成非传递序）。
  - **P2-21**（`internal/linkaudit/audit.go`）— 重试循环加 `retryLoop:` 标签，`case <-ctx.Done(): break retryLoop`（原为裸 `break`，只跳出 select，取消信号被吞掉、还会再跑两轮）。行为差异仅在「取消后不再多跑」。
  - **P2-22**（`internal/util/hmac_key.go`，**仅注释**）— 删除「猜不到用户名+主机名就无法伪造」这一不成立的对抗性声明；如实写明：密钥与用户环境绑定、输入全是本机公开信息 + 固定盐、**只防误改**，防伪造由 Ed25519 签名链负责。零逻辑改动（改派生算法会让所有既有 config/lock 的 HMAC 失效，属兼容性破坏，而该项的审计点正是注释里的不成立声明）。
  - **P2-30**（`internal/remote/remote.go`）— `httpTransport` 包级单例（`init` 内装好 Proxy 分发器后**字段永不改写**）+ `currentProxyFn`（`httpTransportMu` 保护写、请求侧读）；每次重配置调用 `CloseIdleConnections()`；非法 URL 走「返回 nil 的 Proxy 函数」表达直连（与 `Proxy == nil` 等价），不更换 transport 实例。
  - **P2-31**（`internal/cli/register.go`）— `registerLuaPlugins` + `registerVfoxPlugins` 合并为 `registerUserPlugins`：**一次目录扫描 + 一个 LuaEngine**，每个 `.lua` 先试 xdev 格式、失败再试 vfox，成功加载的文件只 `DoFile` 一次；两格式都失败时两个错误一起打印（不再静默）；既有冲突警告与 `hintLegacyPluginsDir` 迁移提示语义不变；新增测试接缝 `pluginsReadDir`。
  - **P2-16 后半**（转派项，`internal/remote/remote.go`）— `ConfigureHTTPClient` 解析失败分支补 WARNING 日志，凭据先经新增 `redactProxyCredentials`（正则 `//[^/@\s]*@` → `//<redacted>@`，就地实现以免 cli→remote 反向依赖）脱敏；直连语义不变。
- 守卫测试：`internal/remote/remote_b5_guard_test.go`（8 例，含 304 恢复缓存游标、跨主机游标不缓存、单例 transport 复用（3 次回退 1 条 TCP）、注入 hook 优先级、重配置不累积 transport、非法代理走直连且告警脱敏、重配置关闭空闲连接）、`internal/shim/shim_pathcandidate_b5_test.go`（6 例，Unix 执行位/符号链接两例在本机 SKIP、由 `GOOS=linux go vet` + CI 覆盖）、`internal/linkaudit/audit_retry_cancel_b5_test.go`、`internal/cli/register_b5_guard_test.go`（2 例：单次扫描 + 单次 DoFile、vfox 回退且失败被记录）、`internal/gui/app_catalog_singleflight_b5_test.go`（2 例）。
- **必红（先红后绿）**：P2-08「304 分支丢失缓存游标」（修复前只拿到 1/2 版本）、P2-11「retryDirect 3 次回退开了 3 条 TCP，期望 1」、P2-30「21 次 ConfigureHTTPClient 造出 21 个 transport / 空闲连接在重配置后存活 / 非法 URL 更换了实例」、P2-16「非法代理无 WARNING 日志、userinfo 段未脱敏」、P2-07「快照命中仍落盘 1 次 / 未知工具落盘 2 次 / 9 个并发调用者触发 9 次 LoadCatalog、在途峰值 8」、P2-09「候选顺序 [git git.exe git.cmd git.bat] 期望 [git.exe git.cmd git]」、P2-21「取消后仍收尾 4 次，期望 ≤2」、P2-31「插件目录被扫描 2 次 / 顶层代码执行 2 次（stdout 两遍 B5-DOFILE-MARK）」。
- **本会话复核追加（我本人）**：P2-07 的并发用例原先用「静置 50ms 赌调度」作同步手段，在 `go test ./...`（30 个包并发）下被调度饥饿打成假红（实测 `--- FAIL: TestGetLatestVersionForTool_ConcurrentMissSharesSingleLoad`：迟到的 worker 在领飞者完成后另起一次加载）。已改为**确定性屏障**：生产侧新增 `catalogSharedWaitHook`（仅在「调用者加入在途加载」时触发，生产恒为 nil），测试等齐 8 个等待者上报后才放行领飞者；同时保留兜底——单飞缺失时 hook 永不触发，等待退化为超时后由 `loads` 断言给出强红。临时关闭单飞复验：`9 concurrent callers triggered 9 LoadCatalog calls, want 1` + `peak concurrent catalog loads = 9, want ≤ 1` ✓；用例耗时从 2.08s（含 2s 睡眠）降到 0.02s。
- 验证：`go build ./...` exit 0；`go vet`（remote/shim/gui/linkaudit/signer/cli）exit 0；六包 `-count=1` 全绿（shim 包 41 个顶层用例全 PASS，`[no tests to run]` 是该包既有测试以 `-test.run=^$` 自执行触发的 cmd/go 标记文本，非失败）；全量 `go test ./... -short -count=1` 全包 ok。
- 未纳入本票（白名单外，仅记录）：①`internal/gui` 三个既有用例污染真实 home —— 已由我在本批次统一修复（`hostsnapshot.RunWithPinnedHome`，见票 40 记录）；②`shim.InvalidateShimIndex()` 无注入缝隙、内部直接解析真实 home，建议后续改为接收 home/cacheDir 参数；③`configuredProxyURL` 仍是「锁内写、锁外读」的既有竞争（本票未扩大，transport 字段已改为 init 后不可变）；④`internal/remote/remote_test.go` 有 4 处既有 gofmt 漂移（为控制改动面未整文件格式化）。
- 宿主安全复盘的连带发现（新票 43，本批次一并修复）：`<home>/.home.cache` 的 mtime 抖动并非测试写入，而是 `readCacheFile` 要求两行、写入端在生产（`XMDEV_HOME` 未设置）只写一行 → **跨进程缓存从未生效**，每次调用 xdev/shim 都重做全盘解析并重写缓存。修复后单行缓存被接受，`readHomeCache` 命中耗时 0.51s → 0.01s。
