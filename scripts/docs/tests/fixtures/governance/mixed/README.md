# Governance Mixed Fixtures

此目录用于 `validate-governance.ps1` 的综合场景测试。

## 用途

`validate-governance.ps1` 的 SelfTest 模式可使用此目录创建跨文档一致性测试场景，例如：

- Spec 引用不存在的 Constitution 版本
- Plan 引用的 Spec 版本与实际 spec.md 版本不一致
- `.specify/feature.json` 指向 `specs/archive/` 路径
- 安全例外已过期
- 活跃 Spec 目录缺少 spec.md

## 固件创建说明

在 `validate-governance.ps1` 的 `Invoke-SelfTest` 函数中按需动态创建临时测试场景。
此目录作为预留位置，不包含预置固件文件。
