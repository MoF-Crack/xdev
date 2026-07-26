# ADR-0001: Ed25519 Manifest 签名验证

> **状态**：Accepted
> **日期**：2026-07-25

## 背景

XDev 通过 manifest 文件声明工具的下载 URL 和 SHA-256 校验和。在无签名验证的情况下，
攻击者可通过篡改 manifest 文件注入恶意下载 URL 或替换校验和。Constitution 原则 I 要求
"正式发布必须启用 manifest 签名验证"，且"信任判断必须发生在执行和提取之前"。

## 决策

采用 Ed25519 签名算法对 manifest 文件签名：
- 私钥（`keys/priv.key`）用于在 release CI 中签名 manifest
- 公钥通过 Go ldflags `-X` 注入二进制（`internal/signer.rootPublicKeyB64`）
- 运行时验证 manifest 的 `.yaml.sig` 签名文件
- 签名密钥缺失时 CI release job 硬门禁失败（Spec 003）

## 后果

- 正面：签名验证链端到端覆盖，manifest 篡改可检测
- 正面：公钥嵌入二进制，无需额外配置
- 负面：密钥轮换需要重新编译发布
- 负面：签名密钥泄露将导致整个信任链失效

---

## Approval History

| approval_id | decision | date |
|---|---|---|
| 2026-07-26-adr-0001-001 | Accepted | 2026-07-26 |
