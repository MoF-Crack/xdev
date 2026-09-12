# 42 P2 env_vars 值校验（X-08）

Status: open
Type: task
批次: B5
对应修复项: P2-28（X-08）（方案 §6）
依赖: 01（GOROOT 从 manifest 删除后该面自然收窄——**声明对 P0-17 的实施顺序依赖**；若 42 先落地或 01 回滚，`{{install}}` 值在嵌套布局下「在管理树内但语义错」会通过校验）

## 方案

`SetUserEnvVarTrusted`（win_path_windows.go:506-522）对路径型变量增加校验：

1. 值须为合法绝对路径形态且位于 xdev 管理树内——复用 **`util.ValidateAbsPathUnder`（sanitize.go:118-135，已存在**，app_migration.go:63 已实用），无需新写 helper。
2. `%VAR%` 引用从「仅警告」升级为「白名单转硬检查」——现有 7 项硬编码豁免（win_path_windows.go:518：USERPROFILE/APPDATA/LOCALAPPDATA/ProgramFiles/SystemRoot/TEMP/TMP）直接转硬检查。
3. **名称类别过滤器须显式列 GOPATH**——「*_HOME/GOROOT/*_ROOT 类」按字面不含 GOPATH，而 GOPATH（`{{install}}\..\gopath`）恰是唯一单独放宽项：放宽到 install_root 下。
4. `{{install}}` 模板插值后的值天然在管理树内（现有 manifest 不破坏）；GODEBUG 类非路径变量不在过滤器内。

## 守卫测试

`%TEMP%\evil` 形态的 manifest 值被拒；白名单 7 项引用通过；GOPATH 值（跨出 install、在 install_root 下）通过；合法 `*_HOME` 通过。
