# Tasks 015: 用户自定义工具——自动版本发现 + 动态 manifest 生成

> **依据**：`specs/015-custom-tool-manifest/plan.md`

## Phase 1: 后端

- [ ] T1.1 `config.go`：新增 `CustomToolConfig` 结构体 + `CustomTools` 字段
- [ ] T1.2 `manifest.go`：新增 `GenerateCustomManifest` 函数
- [ ] T1.3 `internal/gui/app_custom_tools.go`：新增 4 个 binding 方法
- [ ] T1.4 测试 T1.5.1–T1.5.10（生成/校验/添加/删除/列表/预览）
- [ ] T1.5 `go build ./...` + `go test ./internal/gui/... ./internal/manifest/... ./internal/config/...` 通过

## Phase 2: 前端

- [ ] T2.1 `SettingsPage.vue`：新增自定义工具区块（表单+预览+列表+删除）
- [ ] T2.2 `AppContent.vue`：新增 state/函数/onMenuSelect 分支
- [ ] T2.3 i18n：新增 `custom_tools.*` key
- [ ] T2.4 测试 T2.5.1–T2.5.5
- [ ] T2.5 `npm run build` + `npm run test:unit` 通过

## Phase 3: 构建验证 + 性能 + 安全审计

- [ ] T3.1 `go build ./...` + `go test ./...` 通过
- [ ] T3.2 `wails build` 通过
- [ ] T3.3 功能测试 T3.5–T3.11
- [ ] T3.4 性能约束测试 T3.P1–T3.P4
- [ ] T3.5 回归安全审计 T3.S1–T3.S10