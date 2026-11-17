# ADR-0007：Wails 3 桌面运行时与单二进制构建边界

> **状态**：Accepted
> **文档版本**：`1.0.0`
> **日期**：2026-11-17

实施验收仍按平台分别记录。
- 范围：Spec 040 的桌面适配、绑定、启动策略与构建发布
- 授权来源：用户审阅完整架构优化与 Wails 3 迁移方案后明确要求「按照方案执行，注意并发数量」。该批准已记录于 Spec 040；不是仅凭技术偏好自行替换框架。

## 背景

XDev 已采用单二进制 CLI/TUI/GUI/shim 模型。Wails v2 运行时调用散布在业务方法中，旧文档仍混用双二进制、旧绑定目录与标签约定。安装入口又需要明确区分受理和终态，因此迁移必须同时稳定桌面适配边界，而不能通过更换 UI 框架重做产品。

维护和签名命令需要从干净检出运行。若整个默认包图都嵌入前端 dist，维护命令与绑定生成会依赖尚未生成的前端资源。另一方面，正式发布必须仍包含 GUI，不能把无桌面依赖的开发构建误发为完整产品。

## 决策

1. 固定 Go 依赖 `github.com/wailsapp/wails/v3 v3.1.0`，前端固定 `@wailsio/runtime` 3.1.0；继续使用 Vue 3 和 Naive UI。
2. `internal/gui/gui.go` 负责 Wails application、service、window、dialog 与 lifecycle 接线。业务代码通过 `desktopRuntime` 发事件和请求窗口操作，应用/会话 context 管理后台任务。
3. 正式生成绑定存放在 `frontend/bindings`。`frontend/src/desktop/bridge.js` 规范 v3 event envelope 为页面 payload，并管理事件退订及窗口动作。现有页面的兼容 namespace 可以保留，但生产调用来自正式生成绑定。
4. 正式分发仍只有 `xdev[.exe]`：Windows/macOS 用 `desktop,production`，Linux 加 `gtk3`。无参数先做 shim 分派，再进入 GUI；CLI/TUI/gui 子命令与工具 shim 共用入口。
5. 无 desktop 标签的构建用于开发和维护；GUI 返回明确的不可用错误，不嵌入 dist。`desktop,bindings` 使用仅供生成的空资源 stub；正式 desktop 构建用 `desktop && !bindings` 嵌入真实资源。bindings 标签不得用于发布产物。
6. 构建脚本统一委托 `node scripts/build.mjs`。生成器安装命令固定为 `go install github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0`。PowerShell 包装保留 SkipBuild 和 SkipAudit 两项既有开关。Wails v3 使用 `wails.yaml`，替代旧 `wails.json`；直接 `wails3 build` 不保证与项目发布签名流程等价。原生开发配置可供后续开发使用，本轮未验证直接 `wails3 dev`。
7. CLI 已解析的配置和 `--no-path-fix` 策略经 `runGUIWithConfig` / RunOptions 传入 GUI。显式 home 不得在 GUI 入口重载丢失；NoEnvWrite 不仅传给 installer，也约束 GUI 直接持久化 PATH、环境变量及 shell hook 的操作。
8. 发布签名由单一作业生成 manifests、签名与公钥，所有平台构建消费同一输入。release 必须验证可签清单覆盖，缺签失败，不生成替代生产签名的假数据。

## 安装契约与迁移边界

App 保留 legacy Install* 方法，但它们等待真实会话终结。新前端使用 StartInstall 返回唯一 sessionId，订阅 installSession 并可查询快照；accepted 不是 completed，取消 ACK 不是 cancelled。终态包括 completed、error、cancelled、needs-repair，revision 防止旧事件回写。

BeforeCommit 在实际安装树写入/激活前与取消仲裁，committing 后不再接受用户取消。单装、修复、PM 和 batch 共用前端 coordinator；后端按实际写入资源互斥。形态选择必须贯穿归档选择、缓存与已安装状态，不能仅改变 UI 文案。

Catalog 在本 Spec 中统一加载/指纹来源集合，保留 embedded-first 的同版本可信数据；latest 推进以合并有效版本和通道规则决定。框架迁移不改变这些信任边界。

## 取舍

- 默认 `go build .` 可用于维护命令，但不提供 GUI。文档和发布脚本必须始终区分维护构建与正式桌面构建。
- desktop adapter 和前端 bridge 暂时保留兼容调用形状，降低一次性改动范围；后续删除兼容 namespace 必须单独迁移消费者。
- Linux 正式构建依赖 GTK3/WebKitGTK 4.1，macOS 构建依赖平台工具链。没有对应宿主时，不把交叉编译或 CI 配置声明为运行验证。
- 对话框后端可能同步或异步完成，等待代码必须支持两者，并在应用/会话取消时结束等待。提交阶段不可被用户取消不代表可以脱离应用退出管理。

## 验证状态与出口条件

截至 2026-11-17，集成负责人报告：Windows desktop 构建、原生 WebView2 GUI 渲染、生成绑定调用、GetConfig 保持隔离临时 home 及正常退出均通过；Linux manifest/plugin/gui/installer 全包测试和 `desktop,production,gtk3` 构建通过。macOS 尚未实机运行，Linux 原生窗口交互尚不能由构建结果推断。这些结果不代表 Spec 040 已全部验收完成。

出口条件包括：默认 Go 测试/vet；正式 desktop 编译；前端单测、smoke 与生产构建；无 dist 的绑定生成；隔离 HOME 下 CLI/shim stdin/stdout/exit-code 回归；会话取消/终态/形态与查询缓存行为回归；发布归档经 updater consumer 验证；release 缺签明确失败。平台运行证据与缺失项由验收记录分别记录。

## Approval History

| 阶段 | 状态 | approval_id | 来源 | 日期 | 文档版本 | 范围 |
|---|---|---|---|---|---|---|
| 决策实施 | Accepted | `2026-11-17-adr-0007-001` | 用户明确指令，逐字授权文本见 approval JSON | 2026-11-17 | `1.0.0` | 已展示方案实施；不含最终验收、归档或发布授权 |
