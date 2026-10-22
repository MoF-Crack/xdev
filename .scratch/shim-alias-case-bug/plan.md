# 方案：shim 调用路径大小写归一化 + 透传部署闭环（缺陷①②③ 解决方案）

Status: **implemented**（T1-T4+T6 已实施并全门验证；T5 流程修正已登记；0.4.1 tag 待发布）
Date: 2026-09-18
关联票：`.scratch/shim-alias-case-bug/issue.md`（①）· `.scratch/shim-passthrough-fallthrough-bug/issue.md`（②）
证据链：`.scratch/bugx-host-dsh/spec.md` §1.2 P-B / `knowledge-mcp-failure.md`

---

## 7. 实施记录（2026-09-18）

**代码改动**：
- `internal/shim/shim.go`：+`trimExeSuffix` / `+normalizeShimAlias` helpers；
  MaybeExec L64/L78 改用归一化管线（base := normalizeShimAlias(trimExeSuffix(selfName))）；
  wailsbindings 判定改用 base（涵盖大小写变体）；Lookup 入口归一化；
  addExecutables 索引键折叠；loadDiskShimIndex 读取侧同域折叠
- `internal/shim/refresh.go`（新）：RefreshDispatchers（缺陷④：分发器版本一致性刷新）
- `internal/shim/shim_alias_case_test.go`（新）：G1-G6 行为级守卫 + G2 helper 表测
- `internal/shim/refresh_test.go`（新）：RefreshDispatchers 守卫（4 个用例）
- `internal/shim/shim_fallthrough_test.go`：TestDefaultExcludedDirs 补 ResetHomeDirForTest
  （测试隔离修复——新文件按字母序先跑、消费 ResolveHomeDir sync.Once）
- `internal/gui/app.go`：+import shim；+refreshShimDispatchers 方法；EnsureSelfInstalled 两分支钩入

**验证**：
- 先红后绿：G1/G3/G4/G5 修复前全红（错误信息与生产事故逐字节一致）→ 修复后全绿
- 全门：gofmt ✓ · go vet ./... ✓ · GOOS=linux go build ✓ · go test ./... -short -count=1 全仓 28 包 ✓ ·
  internal/shim -count=5 ✓
- 部署冒烟（E:\XDev 已升级到 0.4.0+修复，100 shims 刷新）：
  - `npm.EXE --version` → 11.19.0 exit 0（原事故用例红→绿）
  - `git.exe --version` → 2.55.0 exit 0（9/8 透传案例）
  - `node.EXE -v` → v26.8.1 / `NPM.EXE --version` → 11.19.0（大小写矩阵）
  - Python shutil.which('npm') → spawn → 11.19.0 exit 0（bugx 生产链端到端）
  - bugx doctor Node 探针 ✗ → ✓

**T5 流程修正**（发布流程检查单，写入 specs/release-checklist.md）：
1. 打 tag 前扫描 .scratch/**/issue.md：Status open 且代码修复已合入的票，确认随版发布
2. 发布后验证清单加"部署面冒烟"（shim 大小写矩阵 + 孤儿透传 + ResolveHomeDir 探针）

## 0. 结论摘要

| 缺陷 | 解法 | 层 |
|---|---|---|
| ① 别名剥离大小写敏感 | 消费端归一化：大小写不敏感剥扩展名 + Windows 折叠别名；索引键同域折叠 | 代码（shim 包） |
| ② 透传未到部署面 | 无需改透传代码（bf69b85 已在仓）；**重部署 + shims 刷新** | 部署 |
| ③ 发布→部署链断裂 | 0.4.1 发布检查单两条修正 + self_update 链收口 | 流程 |
| ④（本方案新发现）自更新不刷新 shims 分发器副本 | EnsureSelfInstalled 增加分发器版本一致性刷新（待 T4 验证缺口后定案） | 代码（小） |

## 1. 缺陷① 代码修复设计

### 1.1 设计原则

- **单一 choke point**：在别名推导与 Lookup 入口归一化，下游（L156/160/184/188 的
  `alias+exeSuffix()` 拼装、`shim_fallthrough.go` L181 透传候选）自动恢复，**不改透传代码**。
- **平台门控**：折叠仅限 Windows（`exeSuffix()` 同款判定 `os.PathSeparator == '\\'`）——
  Unix 工具名大小写敏感（Go/GitHub token 等先例），必须精确保留；跨平台门 `GOOS=linux build` 守住。
- **两端同域**：消费端（MaybeExec/Lookup）与索引键（addExecutables）同规则折叠，
  消除"构建端 L384 已 ToLower、消费端敏感"的不一致；旧磁盘索引（shim-index.v1）键
  本就由 xdev 以小写写入 → 向后兼容。

### 1.2 具体改动（internal/shim/shim.go）

新增两个 helper（置于 exeSuffix() 附近，L462 区域）：

```go
// trimExeSuffix strips the platform exe suffix case-insensitively.
// Python shutil.which 在 Windows 按 PATHEXT（大写 .EXE）构造 argv[0]，NTFS 大小写
// 不敏感命中磁盘小写文件——扩展名必须折叠剥离（缺陷：bugx 报 "alias npm.EXE not installed"）。
func trimExeSuffix(name string) string {
	ext := exeSuffix()
	if ext == "" {
		return name
	}
	if len(name) >= len(ext) && strings.EqualFold(name[len(name)-len(ext):], ext) {
		return name[:len(name)-len(ext)]
	}
	return name
}

// normalizeShimAlias folds the alias on Windows, where command resolution
// (NTFS / CreateProcess / PATHEXT) is case-insensitive; index keys and
// lookups must share one case domain. Unix keeps exact case.
func normalizeShimAlias(alias string) string {
	if os.PathSeparator == '\\' {
		return strings.ToLower(alias)
	}
	return alias
}
```

改动点（含前后对照）：

```go
// L64（前）base := strings.TrimSuffix(selfName, exeSuffix())
base := normalizeShimAlias(trimExeSuffix(selfName))
// L72 wailsbindings 判定改用 base（原 selfName 精确比较），行为等价且获得折叠
if base == "wailsbindings" { return false, nil }
// L78（前）alias := strings.TrimSuffix(selfName, exeSuffix())
alias := normalizeShimAlias(trimExeSuffix(selfName))
```

`Lookup` 入口（防御性归一，使既有/未来调用方与测试受益）：

```go
func Lookup(alias string, args []string) (...) {
	alias = normalizeShimAlias(alias)
	...
}
```

索引键同域（addExecutables，L389 后追加）：

```go
		if alias == "" {
			continue
		}
		alias = normalizeShimAlias(alias) // 与消费端同域（Windows 折叠）
```

**非目标**：不改 `shim_fallthrough.go`（alias 归一化后 L181 候选 `npm.com/npm.exe/npm.bat/npm.cmd`
自动正确）；不改 run()/CreateProcess 层；不改 Unix 语义。

## 2. 守卫测试（先红后绿，红绿输出登记票卡 Comments——house 方法论）

| # | 测试 | 修复前（红） | 修复后（绿） |
|---|---|---|---|
| G1 | `TestMaybeExecShimAliasCaseMatrix`（Windows-gated；复用既有 fakepy 基建 shim_test.go L432 模式）：`MaybeExec("fakepy.EXE"/"FAKEPY.EXE"/"FakePy.Exe")` 全部分发成功且参数透传 | alias error，分发失败 | 全绿 |
| G2 | `TestTrimExeSuffix_And_NormalizeShimAlias` 表测：`npm.exe/npm.EXE/NPM.exe/NPM.EXE/npm.cmd/npm→npm`；Unix 子测试断言不折叠 | helper 不存在（测试先行即红） | 全绿 |
| G3 | `TestLookupAliasCaseFold`（Windows-gated）：`Lookup("NPM")` 与 `Lookup("npm")` 命中同一 target | NPM miss | 同 target |
| G4 | `TestShimIndexKeysFolded`：fixture 写入大写 `NPM.CMD` → buildShimIndexMap 键为 `npm` | 键 `NPM` | 键 `npm` |
| G5 | 透传联动（复用 shim_fallthrough_test 基建）：未管理别名 + 大写 selfName → MaybeExec 经 LookupOnPath 命中 PATH 真实工具 | 报 alias error（L181 候选无效） | 透传成功 |
| G6 | Unix 回归（CI linux 跑）：别名大小写精确保留（`Foo` ≠ `foo`） | — | 精确 |

## 3. 缺陷② 部署闭环（代码合入后）

机制事实（本会话取证）：`E:\XDev\shims\*.exe` 由 **工具 Activate 时** 的 `InstallShim`
写入（installer.go L1343-1348；Windows=硬链接回退 copy，且同时写 `<name>` 与 `<name>.exe`，
shim.go L469-476）——**主二进制自更新/镜像替换不会刷新它们**。

工序：
1. `.\build.ps1 -SkipAudit` → `build\bin\xdev.exe`（含 bf69b85 + ①修复）；
2. 运行新二进制一次 → EnsureSelfInstalled 镜像一致性替换 `E:\XDev\xdev.exe` +
   `current/xdev` junction（Spec 036 stale.go 语义：探测版本 ≠ 当前即替换）；
3. **shims 刷新**（T4 先验证 EnsureSelfInstalled 是否已刷新；若否 → 补最小修复：
   EnsureSelfInstalled 对现存 shims 逐一重跑 `shim.InstallShim`——"分发器版本一致性"
   语义，与 stale.go 镜像替换同型。此即 **缺陷④**：见 §0）；
4. 机器冒烟（组合矩阵，结果登记票卡）：
   - `E:\XDev\shims\npm.EXE --version` → `11.19.0` exit 0（原事故用例，红→绿）
   - `E:\XDev\shims\npm.exe --version` → 11.19.0（回归）
   - `E:\XDev\shims\git.exe --version` → 真实 Git 2.55.0（9/8 透传案例回归）
   - `E:\XDev\shims\node.EXE -v` → v26.8.1
   - `bugx doctor` 的 Node 探针随之转绿（bugx 侧无代码改动）
5. 回滚：替换前对 `E:\XDev\shims` + `E:\XDev\xdev.exe` 做哈希清单备份；
   junction 重建程序已知（HANDOFF Spec 036）；InstallShim 自带运行中 exe 文件锁处理（L476 注释）。

## 4. 缺陷③ 发布闭环（流程修正）

v0.4.1 发布检查单新增两条（教训：bf69b85 修复 9 天未进任何发布面/部署面）：
1. **打 tag 前**扫描 `.scratch/**/issue.md`：Status open 且代码修复已合入的票，
   逐一确认"修复是否随本版本发布"；未随版的票必须注明阻塞原因；
2. **发布后**验证清单加"部署面冒烟"（shim 大小写矩阵 + 孤儿透传），CI 绿 ≠ 部署面绿。

self_update 链收口：0.3.3-rc.2 机器（`core.self_update_mode=prompt` 默认）启动将收到
0.4.1 提示 → 用户确认后走 §3 工序，一次动作闭合 ②③。

## 5. 验证门（house 标准）

- gofmt（本批文件）· `go vet ./...` · `GOOS=linux go build ./...`（折叠必须平台门控）
- `go test ./... -short -count=1` 全包 · `go test ./internal/shim/... -count=5` 防 flake
- `-race` 本机不可用（HANDOFF 在案约束）→ CI race 门补跑
- 票卡 Comments 登记红绿证据（G1-G6 + 机器冒烟矩阵）

## 6. 任务分解与验收

| # | 任务 | 验收 |
|---|---|---|
| T1 | helpers + L64/L78 + wailsbindings + Lookup 入口（§1.2） | G1/G2/G3 红→绿 |
| T2 | 索引键折叠 + 磁盘索引读写共用归一化确认 | G4 红→绿；旧索引兼容 |
| T3 | 透传联动守卫 | G5 红→绿 |
| T4 | 部署闭环工序 + shims 刷新缺口验证（必要时补 EnsureSelfInstalled 刷新=④） | §3.4 冒烟矩阵全绿并登记 |
| T5 | 0.4.1 检查单两条 + 流程修正落文档 | §4 两条进入发布流程 |
| T6 | 全门验证 + 两票 Comments 收口 + （如适用）0.4.1 tag | 全部门绿；票卡闭环记录 |
