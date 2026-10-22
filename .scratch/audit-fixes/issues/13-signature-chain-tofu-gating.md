# 13 签名链完整性 + TOFU 门控（P0-02 阶段1 + P0-03 同一 PR）

Status: resolved
Type: task
批次: B3
对应修复项: P0-02 阶段 1 + P0-03 + N-02/N-03/N-04（方案 §4 P0-02+P0-03——**本票以该节为唯一权威**）
耦合: **同一 PR 不可拆**（UnsignedVersion 三处门控一体）；Blocked by 01（守卫测试需可运行 go test）

## 核心修正点（相对 v1 审计稿，实施前必读）

1. **新事实前提**：LoadCatalog 为「嵌入优先」合并（manifest.go:437-474）——磁盘篡改内置字段不生效，**真实攻击路径是新增版本条目**；全部守卫测试按「新增版本」场景构造；顺带更正 manifest.go:374-378 注释。
2. **UnsignedVersion = (v.Discovered || v.Unverified)**，**不带哈希条件**（带哈希条件 → TOFU 写回后重装推导翻转 → 重装必败——v1 宣称已解决实未解决）；推导 `v, found := m.FindVersion(ver)` 不得丢 found（nil panic）。
3. **三处门控同一标志**：L-10 拒绝分支（downloader.go:544/:586）+ 签名成员检查（:705-712）+ **逐 URL 过滤**（漏过滤则重装时全候选被剔报错）。
4. **逐 URL 过滤判定语义**：精确键 `m[version][os/arch]` 存在且等于该 URL 生效哈希（经 URLShaMap 解析）；未命中剔除并 log；官方 URL 恒在候选末位（buildMirrorCandidates :971-972 证实）。
5. **回退语义收紧**：精确键不存在 → 仅当 ExpectedSHA 在**该 version 的任一 os/arch 键**下存在（老 sig 同版本跨平台兼容）且 mirror 场景 URLShaMap==官方哈希才放行——**全表跨版本回退删除**。
6. 签名图收集落点：**internal/cli/sign_manifest.go:64-86**（`internal/signer/sign_manifest.go` 不存在）。
7. **同名防线的三条腿都堵**：third-party 判定不以内置同名工具放行（含嵌入式签名命中后 :739 仍置 true 的问题——N-03）+ TOFU 资格判定施加同名守卫 + P1-18 写前存在性门（另票）。
8. verifyManifestSignature 增加 **ms.Tool == 所装工具** 校验（N-02）；TOFU 写回目录改 `i.cfg.Core.ManifestDir`（N-04，installer.go:772）。
9. GUI 侧自建推导（**不得复用 versionUnverified helper**——:458-473 漏 Discovered 且带 npm 特例）；installer_test.go:840 唯一测试调用点同步加参。

## 守卫测试（⑥为 v1 缺失的回归判据）

①磁盘**新增版本** + 篡改 mirror_url/mirror_sha256 + httptest 攻击服务器 → 拒；②新增版本复用已签名哈希（跨版本/跨 os-arch/跨工具）→ 拒；③官方 URL 正常下载过；④老 sig 同版本跨平台兼容过；⑤占位公钥构建全路径不变；⑥**TOFU 写回后重装（Discovered、哈希已实）→ 成功**。

## Comments（Batch 3 实施记录）

- 改动：`internal/downloader/downloader.go` — Options 增 `ToolName`/`Version`/`OSArch`/`UnsignedVersion`；verifyManifestSignature 增 ms.Tool ↔ ToolName 校验（N-02）+ 成员检查加 `!UnsignedVersion` 门控并改用新 `verifySHAInMapForVersion`（精确键 + **同版本** os/arch 回退，**全表跨版本回退删除**，无 version 上下文 fail-closed）；新增 `signedSHA256Map`/`filterSignatureCoveredURLs`（**逐 URL 签名覆盖过滤**，`HasRootKey()` 门控 + UnsignedVersion 门控 + 全剔报错 + 主 URL 被剔顺延）；L-10 两处拒绝分支加 `!UnsignedVersion`。
- `internal/installer/unsigned_version.go`（新）— `DeriveUnsignedVersion(m, ver)`：`m==nil→true`；`FindVersion` 命中 → `Discovered||Unverified`（**不带哈希条件**）；未命中 → true。两管线共用（GUI 经 `installer.DeriveUnsignedVersion(p.Manifest(), ver)`，替换旧 versionUnverified 分叉）。
- `internal/installer/installer.go` — downloadWithProgress 注入四字段（OSArch=`a.OS+"/"+a.Arch`，与签发端 sign_manifest.go 键格式一致）；LoadManifestSignature 同名防线（N-03：`!embeddedHit && !isBuiltinManifest(tool, ManifestDir)` 才置 isThirdParty）；TOFUArchiveIfNeeded 写回改 `i.cfg.Core.ManifestDir`（N-04）。`internal/manifest/embed.go` 增 `HasEmbeddedManifest`。
- `internal/signer/verify.go` 增 `SetRootPublicKeyForTesting`（测试注入真实根公钥，使签名门控路径可测；占位公钥构建下这些路径本不可达）。`internal/manifest/manifest.go` loadMergedCatalog 注释更正（嵌入优先 vs 原「disk as override」矛盾）。
- 守卫测试：`internal/downloader/signature_gate_test.go`（G1 三变体 / G2 红线 / G3 / G4 / G5 占位零回归 / G6 红线 / G9）；`internal/installer/signature_tofu_test.go`（G7 六态 + 「写回后哈希已实仍为 unsigned」/ G8 同名 + 真第三方对照 / G10 ManifestDir 写回 + legacy 未创建）。
- **PoC 先红后绿（G2 + G6 两条红线）**：临时恢复旧语义（全表回退 + 去掉三处 `!UnsignedVersion` 门控）后运行 → 红：
  - G2：`signature_gate_test.go:184: cross-version hash reuse must be rejected (G2 red line)`（旧全表回退放行跨版本复用）
  - G6：`signature_gate_test.go:298: first (TOFU) install failed: ... manifest has no SHA-256 hash for and signature verification is enabled — refusing to download without a verified hash`（修复前 Discovered 版本首装被 L-10 门误拒）
  恢复后：`ok internal/downloader 6.372s`（全包绿），探针 grep 无残留。
- 实施中的两处修正（测试驱动）：①逐 URL 过滤必须加 `signer.HasRootKey()` 门——占位公钥构建下过滤仍会生效，违反「发布形态零回归」锚点（G5 抓出）；②G1 的「新增版本」场景中成员检查会先于逐 URL 过滤拒绝，故 G1 改为签名覆盖该版本官方哈希的场景以精确命中过滤逻辑，另设变体 C 覆盖全未签版本的成员检查拒绝。
- 遗留：票 17/18（数据线）阻塞——签名私钥 `keys/priv.key` 不存在（.gitignore 排除、本地/环境变量均无），无法执行 52 manifest 重签。
