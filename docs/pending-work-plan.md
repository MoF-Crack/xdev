> **文档类型**：工作计划
> **状态**：Frozen（待拆分）
> **来源**：docs/pending-work-plan.md
> **P4B 迁移目标**：独立事项转为 Feature Spec 后归档至 docs/history/implementation-plans/
> **替代文档**：无
> **治理记录**：P4A 元数据治理，2026-07-16

# 后续待处理工作方案

> **创建日期**：2026-07-15
> **状态**：待实施
> **关联**：`docs/history/implementation-plans/2026-07-11-xdev-ui-download-sources.md` Task 13-17 后续
> **缓存回收方案**：`docs/security/procedures/download-cache-recovery.md`

---

## 总览

xdev UI 重设计与下载源治理已完成 Task 1-18。以下 5 项后续工作待处理，每项包含主方案、第二方案和兜底方案。

| # | 待处理项 | 优先级 | 预计工作量 | 依赖 |
|---|---|---|---|---|
| 1 | Windows full SHA audit（~29GB 下载） | 高 | 6-10 小时 | 无 |
| 2 | `.yaml.sig` 重签 | 高 | 1 小时 | 依赖 #1 的 SHA 结果 |
| 3 | 不可修复工具（Haskell/Kotlin/Ninja/UV） | 中 | 文档化，30 分钟 | 无 |
| 4 | TRANSIENT 错误重试 | 中 | 1 小时 | 无 |
| 5 | `toolsHashFullCheck` vet 告警 | 低 | 5 分钟 | 无 |

---

## 1. Windows full SHA audit

### 精确范围

#### 占位 Hash（32 条，4 个 Android manifest）

| Manifest | 版本 | 平台 | 条目数 | 行号 |
|---|---|---|---|---|
| `android-build-tools.yaml` | 33.0.2, 34.0.0, 35.0.0 | win/linux | 6 | 24,32,44,52,64,72 |
| `android-cmdline-tools.yaml` | 9477386, 10406996, 11076708 | win/linux/darwin | 9 | 22,30,38,50,58,66,78,86,94 |
| `android-ndk.yaml` | r25c, r26d, r27c | win/linux/darwin | 9 | 23,31,39,51,59,67,79,87,95 |
| `android-platform.yaml` | 31, 33, 34, 35 | win/linux | 8 | 20,28,40,48,60,68,80,88 |

占位 hash 值：`a04cfd774f522d7944520b01a09b3e0fbf4069e9a806cd0c620179b987340029`

#### 同 URL 不同 SHA 冲突（5 个 manifest）

| Manifest | 版本 | URL 数 | 每URL的SHA数 | 说明 |
|---|---|---|---|---|
| `kotlin.yaml` | 2.0.21, 1.9.24, 1.8.22 | 3 | 3 | 同一 ZIP 在 win/linux/darwin 配不同 SHA |
| `erlang.yaml` | 27.1.2, 26.2.5, 25.3.2 | 3 | 2 | 同一 source tarball 在 linux/darwin 配不同 SHA |
| `git.yaml` | 2.47.0, 2.45.2, 2.43.0 | 3 | 2 | 同一 kernel.org tarball 在 linux/darwin 配不同 SHA |
| `gradle.yaml` | 7.6.4 | 1 | 2 | 同一平台无关 ZIP 在 win/linux 配不同 SHA |
| `mingw.yaml` | 12.2.0, 11.2.0 | 2 | 2 | 同一 .7z 在 win/linux 配不同 SHA |

#### 已知 HASH_MISMATCH

| Manifest | 版本 | 平台 | 当前 SHA |
|---|---|---|---|
| `ninja.yaml` | 1.10.2 | windows/amd64 | `76e4c61ca3f06ce8eb92deb306cacd90193af1a3ec96709d0506bc643817a59e` |
| `ninja.yaml` | 1.10.2 | linux/x86_64 | `763464859c7ef2ea3a0a10f4df40d2025d3bb9438fcb1228404640410c0ec22d` |

#### 批次划分

| 批次 | 工具 | 预计大小 | 条目数 | 优先原因 |
|---|---|---|---|---|
| 小批 | kotlin, erlang, git, gradle, mingw, ninja 1.10.2, android build-tools/cmdline-tools/platform | ~1.3GB | ~40 | 有 SHA 冲突，影响最大 |
| 中批 | android-ndk | ~5.8GB | 9 | 占位 hash，体积大 |
| 大批 | 其余所有工具 | ~21GB | ~150 | 验证 SHA 正确性 |

### 方案 A（主方案）：分批 full audit + 真实 SHA 回写

#### 执行步骤

1. **修复 `toolsHashFullCheck/main.go:96,103`** 的 vet 告警（删除 `\n`，2 行改动）

2. **小批**（~1.3GB，~40 分钟）：
   ```
   go run ./cmd/linkaudit -tool kotlin,erlang,git,gradle,mingw,ninja \
     -version 2.0.21,1.9.24,1.8.22,27.1.2,26.2.5,25.3.2,2.47.0,2.45.2,2.43.0,7.6.4,12.2.0,11.2.0,1.10.2 \
     -role official -mode full -platform windows/amd64 \
     -cache %TEMP%/xdev-linkaudit -cleanup
   ```
   - 下载每个 archive → 计算 SHA-256 → 与 manifest 比对
   - 同 URL 不同 SHA：取下载计算的真实 SHA，统一写入所有平台条目
   - Android 占位 hash：填入真实 SHA

3. **中批**（~5.8GB，~2 小时）：
   ```
   go run ./cmd/linkaudit -tool android-ndk -role official -mode full \
     -platform windows/amd64 -cache %TEMP%/xdev-linkaudit -cleanup
   ```

4. **大批**（~21GB，~4-8 小时）：
   - 按 linkaudit full 模式逐工具验证
   - 预期大部分 PASS，记录任何 HASH_MISMATCH

5. **SHA 回写**：
   - 修改 manifest YAML 中的 sha256 值
   - 更新 `build/manifest-changes-unsigned.md`
   - 重签 `.yaml.sig`（见待处理项 2）

#### 预期结果

- 32 个 Android 占位 hash 被真实 SHA 替换
- 12 个同 URL 不同 SHA 冲突解决（统一为下载验证的正确 SHA）
- Ninja 1.10.2 SHA 修正
- 大批工具 SHA 验证通过或修正
- 总下载 ~29GB，完成后通过 `-cleanup` 自动回收

### 方案 B（第二方案）：部分 audit + 保守降级

如果方案 A 因网络/磁盘限制无法完成全部 ~29GB：

1. **只执行小批**（~1.3GB，~40 分钟）：
   - 完成所有有冲突工具的 SHA 验证
   - Android build-tools/cmdline-tools/platform（非 NDK）

2. **NDK 和大批工具**：
   - 保留占位 hash，在 manifest 中加注释 `# SHA unverified - placeholder`
   - 在 `build/manifest-changes-unsigned.md` 记录为 "SHA pending verification"
   - CI gate 对这些工具设为 warning 而非 fail

3. **运行时行为**：
   - downloader 在 `rootPublicKeyB64=""`（当前默认）时，manifest SHA 不匹配只记 WARNING 不阻断下载
   - 用户仍可安装工具，只是无法验证完整性
   - TOFU 机制在首次下载后写 `.sha256` sidecar，后续安装可验证

#### 预期结果

- 有冲突的工具 SHA 修正（影响最大）
- NDK 和大批工具 SHA 标记为未验证
- CI gate 通过（warning 不阻断）
- 用户可用性不受影响

### 兜底方案：全部标记为未验证 + TOFU

如果连小批都无法完成（网络不可用/磁盘不足）：

1. **不修改任何 SHA**
2. **在 32 个 Android 占位 hash 条目加 YAML 注释**：
   ```yaml
   # SHA-256 unverified - placeholder value, requires full audit
   sha256: a04cfd774f...
   ```
3. **在 5 组同 URL 不同 SHA 冲突中**：
   - 保留第一个条目的 SHA，其余改为与第一个一致
   - 注释 `# SHA unified - cross-platform archive, single hash expected`
4. **CI gate 配置**：
   - `MISSING_CHECKSUM` 和 `HASH_MISMATCH` 设为 warning（不阻断）
   - 仅 `URL_DEAD` 和 `TRANSIENT_ERROR`（official）阻断
5. **文档**：
   - 在 `docs/security/policy/download-source-policy.md` 增加 "SHA verification status" 章节
   - 标注哪些工具已验证、哪些是占位值
6. **TOFU 兜底**：
   - 首次下载时 downloader 计算实际 SHA 并写 sidecar
   - 后续安装验证 sidecar 而非 manifest
   - 风险：首次下载无法防止中间人攻击（但 HTTPS 本身提供传输层保护）

---

## 2. `.yaml.sig` 重签

### 现状

- `keys/pub.key` + `keys/priv.key` 存在于项目树内
- `signer.go:32-35`：payload = `tool + "\n" + sha256_map + "\n" + timestamp`
- `verify.go:60`：`rootPublicKeyB64=""` 默认跳过验证
- `main.go:19-23`：embed `manifests/*.yaml` 和 `manifests/*.yaml.sig` 到二进制
- 修改 sha256 会使现有 `.yaml.sig` 失效（sig payload 含 sha256_map）

### 方案 A（主方案）：用现有 priv.key 重签

#### 执行步骤

1. 对每个修改了 sha256 的 manifest YAML，运行签名工具：
   ```powershell
   go run ./cmd/xdev-keygen -sign manifests/{tool}.yaml -key keys/priv.key
   ```
   或用 `signer.go` 的 `SignManifest` + `WriteSignatureFile`

2. 生成新的 `manifests/{tool}.yaml.sig`

3. 发布构建时注入公钥：
   ```powershell
   go build -ldflags "-X internal/signer.rootPublicKeyB64=$(Get-Content keys/pub.key -Raw)" .
   ```

4. 验证签名：
   ```powershell
   go run ./cmd/xdev-keygen -verify manifests/{tool}.yaml -pub keys/pub.key
   ```

#### 预期结果

- 所有修改的 manifest 有匹配的 `.yaml.sig`
- 发布二进制可验证签名
- 运行时 `RootPublicKey()` 返回真实公钥，manifest 验证启用

### 方案 B（第二方案）：不重签，保持验证关闭

如果 priv.key 不应作为生产密钥（安全风险）：

1. **不生成新 `.yaml.sig`**
2. **保持 `rootPublicKeyB64=""`**（默认跳过验证）
3. **在 `build/manifest-changes-unsigned.md` 中记录所有 SHA 变更**
4. **发布说明**：
   - 标注 "manifest signatures not updated - verification remains disabled"
   - 维护者需生成新 Ed25519 密钥对后重签

#### 预期结果

- 二进制运行时跳过签名验证（当前行为）
- SHA 值已修正但无签名保护
- 首次下载的 TOFU 提供完整性保护

### 兜底方案：生成新密钥对 + 全量重签

如果需要启用签名验证但 priv.key 不可信：

1. **生成新密钥对**：
   ```powershell
   go run ./cmd/xdev-keygen -generate -out keys/
   ```
   （覆盖旧 key 或写入 `keys/new/`）

2. **对全部 36 个 manifest 重新签名**（不只改了 SHA 的）：
   ```powershell
   Get-ChildItem manifests/*.yaml | ForEach-Object {
     go run ./cmd/xdev-keygen -sign $_.FullName -key keys/priv.key
   }
   ```

3. **发布构建注入新公钥**

4. **安全措施**：
   - 新 priv.key 不放入版本控制
   - pub.key 可公开
   - 在 `.gitignore`（如有 git）中加 `keys/priv.key`

#### 预期结果

- 全新密钥对
- 所有 manifest 签名一致
- 发布二进制启用完整签名验证
- priv.key 不泄露

---

## 3. 不可修复工具（Haskell/Kotlin/Ninja/UV）

### 现状

这 4 个工具无可信第二镜像源：

| 工具 | 调查结果 | 官方源 |
|---|---|---|
| Haskell/GHC | USTC/BFSU/SJTUG 镜像均 404 | `downloads.haskell.org` |
| Kotlin | JetBrains CDN 404 | GitHub release |
| Ninja | npmmirror 404, TUNA github-release 下线 | GitHub release |
| UV | npmmirror 404, TUNA github-release 下线 | GitHub release |

### 方案 A（主方案）：保留官方单源 + CI 降级

1. **manifest 不设 mirror_url**（当前状态）
2. **CI gate**：official `URL_DEAD`/`TRANSIENT_ERROR` 阻断；无镜像仅 warning
3. **安装器行为**：官方 URL 失败时无 fallback，直接报错
4. **文档**：在 `docs/security/policy/download-source-policy.md` 标注 "official-only, no mirror available"

#### 预期结果

- GitHub release URL 可用时正常安装
- GitHub 暂时不可用时安装失败（可重试）
- 无误导性镜像 URL

### 方案 B（第二方案）：GitHub release 代理镜像

如果 GitHub 在某些地区不可达：

1. **调研 GitHub release 代理**：
   - `https://ghproxy.com/`（社区代理，可信度未验证）
   - `https://mirror.ghproxy.com/`（同上）
   - `https://gh-proxy.com/`

2. **在 manifest 中设 mirror_url 为代理 URL**：
   ```yaml
   mirror_url: https://ghproxy.com/https://github.com/JetBrains/kotlin/releases/download/v2.0.21/kotlin-compiler-2.0.21.zip
   ```

3. **CI gate**：镜像 dead 仅 warning

#### 预期结果

- GitHub 不可达时通过代理安装
- 代理不稳定时降级到官方
- 风险：代理 URL 可能随时失效或被劫持

### 兜底方案：用户自配代理

1. **Settings 页面**：允许用户在 UI 中配置全局 HTTP 代理
2. **代理设置**：用户可设 `HTTPS_PROXY` 环境变量
3. **manifest 不设 mirror_url**
4. **文档**：推荐用户在 GitHub 不可达时配置代理

#### 预期结果

- 不依赖第三方镜像
- 用户有完全控制
- 需要用户自行解决网络问题

---

## 4. TRANSIENT 错误重试

### 现状

#### Windows official TRANSIENT（11 条，statusCode=0）

| 工具 | 版本 |
|---|---|
| dotnet | 8.0.403 |
| go | 1.21.13, 1.22.7, 1.23.2 |
| lua | 5.4.8 |
| maven | 3.8.8, 3.9.6, 3.9.9 |
| php | 8.1.30, 8.2.25, 8.3.13 |

#### 跨平台 official TRANSIENT（5 条，darwin/arm64）

| 工具 | 版本 |
|---|---|
| git | 2.43.0, 2.45.2, 2.47.0 |
| maven | 3.9.6, 3.9.9 |

### 方案 A（主方案）：重试 probe

1. **对每个 TRANSIENT 条目重试 3 次**，间隔 30 秒
2. **如果仍失败**，改用 full GET 下载（Range GET 可能被某些 CDN 拒绝）
3. **如果 full GET 也失败**，标记为 `URL_DEAD` 并调查

#### 预期结果

- 大部分 TRANSIENT 应该重试成功（网络瞬断）
- 少数可能确实是 CDN 问题（如 dotnet 某些版本的 Azure CDN 在特定地区不可达）

### 方案 B（第二方案）：改用第二官方源

dotnet 有第二微软 CDN（`dotnetcli.azureedge.net`，已在 Task 17 设为 mirror_url）：

1. 如果 official TRANSIENT 但 mirror PASS，说明是特定 CDN 节点问题
2. 在 CI gate 中：official TRANSIENT + mirror PASS = 不阻断（降级为 warning）

#### 预期结果

- 有镜像的工具不阻断 CI
- 无镜像的工具（go/lua/maven/php）仍需重试

### 兜底方案：标记为 known-transient + CI 豁免

1. **在 `docs/security/policy/download-source-policy.md` 中维护 known-transient 列表**
2. **CI gate**：known-transient 工具的 TRANSIENT_ERROR 不阻断
3. **定期重试**：每周自动运行 probe 审计，更新 known-transient 状态

#### 预期结果

- CI 不因已知网络问题阻断
- 定期监控确保问题不会持续恶化

---

## 5. `toolsHashFullCheck` vet 告警

### 现状

`go test ./...` 唯一失败：

```
toolsHashFullCheck\main.go:96:3: fmt.Println arg list ends with redundant newline
toolsHashFullCheck\main.go:103:3: fmt.Println arg list ends with redundant newline
```

### 方案（唯一）

修改 `toolsHashFullCheck/main.go:96` 和 `:103`：

```go
// 第 96 行
// 前: fmt.Println("🔄 Resetting all failed entries to pending...\n")
// 后: fmt.Println("🔄 Resetting all failed entries to pending...")

// 第 103 行
// 前: fmt.Println("🔧 Finalize mode: writing hashed entries to manifests and cleaning up...\n")
// 后: fmt.Println("🔧 Finalize mode: writing hashed entries to manifests and cleaning up...")
```

#### 预期结果

`go vet ./...` 全量通过，无任何告警。

> 此修复已纳入缓存回收方案 `docs/security/procedures/download-cache-recovery.md` 场景 3。

---

## 执行优先级与依赖关系

```
[5] vet 修复 ──────────────────────→ 独立，可立即执行（5 分钟）
[1] full SHA audit ──┐
                     ├─→ [2] 重签 ─→ 依赖 [1] 的 SHA 结果
[3] 不可修复工具 ────┘
[4] TRANSIENT 重试 ──→ 可与 [1] 并行
```

### 建议执行顺序

1. `[5]` vet 修复（2 行改动，5 分钟）
2. `[1-A]` 小批 audit（有冲突工具，~1.3GB，40 分钟）
3. `[2-A]` 用现有 priv.key 重签小批
4. `[1-A]` NDK audit（~5.8GB，2 小时）
5. `[1-A]` 大批 audit（~21GB，4-8 小时）
6. `[2-A]` 全量重签
7. `[4-A]` TRANSIENT 重试
8. `[3-A]` 不可修复工具文档化

### 缓存回收

所有 audit 步骤应配合 `-cleanup` flag 使用，确保下载文件在 SHA 验证完成后自动回收。详见 `docs/security/procedures/download-cache-recovery.md`。

### 磁盘空间预算

| 执行阶段 | 临时占用 | 回收后 |
|---|---|---|
| 小批 audit | ~1.3GB | ~0（cleanup 后） |
| 中批 audit | ~5.8GB + 小批残留 | ~0 |
| 大批 audit | ~21GB + 前批残留 | ~0 |
| 最大并发占用 | ~21GB（大批单批） | - |

建议预留至少 40GB 可用磁盘空间。
