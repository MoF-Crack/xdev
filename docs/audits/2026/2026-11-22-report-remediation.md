# XDev：缺陷修复方案复核与实施

> 归档说明：本文记录移植前 Wails 2 修复阶段的基线和结果，不是 RC4 整合版验收。RC4 保留 Wails 3、新 UI 与自更新，另见 [RC4 发布说明](<../../releases/1.0.0-rc.4.md>)。原始 `.scratch`/`.bugx` 证据保存在本地工作区，不作为仓库公开链接。

状态：报告确认项的代码修复和实际入口接线已完成。冻结源码上的跨平台验收结果见下文；D-19 按复核结论不修，macOS 真实运行及明确兼容边界保留。

## 复核结论

以用户指定的 verification-and-remediation.md（本地证据：`.bugx/artifacts/report-verification-20261122/verification-and-remediation.md`） 为输入，结合当前源码、调用方、现有测试及独立复现复核。方案的分级、平台与入口限制成立；不是将原报告 28 项全部当成 P0，也不是将观察探针 PASS 当成修复成功。

实施时保留的重要约束：

- **D-19 不作功能修复**。Windows 的真实进程消费已正确覆盖，Unix 不同大小写变量必须仍可并存。
- 下载清理持有 writer 的同一排他锁，锁文件保持稳定，不以 unlink 锁文件制造两套锁。
- Lua 的 load、metadata、promotion、call 与每次 coroutine resume/wrap 共用有效预算，不能把上一加载 deadline 留给未来协程。
- HMAC 覆盖字段、序列化与 nil/空序列归一保持，未更改算法让存量配置失效。
- 未确认的归档越界后果、实际掉电、真实权限边界和 macOS 运行结果不冒充已验证。

## 基线与交付

基线为 `921e35adce58ab12b9c3d4cb09898a9fc638bf9b` **加任务开始时全部既有工作树修改**。原有修改和删除没有被 reset、checkout、还原或混同为本次修复。开始状态、差异和 1163 个文件身份保存在 baseline（本地证据：`.scratch/report-remediation-20261122/baseline/status.txt`）。

本次可审阅的 增量补丁（本地证据：`.scratch/report-remediation-20261122/final/remediation.patch`） 通过专用临时 Git index 对比开始树导出，**不修改真实 index**；包含新回归测试，而不把开始时已有更改重新算进补丁。补丁清单与 SHA-256（本地证据：`.scratch/report-remediation-20261122/final/patch-manifest.json`）。

主分支 HEAD 和真实 index 保持，未推送，未运行产品的 self install 或替换真实部署。实际构建产物只在私有验证目录。遵循仓库 AGENTS.md，在本地 handoff-backup 分支保存恢复快照；恢复快照收据（本地证据：`.scratch/report-remediation-20261122/final/recovery-snapshot.json`） 给出提交身份。恢复快照包含完整开始树与本次更改，单独增量补丁用于区分本次范围。

## 分项实施

| ID | 实施结果 | 正确契约与证据 |
|---|---|---|
| D-01、D-02 | PATH 处理统一 | shim/auto 同组；配置 ShimDir；UNC、扩展和设备路径原文保留，普通盘符冗余分隔符保守修正。环境批次（本地证据：`.scratch/report-remediation-20261122/env/implementation.md`） |
| D-03 | 候选保存成功才发布 | saveLocked 统一 HMAC/schema；真实 CLI/GUI setter 重载；保存失败不改 PATH/shim，模式切换的 shim 发布或末次 PATH 写失败时补偿恢复配置与新增入口，保留原始错误。配置批次（本地证据：`.scratch/report-remediation-20261122/config/implementation.md`） |
| D-04 | 非活动卸载不动活动入口 | 不删活动 current/shim/env；阶段错误保留原因；活动重指与最终清理失败可见。卸载与分发器（本地证据：`.scratch/report-remediation-20261122/uninstall-shim/implementation.md`） |
| D-05 | 清理生命周期收口 | 两底层 API、GUI 分类/单文件/retention、在途与跨进程锁、literal 路径、busy/error 计数。下载批次（本地证据：`.scratch/report-remediation-20261122/download/implementation.md`） |
| D-06 | PM 信任来源分开 | 官方执行 YAML 独立 Ed25519 域完整签名；坏签拒绝；local/third-party 独立命名空间，具体摘要明确批准。PM 批次（本地证据：`.scratch/report-remediation-20261122/pm/implementation.md`） |
| D-07 | 根切换与迁移分开 | 六派生目录重锚；旧 XMDEV_HOME 新进程可定位新根；稳定双根锁和提交点；复制校验后切换，旧树保留。配置批次（本地证据：`.scratch/report-remediation-20261122/config/implementation.md`） |
| D-08 | cron 归属准确 | 稳定 marker；精确已知绝对路径 legacy 识别；不认领 bare PATH 用户命令，读错误不覆盖。嗅探批次（本地证据：`.scratch/report-remediation-20261122/discovery/implementation.md`） |
| D-09、D-10 | 纯 Lua 执行边界完整 | 有限子进程回归、有效 resume/wrap ctx、metadata/promotion 预算、per-path 发布锁；真正执行退出后 Close。Lua 批次（本地证据：`.scratch/report-remediation-20261122/lua/implementation.md`） |
| D-11 | 远程非零状态可见 | Docker 原样透传 error/result；GUI error 保留 exitCode/stdout/stderr。远程批次（本地证据：`.scratch/report-remediation-20261122/remote/implementation.md`） |
| D-12 | SSH 恢复目标与认证定位 | 版本化非敏感字段；密码只在 App 会话；创建时锚定 key/known_hosts，旧相对记录明确重新配置，不猜 ID。远程批次（本地证据：`.scratch/report-remediation-20261122/remote/implementation.md`） |
| D-13、D-14 | outcome、TTL、退避与恢复统一 | LastSuccess/LastAttempt/RetryAfter 分离；跳过不扣 token；未完成与当前新增/到期集合并集；成功版本保留、失败不虚构全局成功。嗅探批次（本地证据：`.scratch/report-remediation-20261122/discovery/implementation.md`） |
| D-15、D-16 | 结构化解包与写中预算 | 完整原名、链接检查、单文件/总量/条目/深度/期限/空间保留、私有 staging；原生 Go 7z 不依赖外部写盘。installer 传 ctx/配置预算，失败不删旧目标。归档批次（本地证据：`.scratch/report-remediation-20261122/archive-key/implementation.md`） |
| D-17 | 密钥独占创建与错误根拒绝 | 不覆盖已有/别名输出；私钥权限先就位；absent/valid/malformed 三态；发布任务配对校验；缓存早返同样拒绝坏根。签名批次（本地证据：`.scratch/report-remediation-20261122/archive-key/implementation.md`） |
| D-18 | 工具级版本方案与真实安装根 | NDK rNN/修订与数字版有明确定义；未知不可比；排序、晋升、升级、卸载、GUI/TUI/CLI 共用。hook、project shim、三类 list、Wails 的实际目录来自 Plugin.InstallDir，覆盖嵌套根及配置 override。版本消费者（本地证据：`.scratch/report-remediation-20261122/integration-version/implementation.md`） · 安装根入口（本地证据：`.scratch/report-remediation-20261122/installed-root/implementation.md`） |
| D-19 | 不改 | 保留原跨平台真实消费语义，未做全平台 EqualFold 替换。 |
| D-20 | 原值事务完整 | 写前全部快照 exists/name/raw/type；casefold 查询；确定顺序写、逆序恢复，读错误拒写。环境批次（本地证据：`.scratch/report-remediation-20261122/env/implementation.md`） |
| D-21、D-22 | 加载与缓存同一来源快照 | top/third-party 约定枚举、内容摘要、schema 2、空与不可读区别；不递归备份。清单批次（本地证据：`.scratch/report-remediation-20261122/manifest/implementation.md`） |
| D-23 | 可安装候选先过滤 | 保留平台/形态优先级与 npm 例外，851 个真实组合守卫。清单批次（本地证据：`.scratch/report-remediation-20261122/manifest/implementation.md`） |
| D-24 | 一次逻辑请求最终审计 | cache/network/resume/fallback 与前置、校验、HTTP、取消、rename 失败；含 tool/ver、类别和数量；URL/error 脱敏。下载批次（本地证据：`.scratch/report-remediation-20261122/download/implementation.md`） |
| D-25 | 实例原子发布与诊断 | write/sync/close/rename 故障对照；CLI/GUI 健康项与逐文件诊断同快照，保留坏原字节。远程批次（本地证据：`.scratch/report-remediation-20261122/remote/implementation.md`） |
| D-26 | 按管理归属刷新 | alias/孪生与来源身份记录、内容核对、temp+rename；未知文件/链接与 .old/.tmp 保留。卸载与分发器（本地证据：`.scratch/report-remediation-20261122/uninstall-shim/implementation.md`） |
| D-27 | 正式源码守卫恢复 | 排除辅助/嵌套工作树，白名单精确路径；正式新增违规仍报错；AtomicWrite 移文件后分列保持总调用数。[守卫测试](<../../../internal/config/atomicwrite_convergence_test.go>) |
| D-28 | 完整根 preview 只读 | 从实际 parsed command 选 LoadReadOnly，不建 scheduler、不迁移、检测、提权或 shell；真实新进程与文件树对照。PM 批次（本地证据：`.scratch/report-remediation-20261122/pm/implementation.md`） |

## 独立复核发现的附加修正

不是只相信实现方的绿色报告。两名只读复核者用自己的 fixture 重新验证，并确认修复前反例转绿：

1. 嗅探旧 Pending 饿死新增/再次过期工具、完成部分却推进全局成功时间。
2. 无可用归档模板的 no-op 消耗 token 并假记成功。
3. 无 marker 的 bare PATH 用户 cron 被误删。
4. third-party 嗅探写回顶层影子，缓存成功但真实加载仍旧版本。
5. GUI 取消丢失 context 错误类型，使最终审计误记普通失败。
6. 即时清理的工具前缀误删 sibling 归档。
7. 本地 PM 批准程序被父工具 corepack/检测程序替换。
8. SSH 相对认证路径跨 CWD 漂移。
9. 完整 CLI dry-run 根初始化的遗留迁移副作用。

运行时最终只读复验（本地证据：`.scratch/report-remediation-20261122/review-runtime/final-review.md`） · 信任与实例最终只读复验（本地证据：`.scratch/report-remediation-20261122/review-trust/final-review.md`）。这些独立复验是指定 Windows 用例，不冒充独立全仓、Linux/race 或 macOS 测试。

## 最终验证

最终代码已冻结；包括共享安装根与 GUI 模式切换三阶段故障补偿在内的最终源码集合指纹为 `a0ae8eb7b3361bd37fccbf875ba94f33380d1cdce1bd274787772ef3a821e91d`。下列命令均记录了运行前后同一指纹，`source_stable=true`，实际退出码均为 0。

| 冻结源码验收 | 真实命令/范围 | 结果 |
|---|---|---|
| Windows 全仓 | Go 1.27.4 `go test -mod=readonly -p=2 ./... -count=1 -timeout=300s` | 全部包通过 |
| Linux 全仓 | WSL Go 1.27.3，同命令，CGO=0 | 全部包通过 |
| Linux 竞态 | CGO=1 `go test -mod=readonly -race -p=2 ./internal/... -count=1 -timeout=300s` | 全部受测包通过，无 race |
| Linux integration | `go test -mod=readonly -tags integration -p=2 ./... -count=1 -timeout=300s` | 全部包通过 |
| 静态与依赖 | Windows `go vet -mod=readonly ./...`、`go mod verify` | 通过 |
| 前端 | `test:unit`、smoke、Vite production build | **51 文件 / 314 用例通过**，smoke/build 通过 |
| Windows 原生构建 | `CGO_ENABLED=0 go build -mod=readonly -tags desktop,production,webkit2_41` | 通过 |
| Linux 原生桌面构建 | `CGO_ENABLED=1`，同 tags，实际 GTK/WebKit 库 | 通过 |
| Darwin/arm64 | 受影响 extractor/signer/discovery/manifest/orchestrator/config/env 包交叉编译 | 通过；不等于 macOS 实跑 |

原始命令、日志身份和源码身份详见 统一验证收据（本地证据：`.scratch/report-remediation-20261122/final/verification-summary.json`）。隔离 HOME 的最终构建冒烟覆盖 version/help、完整 PM preview、改根后旧 XMDEV_HOME 新进程定位及 list；不运行真实安装。

实际命令、原始输出、退出码与 SHA-256 均保存在 验证目录（本地证据：`.scratch/report-remediation-20261122/final/patch-manifest.json`）。编译中间态、旧源码守卫结构断言、模块缺失与最初 go.sum 非原子临时截断的失败均保留，不算行为修复红证据。日志打印编码错误也不冒充前端测试失败。

## 运行及兼容边界

- **macOS 只有相关包交叉编译，未跑真实 DMG/桌面验收。** XAR 仍需支持它的 bsdtar。未支持的 NSIS/Inno/自定义 SFX 明确拒绝，不回退到外部直接写盘。
- Lua 和解码器的进程内预算不等于任意 Go/native 阻塞、解码元数据内存或 OS 强制配额。多 Lua 执行重叠时堆采样不能归因。更强硬保证需要独立可回收进程。
- **未登记的旧 shim 保留并诊断**，需重新 activate/use 建立管理记录；不宣称历史未知条目会自动刷新。
- 改根是选择新空根；迁移要求源静止，不自动删除恢复旧树，也不改写任意文件内部绝对路径或外部 override/系统引用。崩溃可能留下未提交目标，不能宣称跨目录原子事务或所有掉电耐久性。
- GUI 模式切换是可报告错误下的补偿事务；配置、多个 shim 与注册表不构成跨资源崩溃原子事务。补偿失败会合并返回并保留恢复目录；外部未遵守同一锁协议的写者不在保证范围。direct 模式保留不在 PATH 中的闲置 shim，以免通过破坏性删除扩大失败风险。
- 真实管理员 token、UAC、远端 Docker/SSH、掉电和真实工具安装未测。测试使用本地服务、替身、临时根与隔离后端。
- 构建依赖确有联网获取，不声明整个任务零联网。首轮旧 localmigrate 红测漏内存 PATH 后端，执行过旧清理路径且无该次真实 PATH 前后快照；已记录并修掉生产清理路径。**因此不宣称整个会话零真实注册表接触或零修改。** 后续最终测试的 hostsnapshot 结果与这次早期事件必须分开。
- BugX 历史生命周期缺 runtime 文件；已保全，未重建、reset 或改账本制造绿色，本任务使用项目自身测试验证。

## 复查与回滚

补丁以开始树为基准，应用前先用 `git apply --check`。本工作区已包含修改，不要再次正向应用。撤销本次补丁前用 `git apply --reverse --check` 验证，并保管后续改动；不要用 `git reset --hard` 丢弃开始时已有工作。新根已切换时，恢复旧根需明确处理旧根 redirect 与旧配置，不能只删除新根。部署尚未替换，恢复只涉及源码，不涉及真实已安装产品。
