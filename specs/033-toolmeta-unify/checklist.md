# Spec 033 Checklist

> **状态**：✅ 完成（Phase A-F 全部收敛，LOOP 出口；tag v0.3.3 已本地打，未 push）
> **前置**：Spec 033（Approved）+ Task 清单（2026-08-22 批准，task-033-001）
> **日期**：2026-08-22

## 一、实施前基线（现状证据，先行核对）

- [x] R1 三份二进制名映射逐条核对：`env/scan_local.go:35`（23条）/ `gui/app_local_detect.go:1788`（14case）/ `:1942`（33键）—— 差异清单：gui 独有 typescript/mingw/uv/sbt/ant/kotlin/scala/erlang/haskell/julia/lua/make/ninja/adb；env 独有 android-platform-tools/android-ndk/yarn/pnpm
- [x] R2 gradle 冲突确认：env `--version`（L428）vs gui `-version`（L1868）—— 统一决策 `{-version}`
- [x] R3 前缀集差异：env 11 个 vs gui 17 个 —— 并集 19 项（env 独有 java/javac/python/node 4 项）
- [x] R4 路径表差异：env 9 case（Windows）vs gui 超集（+mingw/typescript + darwin/linux + nvm/pyenv/sdkman）
- [x] R5 `isInsideXdevDir`（全平台 ToLower）vs `hostscan.InsideDir`（Windows 才 lowercase）—— 以 hostscan 为规范
- [x] 现有测试依赖盘点：`env/scan_local_test.go`（9 个直调函数）、`gui/app_test.go`（3 个）、`gui/app_version_output_test.go`（1 个）、`gui/app_version_test.go`（11 个）→ 全部迁移至 toolmeta（语义不变，gui 锁定用例 `node v20.11.0→"v20.11.0"` 与 toolmeta 一致）
- [x] 额外发现：`collectEnvVars` 内联 envVarMap（第三份 env 变量映射）→ 按决策点 1 顺带收口

## 二、测试用例设计（TC 系列）

### 2.1 toolmeta 表（TC-M1~M7，`internal/toolmeta/meta_test.go`）

| 编号 | 用例 | 输入 | 预期 |
|---|---|---|---|
| TC-M1 | 表键集完整（并集） | 三份来源全部工具名清单 | `Lookup` 全部命中；清单与源码盘点一致（实施前基线 R1 产出物作为测试数据源） |
| TC-M2 | BinNames 等价 | env scanRules 23 条 + gui 两表的每个工具 | `toolmeta.BinNames(name)` 是各来源 binNames 的超集，且首元素一致（如 java→[java,javac]、mingw→[gcc,g++]、typescript→[tsc]） |
| TC-M3 | BinNames 未知回退 | `"no-such-tool"` | 返回 `["no-such-tool"]` |
| TC-M4 | VersionArg 特例表 | go/zig/adb → `{version}`；lua → `{-v}`；gradle/maven/mvn/ant/erl → `{-version}` | 命中对应参数数组 |
| TC-M5 | VersionArg 默认 | `"cmake"` 等其余 | `{"--version"}` |
| TC-M6 | 别名键一致 | `mvn`/`maven`、`rustc`/`cargo`、`python3`/`python`、`tsc`/`typescript`、`rust` | 同工具不同键的 VersionArg 一致（mvn==maven==`-version`；rustc==cargo==`--version`） |
| TC-M7 | EnvVarKeys 覆盖 | java/node/go/dotnet/android-platform-tools/android-ndk/android-build-tools/typescript | 命中对应键（java→JAVA_HOME；android-build-tools→ANDROID_HOME,ANDROID_SDK_ROOT；typescript→TYPESCRIPT_HOME） |
| TC-M8 | 表不变量 | 全表 | Name 唯一、BinNames 非空、VersionArg 非空、重复键无歧义 |

### 2.2 版本提取（TC-V1~V8，`internal/toolmeta/version_test.go`）

| 编号 | 用例 | 输入 | 预期 |
|---|---|---|---|
| TC-V1 | ExtractVersion env 前缀并集 | `"java 25.0.3"` / `"Python 3.12.7"` / `"node v20.18.0"` | `25.0.3` / `3.12.7` / `20.18.0` |
| TC-V2 | ExtractVersion gui 前缀并集 | `"yarn 9.1.2"` / `"gradle 9.4.1"` / `"cmake version 3.30.1"` / `"git version 2.47.0.windows.1"` | `9.1.2` / `9.4.1` / `3.30.1` / `2.47.0` |
| TC-V3 | ExtractVersion 无版本 | `"foo bar"` / `""` | `""` |
| TC-V4 | ExtractVersionFromOutput 单行 | `"v20.18.0"` | `20.18.0` |
| TC-V5 | ExtractVersionFromOutput 逐行 banner | `"----...\nGradle 9.4.1\n..."`（先分隔线） | `9.4.1`（跳过首行） |
| TC-V6 | ExtractVersionFromOutput 回退截断 | 61+ 字符无版本行 | 首行截断 60 + `...` |
| TC-V7 | IsVersionLike | `1.22`/`1.22.3`/`v1.2.3`/`go1.22`/`2.47.0.windows.1` / `abc`/`1`/空 | true×5 / false×3 |
| TC-V8 | StripWindowsSuffix | `2.47.0.windows.1` / `1.2.3` | `2.47.0` / `1.2.3`（原样） |

### 2.3 路径与 env 探测（TC-P1~P4，`internal/toolmeta/paths_test.go`）

| 编号 | 用例 | 输入 | 预期 |
|---|---|---|---|
| TC-P1 | CommonInstallPaths env 兼容 | windows 下 9 个 env 原有工具 | 前序 case 与 `env/migrate.go` 旧实现逐字节一致（python/node/go/java/dotnet/ruby/git/flutter/android-platform-tools） |
| TC-P2 | CommonInstallPaths 超集保留 | `mingw`/`typescript`（windows）、`go`/`java`（darwin/linux） | 命中 gui 版对应路径集合 |
| TC-P3 | CommonInstallPaths 条件探测 | `~/.nvm`/`~/.pyenv`/`~/.sdkman` 存在与否 | 仅在存在时追加（行为与 gui 现状一致） |
| TC-P4 | CheckEnvVars 查表 | 环境变量指向含 bin 子目录的假目录（构造临时目录 + t.Setenv） | 返回 bin 内可执行文件路径；`android-platform-tools` 命中 `platform-tools/adb` 特判 |

### 2.4 env 行为不回归（TC-E1~E4，`internal/env/scan_local_test.go` + 迁移）

| 编号 | 用例 | 说明 |
|---|---|---|
| TC-E1 | env 现有 9 个单测迁移后语义等价 | `TestExtractVersionFromOutput_*`（6 个）→ `toolmeta.ExtractVersionFromOutput`；`TestIsVersionLike_*`（2 个）→ `toolmeta.IsVersionLike`；改调后输入/预期不变 |
| TC-E2 | isInsideXdevDir 委托 | `TestIsInsideXdevDir_*` → 基于 `hostscan.InsideDir` 重写断言（含 Windows 大小写不敏感用例，Linux 下行为由规范版接管） |
| TC-E3 | getToolVersion 外壳不回归 | 假 bin 目录 + 桩命令（尽量不 exec 真工具）：binNames[0] 查询 VersionArg 后执行、3s 超时保留 |
| TC-E4 | ScanLocalTools 覆盖不缩水 | 断言扫描工具名集合 ⊇ 原 23 条（并集后≥23，接受多扫为正确性提升） |

### 2.5 gui 行为不回归（TC-G1~G5）

| 编号 | 用例 | 说明 |
|---|---|---|
| TC-G1 | `gui/app_test.go` 3 个单测迁移 | TestExtractVersion / TestStripWindowsSuffix / TestIsVersionLike → `toolmeta.*`，输入/预期不变 |
| TC-G2 | `gui/app_version_output_test.go` 迁移 | TestExtractVersionFromOutput → `toolmeta.ExtractVersionFromOutput`（banner/截断用例原样保留） |
| TC-G3 | `gui/app_version_test.go` 迁移 | TestExtractVersion_*（8 个含 CJC 输出、windows 后缀）/ TestIsVersionLike_*（4 个）→ `toolmeta.*` |
| TC-G4 | detectLocalToolPath 链 smoke | 临时目录 + env 变量：checkEnvVars→toolmeta.CheckEnvVars、commonPaths→toolmeta.CommonInstallPaths 后结果路径一致 |
| TC-G5 | 关键既有测试不回归 | `TestPmDefSubToolFields` / `TestIndependentPMOverlaysXdevWhenPATHHasLeftover` / `TestIsInsideDir_*`（已委托 hostscan）全绿 |

### 2.6 收口守卫（TC-GRD1~5，`internal/toolmeta/consolidation_guard_test.go`，先红后绿）

| 编号 | 守卫 | 断言 |
|---|---|---|
| TC-GRD1 | `versionFlags` map 灭绝 | `internal/env` + `internal/gui` 源码中无 `versionFlags` 字面量（仅 toolmeta 允许） |
| TC-GRD2 | 映射三合一 | 无 `scanRules` 定义 / 无 `binaryNames` / 无 `getBinNamesForTool` |
| TC-GRD3 | isInsideXdevDir 灭绝 | 无 `isInsideXdevDir` 标识符；env 中 `hostscan.InsideDir` 出现 ≥3 次（3 个调用点） |
| TC-GRD4 | 单点定义 | `extractVersionFromOutput` / `getCommonInstallPaths` / `isVersionLike` / `stripWindowsSuffix` 定义仅出现在 `internal/toolmeta` |
| TC-GRD5 | 无死代码 | 收口后 `go vet ./internal/env ./internal/gui ./internal/toolmeta` 无未使用告警 |

## 三、验收标准（Spec 四、1-10 映射为可执行清单）

| Spec 条目 | 检查方式 | 验收判定 |
|---|---|---|
| 1. env/gui 无 `versionFlags` map 字面量 | TC-GRD1 | 绿 |
| 2. 无 `binaryNames`/`getBinNamesForTool`/`scanRules` 三份映射；仅 toolmeta 有表 | TC-GRD2 + TC-M1 | 绿 |
| 3. `extractVersionFromOutput` 仅在 toolmeta 定义一次 | TC-GRD4 | 绿 |
| 4. `isInsideXdevDir` 不再存在；env 调 `hostscan.InsideDir` | TC-GRD3 | 绿 |
| 5. `getCommonInstallPaths` 仅在 toolmeta 定义一次 | TC-GRD4 | 绿 |
| 6. `go test ./... -short` 全绿 | T-F1 | EXIT=0 |
| 7. vitest 27/156 全绿 | T-F3 | 全绿 |
| 8. `go build .` + `wails build -tags gui` 通过 | T-F2 | EXIT=0 |
| 9. `TestScanLocalTools*`/`TestPmDefSubToolFields`/`TestIndependentPMOverlaysXdevWhenPATHHasLeftover` 不回归 | TC-E1~E4 + TC-G5 | 全绿 |
| 10. `grep -r "versionFlags" internal/` 仅命中 toolmeta | TC-GRD1 | 绿 |

## 四、收口后证据记录

- [x] T-F1 `go test ./... -short -count=1` EXIT=0（26 包全 ok，含 internal/toolmeta）
- [x] T-F3 vitest 27 文件 156 测试全绿 + `npx vite build` 通过
- [x] T-F2 `go build .`（CLI OK）+ `wails build -tags gui` 通过（wails CLI v2.12.0 匹配 go.mod；产物 `xdev-0.3.3.exe` 12.58MB + `xdev-gui-0.3.3.exe` 15.39MB）
- [x] T-F4 HANDOFF.md 更新 + 本地 commit + tag `v0.3.3`（未 push）

## 五、执行记录

| 日期 | Task | 验收结果 | 备注 |
|------|------|----------|------|
| 2026-08-22 | 规划 | 已批准（task-033-001：collectEnvVars 收口 + bump 0.3.3 + 本地 tag 不 push） | |
| 2026-08-22 | Phase A | 通过（toolmeta 包三文件落地；go build 通过） | meta.go 51 条目（41 主 + 10 别名） |
| 2026-08-22 | Phase B | 通过（TC-M/V/P 系列；修复 3 个测试断言错误：mingw 路径 Join 行为、1. 版本形态、node v 前缀 gui 行为锁定） | go test ./internal/toolmeta 全绿 |
| 2026-08-22 | Phase C | 通过（env：scanRules→ScanTargets 表驱动；versionFlags→VersionArg；版本提取/isInsideXdevDir/collectEnvVars/getCommonInstallPaths/isVersionLike 收口；测试 11 个迁移） | go test ./internal/env 全绿 |
| 2026-08-22 | Phase D | 通过（gui：getBinNamesForTool/checkEnvVars/findBinaryInDir map/getVersion versionFlags/extractVersion*/getCommonInstallPaths 删除；3 处包管理器检测调用迁移；测试 3 文件迁移） | go test ./internal/gui 全绿 |
| 2026-08-22 | Phase E | 通过（TC-GRD1~5 守卫落地，实现先行直接绿） | consolidation_guard_test.go |
| 2026-08-22 | T-F1 | 通过（go test ./... -short -count=1 EXIT=0） | 26 包全 ok |
| 2026-08-22 | T-F3 | 通过（vitest 27/156 + vite build） | |
| 2026-08-22 | T-F2 | 通过（CLI+GUI 构建成功；wails CLI 升 v2.12.0 修 internal error） | 产物 0.3.3 双 exe |
| 2026-08-22 | T-F4 | 通过（HANDOFF 更新 + commit + tag v0.3.3 未 push） | |