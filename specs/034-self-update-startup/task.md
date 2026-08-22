# Spec 034 Task 清单

> **状态**：✅ 完成（Phase A-E 全部收敛，2026-08-22）
> **前置**：Spec 034（Approved）+ Task 2026-08-22 批准
> **日期**：2026-08-22
> **范围**：启动自检更新（prompt/auto/notify 三策略）+ 三选交互 + 旧版本副本替换 + 可配置默认选项；全量复用既有能力

## 复用清单（不重复实现）

- `internal/update.Check/Apply`（SHA256 + HTTPS + Windows 替换/回滚）→ 自检引擎
- `util.CompareSemver / ParseSemverParts / FileSHA256 / CopyFile` → 比较/校验/替换
- `core.self_update`（已有字段）→ 补消费者
- `cli/config.go` `coreField/setCoreField` 框架 → 加 2 字段
- GUI `App.CheckForUpdate()` 绑定 + 前端 `checkUpdate()`（AppContent.vue:1060）→ 交互复用
- naive-ui dialog/message → 三选对话框
- cache JSON 读写模式（`saveJSONToCache` / ignored_tools.json 同款）→ 忽略/频率记录
- `xdev self version` 输出 + `toolmeta.ExtractVersionFromOutput` + `EnsureSelfInstalled`（app.go:844）→ 副本版本探测与替换
- `installer.InstallSelf`（install/ 自管理）→ 不动

---

## Phase A — 配置扩展

### T-A1 CoreConfig 新增字段
- **涉及**：`internal/config/config.go`
- **预期产出**：`SelfUpdateMode string`（yaml `self_update_mode`，默认 `"prompt"`）+ `SelfUpdateStale bool`（yaml `self_update_stale`，默认 true）；`Load` 缺省值时回填默认
- **验收**：`go build ./internal/config` 通过
- **依赖**：无

### T-A2 CLI config set/get 支持新字段
- **涉及**：`internal/cli/config.go`
- **预期产出**：`coreField/setCoreField` 加两字段；`self_update_mode` 校验枚举（prompt|auto|notify），非法拒绝
- **验收**：`xdev config set core.self_update_mode auto` 持久化后再 `get` 回读一致；非法值报错
- **依赖**：T-A1

### T-A3 配置守卫测试
- **涉及**：`internal/config/*_test.go`、`internal/cli/*_test.go`
- **预期产出**：TC-S1 默认值 / roundtrip / 非法枚举拒绝 / 旧 config 文件缺字段回填
- **验收**：`go test ./internal/config ./internal/cli -count=1` 全绿
- **依赖**：T-A1、T-A2

## Phase B — 决策层（internal/update/policy.go）

### T-B1 CheckPolicy + 频率保护
- **涉及**：`internal/update/policy.go`（新建）+ `policy_test.go`
- **预期产出**：`Action` 枚举（Skip/Ask/Update/Notify）；`CheckPolicy(cfg, cacheDir)`；注入 `Check`/`Now`（测试 fake）；24h 限频（`cache/self_update_last_check.json` 原子写）；检查失败 → Skip + log；忽略记录 `cache/self_update_ignore.json`（忽略版本 == Latest 则 Skip）
- **验收**：TC-S2 决策表全过（开关关/未到期/失败/三种 mode × HasUpdate）+ TC-S6 忽略/限频
- **依赖**：Phase A

### T-B2 ReplaceStaleCLICopy（副本替换，CLI/GUI 共用）
- **涉及**：`internal/update/stale.go`（新建）+ `stale_test.go`
- **预期产出**：`ReplaceStaleCLICopy(currentExe, homeDir string) error`——探测 `homeDir/xdev(.exe)` 版本（exec `self version` 1s 超时；`toolmeta.ExtractVersionFromOutput`）→ `CompareSemver < 0` 时 `CopyFile` 替换 + 前后 `FileSHA256` 校验；副本==currentExe / exec 失败 / 解析失败 → 跳过（返回 nil，调用方 log 可选）；无副本 → 保持 EnsureSelfInstalled 缺失复制语义不变（返回特殊结果或由调用方先 EnsureSelfInstalled）
- **验收**：TC-S5 全过（旧版替换/同版跳过/新版跳过/exec 失败跳过/自指跳过/并行安全）
- **依赖**：无（可并行）

## Phase C — GUI 侧

### T-C1 GUI 启动自检接线
- **涉及**：`internal/gui/app.go` `startup()`（或 domReady 后 2s goroutine）
- **预期产出**：goroutine 调 `update.CheckPolicy`；ActionUpdate → `update.Apply` 内部执行；ActionNotify → log；ActionAsk → 触发前端弹窗（经事件/轮询或直接调用既有消息机制——**不新增 Wails 绑定**，优先：前端轮询 `CheckForUpdate` 由后端 set 一个标志？→ 简化：GET 已绑定 `CheckForUpdate` 是手动的；为自动弹窗提供最小通道：新增一次性事件推送 `wailsRuntime.EventsEmit("self-update-prompt", result)`，前端监听（零绑定新增、单事件））
- **验收**：`go build ./internal/gui`；手动冒烟见 T-F
- **依赖**：T-B1

### T-C2 前端三选对话框
- **涉及**：`frontend/src/components/SelfUpdateDialog.vue`（新建，naive-ui Dialog 封装）、`AppContent.vue`（监听 `self-update-prompt` 事件）
- **预期产出**：三按钮——更新（调 `selfUpdate` 执行路径，复用 useInstall 或调 CLI？GUI 场景：新增 `App.ApplySelfUpdate()` 内部调用 `update.Apply`？——**检查既有绑定**：无 Apply 绑定 → 需在 T-C1 一并加 `App.ApplySelfUpdate() UpdateCheckResult`（最小绑定 +1，非"新绑定"泛化，属既有 CheckForUpdate 家族；spec 非目标写"不新增 Wails 绑定"指不带新 UI 概念——此处为执行必需，任务内说明））、忽略并继续（写忽略 cache + 关闭）、退出（`rootCancel` + WindowClose——经绑定 `App.QuitApp()`？已有 window 关闭通道 → 用 wails runtime WindowClose 服务端或事件）
- **验收**：vitest 新增 SelfUpdateDialog.spec.js（TC-S3）：三按钮 emit、忽略写缓存调用、更新转发、退出 emit
- **依赖**：T-C1

### T-C3 EnsureSelfInstalled 升级为 stale 感知
- **涉及**：`internal/gui/app.go`
- **预期产出**：`EnsureSelfInstalled` 在副本存在时调 `update.ReplaceStaleCLICopy`；`self_update_stale=false` 跳过；行为不回归（缺失复制保持）
- **验收**：TC-S5 在 gui 集成路径（fake exe stub）
- **依赖**：T-B2

## Phase D — CLI 侧

### T-D1 交互入口挂载
- **涉及**：`internal/cli/root.go`（无参 RunE）、`internal/cli/extra.go`（`newTuiCmd`/`newGuiCmd` RunE）
- **预期产出**：三入口 RunE 开头调 `update.CheckPolicy`；TTY 交互（`[U]pdate/[I]gnore/[Q]uit`，stdin）；非 TTY（`!isatty` 或 `--no-path-fix`）→ Ask 降级 Notify；Update → 复用 `self update` 流程（抽出共享 `runSelfUpdate(cmd)`，`extra.go` 现有代码复用之）
- **验收**：TC-S4（TTY 三分支 / 非 TTY 不阻塞退出码 0 / 挂载点守卫：子命令与 shim 不触发——守卫测试扫描 `runSelfCheck` 调用点仅 3 处）
- **依赖**：T-B1、T-B2

## Phase E — 收口与回归

### T-E1 全量验证
- **涉及**：无（验证）
- **预期产出**：`go test ./... -short -count=1` 全绿；`go test ./internal/update ./internal/gui ./internal/cli ./internal/config -count=1` 精测；vitest 全绿（含新 SelfUpdateDialog）；`go build .` + `wails build -tags gui` 通过
- **验收**：全绿
- **依赖**：Phase A-D

### T-E2 文档 + HANDOFF + backup 提交
- **涉及**：`HANDOFF.md`、checklist 执行记录、handoff-backup 分支
- **预期产出**：Spec 034 完成记录 + 关键文件索引；恢复点提交
- **验收**：`git log --oneline handoff-backup` 可见
- **依赖**：T-E1

---

## 依赖拓扑

```
Phase A (config)
   ├─ Phase B (policy/stale)   ← 可并行
   ├─ Phase C (gui) ← B
   └─ Phase D (cli) ← B
Phase E (回归) ← A-D
```

## 决策点（需用户确认，默认按 spec 执行）
1. **CLI 挂载范围**：仅 3 个交互入口（无参 TUI / tui / gui）。若你希望所有子命令都检查（会拖慢 shim/脚本），需显式说明——默认按本清单。
2. **GUI 自动弹出通道**：新增一次性事件 `self-update-prompt`（EventsEmit，零绑定）+ 执行必需的最小绑定 `App.ApplySelfUpdate()`（原有手动 `CheckForUpdate` 家族）。若你希望复用现有手动按钮路径（启动只 show info 不弹窗），改为 notify-only，可省掉这两处。
3. **GUI 退出动作**：三选「退出」= 关闭窗口（rootCancel + WindowClose），不杀进程自毁。

---

**审批请求**：请审批本 task 清单。