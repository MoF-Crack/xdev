# 40 P2 CLI/TUI 组（含必红 TUI 测试）

Status: resolved
Type: task
批次: B5
对应修复项: P2-16(E-12) + P2-17(E-13) + P2-18(E-14) + P2-19(E-15) + P2-20(F-08) + P2-33(N-18)（方案 §6）

## 方案要点

1. **P2-16** `config proxy` 复用 cfg.SetProxy（含 ValidateProxyURL）；ConfigureHTTPClient 解析失败 log 告警。
2. **P2-17** plugin install 4MiB：io.Copy 后 `io.ReadFull(resp.Body, []byte{0})` 非 EOF 即报 "exceeds 4 MiB limit"。
3. **P2-18** doctor shims：shimOK 由 ReadDir 成功推导；「零 shim」保持 info 而非 fail（防 doctor 退出码变化破坏首跑体验/CI 断言）。
4. **P2-19 TUI 陈旧消息**：引入 `currentOpID`，判定 `if msg.ID != m.currentOpID { return m, nil }`；①install/uninstall 用**不同消息类型**（防跨空间撞号）；**②判定同时覆盖成功（app.go:282-286）与错误（:267-281）两分支**【N-14：installState==nil 时迟到成功消息可静默恢复批量】；③**必红**：TestInstallResultError_ClearsUpdateQueue（tui_test.go:412-429）手工构造不设 currentOpID——同步更新（相邻 TestInstallCancelKey 不受影响，无需改）。
5. **P2-20 logrotate**：RotateLog 前 AcquireFileLock；Windows copy+truncate 代替 rename（或失败 log）；shift 循环错误不再静默；调用方不再无条件吞错。
6. **P2-33**【复核新增】：cli/profile.go:132 导入前大小检查（对齐 GUI 10MB）；config.go:815 日志文案改指 config.yaml。

## 守卫测试

proxy 非法 scheme 拒绝；超限插件报明确错误；doctor 新装用户零 shim 为 info；TUI 迟到消息（成功+错误两路）被丢弃；测试基线先红后绿。

## Comments（Batch 5 实施记录）

- 改动（6 项全部落地）：
  - **P2-16**（`internal/cli/config.go`）— `config proxy` 的三处「逐字段改 cfg + 自行 Save」分叉路径合并为一个 `apply`，**只经 `cfg.SetProxy` 落盘**（含 `util.ValidateProxyURL` 校验 + 原子保存），语义与 GUI 的 `App.SetProxyConfig` 完全一致；未显式给出的字段保持现值（部分更新语义不变）。校验失败时日志对代理凭据脱敏（`util.ValidateProxyURL` 的既有约束）。`internal/remote/remote.go` 的 `ConfigureHTTPClient` 解析失败分支补 WARNING 告警（P2-16 后半，见票 39 记录）。
  - **P2-17**（`internal/cli/plugin.go`）— H-3 的 4 MiB 上限原先只靠 `io.LimitReader` 实现：超限内容被**静默截断**后交给 Lua 解析，真实的「超过 4 MiB」被伪装成语法错误。改为拷贝后用 `io.ReadFull(body, probe[:1])` 探读：非 EOF 即报 `exceeds the 4 MiB limit`；`maxPluginSize` 常量集中定义；探读自身出错单独上报（不吞）。
  - **P2-18**（`internal/cli/doctor.go`）— `shimOK` 原先写死 true（ShimDir 被清空/被占成文件等情形不可见）。改为由 `os.ReadDir` 结果推导；**「零 shim」仍是 info 而非 fail**（doctor 退出码被首跑体验与 CI 断言依赖，不能因零 shim 变化）。
  - **P2-19**（`internal/tui/{app,install,messages,updates}.go`）— 引入 `Model.currentOpID`（`beginOp` 分配，`installState` 生命周期解耦）：①install 与 uninstall 用**不同消息类型**（各自带 ID，防跨空间撞号）；②陈旧判定同时覆盖**成功**分支（原 `app.go:282-286`——`installState==nil` 时迟到的成功消息可静默恢复批量队列）与**错误**分支（原 `:267-281`）；③取消操作同时作废 `currentOpID`，使被取消操作的迟到结果无法再对上新操作。
  - **P2-20**（`internal/util/logrotate.go`）— `RotateLog` 前 `AcquireFileLock(path+".lock", rotateLockTimeout)`（OS 字节范围锁，进程崩溃自动释放），消除并发轮转互相覆盖备份链；Windows 用 copy+truncate 代替 rename（rename 会与并发读者争用失败）；shift 循环错误不再静默；`OpenLogForAppend` 向上报告轮转失败（调用方不再无条件吞错）。
  - **P2-33**（`internal/cli/profile.go` + `internal/config/config.go`）— `profileImportMaxSize = 10 << 20`，导入前 `Stat` 检查 + `io.LimitReader(N+1)` 兜住 TOCTOU（对齐 GUI 侧 10 MB）；config 保存失败日志文案改指 `config.yaml`（原为目录名，误导排查）。
- 守卫测试：`internal/cli/proxy_setproxy_guard_test.go`（4 例：非法 scheme 拒绝/合法取值 + 部分更新/touched 语义/失败日志脱敏）、`plugin_size_limit_guard_test.go`（4 例：超限拒绝、**恰好 4 MiB 通过**、小插件逐字节一致、探读错误上报）、`doctor_shims_guard_test.go`（4 例：空目录 info、缺目录 fail、路径非目录 fail、shim 计数）、`profile_import_size_guard_test.go`（3 例：超限/恰好上限/正常读取）、`config_log_message_guard_test.go`（文案指向 config.yaml）、`internal/util/logrotate_guard_test.go`（7 例，含并发轮转不撕裂、锁在轮转后释放、轮转失败被上报）、`internal/tui/tui_test.go` 追加 6 例（`TestStartInstall_SetsCurrentOpID` / `TestInstallResultSuccess_LateMessageDoesNotResumeQueue` / `TestInstallResultError_LateMessageDoesNotSetErrMsg` / `TestInstallCancel_InvalidatesOpID` / `TestUninstallResultMsg_OwnTypeAndStaleGuard` / `TestInstallResultMsg_StaleForNewerOp`）。
- **必红（先红后绿，已同步更新既有用例）**：`TestInstallResultError_ClearsUpdateQueue`（`tui_test.go:412-429`）原先只手工构造 `installState` 而不设 `currentOpID`——引入 opID 判定后该消息被判为陈旧消息丢弃，用例红；已按真实起点改造（补 `currentOpID = 1`，并断言结果消费后 `currentOpID` 复位为 0），相邻 `TestInstallCancelKey` 不受影响、无需改。红→绿证据：修复前 `currentOpID` 恒为 0 / 判定只看 `installState.ID`，迟到消息可恢复批量；修复后迟到成功/失败消息均被丢弃且不动 `currentOpID`。
- 验证：`go build ./...` exit 0；`go vet ./internal/cli/ ./internal/tui/ ./internal/util/ ./internal/config/` exit 0；四包 `-count=1` 全绿；全量 `go test ./... -short -count=1` 全包 ok。
- 过程记录（宿主安全，承重）：本票复核发现并修复了**测试污染真实 xdev home** 的一类缺陷——`internal/cli` 的 `runNoPathFixHelperProcess` 子进程用 `append(os.Environ(), …)` 派生环境且未注入 `XMDEV_HOME`，导致 `config.ResolveHomeDir` 的 `sync.Once` 把真实 home 固化并在其中写 `.home.cache` / `cache/shim-index.v1`。修复方式统一为 `hostsnapshot.RunWithPinnedHome`（见 `internal/testkit/hostsnapshot/pinned_home.go`），已在 cli/gui/installer/integration 四个 TestMain 落地，另给快照加了真实 home 追踪（`config.yaml`/`plugins` 用户自有状态做全指纹、派生缓存只记存在性，见票 43）。
