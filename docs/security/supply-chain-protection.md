# 供应链攻击防护策略

> **文档类型**：安全策略
> **状态**：Active
> **创建日期**：2026-07-26
> **所有者**：xinyan <zuijiuhjd@foxmail.com>

## 威胁模型

| 攻击向量 | 描述 |
|---|---|
| 镜像源投毒 | 镜像站被入侵，替换工具下载包为恶意版本 |
| 中间人攻击 | 网络劫持替换下载流量 |
| Manifest 篡改 | 修改 manifest 中的 URL 或 SHA-256 指向恶意下载 |
| 官方源被入侵 | 工具的官方 release 仓库被入侵 |

## 现有防护机制（四层防线）

### 第一层：HTTPS 强制传输

- 所有 manifest 中的 URL 必须使用 `https://`（0 个 HTTP URL）
- 下载器 `internal/downloader/downloader.go` 强制 HTTPS：
  - `http://` URL 被拒绝（除非显式 `AllowHTTPForTests`）
  - HTTPS 重定向到 HTTP 被拒绝（`CheckRedirect` 检查降级）
- 防止中间人攻击窃听和篡改传输内容

### 第二层：SHA-256 完整性校验

- 每个 manifest archive 条目都有 `sha256` 字段（320/320 覆盖）
- 下载后计算实际 SHA-256 与 manifest 声明值比对
- 不匹配时删除已下载文件并报错：
  ```
  sha256 mismatch: expected abc123..., got def456...
  ```
- 占位 hash（全零）时拒绝下载（除非签名验证关闭的 TOFU 模式）

### 第三层：Ed25519 Manifest 签名验证

- 每个 `.yaml` manifest 有对应的 `.yaml.sig` 签名文件（37/37 覆盖）
- 签名私钥仅存在于 GitHub Actions Secrets（`XDEV_SIGNING_PRIVATE_KEY`）
- 公钥通过 Go ldflags 注入二进制（`internal/signer.rootPublicKeyB64`）
- 运行时验证 manifest 签名，签名不匹配则拒绝使用该 manifest
- 签名密钥缺失时 release job 硬门禁失败（Spec 003）

### 第四层：Mirror 智能选择 + 健康缓存（Spec 009）

- 并发 HEAD 探测候选 mirror，选择延迟最低的可用源
- 健康缓存记录已知不可用的 mirror（24h 有效期），避免重复尝试 404 源
- 所有 mirror 不可用时自动降级到官方源
- Mirror 投毒防护：即使 mirror 被投毒，SHA-256 校验仍会拒绝篡改后的下载包

## 投毒场景分析

### 场景 1：Mirror 源被投毒

```
攻击者入侵 TUNA 镜像站 → 替换 python-3.12.7.tar.gz 为恶意版本
→ 用户通过 xdev install python 下载 → SHA-256 校验失败 → 下载被拒绝
```

**防护**：SHA-256 校验（第二层）确保即使 mirror 被投毒，篡改内容也无法通过验证。

### 场景 2：官方源被入侵

```
攻击者入侵 GitHub release → 替换工具包 + 修改 manifest 中的 SHA-256
→ manifest 签名验证失败 → 拒绝使用篡改后的 manifest
```

**防护**：Ed25519 签名（第三层）确保 manifest 完整性。攻击者无法伪造签名（无私钥）。

### 场景 3：中间人攻击

```
攻击者劫持 HTTPS 连接 → 尝试替换下载内容
→ HTTPS 证书验证失败 → 连接被拒绝
```

**防护**：HTTPS 强制（第一层）确保传输安全。

### 场景 4：签名密钥泄露

```
攻击者获取 Ed25519 私钥 → 签名恶意 manifest
→ 用户下载恶意工具
```

**防护**：需要立即轮换密钥对：
1. 生成新 Ed25519 密钥对：`xdev keygen --priv-key keys/priv.key --pub-key keys/pub.key`
2. 更新 GitHub Secrets：`XDEV_SIGNING_PRIVATE_KEY` 和 `XDEV_SIGNING_PUBLIC_KEY`
3. 用新私钥重签所有 manifest：`xdev sign-manifests --manifests-dir manifests --priv-key keys/priv.key`
4. 发布新版本（新公钥通过 ldflags 注入二进制）

## 镜像源策略

### 原则

1. **每个工具至少一个可用 mirror**：减少对单一源的依赖
2. **Mirror 不可用不影响安装**：自动降级到官方源
3. **不限制软件源类型**：官方源、GitHub、第三方云服务商、大学镜像站均可
4. **Mirror 健康自动维护**：Spec 009 健康缓存自动记录和跳过不可用 mirror

### 镜像源可用性要求

- Mirror URL 必须返回 HTTP 2xx/3xx 才被视为可用
- 404/403/超时 的 mirror 会被健康缓存标记为不可用（24h 内不再尝试）
- 不可用 mirror 的 `mirror_url` 字段应设为空字符串 `""`，安装器自动降级到官方源
- 定期审计镜像可用性（如 CI 中运行 linkaudit probe）

### 镜像源推荐优先级

| 优先级 | 类型 | 示例 | 说明 |
|---|---|---|---|
| 1 | 工具官方 CDN | cmake.org, erlang.org | 最可靠 |
| 2 | 云服务商镜像 | npmmirror.com, repo.huaweicloud.com | 商业保障，SLA |
| 3 | 大学镜像 | mirrors.tuna.tsinghua.edu.cn | 公益服务，可能不稳定 |
| 4 | GitHub release | github.com | 官方发布，无需镜像 |