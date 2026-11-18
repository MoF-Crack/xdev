# Spec 040：架构契约收敛与 Wails 3 迁移验收记录

- 日期：2026-11-17
- 实施分支：`implementation/wails3-20261117`
- 隔离工作树：仓库根 `.scratch/worktrees/wails3-upgrade`
- 原始基线：`main@921e35a`，含用户已有的 29 个修改与 16 个未跟踪文件。
- 实施前恢复快照：`recovery/pre-wails3-20261117@f0451d1a19d5f29a39df8e3b3c4af26a56ebc4af`。
- 授权：用户审阅优化与 Wails 3 迁移方案后明确要求“按照方案执行，注意并发数量”。本次未推送、未打 tag、未发布、未更新真实工具安装。

## 1. 完成范围

### Catalog

加载、fallback 与缓存指纹共用顶层 YAML 和 `third-party/` 来源集合。第三方目录新增、修改、删除以及同名来源覆盖均有 cold/warm 行为测试。缓存版本升级，旧的顶层专用指纹不能误命中。

内嵌与磁盘合并保留内置同版本归档和哈希。磁盘 latest 只有在合并后存在可安装版本、更高且满足发布通道规则时推进；稳定版本不自动切换到预发布，原 LTS/首项默认不会因新增版本排序而被意外替代。插件测试覆盖实际 DefaultVersion → ResolveVersion → ArchiveFor 消费链。

### 安装会话

- 新增 StartInstall、GetInstallSession、CancelInstall，受理与终态明确分开。
- 每次安装使用唯一 sessionId，资源身份与会话身份分离，revision 拒绝乱序事件。
- completed、error、cancelled、needs-repair 均只结算一次并释放等待者。
- 用户取消与 BeforeCommit 共用锁；进入提交阶段后取消按钮不再宣称可用。
- 下载完成到安装提交之间仍可取消，不再把下载器的 Completed 当作整个安装完成。
- 单装、修复、批量和 PM 共享前端 coordinator，预约在预检/确认前取得。
- 旧 Install* 导出方法保留，等待真实结果；兼容进度/错误通知仍携带会话路由。
- PM、sub-tool 子进程使用会话/应用 context，并接入平台进程树终止及有界 shutdown。
- 原生确认框支持同步和异步后端，取消会话能释放等待，迟到/重复回调不能恢复取消安装。
- 安装/卸载统一失效 Dashboard、工具列表和大小缓存。
- 取消后工具列表刷新导致原触发器 DOM 替换时，以工具身份恢复焦点；用当前 sessionId、视图与活动状态防止旧回调干扰新安装。

### 形态与缓存身份

Form 参与实际归档解析。形态特定归档使用所选 URL、平台、形态、格式、kind、strip 的身份生成缓存后缀，下载、解包和可执行安装器共用路径。

安装目录原子记录 `.xdev-install-selection.json`。已有形态身份未知或不同，返回 ErrIncompleteInstall 要求显式 force 修复，不再报告成功后仍保留旧形态。无形态旧安装保持兼容。双 ZIP fixture 覆盖空/占位哈希、warm cache、同版本切形态和 URL 更换。

### 桌面运行时

- Go 固定 `github.com/wailsapp/wails/v3 v3.1.0`，前端固定 `@wailsio/runtime` 3.1.0。
- 删除旧 Wails v2 依赖和 `frontend/wailsjs` 生成物，正式绑定位于 `frontend/bindings`。
- Wails application/window/dialog/lifecycle 集中于 Desktop Adapter；业务模块不 import 框架 runtime。
- 前端 bridge 将 v3 event envelope 规范为页面 payload，管理订阅退订；页面暂保留兼容 namespace，但生产调用使用正式生成绑定。
- CLI 已解析 home 与 NoEnvWrite 显式传入 GUI，不再在 GUI 下游重新加载默认配置。
- NoEnvWrite 同时约束直接环境编辑、PATH 模式、hook、Profile、初始化/迁移副作用；可能修改宿主 profile 的官方 PM 脚本在该模式下拒绝执行。
- 正式分发仍是一个包含 CLI/TUI/GUI/shim 的二进制；Windows/macOS 使用 `desktop,production`，Linux 增加 `gtk3`。
- 无 desktop 标签用于开发/维护，GUI 返回明确错误；不需要 dist。`desktop,bindings` 使用仅生成时的空资源 stub，正式 desktop 仍嵌入真实资源。

### 构建、发布和文档

本地包装与 CI 统一使用 `scripts/build.mjs`，固定生成器版本并注入版本/公钥。维护命令可以在没有前端资源时构建；绑定生成不再与 dist 存在循环依赖。

CI 明确三平台验证矩阵，矩阵并发上限 2；每个平台规范归档具有更新器要求的根成员 `xdev` / `xdev.exe`。一次生成已签目录供所有平台使用；release 构建缺公钥、缺签名覆盖必须失败。汇总完整平台资产后生成 SHA256SUMS，先 draft 再发布，不静默改写已发布版本。

保留 Linux AppImage、amd64 deb/rpm 和 Apple Silicon DMG，另补完整的跨平台 tar.gz/zip 更新归档。真实签名密钥未在本次执行中使用；没有远端运行 release。

更新 CONTEXT、架构/安装指南、版本策略、issue 工作文档边界及 ADR-0007。Wails 配置使用 `wails.yaml`。直接 `wails3 dev/build` 不宣称等价于已经验证的项目构建/签名入口。

## 2. 实际验证

| 检查 | 结果与范围 |
|---|---|
| Windows `go test -p=1 ./... -count=1 -timeout 10m` | 33 个测试包通过，4 个包无测试。最终完整串行运行 exit 0。 |
| Windows `go vet ./...` | exit 0。 |
| Windows `go test -tags desktop,production ./internal/gui` | 通过；桌面标签代码实际编译和运行测试。 |
| Windows `go test -tags integration ./internal/integration` | 通过，使用本轮正式桌面二进制、临时 home 和 no-path-fix。最终重跑 4.884s。 |
| Windows 生产二进制 | 项目脚本成功生成单二进制，并通过 version 自证。 |
| Windows 原生 WebView2 冒烟 | 隔离临时 home 下窗口渲染、生成绑定 GetAboutInfo/GetConfig/GetBootstrapState、未知工具错误和正常退出通过；page errors 为空。调试连接仅通过测试 overlay 加入本地端口，未进入生产源码。 |
| Linux WSL 核心测试 | manifest、plugin、gui、installer 全包通过。 |
| Linux `-race` | 上述四个核心包通过，exit 0。不是全仓竞态覆盖声明。 |
| Linux 生产桌面编译 | `CGO_ENABLED=1 go build -tags desktop,production,gtk3` 通过。未实测 Linux 原生窗口交互。 |
| 跨平台测试编译 | linux/amd64、darwin/arm64 的非 desktop `go vet ./...` 通过。不是 macOS 原生 GUI 运行验证。 |
| 前端完整 unit | 51 个文件、312 个用例通过；最终代码再跑 exit 0。 |
| 前端 smoke / production build | 通过。最终 Vite build 转换 4,254 个模块。 |
| Playwright 安装流程 | 单 worker，后台进度、暂停/继续/取消与焦点、并行批量安装三条通过，9.0s；未更新视觉基线、未执行全套截图验收。 |
| 干净源码副本 | 没有 dist/node_modules/vendor 时，维护二进制、bindings、npm ci/build、正式桌面编译均通过。 |
| 发布归档契约 | Node 打包/完整校验和测试通过；实际 Windows ZIP 输入更新器提取函数通过，拒绝缺归档集合。 |
| 严格签名负例 | 原始缺签目录设置 XDEV_REQUIRE_SIGNATURE_COVERAGE=1 后明确失败。 |
| 严格签名正例 | 仅在复制的临时源码中生成测试密钥，51 个可签工具签名、1 个无哈希豁免，覆盖与验签测试通过；仓库 keys/manifests 未修改。 |
| 工作流语法 | actionlint 1.7.7 对 CI、独立签名和 URL 巡检通过。 |
| 依赖与空白 | `go mod verify`、最终 `git diff --check` 通过。 |

本机执行通过明确的真实 Go/Node/Git 路径，避免已安装 XDev shim 对工具选择和捕获输出的干扰。

### 异常及处置

- 早期多个子代理被环境中断，后续从磁盘半成品接管，最后最多两个并行，未叠加更多执行者。
- 阿里云 Go 代理一度中断，使用备用代理补齐固定依赖；未降低依赖校验。
- 安装接线曾被错误文本编码破坏，已从基线 UTF-8 恢复并以显式 UTF-8 重放；实际 AppContent 挂载、unit、生产 build 和浏览器 E2E 均已通过。
- 一次并行全量出现既有 isolator 临时文件 rename denied。该用例单独连续五次通过，最终完整串行套件通过；未为通过测试改掉失败断言。
- 原生窗口第一次只靠环境变量连接 CDP 超时，但日志证实启动；使用仅测试 overlay 的 v3 AdditionalBrowserArgs 后实际渲染/调用/退出通过。
- PowerShell 文档检查脚本受本机签名执行策略阻止，未修改系统策略；更新文档的本地文件链接用只读检查验证，构建参数守卫通过。

## 3. 独立审查

合规审查先后指出并修复：异步 native dialog 提前返回、NoEnvWrite 旁路、干净 bindings/dist 构建循环、确认期间取消、形态缓存/安装身份、PM 命令脱离退出 context。实现后的只读复核确认上述接线无剩余代码阻塞。

工程质量审查覆盖 Catalog 默认消费链、Desktop Adapter/bridge、会话锁内 commit/cancel、revision 与 coordinator 所有权；明确保留平台和资源生命周期限制，不以 mock 通过代替真实 GUI 或发布证据。

附加发布工作流审阅确认 Linux deb/rpm、双架构 AppImage、arm64 DMG 和集成测试职责保留，并指出 Windows 同一步多条原生命令可能覆盖 go vet 退出码。已拆成独立 Lint/Test 步骤并新增 CI 结构守卫。最后的 focus-only 追加审阅受到环境中断，未计为独立通过；该小改动由主代理复核，并以三条实际浏览器流程和完整前端测试验证。

本地实现通过不等于用户已经批准最终验收或归档；本记录为交付证据，最终接受由用户决定。

## 4. 仍需保留的限制

1. macOS 未实机运行，Linux 未实测 native window；正式发布前必须完成相应 CI/平台验证。
2. 本次未触发 GitHub Actions、真实发布、代码签名/公证，也未运行真正的外部 PM 安装脚本。
3. 当前仓库仍有历史缺签清单；本次只建立严格门禁并在测试副本验证签名链，不使用临时密钥替换生产签名。正式 release 作业须使用真实受保护 secrets 生成共同输入。
4. 会话终态与 ID 在 App 生命周期保留，长期高频操作可能增长内存；后续可设计有界 tombstone/历史清理，不在本次临近交付时改变身份保证。
5. Catalog 使用路径/mtime/size 指纹，不保证识别刻意保留 mtime 的等长替换；这是缓存模型，不是完整性认证。
6. 取消确认能结束会话，但原生模态框可能留到用户关闭；迟到回调不会恢复已取消安装。
7. Windows 子树取消使用有界 taskkill /T，不是新引入的 Job Object；退出等待有界，超时会告警。
8. 前端保留 window.go/window.runtime 兼容 namespace；它们使用生成绑定和正式运行时，不是额外一套 RPC。

## 5. 使用与恢复

- 在隔离工作树继续：`.scratch/worktrees/wails3-upgrade`。
- 正式本地构建：安装固定 `wails3@v3.1.0`，执行 `node scripts/build.mjs --skip-audit` 或平台包装入口。
- 日常测试：`go test ./...`；桌面分发必须带 desktop 标签或使用项目脚本。
- 实施前恢复快照保留完整原有修复。原 main 工作区尚保留相同的未提交状态，未自动覆盖或合并。
- 不要将隔离工作树内的发布用二进制直接替换真实 XDev 安装；先由用户审阅改动、完成目标平台验证，再决定合并和发布。
