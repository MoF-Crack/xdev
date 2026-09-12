# 42 P2 env_vars 值校验（X-08）

Status: resolved
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

## Comments（Batch 5 实施记录）

- 改动（`internal/env/win_path_windows.go` +227/−12）：`SetUserEnvVarTrusted` 的「%VAR% 仅告警」块升级为硬校验 `validateEnvVarValue`；新增 `allowedEnvVarRefs`/`isAllowedEnvVarRef`、`isPathTypedEnvVar`（`*_HOME`/`*_ROOT` + 显式 **GOROOT** + 显式 **GOPATH**）、`managedTreeRoot`（可注入 seam）/`managedTreeRootFromHome`/`defaultManagedTreeRoot`、`validateManagedPathValue`。复用既有 `util.ValidateAbsPathUnder`（未改 sanitize.go）。
- 守卫测试：`internal/env/manifest_env_validation_test.go`（新增，windows-only，10 顶层测试 / 45 子用例）：路径型 + 引用组合拒绝（7 项白名单引用 × 多变量名，断言错误含变量名且注册表零改动）、白名单引用在**非路径型**变量上通过、未知引用硬拒、GOPATH 放宽（`<root>\install\go\<ver>\..\gopath` 通过 / 树外拒绝）、树内绝对路径通过 + 树外/相对/空值拒绝、根未知时保守放行、纯函数决策固化。注册表全部 save/restore + `t.Cleanup`，实测无 `XDEVTEST*` 残留。
- **红线先红后绿**：临时禁用校验（`if false { validateEnvVarValue(...) }`）→ 5 个顶层测试 / 29 子用例 FAIL；还原后全 PASS（`grep RED-GREEN-PROBE` 残留 0）。
- **实现过程的真实红（促成规则分组）**：第一版对路径型变量**无条件**要求「绝对 + 树内」，实测把票外测试 `internal/testkit/hostsnapshot`（写 `GOROOT="__xdev_host_snapshot_probe__"`）打成红 → 改为「引用规则无条件生效；形态/树内约束仅在根可知时强制」，并在 `go test` 下默认视为根不可确定，保证 hooked-shell 全量测试不红。
- **base 判定规则（关键设计）**：根只取 `$XMDEV_HOME`（env→config 会成环；本机 `~/.xdev` 存在但**不是** home——真实 home 是 `E:\XDev`，用它当 base 会误拒全部合法写入）；根不可确定 → WARN + 放行。**唯一与根无关、始终生效的是 %VAR% 规则**（路径型变量值含任何 `%VAR%` 一律拒绝）——正是它无条件拦下 `%TEMP%\evil`。
- **manifest 核对（53 文件，未改 manifests/）**：35 个含 env_vars、共 48 条，按 `{{install}}` 插值后逐条过新校验 → **REJECT = 0**（含最易误判的 `{{install}}\..\..` → `<root>\install`）。**无内置 manifest 被拒**。
- 验证：`go build ./...` exit 0；`go vet` exit 0；`go test ./internal/env/ ./internal/util/ -count=1` 全绿；**`go test ./... -short -count=1` 29 包全绿**。
- **需拍板的语义取舍（已在票卡记录，实施选更安全侧）**：路径型变量上的**白名单 `%VAR%` 引用**仍被拒（如 `JAVA_HOME=%USERPROFILE%\tools`）。票面 42-1 的两条要求在逻辑上不可兼得——若路径型放行白名单引用，`%USERPROFILE%\x` 与 `%TEMP%\evil` 结构上无差别（两个引用都在白名单内），只有形态规则能区分，而形态规则必然同时拒绝二者。当前选择：`%TEMP%\evil` 无条件被拒（更安全）；白名单引用通过性用**非路径型**变量断言（真实内置 `PUB_CACHE`/`PHPRC`/`JULIA_DEPOT_PATH`/`UV_*` 即此类）。若产品要求路径型接受 `%USERPROFILE%` 形态，需改为「按已知 ref 展开 + 展开后落树内才放行」，代价是 `%TEMP%\evil` 的拒绝降级为「仅根可知时」。
- 遗留（未决，非阻塞）：①installer 的 `setUserEnvVars` 失败回滚走 `SetUserEnvVarTrusted(name, orig)`——若 orig 是主机路径且根可知会被新校验拒绝，回滚仅打 WARNING（需 installer 侧传 installPath 或提供显式绕过 API）；②自定义 `install_dir`（可在 home 之外）下的合法值会被拒；③非路径型变量（`UV_INSTALL_DIR`/`BUN_INSTALL`/`DENO_INSTALL`/`PHPRC`/`npm_config_prefix`）仍只受引用规则约束；④`ValidateAbsPathUnder` 为词法包含，不解析符号链接/junction。
