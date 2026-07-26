# Implementation Plan: 发布签名硬门禁

> **文档类型**：Implementation Plan
> **状态**：Ready
> **文档版本**：0.1.0
> **创建日期**：2026-07-25
> **最后更新**：2026-07-25
> **所有者**：xinyan <zuijiuhjd@foxmail.com>
> **关联 Spec**：`specs/003-release-signing-hard-gate/spec.md` `0.2.0` (Accepted)
> **权威范围**：定义所选技术方案、模块边界、接口、数据流和验证方案

**Branch**: `003-release-signing-hard-gate` | **Date**: 2026-07-25 | **Spec**: `specs/003-release-signing-hard-gate/spec.md` `0.2.0`

---

## Summary

在 CI release job（`release`、`release-macos`、`release-arm64`）中添加 "Verify signing secrets (hard gate)" 前置检查步骤。该步骤在签名和构建操作之前检查 `XDEV_SIGNING_PRIVATE_KEY` 和 `XDEV_SIGNING_PUBLIC_KEY` 两个 secret 是否存在且非空，任一缺失时以非零退出码立即失败并明确标识缺失的 secret 变量名，阻止发布产物生成。

---

## Technical Context

**Language/Version**: Go 1.25
**Primary Dependencies**: GitHub Actions secrets 机制、bash shell
**Storage**: 无
**Testing**: 负向测试（删除 secret 时 CI 失败验证）
**Target Platform**: Windows/Linux/macOS（release job 覆盖三平台 runner）
**Constraints**: 密钥检查步骤不得将密钥内容输出到日志；仅输出变量名

---

## 技术方案

### 模块边界

| 文件 | 职责 |
|---|---|
| `.github/workflows/ci.yaml` | 在 `release`、`release-macos`、`release-arm64` 三个 job 中分别添加 "Verify signing secrets (hard gate)" 步骤 |
| `.specify/approvals/2026-07-25-spec-003-001.json` | Spec 003 审批记录 |

### 接口

**检查步骤**: "Verify signing secrets (hard gate)"
**检查逻辑**（每个 release job 均执行）:
```bash
if [ -z "$XDEV_SIGNING_PRIVATE_KEY" ]; then
  echo "::error::Missing required secret: XDEV_SIGNING_PRIVATE_KEY"
  exit 1
fi
if [ -z "$XDEV_SIGNING_PUBLIC_KEY" ]; then
  echo "::error::Missing required secret: XDEV_SIGNING_PUBLIC_KEY"
  exit 1
fi
echo "Signing secrets verified"
```

### 数据流

```
tag push → release workflow triggered
  ├── release (ubuntu-latest)
  │     → Verify signing secrets (hard gate) ← 本 Spec 实现
  │     → Sign manifests
  │     → Build all platforms
  │     → Create Release
  ├── release-macos (macos-latest)
  │     → Verify signing secrets (hard gate) ← 本 Spec 实现
  │     → Build macOS DMG
  │     → Upload macOS DMG to Release
  └── release-arm64 (ubuntu-24.04-arm)
        → Verify signing secrets (hard gate) ← 本 Spec 实现
        → Build Linux arm64 AppImage
        → Upload arm64 AppImage to Release
```

---

## 任务分解

| 任务 | 描述 | 进度 | 提交 |
|---|---|---|---|
| T001 | 在 `.github/workflows/ci.yaml` 的 `release` job 中添加 "Verify signing secrets (hard gate)" 步骤 | Completed | `d4ea53b` |
| T002 | 在 `release-macos` job 中添加相同的密钥检查步骤 | Completed | `d4ea53b` |
| T003 | 在 `release-arm64` job 中添加相同的密钥检查步骤（后续添加） | Completed | `d4ea53b` |
| T004 | 创建 Spec 003 审批文件 `.specify/approvals/2026-07-25-spec-003-001.json` | Completed | `444ec6f` |

---

## 验证方案

- **正向验证**: 在 CI 环境中正确配置 `XDEV_SIGNING_PRIVATE_KEY` 和 `XDEV_SIGNING_PUBLIC_KEY` secret，触发 release workflow，确认 "Verify signing secrets" 步骤通过
- **负向验证**: 删除任一 secret 后触发 release workflow，确认 CI 在检查步骤失败，输出 "Missing required secret: XDEV_SIGNING_..."，无发布产物生成

---

## 风险与缓解

| 风险类别 | 风险描述 | 严重度 | 缓解措施 |
|---|---|---|---|
| 安全 | 密钥检查失败但错误处理不当导致密钥泄露到日志 | 高 | 仅输出变量名，严禁将 secret 值输出到日志 |
| 兼容性 | 硬门禁可能阻塞紧急 hotfix 发布 | 中 | 紧急发布需通过安全例外流程临时豁免 |
| 签名/发布 | 门禁逻辑本身的 bug 可能错误放行 | 高 | 必须包含负向测试覆盖；代码审查重点关注边界条件 |

---

## 批准记录 (Approval History)

| 阶段 | 状态 | 批准者 | 日期 | 文档版本 | 批准范围 |
|---|---|---|---|---|---|
| Plan Ready | Ready | xinyan <zuijiuhjd@foxmail.com> | 2026-07-25 | 0.1.0 | 2026-07-25-plan-003-001 实施计划已就绪 | 实施计划已就绪 |
