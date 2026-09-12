# 13 签名链完整性 + TOFU 门控（P0-02 阶段1 + P0-03 同一 PR）

Status: open
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
