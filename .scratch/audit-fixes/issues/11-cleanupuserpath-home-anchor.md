# 11 CleanupUserPath home 锚定

Status: resolved
Type: task
批次: B2
对应修复项: P0-13（方案 §4 P0-13）

## 问题

CleanupUserPath 按 `\xdev\install\`、`\xdev\current\`、`\reverseeng\xmdevenvmt` 子串 + 任意盘符 `\xdev` 后缀（isXDevRoot:1255-1269）匹配删除 PATH 条目 → 用户无关路径（D:\xdev 项目目录）被静默删除，每次安装/doctor 运行。

## 方案

1. 签名加 `home string` 参数；调用方三处同步：cli/install.go:159、cli/doctor.go:161、gui/app_migration.go:310（InitConfig 内传 baseDir）；**internal/env/win_path_other.go:65-68 Unix stub 同签名必须同步改**（否则 Unix 编译失败）。
2. 匹配改精确前缀：`strings.HasPrefix(normalized, ToLower(Clean(home))+PathSeparator)` 或 `EqualFold(Clean(p), Clean(home))`；`\xdev\install\`/`\xdev\current\` 子串规则被 home 前缀覆盖。
3. `\reverseeng\xmdevenvmt` 与 `\ximulocaldev\...` 保留子串匹配（任意深度——本仓库 clone 上 PATH 会被删，release note 提示；彻底做法收紧为历史根全前缀，备选）。
4. 测试改造：win_path_test.go 4 处调用（:152/:196/:235/:274 save/restore 直写真实 PATH）按新签名重写。

## 守卫测试

负向：shimDir=E:\XDev\shims 时 PATH 含 `d:\xdev` 不被删；`e:\xdev\install\go\1.22` 被删。

## Comments（Batch 2 实施记录）

- 改动：签名 `CleanupUserPath(home, shimDir, pathMode)`（Windows 实现 + Unix stub 同步）；`cleanupUserPathLocked` 匹配逻辑重写为 **home 锚定三规则**——`<home>\install\` 前缀无条件删、`<home>\current\` 前缀仅 shim 模式删（保留 `<home>\current\xdev` 自身 junction）、`normalized == homeClean` 删裸 home 根（替代任意盘符的 isXDevRoot 规则）；保留三个历史子串规则（`\ximulocaldev\xmdevenvmt\shims`、`\reverseeng\xmdevenvmt`、`\testinitconfig_nodeadlock`）。调用方三处传 home：`internal/cli/install.go`（`Cfg().Core.XMDevDir`）、`internal/cli/doctor.go`（`cfg.Core.XMDevDir`）、`internal/gui/app_migration.go`（`baseDir`）。
- 守卫测试：`internal/env/win_path_test.go` — TestCleanupUserPath_KeepsUnrelatedXdevDir（H1）/ _RemovesUnderHome（H2）/ _KeepsHistoricalSubstrings（H3）/ _CaseInsensitiveHomePrefix（H4）；既有四处用例（ShimMode/DirectMode/RemoveXDevRoot/KeepCurrentXdev）按新签名改写并保持原断言语义（direct 模式 current 条目保留 = 行为等价性硬约束）。
- **PoC 先红后绿（H1）**：临时把 home 等值规则换回旧 `isXDevRoot(normalized)` 后运行 → 红：`unrelated D:\xdev directory was deleted — home anchoring regression: PATH="E:\XDev\shims;C:\Windows\System32"`；恢复后 → `--- PASS: TestCleanupUserPath_KeepsUnrelatedXdevDir (1.05s)`。
- Release note 条目：**home 迁移后旧 home 的 install/current PATH 残留不再被清理**（新 home 之外的 xdev 形态目录一律视为用户资产保留）；`\reverseeng\xmdevenvmt` 子串规则保留意味着本仓库 clone 的 PATH 条目仍会被删（历史规则，已在 b2.md 记录为备选收紧项）。
