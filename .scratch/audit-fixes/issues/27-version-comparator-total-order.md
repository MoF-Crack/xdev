# 27 版本比较器全序化（必红测试反转）

Status: resolved
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

## Comments（Batch 5 实施记录）

- 改动：`internal/manifest/manifest.go` `versionReleaseNewer` 改**全序元组**（semver 主键 → 日期次键（缺省=零值，有日期者排前）→ 原始字符串），头注释重写为完整语义 + 非传递性说明。
- **必红反转（V1）**：既有 `TestSortVersionsByRelease_DatedNewestBeatsOlderSemver` 重命名/改造为 `TestSortVersionsByRelease_SemverPrimaryThenDate`，断言**反转**为「2.0.0 排顶」。红绿记录（临时回旧日期优先比较器实测）：
  - V1 红：`got "1.9.0" first, want semver-newest 2.0.0 (date must not outrank semver)`
  - V2 红：`2.0.0 must rank above 1.0.0 regardless of dates` + `comparator is not transitive — sort order would depend on input order`（**非传递性实证**：A>B 按日期、B>C 按 semver、C>A 按 semver 构成环）
  - V4 红：`lua.yaml sorted top = "5.4.8", want "5.5.0"（order: [5.4.8 5.5.0 5.3.6]）` + `java.yaml sorted top = "25", want "26"（order: [25 26 21 17 11]）` ← **真实数据日期倒挂实证**
  恢复后：`ok internal/manifest 0.689s`，探针 grep 0 残留。
- 守卫测试：V1 `TestSortVersionsByRelease_SemverPrimaryThenDate` / V2 `TestVersionReleaseNewer_TransitiveNoCycle`（三元组 + sort 后顺序 [2.0.0, 1.5.0, 1.0.0]）/ V3 `TestVersionReleaseNewer_MissingDateSortsAfter`（同 semver 无日期恒排后 + 双日期按日期）/ V4 `TestSortVersionsByRelease_RealDataOrder`（真实 lua.yaml 5.5.0 顶、java.yaml "26" 顶）。
- Release note 条目：**版本列表/`latest` 语义变化**——此前日期优先会使 lua 顶为 5.4.8、java 顶为 "25"；现在按 semver 排序（lua 5.5.0、java "26" 顶）。`UpdatePackageManager` 的实装版本取 [0]，方向正确（装最新 semver）。e2e 视觉基线若依赖具体版本顺序需重拍（本批 e2e 在最终门禁里核验）。
