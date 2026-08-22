# Spec 030: Shim 热路径与 CLI/GUI 分离

> **文档类型**：Feature Spec
> **状态**：Approved
> **文档版本**：1.0.0
> **创建日期**：2026-08-21
> **所有者**：xinyan
> **权威范围**：CLI 默认构建不含 Wails；shim 热路径降成本；嗅探禁止 N×LoadCatalog；self-update 对齐真实 Release
> **Constitution**：`1.0.0`

## 一、目标

把性能分析里杠杆最高、且不推翻 ADR-0002/0003 的四件事落地：

1. **兑现 ADR-0002**：默认 `go build` 的 CLI/shim 二进制不链接 Wails / `internal/gui`。GUI 仅 `wails build -tags gui`（及 `bindings` 生成绑定）。
2. **Shim 热路径**：Unix 用 `unix.Exec` 覆盖进程；Windows `EnrichFromRegistry` 一次枚举 HKCU，不再对白名单逐个 `RegOpenKeyExW`。
3. **嗅探**：`SniffAll` 只 `LoadCatalog` 一次，把 `*manifest.Tool` 传给单工具嗅探；`interval` OnStartup 改为异步（与 `startup` 相同），禁止挡住 `xdev list`。
4. **self-update**：GitHub owner 改为 `MoF-Crack`；下载 CI 真实归档（zip/tar.gz），按 `SHA256SUMS` 校验归档后再抽出 `xdev`/`xdev.exe`。

**明确不做（本 Spec）**：Naive UI 拆包、PATH 一次 ReadDir 索引、hook/lockfile 恢复、isolate 会话持久化、前端启动瀑布。那些是下一轮。

## 二、现状（代码证据）

| 问题 | 证据 | 用户可见 |
|---|---|---|
| CLI 15.1MB 含 Wails | `main.go` 与 `internal/cli/extra.go` import `internal/gui` | 每次 `go`/`node` shim 映射胖 PE |
| Unix shim 双进程 | `shim.go:322` `exec.Command.Run` | `go test` 期间多驻留一份 Go runtime |
| Windows 注册表 N+1 | `EnrichFromRegistry` 对 ~56 个名字各开 HKCU | shim 额外 2–25ms |
| 嗅探 N×YAML | `sniffNowInternal` `LoadCatalog`（`scheduler.go:205`）且 `SniffAll` 已加载一次（`:407`） | 嗅探 CPU 高于 HTTP |
| interval 同步 60s | `onStartupInterval` `sniffAllWithForce` 在 OnInitialize 里等待 | `xdev list` 可能卡死 |
| self-update 404 | `githubOwner=xmdemt`；找 `xdev-windows-amd64.exe`；CI 发 `xdev-windows-amd64-v*.zip` | 更新不可用 |
| About 仓库 URL | `app.go:818` `github.com/xmdemt/xdev` | 错误链接 |

## 三、方案

### B1 — CLI 不含 GUI（深模块：入口 seam）

- 根包三个入口，互斥 build tag：
  - `main.go`：`!bindings && !gui` — 仅 CLI/TUI/shim
  - `main_gui.go`：`gui && !bindings` — `runGUI()`，直接 `gui.NewApp` / `gui.Run`
  - `main_bindings.go`：保持 `bindings`
- `internal/cli` **删除** 对 `internal/gui` 的 import。`xdev gui` 查找同目录 `xdev-gui`/`xdev-gui.exe` 并 exec；找不到则报错说明用 GUI 构建。
- `build.ps1`：`wails build -tags gui`。
- 守卫：CLI 包非 `gui` 源文件不得出现 `internal/gui`。

### B2 — Shim run / 注册表

- `run()` 拆到 `run_unix.go`（`unix.Exec`）与 `run_windows.go`（仍 `exec.Command`，因 Windows 无 overlay）。
- `EnrichFromRegistry` 改为 `GetAllUserEnvVars()` 一次枚举，再按白名单（大小写不敏感）合并。Unix stub 不变。

### B3 — 嗅探 catalog 与 OnStartup

- `sniffNowTool(ctx, *manifest.Tool, force)` 为唯一实现；`SniffNow(name)` 只为单工具加载一次 catalog。
- `sniffAllWithForce` 加载一次后把 `*Tool` 交给 worker。
- `onStartupInterval`：TTL 未过则返回；否则异步跑（60s timeout），`Shutdown` 可取消。
- `Registry.All` 改 `sort.Slice`（顺手，同批）。

### B4 — self-update

- `githubOwner = "MoF-Crack"`。
- 产物名：`xdev-{GOOS}-{GOARCH}-v{ver}.zip`（Windows）/ `.tar.gz`（Unix）。
- 校验 `SHA256SUMS`（已有 ToLower 匹配）针对**归档文件名**。
- 解开根级 `xdev` / `xdev.exe`（zip-slip：只允许 basename，禁止 `..`）。
- GUI About `RepoURL` 改为 `https://github.com/MoF-Crack/xdev`。

## 四、验收标准

1. `go list -deps .`（无 extra tags）不含 `github.com/wailsapp/wails`。
2. `go test ./internal/cli ./internal/shim ./internal/discovery ./internal/update ./internal/env ./internal/plugin ./internal/gui -count=1` 全绿。
3. `go test ./... -short -count=1` 全绿。
4. Unix：`run` 源码含 `unix.Exec`；Windows：`EnrichFromRegistry` 调用 `GetAllUserEnvVars`。
5. `SniffAll` 对 N 个工具只调用一次 catalog 加载（测试计数器）。
6. `OnStartup` interval 在缓存过期时 **500ms 内返回**（不跑完嗅探）。
7. `assetNameForPlatform("0.3.2")` 在 windows/amd64 得到 `xdev-windows-amd64-v0.3.2.zip`。
8. `xdev gui`（CLI 构建）在没有 `xdev-gui` 时非 0 退出并提示。

## 五、非目标 / 风险

- CLI 体积下降取决于链接器是否甩掉 Wails；以 `go list -deps` 为准，不以「必须小于 X MB」为门（本机仍可能因 TUI/embed YAML 保持十余 MB）。
- `wails build` 必须带 `-tags gui`，否则 GUI 入口不编译。
- Unix `Exec` 失败时回退错误返回（不静默 `Command`）。
- 不解压带路径穿越的更新归档。

## 批准记录 (Approval History)

| 阶段 | 状态 | 批准者 | 日期 | 文档版本 | 批准范围 |
|---|---|---|---|---|---|
| Spec 批准 | Approved | `xinyan <zuijiuhjd@foxmail.com>` | 2026-08-22 | `1.0.0` | `2026-08-22-spec-030-approved-001` Shim 热路径与 CLI/GUI 分离（提交时补录治理批准记录） |
