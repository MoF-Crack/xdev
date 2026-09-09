# 01: util semver 预发布语义

Status: done

CompareSemver 扩展为 SemVer 2.0.0 预发布比较（核心段相同：无预发布 > 有预发布；
标识符逐段：数字段数值比较、字母段 ASCII、数字段 < 字母段、字段多者高）。
新增 IsPrereleaseVersion。对齐 resolver 包既有 comparePreRelease 规则。
更新 semver_test.go（原「pre-release stripped → 0」断言改为 -1）。

## Comments

- 影响面复核：CompareSemver 调用点（installer/manifest/cli/tui/doctor/gui update 检测）
  均为「比较版本新旧」语义，预发布语义使结果更正确（rc.2 > rc.1、1.0.0 > 1.0.0-rc.1）。
