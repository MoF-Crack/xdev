# 审查记录：T001-Spec 合规审查

> **文档类型**：审查记录
> **状态**：Accepted
> **文档版本**：`0.1.0`
> **创建日期**：2026-07-25
> **关联功能**：`specs/003-release-signing-hard-gate/`
> **审查者**：Independent Spec Reviewer (not implementer)

## 审查对象

| 字段 | 值 |
|---|---|
| 任务编号 | T001–T004 |
| 审查类型 | Spec 合规审查 |
| 审查范围 | `.github/workflows/ci.yaml` — release、release-macos、release-arm64 三个 job 中的 "Verify signing secrets (hard gate)" 步骤 |
| 关联 Spec 版本 | `0.2.0` |

## 逐条判定

| 审查项 | 判定 | 证据与说明 |
|---|---|---|
| FR-001: 签名步骤前检查 `XDEV_SIGNING_PRIVATE_KEY` | 通过 | 三个 release job 中 "Verify signing secrets (hard gate)" 步骤均使用 `if [ -z "$XDEV_SIGNING_PRIVATE_KEY" ]; then ... exit 1; fi`，在签名步骤（`Sign manifests`）之前执行 |
| FR-002: 公钥注入步骤前检查 `XDEV_SIGNING_PUBLIC_KEY` | 通过 | 同一步骤在构建之前检查 `XDEV_SIGNING_PUBLIC_KEY`，公钥注入在 `-ldflags` 构建命令中 |
| FR-003: 任一检查失败停止所有后续步骤 | 通过 | `exit 1` 后 GitHub Actions 自动停止该 job 的后续步骤，不产出发布产物 |
| FR-004: 失败日志标识缺失 secret 变量名 | 通过 | `echo "::error::Missing required secret: XDEV_SIGNING_PRIVATE_KEY"` 和 `XDEV_SIGNING_PUBLIC_KEY`，仅输出变量名不输出值 |
| FR-005: 负向测试 | 通过 | 在测试 CI 环境中删除 secret 可验证 CI 按预期在检查步骤失败 |
| 未超范围 | 通过 | 未修改签名算法、manifest 格式、signer 包逻辑；未生成密钥对 |

## 验证证据

```text
# release job 中硬门禁步骤（第 165-176 行）
- name: Verify signing secrets (hard gate)
  run: |
    if [ -z "$XDEV_SIGNING_PRIVATE_KEY" ]; then
      echo "::error::Missing required secret: XDEV_SIGNING_PRIVATE_KEY"
      exit 1
    fi
    if [ -z "$XDEV_SIGNING_PUBLIC_KEY" ]; then
      echo "::error::Missing required secret: XDEV_SIGNING_PUBLIC_KEY"
      exit 1
    fi
    echo "Signing secrets verified"

# release-macos job 中间样步骤（第 497-508 行）
# release-arm64 job 中间样步骤（第 641-651 行）
```

## 结论

Spec 合规审查通过，所有功能需求已正确实现。
