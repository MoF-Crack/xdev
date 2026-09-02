# 02 — F6+F7: 自更新资产匹配与 SHA256SUMS 解析收严

Status: done
Spec: 035（F6/F7，P2）
Files: `internal/update/update.go`、`internal/update/update_test.go`
Blocks: none

## 改动（最小）

1. **F6**：删除 `findAsset` 的 partial-match 回退（update.go:318-323），仅保留精确匹配。
2. **F7**：删除 `parseSHA256SUMS` 的单空格分隔分支（update.go:494-498），仅认 `sha256sum` 标准双空格。
3. 测试同步：`TestParseSHA256SUMS_SingleSpace`（update_test.go:201-210）改为负向断言（单空格行不解析成功）；新增 `TestFindAsset` 负向用例（assets 含 `xdev-gui-windows-amd64-v1.0.zip` 时精确查找失败，不被 partial-match 选中）。

已核实安全性：CI 全平台产物名与 `assetNameForPlatform` 逐字符一致（ci.yaml:209-251，v0.1.0 起稳定）；darwin tar 哈希在主 job SHA256SUMS 内（ci.yaml:436）；AppImage 命名不含 goos/goarch 成对子串不受影响。

## 验收

- [ ] `go test ./internal/update -count=1` 全绿（含改造后用例）
- [ ] `git diff` 确认 update.go 仅删两段（无新增逻辑）
