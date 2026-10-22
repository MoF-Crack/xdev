# 01 go.yaml GOROOT v2 + 注册表残留清理

Status: resolved
Type: task
批次: B1（**最优先——全部验证门前置**）
对应修复项: P0-17 第 1 项（方案 §4 P0-17）

## 问题

go.yaml `GOROOT: '{{install}}'` + strip:0（归档顶层 `go/` 未剥）→ 注册表 GOROOT 指向无 src 的父目录 → 本机 go build 报 "package fmt is not in std"（审计基线实锤）。

## 方案

1. manifests/go.yaml 删除 `GOROOT: '{{install}}'`（GOPATH `{{install}}\..\gopath` 不动）。机制依据：Go 1.26.5 `findGOROOT` 从 go.exe 向上两级自推导 + isGOROOT 校验（`go\bin\go.exe` 自推导正确——复核源码级确认）。
2. **注册表残留清理（承重，必须同票）**：Activate/doctor 检测「GOROOT 值 V 下 `V/pkg/tool` 不存在而 `V\go` 存在」（镜像 isGOROOT 探测）即删除该注册表值。不做它，存量用户升级后依旧坏（Activate 无删除机制，EnrichFromRegistry 会继续注入旧错值）。
3. doctor 检查与 P0-13 的 CleanupUserPath、P0-07 的注册表健壮性共享触点——实现时同函数域设计。

## 验收

- 本机删注册表 GOROOT 后 `go build ./...` 全绿（验证门解除）。
- 已装 1.26.5 嵌套布局机器上 `xdev use go` / `xdev doctor` 后注册表 GOROOT 被清理，`go version` 正常。
- 新装 go：注册表不写 GOROOT，`go env GOROOT` 自推导正确。

## Comments（Batch 1 实施记录）

- 改动：`manifests/go.yaml`（删 GOROOT 行 + 注释说明 v2 依据）；`internal/env/goroot_cleanup_windows.go`（CleanupStaleGOROOT + isGOROOTDir，删除条件 `!isGOROOTDir(V) && isGOROOTDir(V+"\\go")`，删除后广播 WM_SETTINGCHANGE）；`internal/env/win_path_other.go`（Unix no-op stub）；`internal/cli/doctor.go`（步骤 0 无条件自愈）；`internal/installer/installer.go`（Activate 末尾对 tool=="go" 且 !noEnvWrite 时自愈）。
- 守卫测试：`internal/env/goroot_cleanup_test.go` — TestCleanupStaleGOROOT_RemovesWrongLayer / _KeepsValidValue / _KeepsUnknown / _NoopWhenAbsent / TestIsGOROOTDir_Probe；`internal/manifest/go_env_guard_test.go` — TestGoYAML_NoGorootEnv。
- G5 手工冒烟（本机实测 2026-09-12）：注册表 GOROOT 确为 `E:\XDev\install\go\1.26.5`（错层，V\go\pkg\tool 存在而 V\pkg\tool 不存在）；修复版 `xdev doctor` 输出 "removed stale GOROOT"；注册表值删除后经 shim `go env GOROOT` → `E:\XDev\current\go\go`（自推导正确）、`go build ./...` 与 `go vet ./...` 全绿（修复前报 "package fmt is not in std"）。
- 实证发现：shim 的 EnrichFromRegistry 会把注册表值重新注入子进程环境——即使进程内清了 GOROOT 也会被打回，证实「必须删注册表值本身」的设计。
