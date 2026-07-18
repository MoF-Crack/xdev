> **文档类型**：分析报告
> **状态**：Snapshot
> **来源**：docs/progress-and-defects-analysis.md
> **P4B 迁移目标**：待用户决定
> **替代文档**：无
> **治理记录**：P4A 元数据治理，2026-07-16

# XDev 开发进度与功能缺陷分析报告

> 基于本会话源码探查 + 测试执行实证 · GOROOT 已恢复至 `D:/Env/golang` · 当前版本 1.2.2-alpha
> 生成时间：本次会话末

---

## 一、开发进度总览

### 1.1 三层测试体系已建立（本会话成果）

| 层级 | 状态 | 用例数 | 触发方式 | 备注 |
|------|:----:|:------:|---------|------|
| **L1 单元** | ✅ | 374 | 默认 `go test ./internal/...` | 24 包全绿 |
| **L2 集成** | ✅ | 19 | `-tags integration` | 驱动真实二进制，离线 |
| **L3 端到端** | ✅ | 2 | `-tags online` | 真实下载；网络中断时 SKIP |
| **L4 GUI** | ⏳ | — | 人工 | 清单已列，未执行 |

测试函数总数：332 → **395**（+63）。

### 1.2 路线图阶段完成度（代码实证）

| 阶段 | 完成度 | 证据 |
|------|:------:|------|
| M1–M6 核心 | 100% | 38 插件、CLI/TUI/GUI、Shim、断点续传、签名 |
| Phase 1 项目级 | 100% | `.xdev.toml`/cd hook/local/task 全落地 |
| Phase 2 Lockfile+插件 | 100% | Lockfile 闭环（本次补齐 url/sha256/spec）、Lua 沙箱、vfox 兼容、远程发现 |
| Phase 3 服务编排 | 100%（服务） / 0%（跨平台） | service.Manager 真实进程管理；macOS/Linux 完全未动 |
| Phase 4 隔离/远程/AI | 框架 100% / 跨平台生产化未做 | isolator(none/container/nix)、orchestrator(docker/ssh)、ai(Ollama) 均落地，CLI+GUI 双通 |

---

## 二、功能缺陷分析（按严重度排序）

### 🔴 P0 — 测试隔离失效：激活写入持久化用户环境变量

**现象**：本会话执行 L2 集成测试 `TestWhich_ResolvesInstallPath`（`use go@1.22.0`）后，Go 工具链被永久破坏：
```
go: cannot find GOROOT directory:
  C:\Users\xinyan\AppData\Local\Temp\TestWhich.../install/go/1.22.0
```
`GOROOT` 被写入指向已删除的 tmpdir，导致 `go vet`/`go build` 全部 exit 2，需手动 `export GOROOT=...` 恢复。

**根因**：`installer.go:452 setUserEnvVars()` → `env.SetUserEnvVar()` 直接写 **Windows 用户级注册表**（`HKCU\Environment`），且：
- `go` 插件 manifest 设 `GOROOT`/`GOPATH`（`manifests/go.yaml:11`）。
- `use` 命令**无 `--no-activate` 标志**（只有 `install` 有），无法跳过激活。
- `--no-path-fix` 只阻止 PATH/shell RC 写入，**不阻止 `setUserEnvVars`**。
- 测试用 `t.TempDir()` 作 `--xmdevenv-dir`，但注册表是宿主共享的，tmpdir 删除后留下悬空 `GOROOT`。

**影响**：
1. 任何 `use <tool>` 的测试/CI 都会污染宿主环境变量（不仅 Go，还有 `JAVA_HOME`/`NODE_HOME`/`PYTHONHOME` 等）。
2. 普通用户卸载工具后，`*_HOME` 环境变量可能残留指向已删路径。
3. 多用户/CI 共享机器上测试不可重复。

**修改建议**：
- **短期（测试侧）**：L2 中所有触发真实激活的测试（`TestUse_*`、`TestWhich_*`、`TestCurrent_*`、`TestList_*`、`TestUninstall_*`、`TestUse_SwitchesBetweenVersions`）应：
  - 测试后用 `reg delete HKCU\Environment /v GOROOT /f` 等清理，或
  - 改用不设 `env_vars` 的工具（如 `ninja`，无 `*_HOME`），或
  - 临时备份/恢复相关注册表项。
- **中期（产品侧）**：给 `use`/`install` 增加 `--no-env-write` 标志，在 `setUserEnvVars` 入口处 `if opts.NoEnvWrite { return }`。这使集成测试真正无副作用。
- **长期**：激活写注册表改为写 `<home>/env/<tool>.env`（进程级注入），仅在用户显式 `xdev env --persist` 时落注册表。

---

### 🔴 P0 — `search` 无匹配时退出码不语义化 ✅ 已修复

**现象**：`xdev search zzz` 找不到工具时输出 `no tool matches "zzz"` 但 **exit 0**。
```
=== search zzz ===
no tool matches "zzz"
EXIT=0
```

**影响**：脚本/CI 无法用退出码判断「是否有匹配」，必须解析输出文本——脆弱。与 `install foo@1`（exit 1）语义不一致。

**修改建议**：`internal/cli/search.go` 中无匹配时返回非零退出码（建议 exit 1），并在 `--help` 注明。或新增 `--fail-on-empty` 标志保持向后兼容。

**修复状态**：✅ 已修复。`available.go:34` 现在返回 `fmt.Errorf("no tool matches %q", args[0])`，Cobra 自动 exit 1。

---

### 🟡 P1 — `init` 触碰宿主环境（即使 `--no-path-fix`）

**现象**：`xdev init --yes --no-path-fix` 仍执行 `InstallSelf`（复制二进制到 `install/xdev/<ver>` + 创建 `current/xdev` junction），且日志显示「injected shim path into PowerShell profile」「added shim dir to Windows user PATH (registry)」——尽管 `--no-path-fix` 应阻止这些。

**复核**：实际探测显示 `--no-path-fix` 确实阻止了 profile/PATH 写入（`init --yes --no-path-fix` 输出无「injected」「added to PATH」行）。但 `InstallSelf` 仍执行（复制 ~20MB 二进制到 tmpdir），拖慢测试且非必要。

**影响**：每次集成测试 `initHome` 复制 20MB，18 个用例累计 ~360MB IO；`init` 语义混淆（初始化 vs 自安装）。

**修改建议**：`init` 增加 `--no-self-install` 标志跳过 `InstallSelf`；或 `InstallSelf` 仅在 `current/xdev` 不存在时执行（幂等短路）。

---

### 🟡 P1 — `install --locked`/`--from-local` 对未注册工具静默跳过 ✅ 已修复

**现象**：lockfile/toml 含未注册工具（如 `nosh`）时：
```
⚠ unsupported tool "nosh" in lockfile, skipping
EXIT=0
```
**整个安装过程退出 0**，尽管什么都没装。

**影响**：团队协作场景下，lockfile 笔误（工具名写错）会**静默成功**，开发者以为环境就绪实则未装任何东西。违背 lockfile「确定性」承诺。

**修复状态**：✅ 已修复。`install.go:134-136` 和 `install.go:430-432` 现在统计 skipped/failed 计数，>0 时返回 error → exit 非 0。

**修改建议**：`installLocked`/`installFromLocal` 末尾统计：若有任何工具被跳过，则 **exit 非 0** 并打印汇总（`N tools skipped, M installed`）。或新增 `--strict` 标志在跳过时即失败。

---

### 🟡 P1 — Lockfile 与 `.xdev.toml` 不同步风险 ✅ 已修复

**现象**：`updateLockfile` 在 `install` 后写入，但：
- 用户手动改 `.xdev.toml` 的版本 spec 后，`.xdev.lock` **不会自动失效**。
- `install --locked` 只读 lockfile，不校验其 spec 是否仍匹配 toml。
- 无 `xdev lock` 显式命令重新生成 lockfile。

**影响**：toml 改 `node = "22"` 但 lockfile 仍 `resolved=20.18.0`，`--locked` 会装旧版本，开发者无感知。

**修复状态**：✅ 已修复。新增 `xdev lock` 命令（`internal/cli/lock.go`）支持重新生成和 spec 校验。

**修改建议**：
- 新增 `xdev lock` 命令：读取 `.xdev.toml`，重新解析并写 `.xdev.lock`。
- `install --locked` 启动时校验 lockfile 的 `spec` 字段是否与 toml 一致，不一致则警告/失败。

---

### 🟡 P1 — `init` 不创建 `current/` 目录（文档与实现不符）

**现象**：`init` 创建 `install/shims/cache/manifests/downloads/env/config.yaml`，但**不创建 `current/`**（惰性创建于首次 Activate）。原 L2 测试断言 `current/` 存在导致全 FAIL，修正后通过。

**影响**：技术文档 §3.2 目录结构图把 `current/` 列为 init 产物，误导。

**修改建议**：`init` 显式 `MkdirAll(current, 0o755)`（一行代码），消除惰性创建的隐式依赖；或更新文档明确标注「current/ 首次激活时创建」。

---

### 🟢 P2 — `install --locked` 缺少 lockfile 时报错信息不友好

**现象**：无 `.xdev.lock` 时 `install --locked` 报 `no .xdev.lock found or no tools locked`——尚可，但未提示如何生成。

**修改建议**：错误信息附「Run `xdev install <tool>` in a project with .xdev.toml to generate one.」

---

### 🟢 P2 — 在线 E2E 测试无共享缓存，重复下载

**现象**：`t.TempDir()` 作 home，cache 在 home 内，每个 EPIC 测试独立 → 相同工具重复下载（网络好时 EPIC-1+EPIC-3 各下一次 ninja）。

**影响**：在线测试慢且对网络抖动敏感（本次 GitHub 中断导致全 SKIP）。

**修改建议**：在线测试用共享 `testdata/cache/` 目录（`--xmdevenv-dir` 指向持久 tmpdir，跨测试复用），或 `t.Setenv("XDEV_CACHE_DIR", sharedCache)`。

---

### 🟢 P2 — 文档同步滞后（本会话已部分修正）

**现象**：README/technical-reference 已在本会话更新（Phase 3/4 状态修正、命令表补全、§14/§15 Lockfile/隔离章节已存在）。但 `UNI_ENV_开发文档.md` 仍标「规划阶段」，易误读为 xdev 待办。

**修改建议**：已在计划中（第 5 项）——顶部加「竞品对标愿景，非 xdev 功能规格」声明。

---

## 三、修改建议优先级与工作量评估

| 优先级 | 缺陷 | 建议动作 | 工作量 | 影响面 |
|:------:|------|---------|:------:|:------:|
| 🔴 P0 | 测试隔离（env 写注册表） | ① 测试改用无 env_vars 工具或加清理 ② 产品加 `--no-env-write` | 小/中 | 测试+CI+用户 |
| 🔴 P0 | search 无匹配 exit 0 | `search.go` 无匹配时 exit 1 | 极小 | 脚本/CI |
| 🟡 P1 | init 触碰宿主/自安装 | 加 `--no-self-install` 或幂等短路 | 小 | 测试+用户 |
| 🟡 P1 | --locked 静默跳过未注册 | 跳过计数 → exit 非 0 | 小 | 团队协作 |
| 🟡 P1 | lockfile/toml 不同步 | 新增 `xdev lock` + 启动校验 | 中 | 团队协作 |
| 🟡 P1 | init 不建 current/ | `init` 加 `MkdirAll` 一行 | 极小 | 文档一致性 |
| 🟢 P2 | --locked 错误提示 | 改错误信息 | 极小 | UX |
| 🟢 P2 | E2E 无共享缓存 | 测试用共享 cache 目录 | 小 | 测试稳定性 |
| 🟢 P2 | uni-env 文档声明 | 加顶部 disclaimer | 极小 | 文档 |

---

## 四、本会话已修复的缺陷

| 缺陷 | 修复 | 验证 |
|------|------|------|
| `InstallResult` 缺 Spec/URL/SHA256 字段 | 扩展结构体 + 4 返回点填充 | L1 测试断言 |
| `updateLockfile` 丢 url/sha256、spec=resolved 混淆 | 改签名 + 填全字段 | L1+L2 测试 |
| `SHA256[:16]` 短哈希越界 panic | 已有长度保护（`if len > 16`）| grep 确认 L215-222 |
| Lockfile 包无测试 | 新增 `lockfile_test.go` | `go test` ok |
| 文档 Phase 3/4 误标 ✅ | README 修正为 ✅/🔶 | 已提交 |

---

## 五、结论

**整体进度健康**：三层测试体系建立，路线图 Phase 1-2 完整，Phase 3-4 框架齐备。**最大风险不在功能缺失，而在测试隔离与 lockfile 语义**：

1. **测试隔离（P0）** 是当前最痛的点——本会话被 `GOROOT` 污染打断过。不修复则 CI 不可信、开发者不敢跑集成测试。
2. **lockfile 静默跳过 + 不同步（P1）** 削弱了「可复现环境」这一核心卖点，团队协作场景下尤其危险。

**建议立即处理顺序**：① 测试侧改用无 env 工具或加清理（止住污染）→ ② 产品侧加 `--no-env-write` + search exit 码 → ③ lockfile 语义加固（`xdev lock` + strict）。前两项可在一个小迭代内完成，第三项作为 Phase 2 收尾。
