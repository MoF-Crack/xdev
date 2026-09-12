# Spec: 审计发现问题修复（audit-fixes）

> 依据文档：仓库根《审计发现问题修复方案.md》（v2 统一稿，2026-09-11——7 分区审计 + 三轮复核后的最终方案）。
> 本 spec 按仓库 issue-tracker 约定将修复方案拆为 42 张实施票（`.scratch/audit-fixes/issues/`），批次与耦合规则沿用方案第八节。

## 目标

按批次实施全部 P0（17 项）+ P1（41 项）+ P2（33 项，含复核新增 4 项）修复，每批以全量验证门收口。优先级由批次决定：**01（go v2）是本仓库一切验证门的前置，必须最先完成**。

## 批次与任务索引

| 票 | 标题 | 修复项 | 批次 | 耦合/依赖 |
|---|---|---|---|---|
| 01 | go.yaml GOROOT v2 + 注册表残留清理 | P0-17(go 部分) | B1 | **最优先**（全部验证门前置） |
| 02 | sshpass argv + SSH Exec 错误传播 | P0-01 + P1-32 + N-05 | B1 | 同 PR（sshPassArgv 单一来源） |
| 03 | Uninstall 嵌套锁释放 | P0-05 | B1 | — |
| 04 | 插件目录单源化 | P0-06 | B1 | — |
| 05 | RegEnumValueW 健壮性 | P0-07 + N-10 | B1 | — |
| 06 | SwitchToLocalVersion 校验顺序 | P0-08 | B1 | — |
| 07 | SSH adapter Create 消费 spec | P0-10 | B1 | 依赖 02（同文件域）|
| 08 | i18n 7 键 + phaseKeyMap | P1-36 | B1 | — |
| 09 | copyDir SkipDir + junction 机制 + 断链可见化 | P0-04 + N-01 | B2 | — |
| 10 | PATH RMW 四函数锁 + C-13 双函数大小写 | P0-11 + P2-10 + N-11 | B2 | — |
| 11 | CleanupUserPath home 锚定 | P0-13 | B2 | — |
| 12 | 批量会话 dispose + 重入守卫 | P0-14 | B2 | — |
| 13 | 签名链完整性 + TOFU 门控（统一 UnsignedVersion） | P0-02 阶段1 + P0-03 + N-02/03/04 | B3 | **同 PR 不可拆** |
| 14 | CLI profile 导出白名单 | P0-12 | B3 | — |
| 15 | CI 三渠道签名公钥注入 | P0-16 | B3 | — |
| 16 | ImportProfile mirrors 校验 + JSON 上限 | P1-17 + P1-31 | B3 | — |
| 17 | fill-sha256 163 + 52 manifest 重签 | 数据线 + P1-12(D-07) | B3 数据线 | Blocked by 13（先代码后数据）|
| 18 | CI 守卫 job 四边界 + index 重建 + mingw 处置 | P2-15(D-15) + P2-29 | B3 数据线 | Blocked by 17 |
| 19 | GUI 安装工具锁（含 runInstaller） | P0-09 | B4 | — |
| 20 | Lua 引擎三项加固 | P0-15 | B4 | — |
| 21 | .part.lock 互斥 + Pause 语义重做 | P1-01 + P1-02 + N-06/07 | B4 | **同 PR 不可拆** |
| 22 | saveLocalCache 原子化 + 刷新上提 | P1-08 | B4 | — |
| 23 | manifest 文件锁 + sidecar 原子写 | P1-09 + P1-13 + N-16 | B4 | — |
| 24 | catalog 快照 + Generic 锁 + 双 nil 门 | P1-16 | B4 | — |
| 25 | loadVersions 三入口过期守卫 | P1-22 + N-12 | B4 | — |
| 26 | 安装器正确性组 | P1-03/04/05/06/07 | B5 | — |
| 27 | 版本比较器全序化 | P1-10 | B5 | 必红测试反转 |
| 28 | manifest 加载/resolver/shim 索引组 | P1-11/14/15 + N-20 注释 | B5 | — |
| 29 | 自定义工具 + profile 导入组 | P1-18/19/20 + N-13 | B5 | — |
| 30 | CleanEnvVarPath %VAR% 展开 | P1-21 | B5 | — |
| 31 | AtomicWriteFile 改造 + no-path-fix 一致化 | P1-23 + P1-24 + N-15 | B5 | — |
| 32 | 自更新自愈组 | P1-25 + P1-26 | B5 | — |
| 33 | 配置枚举/退出码组 | P1-27/28/29 | B5 | — |
| 34 | startup 嗅探三入口 | P1-30 | B5 | — |
| 35 | shim 透传/隔离/远程持久化组 | P1-33/34/35 | B5 | — |
| 36 | 前端 Escape + PM 守卫组 | P1-37 + P1-38 | B5 | — |
| 37 | 构建链组（appimagetool/sed/BuildDate） | P1-39/40/41 | B5 | — |
| 38 | P2 抽取器与文件系统组 | P2-01/02/03/04/05/06/26/32 | B5 | — |
| 39 | P2 远程/semver/杂项组 | P2-07/08/09/11/12/13/14/23/21/22/30/31 | B5 | P2-12+14+23 同批（统一饱和）|
| 40 | P2 CLI/TUI 组 | P2-16/17/18/19/20/33 | B5 | P2-19 必红测试 |
| 41 | P2 前端组 | P2-24/25 | B5 | — |
| 42 | P2 env_vars 值校验 | P2-28 | B5 | 依赖 01（GOROOT 删除后收窄）|

## 耦合与顺序规则（违反即返工）

1. **票 01 最先**：go v2 未完成前任何验证门不可信（本机 go build 报 "package fmt is not in std"）。
2. **同 PR 不可拆组**：02（sshPassArgv 单一来源）、13（UnsignedVersion 三处门控）、21（.part.lock + Pause 快照/次序一体）。
3. **顺序依赖**：13 → 17 → 18（先代码语义、后数据填充重签、后 CI 守卫）；01 → 42（X-08 收窄以 GOROOT 删除为前提）。
4. **必红基线测试**（先红后绿 + 同票同步更新）：票 27（TestSortVersionsByRelease_DatedNewestBeatsOlderSemver 反转）、票 39（TestIsAllZeroHex `{"0",true}` 改 false）、票 40（TestInstallResultError_ClearsUpdateQueue 设 currentOpID）。
5. **「新增版本」场景前提**：票 13 的全部签名/TOFU 守卫测试按「嵌入优先合并」语义构造（磁盘篡改内置字段不生效——攻击路径是新增版本条目）。

## 每批验证门（方案第八节）

1. `go build ./...` + `go vet ./...`
2. `go test ./... -short -count=1` 全绿；并发类加 `-race`：downloader/gui/manifest/discovery/plugin/installer/config/env/util
3. `npx vitest run`（**在 frontend/ 下执行**）
4. `npx vite build`（frontend/）
5. e2e（Playwright）全绿
6. 各票「守卫测试」字段逐项确认

## 状态

- 所有票 `Status: open`。每票实施前置阅读《审计发现问题修复方案.md》对应小节（票内已标注锚点）。

## 批次实施细化方案（实施前必读）

每批的详细实施方案、测试用例设计与验收通过标准见 `plans/`：

| 批次 | 文件 | 覆盖票 |
|---|---|---|
| B1 | `plans/b1.md` | 01-08（go v2 前置 → sshpass/Exec → 其余行为恢复） |
| B2 | `plans/b2.md` | 09-12（PoC 先红后绿方法论：copyDir/PATH 锁/锚定/批量会话） |
| B3 | `plans/b3.md` | 13-18（签名链+TOFU 单 PR 铁律；六守卫含 G6 重装回归；数据线四边界） |
| B4 | `plans/b4.md` | 19-25（-race 八包清单为核心门；锁基建先行序：23→21） |
| B5 | `plans/b5.md` | 26-42（三必红基线；P1-24 收敛核验；全批次收口三件事） |

批次实施顺序与耦合规则不变；plans 文件中的 PR 划分与验收 DoD 是各批收口的执行依据。
