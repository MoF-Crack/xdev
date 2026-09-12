# 16 ImportProfile mirrors 校验 + discoverJSON 上限

Status: open
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
