> **文档类型**：安全操作流程
> **状态**：Deprecated
> **来源**：docs/signing-guide.md
> **P4B 迁移目标**：docs/security/procedures/signing-guide.md
> **替代文档**：无
> **治理记录**：P4A 元数据治理，2026-07-16
> **迁移记录**：P4B 迁移完成，2026-07-16，源路径：docs/signing-guide.md

# XDev Manifest 签名指南

> 版本: 1.0 | 最后更新: 2026-07-08

---

## 1. 概述

XDev 使用 **Ed25519 签名 + SHA-256 校验** 双层安全机制保护工具下载的完整性。签名系统覆盖所有内置 manifest（36 个工具，277 个归档条目），采用**分层信任模型**平衡安全性与扩展性：

```
开发者持有私钥
    │
    ├── xdev sign-manifests ──→ manifests/*.yaml.sig（Ed25519 签名）
    │
    ├── build.ps1 ──→ 公钥嵌入二进制 ──→ xdev.exe
    │
    └──────────────────────────────────────┘
                                       │
用户运行 xdev install python@3.12
    │
    ├── 1. 读取 python.yaml.sig（签名文件）
    ├── 2. 用嵌入的公钥验证签名（Ed25519）
    ├── 3. 验证通过 → 信任 manifest 中的 SHA-256
    ├── 4. 下载归档
    └── 5. 计算下载内容的 SHA-256 → 与签名的哈希比对
```

### 分层信任模型

| 来源 | 有签名文件 | 无签名文件 |
|------|-----------|-----------|
| **内置 manifest**（随二进制发布） | 验证签名 → 通过则信任 SHA-256 | **生产构建拒绝下载**，开发构建走 TOFU |
| **第三方 manifest**（`xdev manifest add`） | 验证签名（使用嵌入公钥） | TOFU + stderr 警告 |

**设计目标**：
- 内置工具：强制完整性保护，防止供应链攻击
- 第三方工具：不阻碍社区生态，通过 TOFU 提供基线保护

---

## 2. 密钥管理

### 2.1 生成密钥对

```powershell
# 生成 Ed25519 密钥对
go run ./cmd/xdev-keygen --pub-key keys/pub.key --priv-key keys/priv.key
```

输出：
- `keys/pub.key`（44 字节 base64 编码公钥）— 可公开
- `keys/priv.key`（88 字节 base64 编码私钥）— **必须保密**

密钥文件格式：纯文本 base64 编码。公钥为 32 字节 Ed25519 公钥的 base64 编码，私钥为 64 字节 Ed25519 种子 + 公钥连接的 base64 编码。

### 2.2 私钥安全规则

- 私钥文件权限为 `0600`（仅所有者可读写）
- `keys/priv.key` 已在 `.gitignore` 中排除，不会提交到版本控制
- 不要分享给不信任的人
- 建议备份到加密存储（如密码管理器）
- 不要在 Docker 镜像或 CI 日志中泄露私钥内容

### 2.3 密钥轮换

需要轮换密钥的场景：
- 私钥泄露
- 定期安全策略要求
- 签名算法升级

轮换步骤：
1. 生成新密钥对
2. 用新私钥重新签名所有 manifest
3. 用新公钥构建新二进制
4. 发布新版本

> **注意**：旧版本二进制（嵌入旧公钥）无法验证新签名。用户必须升级 xdev 才能使用新签名的 manifest。

---

## 3. 发布流程

### 3.1 发布前检查清单

```
□ 1. 所有 manifest 的 SHA-256 已填充（无全零占位符）
     检查命令：rg "sha256: 0{64}" manifests/ — 应无输出

□ 2. 重新签名所有 manifest（即使只改了一个）
     go run ./cmd/xmdevenvmt sign-manifests --manifests-dir manifests --priv-key keys/priv.key

□ 3. 验证签名文件已生成
     ls manifests/*.yaml.sig — 每个工具对应一个 .sig 文件

□ 4. 构建注入公钥
     ./build.ps1
     输出应包含：Embedding Ed25519 public key

□ 5. 验证签名已激活
     构建后的二进制运行 xdev install <tool>@<ver>
     日志中应出现 "Manifest signature verified successfully"
```

### 3.2 修改 manifest 后的流程

每次修改 `manifests/*.yaml`（新增版本、修改 URL、更新 SHA-256）后：

1. 如果新增了版本且 SHA-256 为全零：先运行 `xdev fill-sha256` 填充哈希
2. 运行 `xdev sign-manifests` 重新签名
3. 重新构建二进制

### 3.3 fill-sha256 与 sign-manifests 的执行顺序

```
xdev fill-sha256 → 下载归档 → 计算 SHA-256 → 写入 manifest YAML
                                                        │
xdev sign-manifests ← 读取 manifest 中的 SHA-256 ←──────┘
        │
        └──→ 用私钥签名 SHA-256 map → 写入 .yaml.sig
```

- `fill-sha256` 是可选的（如果手动填入 SHA-256 可跳过）
- `sign-manifests` 是必须的（内置 manifest 在生产构建中被强制要求签名）

### 3.4 签名单个工具（替代方案）

`sign-manifests` 命令签名所有工具。如需签名单个工具，可以：
1. 临时修改 `manifests/` 目录，只保留目标工具的 YAML 文件
2. 运行 `xdev sign-manifests`
3. 恢复其他工具的 YAML 文件

（单工具签名功能将在后续版本中添加 `--tool` 标志。）

---

## 4. 构建模式对照

| 维度 | 开发构建 `go build .` | 生产构建 `./build.ps1` |
|------|---------------------|----------------------|
| 公钥注入 | ❌ `rootPublicKeyB64=""` | ✅ 从 `keys/pub.key` 读取注入 |
| `signer.HasRootKey()` | `false` | `true` |
| 内置 manifest 有签名 | 签名存在但不验证（无公钥）→ 跳过 | **验证签名** → 通过则信任 |
| 内置 manifest 无签名 | TOFU + stderr 警告 | **拒绝下载** |
| 第三方 manifest 有签名 | 签名存在但不验证 → 跳过 | **验证签名** → 通过则信任 |
| 第三方 manifest 无签名 | TOFU + stderr 警告 | TOFU + stderr 警告 |
| 全零 SHA-256 | TOFU + stderr 警告 | 内置拒绝 / 第三方 TOFU |

### 关键代码路径

- 公钥检查：`internal/signer/verify.go:82` `RootPublicKey()` — 返回 nil（开发）或 32 字节公钥（生产）
- 签名检查：`internal/downloader/downloader.go:102-114` — 有签名则验证，无签名时根据来源决定拒绝或 TOFU
- SHA-256 检查：`internal/downloader/downloader.go:372-401` — 生产构建拒绝内置 manifest 的全零/无哈希

---

## 5. 第三方 manifest 安全模型

### 5.1 TOFU（Trust On First Use）

第三方 manifest 在没有签名文件时使用 TOFU 模型：

1. **首次下载**：计算归档的 SHA-256，保存到 sidecar 文件（`<cache>/<file>.sha256`）
2. **后续下载**：重新计算 SHA-256，与 sidecar 中的哈希比对
3. **不匹配**：拒绝下载并报错

Sidecar 文件写入权限为只读（`0o444`），防止被非授权修改。

### 5.2 安全建议

- 只从可信来源添加第三方 manifest 仓库（`xdev manifest add <url>`）
- 第三方 manifest 的作者可以使用官方公钥签名其 manifest（.yaml.sig），使用户获得签名验证保护
- 定期审计第三方 manifest 的内容和来源

### 5.3 未来路线

- 多签名者支持：允许第三方作者使用自己的 Ed25519 密钥对签名，用户导入对应的公钥
- Manifest 仓库签名：对整个第三方仓库进行签名，而非逐个工具签名

---

## 6. 故障排查

| 错误信息 | 原因 | 解决 |
|---------|------|------|
| `manifest signature is missing for tool download` | 生产构建下载无签名的内置工具 | 运行 `xdev sign-manifests` 签名 |
| `manifest signature verification failed` | 签名验证失败（manifest 被篡改或签名过期） | 重新签名 |
| `expected SHA-256 ... not found in signed manifest` | manifest 中的哈希不在签名覆盖范围内（修改了 manifest 但未重新签名） | 重新签名 |
| `manifest has all-zero SHA-256 placeholder` | 内置 manifest 有全零占位符 | 运行 `xdev fill-sha256` 填充哈希 |
| `manifest has no SHA-256 hash` | manifest 完全没有 sha256 字段 | 填充哈希或降级为开发构建 |
| `[downloader] Manifest signature verified successfully` | 签名验证通过（正常） | — |
| `[signer] WARNING: no public key embedded` | 开发构建（正常行为） | 生产构建用 `./build.ps1` 而非 `go build .` |
| `[signer] WARNING: no real public key embedded` | 开发构建 | 同上 |
| `[download] sha256 is all zeros` | 全零 SHA-256 占位符（开发构建 TOFU） | 生产构建前填充哈希 |

---

## 7. 内部实现概述

签名系统涉及的关键文件和入口：

| 文件 | 作用 |
|------|------|
| `cmd/xdev-keygen/main.go` | 密钥生成 CLI 工具 |
| `internal/signer/signer.go` | Ed25519 签名/验证核心逻辑、密钥加载 |
| `internal/signer/verify.go` | 公钥管理、嵌入签名文件读取 |
| `internal/cli/sign_manifest.go` | `xdev sign-manifests` 和 `xdev fill-sha256` 命令 |
| `internal/downloader/downloader.go:102-114` | 下载前签名检查（分层信任模型） |
| `internal/downloader/downloader.go:484-538` | `verifyManifestSignature()` 签名验证实现 |
| `internal/installer/installer.go:290-317` | 签名文件加载（磁盘 + 嵌入 FS） |
| `main.go:22-23` | `go:embed manifests/*.yaml.sig` 嵌入签名文件 |
| `build.ps1:28-35` | 构建时公钥注入 |
| `build.sh:9-16` | Unix 构建时公钥注入 |

### 签名文件格式

`.yaml.sig` 文件是 JSON 格式的 `ManifestSignature`：

```json
{
  "tool": "python",
  "sha256_map": "{\"3.12.7\":{\"windows/amd64\":\"abc123...\",\"linux/x86_64\":\"def456...\"}}",
  "timestamp": "2026-07-08T00:00:00Z",
  "signature": "base64_encoded_ed25519_signature"
}
```

- `tool`：工具名称
- `sha256_map`：JSON 字符串，版本 → OS/架构 → SHA-256 哈希的映射
- `timestamp`：签名时间戳（RFC3339）
- `signature`：base64 编码的 Ed25519 签名，覆盖 `tool + "\n" + sha256_map + "\n" + timestamp`

---

## 8. 附录：命令速查

```powershell
# 生成密钥对
go run ./cmd/xdev-keygen --pub-key keys/pub.key --priv-key keys/priv.key

# 签名所有 manifest
go run ./cmd/xmdevenvmt sign-manifests --manifests-dir manifests --priv-key keys/priv.key

# 填充 SHA-256 哈希（发布前）
go run ./cmd/xmdevenvmt fill-sha256 --manifests-dir manifests --dry-run
go run ./cmd/xmdevenvmt fill-sha256 --manifests-dir manifests

# 生产构建（自动注入公钥）
./build.ps1

# 手动构建注入公钥
go build -ldflags "-X github.com/xmdemt/xdev/internal/signer.rootPublicKeyB64=$(cat keys/pub.key)"

# 验证签名状态
go build -ldflags "-X github.com/xmdemt/xdev/internal/signer.rootPublicKeyB64=$(cat keys/pub.key)" .
.\xdev.exe install python@3.12 2>&1 | Select-String "signature"
```
