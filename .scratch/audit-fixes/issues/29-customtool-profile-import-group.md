# 29 自定义工具 + profile 导入组（P1-18/19/20）

Status: resolved
Type: task
批次: B5
对应修复项: P1-18(B-05) + P1-19(B-06) + P1-20(B-07) + N-13（方案 §5 主题 C）

## 方案要点

1. **P1-18 AddCustomTool 三重门**：a.cat.Tool(name) 存在 → 报错；cfg.CustomTools 已含同名 → 报错（【N-13】现状 :211-220 无条件 append 不查重）；**写前 os.Stat(manifestPath) 存在即拒绝**（覆盖「磁盘同名 yaml 未进 cat」——坏 yaml/第三方残留场景；使回滚的「本次新建」成为平凡真值）；cfg.Save 失败回滚 os.Remove(manifestPath)。注意「嵌入优先」合并事实：同名自定义工具**追加的 Discovered 版本会并入**内置版本表——与票 13 的 TOFU 同名防线配套。
2. **P1-19 UseVersion 存在性门**：os.Stat(installPath)；ImportProfile 对 !alreadyInstalled 的 active 项直接跳过 UseVersion（消除假错误）。
3. **P1-20 npm/json 死功能**：Preview（:64-68）与 AddCustomTool（:139-143）+ **tool.Remote 持久化块（:181-185）三处同步填 Package**（漏持久化块则 sniff/mergeIntoManifest 时仍报 "package is required"——死功能只修一半）；json 若 UI 无 Path 输入则先移除该选项。

## 守卫测试

重名三场景全拒 + 回滚后无残留 yaml；UseVersion 对不存在版本报错；自定义 npm 工具落盘 manifest 含 package 且 sniff/预览/安装全链可用。

## Comments（Batch 5 实施记录）

- 改动：
  - **B-05 三重门**（`internal/gui/app_custom_tools.go` `AddCustomTool`）— 写盘前任一命中即拒：① `a.getCat().Tool(name)` 命中（内置/已加载 manifest）② `cfg.CustomTools` 快照已含同名（N-13：原 :211-220 无条件 append）③ 写前 `os.Stat(manifestPath)` 存在即拒（覆盖「磁盘同名 yaml 未进 catalog」，并使回滚的「本次新建」平凡化）。`cfg.Save` 失败 → 截断本次 append + `os.Remove(manifestPath)`。
  - **B-06**（`app_install.go` 仅 `UseVersion` + `app_profile.go` 仅 ImportProfile）— UseVersion 在 `newInstaller` 之前 `os.Stat(installPath)`，未安装 → `version X of Y is not installed`；ImportProfile 用 `justInstalled` 标记，`!alreadyInstalled` 的 active 项跳过 UseVersion（消除假错误）。
  - **B-07 三构造点一致**（`app_custom_tools.go`）— 新增 `customToolRemote()` 供「Preview 发现 / AddCustomTool 发现 / 落盘 `tool.Remote`」三处共用（结构上排除只改一处）；`npmPackageFromSource()` 由唯一输入框 sourceURL 派生包名（保留 `@scope`）。
- 守卫测试：`app_custom_tools_b5_guard_test.go`（4 例重名/回滚 + `_NPMPackage_ThreeConstructionPoints`）、`app_profile_b6_guard_test.go`（2 例）、共 7 例。
- **红线先红后绿**：B-05 三场景旧行为全 `Success:true`（重复接受/覆盖既有 manifest/config 变 2 条）、回滚实验旧行为留孤儿 manifest（`stat err=<nil>` 且 `CustomTools=1`）；B-06 旧行为对不存在版本返回 nil，半修复态产生 `result.Errors=[… version 1.0.0 of demo is not installed]`（正是票面「假错误」）——**额外发现**：门缺失时 Windows `mklink /J` 对不存在目标竟能成功 → 旧行为是**静默悬挂 `current/<tool>` 链接**，故测试补了「不得出现 current 链接」断言；B-07 全未修 → `PreviewCustomToolVersions(npm) 未发现任何版本`，半修复 → 落盘 manifest `remote:` 段无 `package`。
- **白名单外发现（未改，建议后续票）**：①前端 `SettingsPage.vue` 自定义工具表单**无 package/path 输入**（type 仅 github|html|npm）→ npm 包名只能服务端从 URL 派生，且**裸包名（如 `typescript`）会被 `util.IsSafeExternalURL` 拒绝**；②前端从未提供 json 选项，但后端仍接受 `type=json` 而 `Remote.Path` 恒空 → json 自定义工具实际不可用（应前端加 Path 或后端拒绝）；③`config.CustomToolConfig` 无 Package/Path 字段（当前可由 SourceURL 派生，无功能损失）；④并发观察：门②读的是 `getCfg()` 快照、锁外判定，理论上双并发同名仍可各写一条（彻底修需把 manifest 写入纳入 `cfgMu`，且锁内二次检查会引入「互删对方 manifest」新风险，按票面保持简单实现；GUI 提交期间按钮已禁用）。
- **顺带修复（来自该 subagent 的发现）**：`app_install.go` 的 `wailsRuntime.EventsEmit` 缺 `if a.ctx != nil` 守卫 → 无 ctx 时 Wails `getEvents` 直接 `log.Fatalf`，测试进程**静默 exit 1 无任何输出**（实测踩到）。已由主 agent 补上 nil 守卫（与 `app.go` 既有写法一致）。
