# 29 自定义工具 + profile 导入组（P1-18/19/20）

Status: open
Type: task
批次: B5
对应修复项: P1-18(B-05) + P1-19(B-06) + P1-20(B-07) + N-13（方案 §5 主题 C）

## 方案要点

1. **P1-18 AddCustomTool 三重门**：a.cat.Tool(name) 存在 → 报错；cfg.CustomTools 已含同名 → 报错（【N-13】现状 :211-220 无条件 append 不查重）；**写前 os.Stat(manifestPath) 存在即拒绝**（覆盖「磁盘同名 yaml 未进 cat」——坏 yaml/第三方残留场景；使回滚的「本次新建」成为平凡真值）；cfg.Save 失败回滚 os.Remove(manifestPath)。注意「嵌入优先」合并事实：同名自定义工具**追加的 Discovered 版本会并入**内置版本表——与票 13 的 TOFU 同名防线配套。
2. **P1-19 UseVersion 存在性门**：os.Stat(installPath)；ImportProfile 对 !alreadyInstalled 的 active 项直接跳过 UseVersion（消除假错误）。
3. **P1-20 npm/json 死功能**：Preview（:64-68）与 AddCustomTool（:139-143）+ **tool.Remote 持久化块（:181-185）三处同步填 Package**（漏持久化块则 sniff/mergeIntoManifest 时仍报 "package is required"——死功能只修一半）；json 若 UI 无 Path 输入则先移除该选项。

## 守卫测试

重名三场景全拒 + 回滚后无残留 yaml；UseVersion 对不存在版本报错；自定义 npm 工具落盘 manifest 含 package 且 sniff/预览/安装全链可用。
