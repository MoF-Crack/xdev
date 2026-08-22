# Spec 033 Task 清单

> **状态**：✅ 完成（Phase A-F 全部收敛，2026-08-22）
> **前置**：Spec 033（Approved）+ Task 2026-08-22 批准（task-033-001）
> **日期**：2026-08-22
> **范围**：`internal/toolmeta` 新建 + env/gui 五类重复收口；不引入回归

## 代码证据基线（任务拆分依据）

| 重复项 | 代码位置 | 行为差异 |
|---|---|---|
| R1 二进制名映射 ×3 | `env/scan_local.go:35` scanRules(23条) / `gui/app_local_detect.go:1788` getBinNamesForTool(14case) / `:1942` binaryNames(33键) | 覆盖不一致，missing legacy |
| R1-D 关联：envVar 映射 | `env/scan_local.go:502` collectEnvVars 内联 envVarMap | 与 scanRules.envVarKeys / gui checkEnvVars envMap 三份重复 |
| R2 versionFlags ×2 | `env/scan_local.go:417`(24键) / `gui/app_local_detect.go:1840`(38键) | gradle：env `--version` / gui `-version` |
| R3 extractVersionFromOutput ×2 | `env/scan_local.go:469`(单行) / `gui/app_local_detect.go:1915`(逐行+banner+截断60) | 前缀集不同、回退行为不同 |
| R4 getCommonInstallPaths ×2 | `env/migrate.go:205`(12工具Windows-only) / `gui/app_local_detect.go:2080`(超集) | gui 是超集（含 mingw/typescript/darwin/linux/nvm 探测） |
| R5 isInsideXdevDir | `env/scan_local.go:527`（全平台 ToLower 独立重写）vs `hostscan.InsideDir`(规范, Windows-only lowercase) | env 未委托 |

**版本参数键别名要求**（收口后 `VersionArg` 同时被 env 以 `binNames[0]`、gui 以 `name` 查询）：
`mvn`/`maven`、`rustc`/`cargo`/`rust`、`python3`/`python`、`tsc`/`typescript`、`go`/`zig`/`adb`=`{version}`、`lua`=`{-v}`、`gradle`=`{-version}`（统一决策）。

---

## Phase A — 新建 `internal/toolmeta` 包

### T-A1 包骨架 + ToolMeta 结构 + 表
- **涉及**：`internal/toolmeta/meta.go`（新建）
- **预期产出**：`ToolMeta{Name, BinNames, EnvVarKeys, VersionArg}`；`var table = []ToolMeta{...}` 取三份映射**并集**，版本参数取 gui 版（更全），`gradle` 统一 `{"-version"}`；键集必须覆盖：
  - env scanRules 23 工具（含 `android-platform-tools`/`android-ndk`）
  - gui getBinNamesForTool 14 case（含 `typescript`）
  - gui findBinaryInDir binaryNames 33 键（含 `mingw`/`uv`/`sbt`/`kotlin` 等）
  - gui versionFlags 38 键（含 `gcc`/`clang`/`ghc`/`erl`/`cjc` 等纯版本键）
  - gui checkEnvVars envMap 特有键（`android-build-tools`/`android-platform`/`android-cmdline-tools`/`android-emulator`）
- **验收**：`go build ./internal/toolmeta` 通过
- **依赖**：无

### T-A2 查询函数
- **涉及**：`internal/toolmeta/meta.go`
- **预期产出**：`Lookup(name) (ToolMeta, bool)`；`BinNames(name)`（未知→`{name}`）；`EnvVarKeys(name)`；`VersionArg(name)`（默认 `{"--version"}`）；`Table()` 导出
- **验收**：`go vet ./internal/toolmeta` 通过
- **依赖**：T-A1

### T-A3 version.go（版本提取收口）
- **涉及**：`internal/toolmeta/version.go`（新建）
- **预期产出**：导出 `ExtractVersion` / `ExtractVersionFromOutput` / `IsVersionLike` / `StripWindowsSuffix`（实现以 gui 版为基，前缀列表取**并集**：env 的 `java `/`javac `/`Python `/`node `/`npm ` + gui 的 `yarn `/`pnpm `/`gradle `/`cmake version `/`gnu make `/`apache maven `/`apache ant(tm) version `/`dart sdk version: `/`git version `/`(gcc) `/`version `/`v `/`go`）
- **验收**：`go build ./internal/toolmeta` 通过
- **依赖**：T-A1

### T-A4 paths.go（路径判定收口）
- **涉及**：`internal/toolmeta/paths.go`（新建）
- **预期产出**：导出 `CommonInstallPaths(name)`（gui `app_local_detect.go:2080` 原样移入，含 Windows/darwin/linux + nvm/pyenv/sdkman 探测）；`CheckEnvVars(name) string`（内部用 `toolmeta.Lookup` 查 `EnvVarKeys` 重建 README 中的探测逻辑：env bin 目录 + 直接目录 + android-* 子目录特判；不再硬编码 envMap）
- **验收**：`go build ./internal/toolmeta` 通过
- **依赖**：T-A1、T-A2

## Phase B — toolmeta 单元测试（TC-M 系列，详见 checklist）

- **涉及**：`internal/toolmeta/meta_test.go`、`version_test.go`、`paths_test.go`（新建）
- **预期产出**：TC-M1~M12 全量落地（表完整性 / BinNames / VersionArg 别名 / EnvVarKeys / 表不变量 / 版本提取并集 / banner 逐行 / 截断回退 / windows 后缀 / CommonInstallPaths 兼容性 / CheckEnvVars 查表）
- **验收**：`go test ./internal/toolmeta -count=1` 全绿
- **依赖**：Phase A

## Phase C — `internal/env` 收口（不改扫描并发结构）

### T-C1 scanRules 删除 + 表驱动化
- **涉及**：`internal/env/scan_local.go`
- **预期产出**：`scanRules` 删除；`ScanLocalTools` 内循环改遍历 `toolmeta.Table()`；`toolScanRule` 保留为桥接结构或直接改用 `toolmeta.ToolMeta`（由 `// 循环入口` 处构造）；保留并发/去重结构
- **验收**：`go build ./internal/env ./internal/gui` 通过
- **依赖**：Phase A

### T-C2 getToolVersion 版本参数表删除
- **涉及**：`internal/env/scan_local.go:385-466`
- **预期产出**：`versionFlags` map 删除；改 `toolmeta.VersionArg(rule.binNames[0])`；执行外壳（3s 超时、HideWindow、首行截取）一行不动
- **验收**：`go build ./internal/env` 通过
- **依赖**：T-C1

### T-C3 版本提取 / 路径判定收口
- **涉及**：`internal/env/scan_local.go:469-537`
- **预期产出**：`extractVersionFromOutput` 删除→`toolmeta.ExtractVersionFromOutput`；`isInsideXdevDir` 删除→`hostscan.InsideDir`（3 处调用点 125/168/197 改）；`collectEnvVars` 内联 envVarMap 改为查 `toolmeta.Lookup(name).EnvVarKeys`（低风险顺带收口）
- **验收**：`go build ./internal/env` 通过
- **依赖**：T-C1、T-C2

### T-C4 verifyToolBinaries 收口
- **涉及**：`internal/env/scan_local.go:361`
- **预期产出**：候选路径逻辑保留；`rule.binNames` 改 `toolmeta.BinNames(name)`
- **验收**：`go build ./internal/env` 通过
- **依赖**：T-C1

### T-C5 migrate.go 收口
- **涉及**：`internal/env/migrate.go:205-273`
- **预期产出**：`getCommonInstallPaths` 删除→`toolmeta.CommonInstallPaths`；`isVersionLike` 删除→`toolmeta.IsVersionLike`
- **验收**：`go build ./internal/env` 通过
- **依赖**：Phase A

### T-C6 env 测试适配
- **涉及**：`internal/env/scan_local_test.go`
- **预期产出**：`TestExtractVersionFromOutput_*`（6 个）/ `TestIsVersionLike_*` 改调 `toolmeta.*`；`TestIsInsideXdevDir_*`（2 个）迁移为基于 `hostscan.InsideDir` 的断言（注意 Windows-only lowercase 语义差异，Linux 上行为由规范版接管）
- **验收**：`go test ./internal/env -count=1` 全绿
- **依赖**：T-C2~T-C5

## Phase D — `internal/gui` 收口（不改 App 方法签名、不新增绑定）

### T-D1 getBinNamesForTool 删除
- **涉及**：`internal/gui/app_local_detect.go:1788-1819`
- **预期产出**：函数删除；调用点（`checkEnvVars` 内部 L1709）改 `toolmeta.BinNames(name)`
- **验收**：`go build ./internal/gui` 通过
- **依赖**：Phase A

### T-D2 findBinaryInDir 内联 binaryNames 删除
- **涉及**：`internal/gui/app_local_detect.go:1940-2003`
- **预期产出**：`binaryNames` map 删除；改 `toolmeta.BinNames(name)`；候选路径（bin/ + 根 + .exe/.cmd）逻辑保留
- **验收**：`go build ./internal/gui` 通过
- **依赖**：Phase A

### T-D3 getVersion versionFlags 删除
- **涉及**：`internal/gui/app_local_detect.go:1824-1908`
- **预期产出**：`versionFlags` map 删除；改 `toolmeta.VersionArg(name)`；2s 超时 + HideWindow + 逐行解析调用点保留
- **验收**：`go build ./internal/gui` 通过
- **依赖**：Phase A

### T-D4 版本提取函数删除
- **涉及**：`internal/gui/app_local_detect.go:1915-2057`
- **预期产出**：`extractVersion` / `extractVersionFromOutput` / `isVersionLike` / `stripWindowsSuffix` 删除，调用点（L1905 `extractVersionFromOutput`、L2280 `extractVersionFromPath` → `toolmeta.ExtractVersion` 等）改 `toolmeta.*`
- **验收**：`go build ./internal/gui` 通过；`extractVersionFromPath`/`extractSpecialVersionPattern` 保留（不在 spec 范围）
- **依赖**：Phase A

### T-D5 getCommonInstallPaths 删除
- **涉及**：`internal/gui/app_local_detect.go:2080-2218`
- **预期产出**：函数删除；调用点（L1617 `detectLocalToolPathIndexed`）改 `toolmeta.CommonInstallPaths(name)`
- **验收**：`go build ./internal/gui` 通过
- **依赖**：T-A4

### T-D6 checkEnvVars 删除
- **涉及**：`internal/gui/app_local_detect.go:1675-1784`
- **预期产出**：函数删除；调用点（L1601 `detectLocalToolPathIndexed`）改 `toolmeta.CheckEnvVars(name)`；android-* 子目录特判逻辑随迁入 toolmeta
- **验收**：`go build ./internal/gui` 通过
- **依赖**：T-A4

### T-D7 gui 测试适配
- **涉及**：`internal/gui/app_test.go`（TestExtractVersion/TestStripWindowsSuffix/TestIsVersionLike）、`app_version_output_test.go`（TestExtractVersionFromOutput）、`app_version_test.go`（TestExtractVersion_*/TestIsVersionLike_*）
- **预期产出**：直接调 gui 内部函数的测试迁移为调 `toolmeta.*`（或移到 toolmeta 包）；`TestIsInsideDir_*` 保留（已委托 hostscan）
- **验收**：`go test ./internal/gui -count=1` 全绿（含 TestPmDefSubToolFields / TestIndependentPMOverlaysXdevWhenPATHHasLeftover 不回归）
- **依赖**：T-D1~T-D6

## Phase E — 收口守卫（先红后绿）

### T-E1 静态守卫测试
- **涉及**：`internal/toolmeta/consolidation_guard_test.go`（新建；模式参考 `internal/manifest/manifest_encoding_guard_test.go` 与 Spec 030 的 CLI-import 守卫）
- **预期产出**：TC-GRD1~5 —— 读取 `internal/env`、`internal/gui` 源码断言：无 `versionFlags :=` 字面量、无 `scanRules` 定义、无 `binaryNames`/`getBinNamesForTool`、无 `isInsideXdevDir`、`extractVersionFromOutput`/`getCommonInstallPaths`/`isVersionLike`/`stripWindowsSuffix` 仅在 toolmeta 定义
- **验收**：先红（5 项全部命中现状）→ 收口后转绿
- **依赖**：Phase C、D

### T-E2 表完整性核对
- **涉及**：`internal/toolmeta/consolidation_guard_test.go`（或 TC-M1）
- **预期产出**：断言 toolmeta 表键集 ⊇ 三份来源的全部工具名（显式化清单，防止并集漏项）
- **验收**：全绿
- **依赖**：Phase A

## Phase F — 全量回归 + 收尾

### T-F1 全量 Go 回归
- **涉及**：无（验证）
- **预期产出**：`go test ./... -short -count=1` 全绿记录
- **验收**：EXIT=0
- **依赖**：Phase B~E

### T-F2 双构建验证
- **涉及**：无（验证）
- **预期产出**：`go build .` + `wails build -tags gui`（或 `go build -tags gui`）通过
- **验收**：两命令 EXIT=0；产出 `build/bin/xdev-gui-0.3.3.exe`（版本号以发版决策为准）
- **依赖**：T-F1

### T-F3 前端回归
- **涉及**：无（验证）
- **预期产出**：`cd frontend && npx vitest run`（27 文件 156 测试）+ `npx vite build` + e2e
- **验收**：全绿
- **依赖**：T-F1

### T-F4 HANDOFF + backup 提交
- **涉及**：`HANDOFF.md`、handoff-backup 分支
- **预期产出**：Spec 033 完成记录 + 关键文件索引；≥1 个恢复点提交（LOOP 出口）
- **验收**：`git log --oneline handoff-backup` 含 `Spec 033` 提交
- **依赖**：T-F1~T-F3

---

## 依赖拓扑

```
Phase A (toolmeta 包)
   ├─ Phase B (TC-M 单测)          ← 可先行（红测）
   ├─ Phase C (env 收口) → T-E1(E 部分红转绿)
   ├─ Phase D (gui 收口) → T-E1
   └─ Phase E (守卫) → Phase F (全量回归)
```

## 决策点（已确认，2026-08-22）
1. `collectEnvVars` 内联 envVarMap **顺带收口**到 `toolmeta.EnvVarKeys`（按 T-C3 执行）
2. 收尾**版本 bump 至 0.3.3**（`internal/version/version.go` + `frontend/package.json`），构建产出 `xdev-0.3.3.exe` / `xdev-gui-0.3.3.exe`
3. 收尾**本地提交 + 打本地 tag `v0.3.3`**，**不 push 到 GitHub**（v0.3.2 tag 保持不动）

---

**审批记录 (Approval History)**

> **批准ID**: `2026-08-22-task-033-001`
> **批准内容**: 上述 3 项决策点 + 任务清单全量执行