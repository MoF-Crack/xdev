# 35 shim 透传/隔离/远程持久化组（P1-33/34/35）

Status: resolved（含两处接线缺口由主 agent 补齐的说明）
Type: task
批次: B5
对应修复项: P1-33(C-09) + P1-34(C-07) + P1-35(C-08)（方案 §5 主题 E）

## 方案要点

1. **P1-33 排除集归一化**：lookupOnPath 每个非空 PATH 条目做与 addExcludedDir 相同的 Abs+EvalSymlinks 归一化后比对（归一化是主防线；os.SameFile 仅辅助——对 copy 型 shim 无效）；裸名保留末位兜底。
2. **P1-34 nix/container 覆盖防护**：Setup 前目标文件存在 → 拒绝（新增 --force flag）；Teardown 仅删自己写入的——①内容 hash 存 **persistedState**（persist.go:12-17 跨进程 JSON，setup/teardown 两次进程调用）；②flake.lock 由 nix develop 生成，按产物无条件删；③旧存量无 hash → 降级跳过；④container teardown 现状不删 devcontainer.json（复核确认）——只需 Setup 存在性门 + hash 记录。
3. **P1-35 远程实例持久化**：统一 `<home>/instances/<id>.json`；SaveInstance 写 JSON；`remote list` 与 GUI ListRemoteInstances 合并读该目录 + docker ps；旧 `remote/<ID>` marker 兼容读；**不把 SSHTarget 塞进 Metadata**（明文落盘新暴露面）。

## 守卫测试

junction/别名 PATH 条目不再自我 exec 递归；用户已有 flake.nix 不被覆盖；CLI/GUI 首次对 SSH 实例互相可见。

## Comments（Batch 5 实施记录）

- **改动落点与白名单偏差**：C-08 的真实落点在 `internal/orchestrator/docker_adapter.go`（旧 `SaveInstance` 写 `<home>/remote/<ID>` 状态文本）+ `internal/cli/remote.go` + `internal/gui/app_project.go`，**不是** `internal/remote/*`（那是版本发现包）。故新增 `internal/orchestrator/instance_store.go`（统一实例库 + `ListInstances`/`SaveInstance`/`RemoveInstance`）并删除旧 `SaveInstance`。
- 改动：
  - **C-09**（`internal/shim/shim_fallthrough.go`）— PATH 条目侧**三层排除判据**：字面兜底 → `Abs+EvalSymlinks` → **Windows `os.SameFile` 文件标识兜底**；抽出 `isExcludedPathEntry`/`excludedDirIdentities`；候选循环原样保留（给票 39 的 P2-09 留位）。**实测关键发现**：Go 1.26.5/Windows 的 `filepath.EvalSymlinks` **不解析目录 junction**（原样返回 junction 路径）→ 方案原稿「归一化是主防线、SameFile 仅辅助」在本机不成立，**junction 场景只有文件标识比对能识别**，故 SameFile 是必需层（仅 Windows 启用）。
  - **C-07**（`internal/isolator/*`）— `EnvironmentSpec.Force`；`setup_guard.go` 的 `ensureOverwritable`（Setup 目标文件已存在 → 拒绝，`--force` 可覆盖）；`persist.go` 增 `WrittenFiles map[path]sha256` + `contentSHA256`/`recordWrittenFile`/`writtenHash`/`removeIfContentUnchanged`/`logLegacyTeardownSkip`；nix/container 的 Setup 记录 hash、Teardown **按 hash 归属删除**，legacy 无 hash → **跳过 + 记日志**，`flake.lock` 按方案例外无条件删（记日志）；container Teardown **不加任何删除逻辑**（复核确认现状不删 `devcontainer.json`，仅加注释）。
  - **C-08**（`internal/orchestrator/instance_store.go` + `internal/cli/remote.go`）— 统一 `<home>/instances/<id>.json`；`remote list` 与 GUI 合并读该目录 + `docker ps`；旧 `remote/<ID>` marker 兼容读（JSON 优先、JSON 胜时不提示）；`shortID` 对非 hex（SSH）ID 不再截断 12 字符（否则主机名被吃、两主机不可区分）；**`SSHTarget` 不入 Metadata、密码绝不落盘**（有专门守卫）。
- **主 agent 补齐的两处接线缺口**：①`internal/cli/isolate.go` 注册 `--force` flag 并 `spec.Force = force`（票面明确要求「新增 --force flag」，原 subagent 白名单外）；②`internal/gui/app_project.go` 的 `CreateRemoteInstance` 现在写 `orchestrator.SaveInstance(...)`——原先 GUI **只读不写**，使「CLI/GUI 互见」仅在 CLI→GUI 方向成立；补齐后真正双向。
- 守卫测试（20 例）：`shim_fallthrough_test.go`（`TestLookupOnPath_SkipsAliasedShimDir` / `_SkipsRelativeBareEntry`）、`overwrite_guard_test.go`（`TestNixSetupRefusesToOverwriteUserFlake` / `_ForceOverwritesUserFlake` / `TestContainerSetupRefusesExistingDevcontainer` / `TestNixTeardownRemovesOnlyFilesItWrote` / `_LeavesUserModifiedFile` / `_LegacyStateSkipsRemovalAndLogs` / `TestContainerTeardownHasNoFileDeletion`）、`instance_store_test.go`（3 例）+ `remote_instances_test.go`（7 例，含 `TestSaveInstance_NeverPersistsCredentials` / `TestListSavedInstances_ReadsLegacyMarkerAndLogs` / `TestRemoteList_ShowsPersistedSSHInstance` / `TestGUIRemoteListSharesInstanceStore` 源码级契约守卫）。
- **红线先红后绿**：C-09 红 —`resolved "…shims-alias\git.exe" inside the shims dir … would exec itself recursively` + 裸名/相对条目解析进 shim 目录（自 exec 递归风险）×3；C-07 红 —PROBE-C07 下 6 例中 4 例红（覆盖用户 flake/devcontainer、删用户改写与 legacy 文件）；C-08 红 —`remote list` 输出 `No remote environments found.`、`<home>/instances/<id>.json` 不存在 → 2 例红。还原后全绿，备份逐字节一致，无探针残留。
- 验证：`go build ./...` exit 0；`go vet` 四包 exit 0；`go test ./internal/shim/ ./internal/isolator/ ./internal/remote/ ./internal/orchestrator/ ./internal/cli/ -count=1` 全绿（shim 35 PASS/0 FAIL，含票 28 的 C-04 用例）。
- **未决/取舍（披露）**：①`remote destroy` 不清 `<home>/instances/<id>.json` → `remote list` 可能留幽灵条目（SSH 类无 docker ps 去重兜底）；建议补 `RemoveInstance` 调用（小票）；②legacy `<home>/remote/<ID>` marker **只兼容读不迁移**（GUI 不读 `remote/`，用户需重建实例才进 GUI）；③`flake.lock` 无条件删（含 legacy 无 hash 场景）——依据方案例外条款，已记日志，若需在 legacy 场景保守可再改；④`persistedState.WrittenFiles` 的 key 在 Windows 下小写化（setup/teardown 跨进程 cwd 大小写可能不同，必须折叠才能命中）；⑤container 侧 hash 记录目前「只记录不使用」（为审计可追溯保留）。
