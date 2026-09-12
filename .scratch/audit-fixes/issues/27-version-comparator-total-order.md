# 27 版本比较器全序化（必红测试反转）

Status: open
Type: task
批次: B5
对应修复项: P1-10（D-05）（方案 §5 主题 B）

## 方案

versionReleaseNewer（manifest.go:615-627）改**全序元组**：`CompareSemver → 日期（缺省零值）→ 字符串`。

## 实施清单（三条缺一不可）

1. **显式反转既有测试** `TestSortVersionsByRelease_DatedNewestBeatsOlderSemver`（manifest_test.go:1247-1256）——现断言「日期优先」，与新语义直接矛盾；**先红后绿，反转不是回归**。
2. 真实数据日期倒挂（lua 5.5.0 早于 5.4.8；java "26" 早于 "25"）→ 修复后 GUI 版本列表顶部变化 + UpdatePackageManager（app_local_detect.go:1587 取 [0]）实装版本变化（方向正确）——**release note + e2e 冒烟**。
3. 「日期缺省零值」语义（同 semver 下无日期者恒排后）写进测试。

## 守卫测试

A(1.0.0,2024-06) / B(2.0.0,2024-01) / C(1.5.0,无) 三元组传递成立且 2.0.0 排顶（复核独立推演同环为反例依据）。
