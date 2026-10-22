# ADR-0006：单二进制分发（CLI/TUI/GUI 合并）

> **文档类型**：ADR
> **状态**：Accepted
> **文档版本**：`1.0.0`
> **创建日期**：2026-10-21
> **最后更新**：2026-10-21
> **ADR 编号**：0006
> **所有者**：`xinyan <zuijiuhjd@foxmail.com>`
> **关联 Constitution**：`.specify/memory/constitution.md` `1.0.0-rc.3`
> **权威范围**：xdev 可执行文件的构建形态、入口分派与发布产物命名
> **替代文档**：ADR-0002 中「独立 `xdev-gui` 产物 / `-tags gui`」的部分（Wails 选型本身仍有效）

## 背景

Spec 030 曾把 CLI 与 GUI 拆成两个可执行文件：CLI 由默认 `go build` 产出（不链接 Wails），
GUI 由 `wails build -tags gui` 产出 `xdev-gui`。

2026-08-23 起实际实现已合并为**单二进制**：

- `main.go` 无参数分支启动 GUI（先做 `shim.MaybeExec` 判定）；
- `xdev tui` / `xdev gui` 为子命令，GUI 通过 `cli.SetGUILauncher` 进程内启动；
- `main_gui.go` 的构建约束是 `!bindings`，默认构建即包含 GUI。

但 `docs/architecture.md`、`docs/installation-guide.md`、`docs/usage-manual.md` 与 ADR-0002
仍按双二进制描述，导致用户按文档寻找不存在的 `xdev-gui-*` 产物（2026-10-21 审计 P2-6）。
本 ADR 把既成事实固化为决策，并明确文档口径。

## 决策

1. **单可执行文件**：每平台只产出一个 `xdev[.exe]`，默认构建即链接 Wails。
2. **入口多态**：
   - 无参数：先 `shim.MaybeExec(basename)`；未命中则启动 GUI；
   - `xdev tui`：TUI；
   - `xdev gui`：进程内启动 GUI；
   - 其余参数：CLI 子命令。
3. **构建标签**：`bindings` 仅用于生成 Wails 绑定。GUI **必须**带
   `-tags desktop,production`（`wails build` 自动带入；手工 `go build`
   必须显式传入，否则 Wails 运行时会弹出
   “Wails applications will not build without the correct build tags”，
   GUI 完全不可用）。CI/Release 的每一条 `go build` 都由
   `frontend/test/r9-ci-wails-tags.spec.js` 守卫。
4. **发布产物命名**：`xdev-<version>-<os>-<arch>[.exe]`（归档为 `.tar.gz` / `.zip`，
   Linux 另有 `.deb` / `.rpm` / `.AppImage`），**不再有** `xdev-gui-*`。

## 候选方案

| 方案 | 优点 | 缺点 | 结论 |
|---|---|---|---|
| 双二进制（Spec 030） | CLI 体积小、不依赖 GUI 运行时 | 两套产物/两条构建链；shim 与 CLI 需额外同步 | 否决 |
| 单二进制 + 运行时分派（本决策） | 一个产物、shim 与 CLI 天然同源、发布简单 | 体积 +~3MB；CLI 也链接 GUI 依赖 | **采用** |
| 单二进制 + `-tags gui` 二选一 | 可裁剪 | 需要用户理解构建标签；发布仍需两条链 | 否决 |

## 影响与后果

- **正面**：每平台单产物；`xdev` 同时是 CLI/TUI/GUI 与所有 shim；发布与自更新只有一条路径。
- **负面**：二进制体积增加约 3 MB；CLI 启动也加载 Wails 链接期依赖（实测可接受）。
- **测试**：`internal/cli` 不 import `internal/gui`（Spec 030 守卫仍成立），GUI 通过注入启动器接入。
- **文档**：architecture / installation / usage 必须以本 ADR 为准。

## 兼容性、迁移与回滚

- **兼容性**：旧版 `xdev-gui.exe` 由 `EnsureSelfInstalled` 精确文件名清理；
  `current/xdev` 指向单二进制。
- **迁移**：用户无需操作；重新运行 `xdev init` 或 `xdev self install` 即可。
- **回滚**：恢复 `-tags gui` 构建与双产物发布链（不推荐，需同步回滚文档与 shim 解析）。

## Approval History

| 阶段 | 状态 | approval_id | 批准者 | 日期 | 文档版本 | 批准范围 |
|---|---|---|---|---|---|---|
| ADR 提案批准 | Accepted | `2026-10-21-adr-0006-001` | `xinyan <zuijiuhjd@foxmail.com>` | 2026-10-21 | `1.0.0` | 批准单二进制分发与文档口径 |

## 变更历史

| 版本 | 日期 | 变更类型 | 说明 | 批准者 |
|---|---|---|---|---|
| `1.0.0` | 2026-10-21 | 新增 | 固化既成的单二进制实现，替代 ADR-0002 的产物部分 | `xinyan <zuijiuhjd@foxmail.com>` |
