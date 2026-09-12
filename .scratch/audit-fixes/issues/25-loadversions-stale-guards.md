# 25 loadVersions 三入口过期守卫

Status: open
Type: task
批次: B4
对应修复项: P1-22 + N-12（方案 §5 主题 C，复核修订版）

## 问题

AppContent.vue loadVersions（:933-944）无过期守卫（对照 toolDeps :883/:885 有）→ 快速连续打开两个抽屉旧响应覆写，可对 B 工具执行 A 的版本号。【N-12】openFavVersionManager（:444-454）与 openDashVersionManager（:463-474）**同型无守卫**。

## 方案

三入口各在落盘前比对**该页面自身**的展开名（对照 toolDeps 模式）：
- loadVersions → `selectedTool.value?.name === requestedName`
- openFavVersionManager → fav 展开条目比对
- openDashVersionManager → dashVersionExpanded 比对

## 守卫测试

交错 resolve 用例：B 打开后 A 慢响应不再覆写（三个入口各有断言）。
