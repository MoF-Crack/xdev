# XDev 审计问题解决方案

> 对应报告：[2026-10-21-code-review.md](2026-10-21-code-review.md)
> 基线：`main` @ `6151252`（2026-10-21）
> 目标：把报告中的 P1/P2/P3 问题收敛为**可执行、可验证、可回滚**的批次
> 原则：**先接门禁，再改代码**；每个问题配一条能红→绿的守卫；不通过修改测试期望来“通过”

---

## 〇、批次总览

| 批次 | 内容 | 对应问题 | 预计工作量 | 出口标准 |
|---|---|---|---|---|
| **B0** | 门禁与跨平台可编译 | P1-1、P1-2 | 0.5 天 | Linux/macOS 测试可编译；PR 上 CI 三矩阵全绿 |
| **B1** | 运行态与用户可见 | P1-3、P1-4、P3-12 | 1 天 | 本机 `current/xdev` 为新版；`xdev list` 无伪版本；doctor 能发现旧副本 |
| **B2** | 语义与安全 | P1-5、P2-8、P3-13/14/15 | 2 天 | `InstallDir` fail-closed；版本目录判据单一来源；shim 覆盖率可测 |
| **B3** | 文档与流程 | P2-6/7/9/10/11 | 0.5 天 | 文档与单二进制一致；HANDOFF 单节；审计证据可归档 |
| **B4** | 高成本/环境项 | 遗留 10–13 | 另立 Spec | 有明确结论或环境证明 |

> 建议顺序：B0 → 推送 PR（拿到第一份真实 CI 结果）→ B1 → B2 → B3。B4 不阻塞发布。

---

## 一、B0：先让结论可被验证

### 1.1 修复非 Windows 测试编译（P1-1）

**根因**：两个测试文件没有平台约束，却引用 `//go:build windows` 文件中的符号。

**方案 A（推荐）：按文件性质拆分**

1. `internal/util/junction_native_r11_test.go`
   - 整个文件都是 junction / reparse buffer 测试，**直接加文件头**：

   ```go
   //go:build windows

   package util
   ```

2. `internal/cli/config_path_guard_test.go`
   - 文件内 4 个用例混合：
     - `TestConfigSet_XMDevDir_RejectsRelativeAndUNC`（依赖 `driveFixture`）
     - `TestPromptDriveCustomPath_RejectsRelativeAndUNC`（Windows 盘符交互）
     - `TestPathModeEnum_SymlinkRejectedAutoAccepted`（Windows 语义）
     - `TestLoad_RelativeHomeBecomesAbsolute`（可移植）
   - **拆文件**：把前 3 个移到 `config_path_guard_windows_test.go` 并加 `//go:build windows`；第 4 个留在原文件（若它不依赖 `driveFixture`）。
   - 若第 4 个也依赖 `driveFixture`，则把 `driveFixture()` 从 `drive_prompt_test.go` 移到不带 tag 的 `testhelpers_test.go`。

**方案 B（快速止血）**：整个文件加 `//go:build windows`。代价是丢掉一个可移植用例的跨平台覆盖——不推荐作为最终态。

**验收**

```bash
GOOS=linux  GOARCH=amd64 go vet ./...
GOOS=darwin GOARCH=arm64 go vet ./...
```

两个命令均 exit 0，且 Windows 下 `go test ./internal/util ./internal/cli` 仍全绿。

---

### 1.2 增加跨平台编译门（防止再次发生）

**新增** `scripts/ci/check-cross-compile.sh`：

```bash
#!/usr/bin/env bash
set -euo pipefail
# Test files are compiled by `go vet`; a Windows-only helper referenced from an
# untagged _test.go fails here instead of in a release PR.
for target in linux/amd64 darwin/arm64; do
  echo "== go vet ${target} =="
  GOOS="${target%/*}" GOARCH="${target#*/}" go vet ./...
done
```

**CI**（`.github/workflows/ci.yaml`，在 `Test` 之前）：

```yaml
      - name: Cross-platform test compile (non-Windows)
        if: matrix.goos == 'ubuntu-latest'
        run: bash scripts/ci/check-cross-compile.sh
```

说明：ubuntu 上跑 linux 是重复的，但保留它能让脚本在本地也有意义；真正新增的覆盖是 darwin。

**本地**：把这条命令加入 HANDOFF 的“验证清单”和 `build.ps1 -Verify`（若存在）。

**验收**：故意在无 tag 测试里引用 Windows 符号，脚本必须 exit 1。

---

### 1.3 让 CI 首次覆盖这 69 个提交（P1-2）

CI 只对 **tag push** 与 **PR** 触发（`.github/workflows/ci.yaml:8-13`），直接 push main 不会触发。

**操作序列**

```bash
git switch -c audit/2026-10-21
git push -u origin audit/2026-10-21
gh pr create --base main --head audit/2026-10-21 \
  --title "audit: 2026-10-21 修复（门禁/运行态/版本目录/文档）" \
  --body-file docs/audits/2026/2026-10-21-remediation-plan.md
```

**预期与预算**

| 检查 | 预期 |
|---|---|
| build ×3（ubuntu/windows/macos） | B0 修复后应全绿 |
| integration ×3 | 可能暴露平台断言问题；逐个修，不改期望 |
| `go test -race ./internal/...`（ubuntu） | 第一次真实并发检查；若有红，按 race 报告修 |
| release | 不触发（非 tag） |

Actions 免费额度有限，建议**只开一个 PR、一次性跑完**，不要反复 push 小提交。

**验收**：PR 页面所有 required checks 绿；把链接与结论回填 HANDOFF。

---

## 二、B1：运行态与用户可见问题

### 2.1 `xdev self install`：让运行副本可修复（P1-3）

**现状**：`InstallSelf` 已存在（`internal/installer/installer.go:2270`），`doctor --fix` 会调用它（`internal/cli/doctor.go:264`），但没有直接的 `self` 子命令；CLI 用户升级后 `current/xdev` 可能仍指向旧版。

**方案**：在 `internal/cli/extra.go:42` 的 `newSelfCmd()` 里新增子命令（该文件已 import `version`，需补 `internal/installer`）：

```go
&cobra.Command{
    Use:   "install",
    Short: "Install/repair the running xdev binary into install/xdev/<version> and switch current/xdev",
    RunE: func(cmd *cobra.Command, args []string) error {
        path, isNew, err := installer.InstallSelf(Cfg())
        if err != nil {
            return err
        }
        if isNew {
            fmt.Printf("installed xdev@%s -> %s\n", version.Get(), path)
        } else {
            fmt.Printf("xdev@%s already installed at %s\n", version.Get(), path)
        }
        return nil
    },
},
```

**关键使用约束**（写进 `--help` 与文档）：必须用**新版本二进制**执行；用旧 rc 执行只会把旧版再装一遍。

**测试**（`internal/cli/self_install_test.go`）

- 从临时目录复制测试二进制，调用命令 → `install/xdev/<version>` 存在，`current/xdev` 指向它。
- 连续执行两次 → 第二次 `isNew=false`，不重复复制。
- `version.Version` 与目录名一致。

**主机操作**

```powershell
E:\XDev\xdev.exe self install     # 用 0.4.0 根副本执行
where xdev                         # 必须命中 current\xdev
xdev version                       # 0.4.0
```

---

### 2.2 `doctor` 增加“运行副本一致性”检查（P1-3 的长期防线）

**新增检查**（`internal/cli/doctor.go` 检查列表）：

```text
running binary:
  exe        = <os.Executable()>
  version    = <version.Get()>
  install    = <home>/install/xdev/<version.Get()>/xdev[.exe]
  current    = <home>/current/xdev -> ...
  verdict    = OK | STALE (running copy is not the managed copy)
```

判定逻辑：

- `os.Executable()` 与 `current/xdev` 解析目标相同 → OK。
- 运行副本版本 ≠ `version.Get()`，或 `install/xdev/<version>` 不存在 → **STALE**，提示 `xdev self install`。
- `current/xdev` 目标不存在（悬空）→ 复用 Round 11 的 `ActiveVersion` 语义报错。

**验收**：在 `E:\XDev` 当前状态（PATH 指向 0.3.3-rc.2）下，新 doctor 必须报 STALE；执行 `self install` 后转 OK。

---

### 2.3 统一版本目录判据（P1-4）

**根因**：4 个消费点各自只判 `IsDir()`；已有 `isComparableVersion` 只服务 GUI 更新路径，且它要求“数字开头”，会误杀合法版本 `go1.22.10`。

**方案：单一事实来源**

新增 `internal/util/versiondir.go`：

```go
package util

// LooksLikeVersionDirName reports whether a directory name under
// install/<tool>/ should be treated as an installed version.
//
// It is a DISPLAY/ENUMERATION filter, not a security check:
//   - path safety is enforced by SafeName at write time;
//   - residue names (.stale-*, .tmp-*) are never versions;
//   - auxiliary dirs (gopath, export_templates, unsafe-version-blocked,
//     lld_is_a_generic_driver._) have no digit and are rejected;
//   - legitimate non-semver names such as "go1.22.10" contain a digit and pass.
func LooksLikeVersionDirName(name string) bool {
    if name == "" || IsResidueName(name) {
        return false
    }
    if _, err := SafeName(name); err != nil {
        return false
    }
    for _, r := range name {
        if r >= '0' && r <= '9' {
            return true
        }
    }
    return false
}

// ListVersionDirNames returns the version-looking child directories of
// install/<tool>/, sorted for stable UI output.
func ListVersionDirNames(toolDir string) ([]string, error) { ... }
```

**顺带消除重复**：把 `shim` 包的 `staleResidueMarkers` 移到 `internal/util`，导出 `util.IsResidueName`；`shim.IsStaleResidueName` 改为一行委托（`internal/shim` 已 import `internal/util`，不会产生环）。

**接入 4 个消费点**

| 位置 | 改法 |
|---|---|
| `internal/cli/install.go:227-246` | 用 `ListVersionDirNames`；**版本数为 0 的工具整行跳过**（修 `perl`） |
| `internal/gui/app.go:863-877` | 同上 |
| `internal/installer/installer.go:2103` `ListInstalledTools` | 用过滤后的版本列表判定“是否已安装” |
| `internal/installer/installer.go:2136` `findOtherVersion` | 只在过滤后的列表里选 `CompareSemver` 最大者 |
| `internal/gui/app_updates.go:125` | 保留 `isComparableVersion` 作为“是否可与远端比较”的更强判据，不动 |

**表驱动测试**（`internal/util/versiondir_test.go`）

| 输入 | 期望 |
|---|---|
| `1.2.2-alpha` | true |
| `go1.22.10` | true |
| `17`、`25` | true |
| `export_templates` | false |
| `gopath` | false |
| `unsafe-version-blocked` | false |
| `lld_is_a_generic_driver._` | false |
| `.stale-123` / `.tmp-abc` | false |
| `..` / `a\b` | false |

**主机验收**：`xdev list` 不再出现 `Godot export_templates`、`gopath`、`unsafe-version-blocked`、`lld_*` 与空 `perl`；`go1.22.10` 仍显示。

> 不在本批次删除这些目录：显示过滤是只读的；清理放到 2.5 的 `doctor --fix`。

---

### 2.4 `doctor` 输出一致性（P3-12）

1. **Catalog 来源**：`manifest.LoadCatalogCached` 若回退到内置 FS，doctor 应打印 `embedded manifests (53 tools)`，而不是外部 `<home>/manifests` 路径。
   - 实现：让加载函数返回 `source`（`embedded` / `dir:<path>`），doctor 直接展示。
2. **Shell RC 作用域**：检查时传入当前 `xmdevenv_dir`；若 profile 块指向另一个 home，标为 `⚠ global profile references <other-home>`，不要报 `✔`。
3. **未初始化 home**：所有 `dir:` 缺失项合并成一条 `home not initialized — run xdev init`，避免 6 行 ✘ 噪声。

---

## 三、B2：语义与安全

### 3.1 `InstallDir` fail-closed（P1-5）

**根因**：接口 `InstallDir(root, version string) string`（`internal/plugin/plugin.go:94`）没有错误返回通道，校验失败只能替换成 `unsafe-version-blocked` 继续。

**两阶段迁移（建议分两个提交）**

**阶段 1（止血，先做，风险低）**

在安装器入口做一次前置校验，确保**任何写路径**在创建目录前就失败：

```go
// internal/installer/installer.go
func (i *Installer) validateInstallTarget(p plugin.Plugin, ver string) error {
    if _, err := util.SafeName(p.Name()); err != nil {
        return fmt.Errorf("refusing install: unsafe tool name %q: %w", p.Name(), err)
    }
    if _, err := util.SafeName(ver); err != nil {
        return fmt.Errorf("refusing install: unsafe version %q: %w", ver, err)
    }
    return nil
}
```

调用点：`install` / `installByKind` / `installFromLocal` / 迁移入口（`app_migration`、`app_local_detect`）在第一次 `InstallDir` 之前。

**阶段 2（接口 error 化）**

```go
// plugin.go
InstallDir(root, version string) (string, error)
```

- 20 个生产调用点（`internal/installer` 6、`internal/gui` 8、`internal/env` 3、`internal/cli` 3）。
- 定义哨兵：`var ErrUnsafeInstallPath = errors.New("unsafe install path")`，用 `%w` 包装。
- `Generic` / `luaBasePlugin` / Java 插件 / 测试 fake 同步改。
- 对**只读展示**且无法返回 error 的调用点，用 `installDirOrEmpty()`（打印一次告警并返回 `""`），调用方跳过该行——绝不返回伪路径。

**验收**

- 表驱动：`version = "../evil"`、`"con"`、`""`、`"unsafe-version-blocked"` → 安装返回错误且**不创建任何目录**。
- 既有合法版本（`go1.22.10`、`1.2.2-alpha`）行为不变。

---

### 3.2 清理已产生的伪版本目录（P1-5 收尾）

`doctor --fix` 增加“已知垃圾目录”计划，**仅显式 `--fix` 且逐项打印**：

- 匹配：`unsafe-version-blocked`、`unsafe-name-blocked`、`unsafe-path-blocked`、`gopath`、`export_templates`、`lld_is_a_generic_driver._`。
- 动作：rename 到 `<home>/.trash/<timestamp>/<tool>/<name>`，**不直接 RemoveAll**（可恢复）。
- 安全：只处理 install/ 下**直接子目录**；不跟随 junction；只处理已知名字，不做通配。

---

### 3.3 Lua 内存看门狗的短期改良（P2-8）

**不改语义，只降低误判与提高可解释性**

1. **串行化计量**：给 `runWithContext` 加进程级 `luaMemMu sync.Mutex`（仅在启用内存监控时持有），保证同一时刻只有一个 Lua 调用在被计量；插件调用本身很短，代价可接受。
   - 这样 `HeapAlloc - base` 只会被**非 Lua goroutine**（下载、GUI、GC）污染，量级远小于 64 MiB。
2. **触发日志带上下文**：打印 `HeapAlloc`、`base`、`NumGoroutine`、插件名、耗时，便于判断是误杀还是真超限。
3. **文档定性**：在 `lua_engine.go` 与用户文档中明确“这是防失控的护栏，不是对抗恶意插件的安全边界”。

**长期（B4 / 独立 Spec）**：fork `vendor/github.com/yuin/gopher-lua`，在 `newLTable` / `newLString` / `newLFunction` 等分配点累加 `LState.allocBytes`，提供 `SetAllocLimit(bytes)`，超限时走现有 ctx 取消路径；并用“分配 N MiB 必须在 limit 处中止、合法插件不受影响”做红→绿。

---

### 3.4 shim 覆盖率伪象（P3-13）

**根因**：测试把 `os.Args` 改成 `-test.run=^$` 再让 `MaybeExec` 以测试二进制作子进程；子进程继承 stdout，打印 `testing: warning: no tests to run` 与 `coverage: 0.0%`，污染父进程的覆盖率汇总。

**方案**：给 `internal/shim` 测试加 `TestMain`，用环境变量短路子进程：

```go
func TestMain(m *testing.M) {
    if os.Getenv("XDEV_SHIM_CHILD") == "1" {
        os.Exit(0) // 子进程只作为“真实工具”的替身，不跑测试、不打印覆盖率
    }
    os.Exit(m.Run())
}
```

把 `shim_test.go:442`、`shim_alias_case_test.go:65,174` 的

```go
os.Args = []string{origArgs[0], "-test.run=^$"}
```

改为：

```go
t.Setenv("XDEV_SHIM_CHILD", "1")
os.Args = []string{origArgs[0]}
```

**验收**：`go test ./internal/shim -cover -count=1` 显示真实覆盖率（不再是 0.0%）；不再出现 `[no tests to run]`；shim 测试耗时下降。

---

### 3.5 residue 收口（P3-14/P3-15）

1. **删除死开关**：`sweepDisabled` 要么删除，要么接到 `XDEV_DISABLE_RESIDUE_SWEEP=1`（并在文档说明）。推荐删除。
2. **取消隐式路径推导**：`removeLinkEntry(path string)` 改为 `removeLinkEntry(path, xmdeHome string)`；5 个调用点都在 installer 内部，直接传 `i.cfg.Core.XMDevDir`。
3. **守卫**：新增 `TestRemoveLinkEntry_UsesExplicitHome`（源码级：函数体内不得出现 `filepath.Dir(filepath.Dir(`），以及一个行为用例：任意 `xmdeHome` 下归档路径都在 `current/` 之外。

---

## 四、B3：文档与流程

### 4.1 单二进制架构收口（P2-6）

1. **新增 ADR**：`docs/adr/0006-single-binary-distribution.md`（按 `.specify/templates/overrides/adr-template.md`）
   - 决策：单二进制，无 `-tags gui`，GUI 由 `main.go` 无参分支进程内启动。
   - 取代：Spec 030 / ADR-0002 中“独立 xdev-gui”的部分（ADR-0002 仍保留 Wails 选型结论）。
   - 后果：体积 +~3MB；每个平台单产物；shim 与 CLI 共用同一 exe。
2. **修订 `docs/architecture.md`**
   - `:10` 版本/日期改为 `0.4.0 / 2026-10`。
   - `:20` 删除“独立 xdev-gui / 默认不链接 Wails”，改为单二进制描述。
   - `:26` 删除“CLI/GUI/bindings 三变体”。
   - `:405` 改为 `build/bin/xdev[.exe]`。
3. **修订 `docs/installation-guide.md`**
   - 产物表改为 `xdev-<version>-<os>-<arch>[.exe|.tar.gz|.deb|...]`，删除所有 `xdev-gui-*`。
   - `:106` 删除“产出 xdev-gui 二进制”。
4. **修订 `docs/usage-manual.md:390,399`**：GUI 启动方式改为“无参数运行 `xdev`”或 `xdev gui`。
5. **守卫**：在 `scripts/docs/` 增加 `validate-asset-names.ps1`（或扩展现有 validate-links）：
   - 禁止 `docs/**`、`README.md` 出现 `xdev-gui-<version>` 形态；
   - 允许历史归档 `docs/history/**` 与 CHANGELOG 中的历史记录。

### 4.2 HANDOFF 结构重置（P2-7）

**一次性操作**：把第 183 行以后的历史移到 `.scratch/handoff-history/2026-10-21-pre-reset.md`，`HANDOFF.md` 只保留：

```markdown
# HANDOFF

## 当前状态（唯一真相）
- HEAD / 版本 / 日期
- 本轮完成项（≤10 条）
- 验证命令与结果（逐条）
- 未完成项（指向 Spec/issue）

## 恢复点
- `handoff-backup` 最近 3 个提交

## 历史索引（只加不改）
- 2026-10-21 Round 13 → .scratch/bugx-round13/report.md
- 2026-10-20 Round 12 → .scratch/bugx-round12/report.md
...
```

**规则**：每轮只更新“当前状态”，历史只追加一行索引，**永不复制标题**。

**顺带修正**：宿主基线统一写“1150 字符 / 2300 字节”，避免下一轮再误读。

### 4.3 审计证据归档（P2-10）

二选一（推荐 A）：

- **A. 本地留痕 + 正式归档**：`.gitignore` 增加 `.scratch/`；把每轮 `report.md` 复制到 `docs/audits/2026/<round>.md` 并提交；`docs/audits/README.md` 建索引。
- **B. 直接提交**：只提交 `.scratch/bugx-round*/report.md`（证据目录仍本地）。

理由：`.scratch` 按 `docs/agents/issue-tracker.md` 是本地 issue tracker，不应整体入库；但审计报告是长期资产，应进 `docs/audits`。

### 4.4 源码级守卫收敛（P2-9）

新增 `internal/testkit/sourceguard`（仅测试使用）：

```go
// FuncBody returns the source of a top-level function with comments and
// string literals stripped, skipping the guard file itself.
func FuncBody(t *testing.T, pkgDir, funcName string) string
```

规则：

- 新守卫**必须**用该 helper；禁止再手写 `strings.Index(src, "func X(")`。
- 现有守卫按“被触碰时迁移”处理，不搞一次性大改。
- 每个源码守卫注释必须写明：**它防的是哪一次回退**（现有报告已做到，保留）。
- 能用行为断言的（依赖注入/接口 seam）优先行为断言。

### 4.5 产物与二进制一致性（P2-11）

1. `build.ps1` 增加 `-Verify`：构建到 `build/bin/xdev-<version>.exe` → 执行 `version` → 校验输出含 `version.Get()` → 打印 SHA256。
2. `doctor` 的“运行副本一致性”（2.2）覆盖日常使用。
3. HANDOFF 每轮记录“本轮验证所用二进制的路径/SHA/构建时间”，杜绝“代码是新的、跑的是旧的”。

---

## 五、B4：高成本与环境项

| 项 | 方案 | 出口 |
|---|---|---|
| 本机 `-race` | 装 VS Build Tools（MSVC）或 MinGW ≥ 11；或直接用 CI ubuntu | 本机能跑 `CGO_ENABLED=1 go test -race ./internal/...`，或明确“以 CI 为准” |
| junction 长路径矩阵 | 在启用 `LongPathsEnabled` 的 Windows VM 上跑 `TestCreateJunction_Native_PathLengthMatrix` | 全矩阵不再 Skip，报告留档 |
| 桌面/TUI 冒烟 | 用 2.1 后的新二进制执行 `.scratch/036-second-round-audit/manual-smoke.md` 清单 | GUI 启动/安装/切换/direct PATH/批量取消 五项通过 |
| gopher-lua 配额 | 独立 Spec：fork + 分配计数 + `SetAllocLimit` | 红→绿：超限中止、合法插件无感 |

---

## 六、逐条问题 → 方案 → 验收对照

| 编号 | 问题 | 方案 | 守卫/验收 | 批次 |
|---|---|---|---|---|
| P1-1 | 非 Windows 测试编译失败 | 拆分/加 build tag | `GOOS=linux/darwin go vet ./...` | B0 |
| P1-2 | 69 提交未过 CI | 开 PR | 三矩阵 + race 全绿 | B0 |
| P1-3 | 运行副本落后 | `self install` + doctor 检查 | 主机 `xdev version`/doctor OK | B1 |
| P1-4 | 伪版本目录 | `util.LooksLikeVersionDirName` + 4 调用点 | 表驱动 + 真机 `xdev list` | B1 |
| P1-5 | `InstallDir` fail-open | 入口前置校验 + 接口 error 化 | 非法版本不建目录 | B2 |
| P2-6 | 文档与实现相反 | ADR-0006 + 三文档 | docs 守卫 | B3 |
| P2-7 | HANDOFF 腐化 | 单节 + 历史索引 | 人工评审 | B3 |
| P2-8 | Lua 内存启发式 | 串行化计量 + 文档；长期 fork | 并发误杀用例 | B2/B4 |
| P2-9 | 源码守卫脆弱 | `sourceguard` helper | 新守卫强制使用 | B3 |
| P2-10 | 证据未跟踪 | `.scratch` ignore + `docs/audits` 归档 | `git clean` 后报告仍在 | B3 |
| P2-11 | 产物滞后 | `build -Verify` + doctor | SHA/版本记录 | B3 |
| P3-12 | doctor 误导 | 返回 catalog source；RC 作用域 | 未初始化 home 输出 ≤2 行 | B1 |
| P3-13 | shim 覆盖率 0% | `TestMain` 子进程短路 | `-cover` 显示真实值 | B2 |
| P3-14 | `sweepDisabled` 死开关 | 删除或接线 | 源码不再有死字段 | B2 |
| P3-15 | 归档路径隐式推导 | `removeLinkEntry(path, home)` | 行为 + 源码守卫 | B2 |

---

## 七、建议提交序列

| # | 提交 | 内容 | 验证 |
|---|---|---|---|
| 1 | `B0: 修复非 Windows 测试编译 + 跨平台编译门` | 2 个测试文件 + script + CI step | 两条 `go vet` 跨平台命令 |
| 2 | `B1: xdev self install + doctor 运行副本一致性` | extra.go + doctor.go + 测试 | 主机 0.3.3-rc.2 → 0.4.0 |
| 3 | `B1: 版本目录统一判据 + xdev list 过滤` | util/versiondir.go + 4 调用点 + 表测试 | 真机 list 无伪版本 |
| 4 | `B2: InstallDir fail-closed 阶段 1（入口校验）` | installer 前置校验 + 测试 | 非法版本零目录 |
| 5 | `B2: InstallDir fail-closed 阶段 2（接口 error 化）` | plugin 接口 + 20 调用点 | 全量测试 |
| 6 | `B2: shim TestMain + residue 收口` | shim tests + removeLinkEntry 签名 | `-cover` + 全量 |
| 7 | `B3: ADR-0006 + 文档收口` | docs | docs 守卫 |
| 8 | `B3: HANDOFF 结构重置 + 审计归档` | HANDOFF/.gitignore/docs/audits | `git clean` 演练 |

> 每个提交单独可回滚；B2 阶段 2 若范围过大，可先只保留阶段 1，接口改造另立 Spec。

---

## 八、风险与取舍

| 风险 | 说明 | 缓解 |
|---|---|---|
| 接口改造波及 20 个调用点 | 编译期可发现，但 GUI 展示路径无法返回 error | 提供 `installDirOrEmpty`，跳过展示；写路径必须 error |
| Lua 串行化影响并发安装 | 插件调用通常毫秒级 | 先测量批量安装耗时；若明显回退，改为“仅单调用时监控” |
| 清理伪版本目录是破坏性操作 | 目录可能是用户数据 | 只 rename 到 `.trash`，仅 `doctor --fix` 显式触发，逐项打印 |
| 跨平台 CI 暴露更多历史问题 | 69 个提交从未在 Linux/macOS 跑过 | 预期第一轮会有红；按失败逐条修，不修改测试期望 |
| 文档守卫误伤历史记录 | CHANGELOG 里有 `xdev-gui-*` 历史 | 守卫范围限定 `docs/**`（排除 `docs/history`）与 README，CHANGELOG 豁免 |

---

## 九、完成定义（DoD）

- B0：跨平台 `go vet` 绿；PR 全部 required checks 绿；HANDOFF 记录 PR 链接。
- B1：主机 `xdev version` = 0.4.0；`xdev list` 无伪版本；doctor 能识别并修复旧副本。
- B2：非法版本安装返回错误且零副作用；`go test ./...` 与 `-race`（CI）全绿；shim 覆盖率可测。
- B3：文档中无过时 `xdev-gui-*` 产物描述；HANDOFF 只有一个“当前状态”；审计报告可从 `docs/audits` 追溯。
- B4：每项有结论（完成或环境证明），不留“待定”。

---

## 十、执行记录（2026-10-21 晚）

| 批次 | 状态 | 提交 | 实际完成 |
|---|---|---|---|
| B0 | ✅ | `bf2bcdb` | 2 个测试文件平台化；`scripts/ci/check-cross-compile.sh`；ubuntu job 增加 darwin `go vet`；`GOOS=linux/darwin go vet ./...` 均为 0 |
| B1 | ✅ | `076554f` | `xdev self install`；doctor running-copy / RC-home / catalog-source / 缺失目录合并；`util.LooksLikeVersionDirName` + `ListVersionDirNames` 接入 6 个消费点；主机 `current/xdev` 已从 0.3.3-rc.2 切到 0.4.0 |
| B2 | ✅ | `7f0f7e9` | 安装/激活/卸载/npm 入口 fail-closed；Lua 计量串行化 + 诊断日志；shim `TestMain`（覆盖率 0.0% → 82.4%）；`removeLinkEntry(path, xmdeHome)` + 删除死开关 |
| B2b | ✅ | `28a02b3` | 整包测试暴露：孤儿 Lua goroutine 污染进程级堆基线 → 新增 `luaActive` 归因守卫（有其它 Lua goroutine 时关闭监控）；plugin 测试用 TestMain 关闭默认监控、内存用例显式注入上限；R8-05 文档守卫排除 `docs/audits`/`docs/history` 快照 |
| B3 | ✅ | `dd591df` | ADR-0006 + ADR-0002 纠偏；architecture/installation/usage 修订；HANDOFF 单节重置；`docs/audits/2026/` 归档；`.gitignore` 覆盖 bugx/handoff-history；`build.ps1` 构建后自证版本 + SHA256 |
| B2 阶段 2 | ✅ | `f62bf7b` | `plugin.InstallDir` 改 `(string, error)` + `ErrUnsafeInstallPath`；删除 `unsafe-*-blocked` 回退；21 个生产调用点 / 8 个测试 fake / 21 个测试调用点全部迁移 |
| B1 遗留 | ✅ | 主机操作 | 全局 PowerShell profile 已备份（`.bak-20261022`）并改为 `E:\XDev` / `E:\XDev\shims`；doctor 的 shell rc 转为 ✔ |
| B4（doctor） | ✅ | `5247551` | manifest 汇总/重播种（旧文件备份 `.corrupt-<ts>`）；伪版本目录隔离到 `.trash`；`gopath`/`export_templates` 永不移动（实测 1.78GB / 2.07GB 完好） |
| Item 5 | ✅ | `16bcf0e` | 冒烟暴露并修复 CI/Release 缺 Wails `desktop,production` 标签（12 条命令 + 守卫）；GUI/TUI/CLI/direct PATH 冒烟通过 |
| Item 1 | ✅ | PR #1 | 推送 `audit/2026-10-22`（78 提交）并创建 https://github.com/MoF-Crack/xdev/pull/1 ，CI 已启动 |

**验证（B0–B3 最终态）**

```text
gofmt                               clean
go build ./...                      exit 0
go vet ./...                        exit 0
GOOS=linux  go vet ./...            exit 0
GOOS=darwin go vet ./...            exit 0
go test ./... -count=1              32 包全绿
go test ./internal/plugin -count=5  绿（看门狗归因稳定）
go test ./internal/shim -cover      82.4%
npx vitest run                      49 files / 305 tests（含 Wails 标签守卫）
npx vite build                      exit 0
```

---

## 十一、按 2→3→4→5→1 执行记录（2026-10-22）

| 顺序 | 项 | 结果 |
|---|---|---|
| 2 | 全局 PowerShell profile | 备份 `Microsoft.PowerShell_profile.ps1.bak-20261022`；`XMDEV_HOME=E:\XDev`、PATH 改为 `E:\XDev\shims`；doctor `shell rc injected` ✔ |
| 3 | `InstallDir` error 化 | `f62bf7b`；非法 tool/version/install_root 返回 `ErrUnsafeInstallPath`；全量测试绿 |
| 4 | doctor 增强 | `5247551`；主机 `cursor.yaml` 已从 embedded 重播种（旧文件 4162B 备份），`gradle/unsafe-version-blocked`、`llvm/lld_is_a_generic_driver._` 已移入 `.trash`；`manifest files` / `install junk dirs` 均 ✔ |
| 5 | 冒烟 | GUI：`wailsWindow` + WebView2 6 进程、正常关闭；TUI：主界面渲染；CLI：`version/list/current/which/use`；direct PATH 增删后逐字节还原；**发现并修复 CI/Release 缺 Wails 标签**（`16bcf0e`） |
| 1 | 推送 PR | `audit/2026-10-22` 已推送；PR https://github.com/MoF-Crack/xdev/pull/1 |
| 1b | 让 CI 真正绿 | 前 4 次全红 → 修复 5 类“只在 CI 现形”的缺陷，见 §9.4；新增提交 `197df78`、`abce3e8`、`bd4c7ed`、`f0a4342`。最终 run `39799617384`：build×3（含 ubuntu `-race`）、integration×3、governance 全绿；release 按策略跳过；PR mergeable |

**仍未覆盖 / 后续**

- release 三 job 未在 PR 中执行（tag 才触发）；已补 Linux 依赖、Windows `CGO_ENABLED=0`、darwin 改原生构建，需下次发版验证。
- GUI 深度交互（安装、批量取消）由 305 个前端用例覆盖，未做手工点击。
- 宿主 `doctor` 仍报 PATH shadow 冲突（`C:\Program Files\dotnet\` 等早于 shim 目录），属机器配置。
- 远端默认分支有 4 个 Dependabot 告警（1 high / 3 moderate），建议单独处理。

