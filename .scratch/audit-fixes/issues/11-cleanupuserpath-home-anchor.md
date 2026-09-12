# 11 CleanupUserPath home 锚定

Status: open
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
