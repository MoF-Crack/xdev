# 04 插件目录单源化（pluginsDir → Cfg）

Status: resolved
Type: task
批次: B1
对应修复项: P0-06（方案 §4 P0-06）

## 问题

register.go:83-93 `pluginsDir()` = $XMDEV_HOME 或 ~/.xdev + /plugins（registerLuaPlugins/registerVfoxPlugins 同用）；plugin.go:283-286 `pluginDir()` = Cfg().XMDevDir + "/plugins" ——双源，默认部署下装好的插件永不被加载。全部 Setenv("XMDEV_HOME")（9 处）在测试。

## 方案

1. `pluginsDir()` 改 `filepath.Join(Cfg().Core.XMDevDir, "plugins")`（两注册函数同改）。runGUI 先 Load 后 LoadRegistry（main_gui.go:20-29）已核实无时序问题。
2. 兼容迁移：新目录不存在时回退探测旧目录，旧目录存在且非空时 log 迁移提示。
3. 测试改造：pluginsDir 抽为可注入变量（或 RegisterAll 加 dir 参数）+ 测试 helper 重置 cli.globalCtx（context.go:46/51-64 全局缓存跨用例污染）；补「不设 env + 自定义 home → 安装后 RegisterAll 可见」端到端断言。

## 涉及文件

internal/cli/register.go、internal/cli/plugin.go、internal/cli/context.go、internal/cli/register_test.go

## 守卫测试

自定义 home 隔离环境下 `xdev plugin install` → RegisterAll 可见；register_test 全绿（显式注入改造后）。

## Comments（Batch 1 实施记录）

- 改动：`internal/cli/register.go` — `pluginsDirFn` 包级变量（测试可替换）返回 `Cfg().Core.XMDevDir/plugins`（与 plugin.go 的 pluginDir() 单源）；`legacyPluginsDir()` + `hintLegacyPluginsDir()`（新目录不存在且旧目录非空 → log 迁移提示，不自动搬移；注册仍以新目录为准；同路径部署免提示）；RegisterAll 接入提示。`internal/cli/context_reset_test.go` — resetGlobalCfgForTest / injectHome（等价 `--xmdevenv-dir <home>` 注入，t.Cleanup 复位防粘连）。
- 守卫测试：`internal/cli/register_test.go` — TestRegisterAll_UsesCfgDirWithoutEnv（P1：真实 `xdev plugin create` cobra 命令写入 → RegisterAll 可见，断言双源断裂不复发）/ TestPluginsDir_MigrationHintForLegacyDir（P2：迁移提示日志 + 注册只认新目录）/ TestRegisterAll_HomeIsolationBetweenRuns（P3：连续两 home 互不粘连）；TestRegisterAllRealCatalog 由 t.Setenv 改为注入式。
- 实施细节：测试驱动真实 cobra 命令（plugin create 与 install 写入同一 pluginDir()，无网络依赖即等价覆盖安装路径）。
