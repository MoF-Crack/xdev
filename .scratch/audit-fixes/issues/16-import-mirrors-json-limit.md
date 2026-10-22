# 16 ImportProfile mirrors 校验 + discoverJSON 上限

Status: resolved
Type: task
批次: B3
对应修复项: P1-17 + P1-31（方案 §5）

## 问题

1. B-04：gui/app_profile.go:199-206 `cfg.Mirror = profile.Mirrors` 直落盘绕过 SetMirrorConfig 的 IsSafeHTTPSExternalURL（cli/profile.go:179 同病）。
2. C-05：remote.go:893-897 discoverJSON 无响应体上限（GitHub 16MiB/HTML 2MiB 有）→ 恶意 manifest 源可 OOM。

## 方案

1. profile.Mirrors 逐项 `util.IsSafeHTTPSExternalURL` 校验，拒绝项计入 result.Errors；cli/profile.go:179 同修。
2. `json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&root)`。

## 守卫测试

含 `http://127.0.0.1/x` 的 profile → errors 出现且 cfg.Mirror 未写入；discoverJSON 对 >16MiB 响应返回 decode 错误而非 OOM。

## Comments（Batch 3 实施记录）

- 改动：`internal/gui/app_profile.go` — ImportProfile 的 mirrors 逐项 `util.IsSafeHTTPSExternalURL` 校验，不安全项按该文件既有写法 append 到 errors，仅在有合法项时 `mutateCfg(cfg.Mirror = safeMirrors; cfg.Save())`。`internal/cli/profile.go` — CLI 侧同语义（不安全项打印 `✗ mirror <name>: <err>` 且不写入；全部被拒时不清空既有 mirrors，只报错）。`internal/remote/remote.go` — 新增 `const jsonBodyLimit = 16 << 20`，discoverJSON 解码改 `json.NewDecoder(io.LimitReader(resp.Body, jsonBodyLimit))`。
- 守卫测试：`internal/cli/profile_import_mirror_test.go` + `internal/gui/app_profile_mirror_test.go`（TestImportProfile_RejectsHTTPMirror：http://127.0.0.1、https://127.0.0.1、https://10.0.0.5 被拒且未落盘；https://1.1.1.1/mirror 保留）/ `internal/remote/discover_json_limit_test.go`（TestDiscoverJSON_LimitsBody：17 MiB body + 60s 看门狗 → `decode JSON: unexpected EOF`，470ms 内返回）。
- **变异校验**：去掉 LimitReader → "discoverJSON accepted a 17 MiB body — FAIL"；mirrors 直接赋值 → cfg.Mirror 出现 `npm=http://127.0.0.1/x` 等 3 项 → FAIL。已还原。
- 测试设计注意：正例镜像统一用 IP 字面量（`https://1.1.1.1/mirror`）规避 DNS 依赖（IsSafeHTTPSExternalURL 对 IP 字面量不做解析）。
