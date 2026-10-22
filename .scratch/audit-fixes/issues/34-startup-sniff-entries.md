# 34 startup 嗅探三入口

Status: resolved
Type: task
批次: B5
对应修复项: P1-30（E-11）（方案 §5 主题 D——复核完全核实）

## 方案要点

1. main.go:79 固定 100ms sleep 改为仅当 `cli.HasStartupSniff()`——**须把 interval 策略 TTL 到期的后台嗅探也算入**（onStartupInterval 同样 spawn，否则 sleep 条件不完整）。
2. strategy=startup 的 OnStartup 仅挂 interactive 入口（root RunE / tui / gui——selfcheck_test.go:93-107 三入口 grep 守卫直接复制）；cd-hook 的 `xdev hook`/`env` 不触发。
3. **别顺手给 gui/app.go 加 OnStartup**（双击零参 GUI main.go:47-62 不走 Cobra，现状就没有 startup 嗅探——加了会双嗅探）。
4. OnInitialize 仍建 sniffScheduler 供 Shutdown 刷缓存（startupCancel==nil 时 Shutdown 秒退 ✓）。

## 守卫测试

三入口 grep 守卫（复制 selfcheck 模式）；单命令耗时 -100ms；startup 用户只在交互入口付出嗅探。

## Comments（Batch 5 实施记录）

- 改动：
  - **`main.go`** — 固定 100ms sleep 改为 `if startupSniffPending`（**在 `ShutdownSniffScheduler()` 之前取值**——Shutdown 会把 `last_sniff_at` 刷成 now，之后判定会失真）。
  - **`internal/cli/root.go`** — `OnInitialize` 只建 Scheduler 供 Shutdown 刷缓存（**不再 `OnStartup`**）；新增 `PersistentPreRunE` + `isInteractiveEntry`（仅 root/tui/gui 三入口，`hook`/`env` 等一律 false）+ `mountStartupSniff`（幂等）+ `HasStartupSniff()`（= 已挂载 ∧ `StartupSniffPending()`）。因 `extra.go`（tui/gui）不在该 subagent 白名单，改用**命令身份判定**而非在 extra.go 逐点插入。
  - **`internal/discovery/scheduler.go`** — 仅 OnStartup 区域（未碰 `mergeIntoManifest`/锁）：新增 `intervalElapsed()`（与 `onStartupInterval` 共用同一判定）、`StartupSniffPending()`、`StartupSniffSpawned()`（诊断）。
  - **零参 GUI 未加 OnStartup**（双击路径不走 Cobra，加了会双嗅探）✓
- 守卫测试：`internal/cli/startup_sniff_test.go`（`TestStartupSniffMountPoints_OnlyInteractive`——复制 selfcheck 三入口守卫写法并扫更多文件；`TestIsInteractiveEntry_OnlyThreeEntries`（真实子命令构造 + 合成 root：hook/activate/deactivate/env/install/list/config set/sniff/isolate exec 全 false）；`TestHasStartupSniff_StrategyAndIntervalTTL`（interval 新鲜=false / 到期=true / startup+入口=true / manual,service=false / **非交互命令=false**）；`TestOnInitialize_DoesNotTriggerStartupSniff`）+ 根包 `main_startup_sniff_test.go`（`TestMainStartupSleep_IsConditional`（唯一一处 sleep 且在条件分支内、HasStartupSniff 在 Shutdown 前取值）、`TestZeroArgGUI_NoStartupSniff`）。
- **红线先红后绿**：root.go 恢复 v1 挂载形态 + `HasStartupSniff` 丢掉挂载条件 → `OnStartup call sites = 2, want exactly 1`、`HasStartupSniff() = true, want false (非交互入口)`、`OnInitialize 不得再调用 OnStartup`；main.go 恢复无条件 sleep → `time.Sleep 必须位于 if startupSniffPending 分支内（不得无条件等待）`。还原后全绿，无探针残留。
- 验证：`go build ./...` exit 0；`go vet` exit 0；`go test ./internal/cli/ ./internal/config/ ./internal/update/ -count=1` 三包 ok；`go test . -count=1` ok；全量单测后**真实 `E:\XDev\config.yaml` mtime 未变**。
- C-11 关联：`intervalElapsed` 与 `onStartupInterval` 共用判定，避免「sleep 等了但没 spawn」或反之的条件分叉。
