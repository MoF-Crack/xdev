# Tasks 014: Manifest 版本同步——数据更新 + sniff 自动更新 latest + TOFU

> **依据**：`specs/014-manifest-version-sync/plan.md`

## Phase 1: A 数据修复

- [ ] T1.1 编写 SHA256 获取工具（`tools/fetch_shasum.go`），支持 16 种获取策略
- [ ] T1.2 运行获取工具，为 17 个可获取工具的新版本条目填充 SHA256
- [ ] T1.3 更新 24 个 manifest YAML（latest + 新版本条目 + sha256）
- [ ] T1.4 补全 7 个工具的 remote.url + java 的 remote 配置
- [ ] T1.5 测试 T1.5.1–T1.5.8（latest 非空/在 versions 中/匹配目标/remote 补全/sha256 状态）
- [ ] T1.6 `go build ./...` + `go test ./internal/manifest/...` 通过

## Phase 2: B 架构修复 + C TOFU 机制（并行）

### B: sniff 自动更新 latest

- [ ] T2B.1 `internal/util/version.go`：实现 `isVersionNewer(a, b string) bool`（semver 比较，非标准跳过）
- [ ] T2B.2 `internal/discovery/scheduler.go`：`mergeIntoManifest` 追加版本后自动更新 latest
- [ ] T2B.3 测试 T2B.1–T2B.12（升级/不降级/取最大/非 semver 跳过/isVersionNewer 各场景）
- [ ] T2B.4 `go test ./internal/discovery/... ./internal/util/...` 通过

### C: TOFU 机制

- [ ] T2C.1 `internal/manifest/manifest.go`：实现 `UpdateArchiveSHA256`（原子写+文件锁）
- [ ] T2C.2 `internal/installer/installer.go`：空 sha256 分支改为 TOFU（计算+写回+通知）
- [ ] T2C.3 `internal/gui/`：Wails event 通知前端弹告警弹窗
- [ ] T2C.4 前端：监听事件，弹出模态告警对话框
- [ ] T2C.5 测试 T2C.1–T2C.10（首次计算/二次校验/篡改阻拦/弹窗/CLI WARNING/并发安全）
- [ ] T2C.6 `go test ./internal/installer/... ./internal/manifest/...` + `npm run build` 通过

## Phase 3: 构建验证 + 性能 + 安全审计

- [ ] T3.1 `go build ./...` 通过
- [ ] T3.2 `go test ./...` 通过
- [ ] T3.3 `npm run build` + `npm run test:unit` 通过
- [ ] T3.4 `wails build` 通过
- [ ] T3.5 功能测试 T3.9–T3.13（GUI 版本一致/安装有 sha256/安装无 sha256+弹窗/二次安装/sniff 更新 latest）
- [ ] T3.6 性能约束测试 T3.P1–T3.P8
- [ ] T3.7 回归安全审计 T3.S1–T3.S15