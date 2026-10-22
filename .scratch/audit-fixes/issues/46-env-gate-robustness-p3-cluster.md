# Issue 46: env 守卫与健壮性 P3 簇 + 防污染闸门跨包缺口（第六轮审计 env 分路）

Status: open（P3 簇 + P2-5 余项）
Type: bug / hardening
Severity: medium（单项 P3；其中「守卫盲区」类合计为防污染体系的结构性缺口）
Discovered: 2026-09-25，第六轮审计（host-mutation 分路）

## A. 防污染守卫跨包缺口（P2-5 余项——本轮已做「EnvKeys 补 Path」一步）

已修（本轮）：hostsnapshot `DefaultOptions().EnvKeys` 补入 `"Path"`。

余项：
1. cli/gui/installer/integration 四个 TestMain 只钉 home + 快照，不装注册表
   接缝、也不受 internal/env 的 `realUserRegistryMutationCount` 约束（独立
   测试二进制）。这些包里任何走到导出 env mutator 的测试仍写真实 HKCU。
2. `hostsnapshot.Capture` 经可替换接缝读 env（`getAllUserEnvVarsFn`）——
   泄漏的内存后端会让 after 快照失明。

修复方向：经 testkit 导出 internal/env 的计数器/断言（`testkit/hostsnapshot`
或新 `testkit/envgate`），四个 TestMain 接入；Capture 在包 init 捕获真实
读取函数的副本。

## B. 闸门自身健壮性（userenv_hermetic_test.go）

1. **P3-7 计数窗口**：TestMain 在读终态计数**之前**就 restorePathBackend/
   restoreEnvBackend（:51-52 → :55）——泄漏的测试 goroutine 在 restore 之后
   持真实接缝，其写入不再计数。修复：先读终值与 NOTE diff，restore 放最后。
2. **P3-8 声明记账是包级全局**：`expectedRealRegistryMutationCount` 全局
   Add，任何测试可声明 N 次后在包内任意位置做 N 次真实写。修复：声明面
   收敛到 hermetic 守卫文件自身（导出仅守卫可调的 mustDeclare）或按测试作用域。
3. **P3-9 隐私**：gate NOTE（diffUserEnvMaps）与 hostsnapshot diff 打印真实
   环境变量**值**到 stderr——dev 机上变量可能含 token。修复：脱敏为
   name+长度（值留哈希）。

## C. PATH/env mutator 健壮性

1. **P3-10 GetEnvVar 吞首查错误**（win_path_windows.go:1180-1191）：第一次
   RegQueryValueExW 的返回被丢弃，ERROR_ACCESS_DENIED 也表现为「变量不存在」。
   修复：按 readRegistryEnvVarRaw 的样式检查 ret。
2. **P3-11 enumerateUserEnvVarsFromRegistry 忽略第二次 RegQueryValueExW
   返回**（:2018-2028）：枚举与读取之间值被删/变长时，零填充 buffer 产出
   伪 "" 值（快照假「变更」）。修复：检查 ret，失败跳过该变量。
3. **P3-12 UpdateShimDirInUserPath 吞写错误**（:1831，void API；经
   config.go:893 迁移链路触达）——失败零诊断。修复：返回 error 或 log.Warn。
4. **P3-13 PATH mutator 小疣**：SetWindowsUserPath 在 PATH 缺失时产出
   `"shimDir;"`（尾部空条目=CWD 搜索二进制植入面）；RemoveFromWindowsUserPath/
   RemoveFromUserPath 缺 changed 检查（no-op 仍重写+广播）。修复：仅
   current!="" 时加前导分隔；kept==parts 跳过写。
5. **P3-16 RMW 类型强转 REG_SZ → REG_EXPAND_SZ**（SUSPECTED）：
   writeUserPathFromRegistry 恒写 _REG_EXPAND_SZ 且读侧忽略 valType——
   含字面 % 文本的 REG_SZ PATH 被任何 xdev 重写后开始展开。修复：保留
   上次观测到的类型。

## D. hostsnapshot 覆盖（P3-14/15）

1. `PeekCachedHomeDir()==""`（新机/冷缓存）时 userOwnedPaths/Dirs/
   VolatilePaths 全部返回 nil，FS 守卫静默无跟踪。修复：大声告警。
2. 指纹缺口：`config.yaml.bak`（恢复路径本体！）、`keys/`、`instances/`、
   `isolate/`、`last_selection.json`、`.locks/`、`shims/` 未跟踪——测试
   同时打掉 config.yaml 与 .bak 会击穿恢复路径而守卫只见前者。
   修复：至少补 config.yaml.bak（全指纹）与 shims/（条目集）。

## 验证要求

B-1 先红：构造一个 spawn 泄漏 goroutine 的用例证明窗口存在。C 类逐项
表测。D-2 用临时 home 断言新键生效。
