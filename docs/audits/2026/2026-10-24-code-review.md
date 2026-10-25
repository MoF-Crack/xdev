# XDev 代码审查报告（2026-10-24）

> **状态**：Snapshot
> **审查日期**：2026-10-24
> **代码基线**：`b8e981c1801e10084e53c820fcbd15b19032859a`
> **审查范围**：远程数据/下载信任边界、进程执行与路径操作、并发与共享状态、Wails 前后端边界
> **验证方式**：静态审查 + `go vet` + 相关包 `go test`（discovery/env/shim/downloader 全部 ok）
> **方法说明**：每条结论均回溯到 `文件:行号`；标注「不确定」的项未做结论

## 一、结论摘要

> **复核状态（2026-10-24 追加）**：下列 12 项已逐条复核。其中 **S-07 部分推翻**（devtools 那半为误报），其余 11 项均确认成立。复核方法与逐条解决方案见第八节。

| 编号 | 严重度 | 问题 | 位置 | 复核结论 |
|---|---|---|---|---|
| S-01 | 🔴 严重 | `SaveCache` 抢锁失败后仍写盘，两进程并发时整份快照互相覆盖 | `internal/discovery/scheduler.go:886-893` | ✅ **成立**（测试实证） |
| S-02 | 🔴 严重 | `UpdateShimDirInUserPath` 吞掉读/写错误并无条件广播，写失败仍告知成功 | `internal/env/win_path_windows.go:2091-2114` | ✅ **成立** |
| S-03 | 🔴 严重 | npm `.cmd` 启动器路径直入 `cmd /c`，路径含 `&` 会被二次解析 | `internal/installer/npm_install.go:105` | ✅ **成立**（Go 实测注入成功） |
| S-04 | 🟡 建议 | `WriteSystemPath` 无长度闸门，两处调用点漏检 32767 上限 | `internal/env/win_path_windows.go:1819`、`:1311` | ✅ **成立** |
| S-05 | 🟡 建议 | `SetEnvVar` machine 分支不持锁直写，与持锁路径竞态 | `internal/env/win_path_windows.go:1310-1315` | ✅ **成立** |
| S-06 | 🟡 建议 | `ExportProfile`/`ImportProfile` 接受任意绝对路径 | `internal/gui/app_profile.go:35-41`、`:143-159` | ✅ **成立** |
| S-07 | 🟡 建议 | Wails 无导航拦截（**devtools 那条为误报**） | `internal/gui/gui.go:54-85` | ⚠️ **部分成立** |
| S-08 | 🟡 建议 | shim 的 `defer release()` 在 panic 时清零 kill-on-close | `internal/shim/run_windows.go:32-33` | ✅ **成立**（测试实证） |
| S-09 | 🟡 建议 | `SwitchToLocalVersion` 缺「不在受管树内」检查 | `internal/gui/app_env_vars.go:790-795` | ✅ **成立** |
| S-10 | 🟡 建议 | 下载 URL 无 SSRF 私网拦截，与 `remote` 包基线不一致 | `internal/downloader/downloader.go:201-207` | ✅ **成立** |
| S-11 | 🟢 风格 | `_setxSetPath` 为死代码（0 调用） | `internal/env/win_path_windows.go:2463-2469` | ✅ **成立** |
| S-12 | 🟢 风格 | `maxWinRegValueLen` 局部常量 shadow 包级常量 | `internal/env/win_path_windows.go:1332` | ✅ **成立** |

**严重度修正**：S-03 由「建议」上调为**严重**——已实测证明注入可执行，且与 Round 9 已修复的 junction 属同一缺陷类。

**总体评价**：这是一个**防御密度显著高于平均水平**的代码库——提取路径穿越、下载续传一致性、GitHub 分页、错误脱敏、PATH 写入等多处实现严谨且注释充分。本次发现的问题集中在**错误处理遗漏**（S-01/S-02）与**校验不一致**（S-04/S-05/S-09/S-10），而非设计性漏洞。

---

## 二、🔴 严重

### S-01 `SaveCache` 抢锁失败仍写盘，导致并发丢写

**位置**：`internal/discovery/scheduler.go:886-893`

```go
// Acquire exclusive (write) lock — blocks up to 5 seconds.
lockPath := s.cachePath + ".lock"
lock, lockErr := util.AcquireFileLock(lockPath, 5*time.Second)
if lockErr == nil {
    defer lock.Release()
}                      // ← 失败时静默继续

if err := util.AtomicWriteFile(s.cachePath, data, 0o644); err != nil {
    return fmt.Errorf("write cache: %w", err)
}
```

**问题**：`lockErr != nil` 时没有任何返回或降级处理，代码照常执行 `AtomicWriteFile`。而同一文件的另一条写路径 `withCacheLock`（`scheduler.go:745-748`）明确写了「抢不到锁直接返回错误、绝不写盘」（第十轮 P2-1 修复）。

**后果**：`SaveCache` 是「整份快照覆盖」语义。两个 xdev 进程（GUI + CLI）同时保存时，后写者覆盖前写者，**已嗅探到的结果被静默丢弃**。这与同文件内另一条路径的既定策略矛盾，属回归缺口。

**修复建议**：与 `withCacheLock` 对齐——抢锁失败即返回错误，或降级为「只读不写」。

```go
lock, lockErr := util.AcquireFileLock(lockPath, 5*time.Second)
if lockErr != nil {
    return fmt.Errorf("acquire cache lock: %w", lockErr)
}
defer lock.Release()
```

---

### S-02 `UpdateShimDirInUserPath` 吞错并无条件广播

**位置**：`internal/env/win_path_windows.go:2091-2114`

```go
func updateShimDirInUserPathLocked(oldDir, newDir string) {
	current, err := ReadUserPath()
	if err != nil {
		return                       // ← 读失败静默返回
	}
	...
	if changed {
		newPath := strings.Join(parts, ";")
		WriteUserPath(newPath)        // ← 返回值被丢弃
		BroadcastEnvironmentChange()  // ← 无条件广播
	}
}
```

**问题**：这是 `win_path_windows.go` 中**唯一不检查写结果的 PATH mutator**。对比同文件其他 4 处（`SetWindowsUserPath:362`、`RemoveFromWindowsUserPath:393`、`AppendToUserPath:429`、`TransformUserPath`）全部是 `if err := WriteUserPath(...); err != nil { return ... }`。

**后果**：注册表写入失败（权限、值超长等）时，`BroadcastEnvironmentChange()` 仍会触发全局环境刷新，**用户被告知「已更新」但 PATH 实际是旧值**。迁移场景下 shim 目录指向失效，表现为「命令找得到却跑的是旧版本」。

**修复建议**：

```go
if changed {
    if err := WriteUserPath(newPath); err != nil {
        log.Printf("[env] update shim dir in user PATH failed: %v", err)
        return
    }
    BroadcastEnvironmentChange()
}
```

函数签名宜改为返回 `error` 并向上传递。

---

## 三、🟡 建议

### S-03 npm `.cmd` 启动器路径直入 `cmd /c`

**位置**：`internal/installer/npm_install.go:103-106`

```go
if runtime.GOOS == "windows" && (strings.HasSuffix(lower, ".cmd") || strings.HasSuffix(lower, ".bat")) {
    return "cmd", []string{"/c", tc.npmBin}, nil
}
```

**问题**：`tc.npmBin` 来自 `exec.LookPath("npm")` 或 `nodeToolchainFromDir`，是磁盘真实路径。`cmd /c <路径>` 会由 `cmd.exe` 二次解析，路径含 `&`、`%`、`^`、`|` 时被当作命令分隔符或变量展开。

**对比**：项目中 `junction_windows.go:20-26` 的注释明确记录了 Round 9 修复过**同一类**问题（`cmd /c mklink` + 含 `&` 的文件名），修法是「拒绝无法安全表达的字符」。此处**未应用同等防护**。

**缓解现状**：优先级 1 是 `node <npm-cli.js>`（`:99-101`），只有在找不到 `npm-cli.js` 时才走 `cmd /c`，触发面较窄；且需要攻击者能让 node 安装目录含特殊字符。

**修复建议**：对 `.cmd`/`.bat` 路径复用 Round 9 的字符闸（拒绝 `"` `%` `&` `|` `^` 及控制字符），或统一改用 `cmd /d /s /c ""path""` 的加引号形式。

---

### S-04 `WriteSystemPath` 缺长度闸门

**位置**：`internal/env/win_path_windows.go:1701-1744`（函数本体）、`:1819`（`cleanupSystemPathLocked`）、`:1311`（`SetEnvVar` machine 分支）

**问题**：全项目唯一的长度判据 `checkPathLength`（`:786-791`，上限 32767）只在 `writeUserPathFromRegistry:801`、`TransformUserPath:730`、`TransformSystemPath:765` 三处调用。`cleanupSystemPathLocked` 与 `SetEnvVar(machine)` 直调 `WriteSystemPath`，**跳过了检查**。

**后果**：系统 PATH 超过 32767 字符时被 `CreateProcess` 静默截断，表现为「某些目录在 PATH 里但程序找不到」。

**修复建议**：把长度检查下沉到 `WriteSystemPath` 内部（单一判据），而非依赖每个调用点自觉。

---

### S-05 `SetEnvVar` machine 分支不持锁直写

**位置**：`internal/env/win_path_windows.go:1310-1315`

**问题**：`SetEnvVar("Path", ..., "machine")` 直接调 `WriteSystemPath(value)`，**不经过 `withUserPathLock`**。而同文件所有 PATH 的「读-改-写」操作（含 `TransformSystemPath:751`，它操作的是**同一个系统 PATH**）都持该锁。

**后果**：一个走持锁的 RMW、一个走无锁直写，锁在此场景形同虚设，存在丢写窗口。

**修复建议**：`SetEnvVar` 的 machine + `Path` 分支纳入 `withUserPathLock`。

---

### S-06 `ExportProfile`/`ImportProfile` 接受任意绝对路径

**位置**：`internal/gui/app_profile.go:35-41`（导出）、`:143-159`（导入）

**问题**：这两个 Wails 绑定方法只校验 `.json` 后缀与父目录存在，**没有「必须落在用户选择的目录内」的约束**。

**后果**：若前端被攻破（当前 CSP 挡住了 XSS，见第四节），可构造「任意路径写小文件 + 任意 `.json` 读取并应用配置」的链路。

**缓解因素**（已核实）：导出内容不含机密；导入的 mirrors 逐项过校验（`:214-233`）；envVars 只接受 `level=="user"` 并过 `ValidateEnvVarName`；tools 必须 `registry.Get` 命中。因此**不是任意文件读写**，是受限的路径穿越。

**修复建议**：与 `DeleteCacheFile:26-32` 对齐，用 `filepath.Rel` 把路径限制在受管目录内；或改为由前端传目录、后端补齐固定文件名。

---

### S-07 Wails 窗口层无导航拦截

> **⚙️ 本节已修正（2026-10-24 复核）**：初稿称「未设 `DevToolsEnabled` → 默认开启」是**错的**。Wails v2.12.0 的 `options.App` **没有这个字段**，devtools 由 `devtools` 构建标签控制，生产构建默认关闭。详见 8.2 节。

**位置**：`internal/gui/gui.go:54-85`

**问题**：`wails.Run` 配置中：
- ~~未设 `DevToolsEnabled`~~（**误报，已剔除**）
- Wails v2.12.0 **不提供** `NavigationStarting` 拦截 API，应用无法阻止顶层导航
- `AssetServer` 只挂 `frontend.DistFS`，无自定义响应头（故也无 CSP **响应头**，仅靠 `index.html` 的 meta）

**后果**：若界面上任何位置渲染出可点击的 `https://` 链接，WebView 会在**应用窗口内**加载外部站点；该页面处于同一 WebView 上下文。

**不确定**：Wails v2 是否对 `window.go.gui.App.*` 做 origin 隔离——未在仓库内找到依据，**需查 Wails 版本文档确认**。若不隔离，则风险显著上升。

**修复建议**：Wails 无钩子，只能在应用层补：前端捕获外链点击 → 调后端 `OpenExternal(url)` → 后端 `IsSafeExternalURL` 校验后用系统浏览器打开。

---

### S-08 shim 的 `defer release()` 在 panic 时清除 kill-on-close

**位置**：`internal/shim/run_windows.go:32-33` + `internal/shim/job_windows.go:87-94`

```go
release, _ := jobKillOnClose()
defer release()          // ← panic 路径也会执行

if err := cmd.Run(); err != nil { ... }
```

**问题**：`release()` 的语义是「正常退出时清零 `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`，避免误杀 `gradle --daemon` 这类守护进程」。但它挂在 `defer` 上，**shim 自身 panic 时同样会清零**——而 panic 属于「异常终止」，恰恰是 Job Object 应当生效的场景。

**后果**：shim panic → 标志被清 → 孙进程不被 Job 清理 → 产生孤儿。这与该修复的目标（`job_windows.go:34` 「上层即可可靠停止服务」）相反。

**旁证**：`job_windows.go:55-57` 的注释自己也在纠结这点（"在子进程运行期间调用会……其实只会关掉标志"），说明语义未定稿。

**修复建议**：改为显式路径而非 defer——

```go
release, ok := jobKillOnClose()
if err := cmd.Run(); err != nil {
    if ok { /* 保留标志，让异常路径连坐 */ }
    ...
    return ...
}
release()   // 仅正常返回时清零
```

---

### S-09 `SwitchToLocalVersion` 路径校验不完整

**位置**：`internal/gui/app_env_vars.go:790-795`

**问题**：只要求「绝对路径 + 存在 + 是目录」，**不校验是否落在受管树外，也不拒绝系统敏感目录**。对比同类方法 `MigrateLocalTool`（`app_local_detect.go:355-367`）明确有「不在 XMDevDir 内」的检查。

**后果**：可把 `current/<tool>` junction 指向 `C:\Windows\System32` 等目录。后续 `LaunchTool` 会拼 `installPath/gui_binary` 执行——虽受 `safeLaunchBinary`（`app_launch.go:109-126`）约束必须落在该目录内，但防线已退到最后一层。

**修复建议**：补齐与 `MigrateLocalTool` 一致的四项校验。

---

### S-10 下载 URL 缺 SSRF 私网拦截

**位置**：`internal/downloader/downloader.go:201-207`

**问题**：`Download` 只强制 `parsedURL.Scheme == "https"`，**没有** `util.IsSafeExternalURL`（私网/保留地址拦截）。而 `remote` 包的 `discoverJSON`（`remote.go:1140`）与 `discoverHTML`（`remote.go:1241`）都调了该函数。

**后果**：恶意第三方 manifest 可让 xdev 对内网 https 地址发起 GET 并落盘。重定向目标受 `SecureRedirectCheck` 每跳校验覆盖，但**首个 URL 不校验**。

**修复建议**：在 `Download` 入口补 `util.IsSafeExternalURL`，与 remote 包基线对齐。

---

## 四、🟢 风格

### S-11 `_setxSetPath` 死代码

**位置**：`internal/env/win_path_windows.go:2463-2469`

已确认全仓库 0 调用。注释自称 "kept as documentation"。若被误启用，会同时带来三个问题：`cmd /c` 类字符二次解析、`setx` 的 1024 字节截断、以及绕过 `checkPathLength`。

**建议**：删除，把历史原因留在注释里即可。

### S-12 常量 shadow

**位置**：`internal/env/win_path_windows.go:1332`

```go
const maxWinRegValueLen = 32767   // 局部，shadow 了 :780 的包级同名常量
```

两处值相同，功能无误。但 `:782` 的注释明确要求「单一判据、共用同一常量」，此处违背了自身约定。

**建议**：删除局部声明。

---

## 五、经核实「已有防护」的高风险面（非缺陷，记录以免重复排查）

以下位置表面可疑，实测**防护完备**，后续审查可跳过：

| 面 | 位置 | 防护事实 |
|---|---|---|
| 提取路径穿越 | `internal/extractor/extractor.go:254-707` | 逐分量 `badComponent` + `Rel` 逃逸判定（用分隔符，不是裸 `HasPrefix("..")`）+ tar symlink 祖先检查 + 单文件 1GiB/总量 10GiB 上限 + 7z 事后走查 |
| git clone 注入 | `cli/manifest_cmd.go:337-363` | `validateGitCloneURL` 为**白名单**：只放行 https，拒绝所有 `::`、`ext::`、http/git/ssh/file |
| Junction 创建 | `util/junction_windows.go:45-105` | 已改原生 `FSCTL_SET_REPARSE_POINT`，**无任何子进程**；失败不回退 mklink |
| PATH 并发写 | `env/win_path_windows.go:238-289` | 13 个 mutator 全走 `withUserPathLock`，锁路径固定不依赖 config |
| `%VAR%` 展开 | `env/win_path_windows.go:1094-1164` | 白名单引用（仅 USERPROFILE/APPDATA 等 6 项），路径型变量**无条件拒绝** `%VAR%`，再锚定受管树 |
| 文件锁崩溃自愈 | `util/filelock_windows.go:52-67` | `LockFileEx`/`flock` 是内核锁，进程死亡自动释放，**无永久死锁** |
| 下载续传一致性 | `downloader.go:1039-1206` | `.part` 按 URL 分代 + `.part.lock` 独占 + `Content-Range` 起点/total 三重校验 + 206-on-fresh 拒绝 |
| 签名验证 | `signer/signer.go:108-118` | Ed25519，长度为 64 校验；**`build.ps1:37` 确实注入 `keys/pub.key`**（已实测该文件存在，44 字节） |
| 命令执行绑定 | `gui/app_project.go:409-459` | `IsolateExec` 白名单严格到**只允许 `--version`/`-v`/`--help`**，拒绝解释器与 shell 元字符 |
| 前端 XSS | `frontend/src` 全量 | `v-html`/`innerHTML`/`evil`/`new Function` **均 0 处**；全部走 `{{ }}` 转义 |
| CSP | `frontend/index.html:11` | `script-src 'self'`，**无 `unsafe-eval`、无 script 的 `unsafe-inline`**；无 CDN 外链 |

---

## 六、不确定项（未下结论）

1. **Wails v2 是否对绑定方法做 origin 隔离** —— 影响 S-07 严重度，仓库内无依据，需查 Wails 版本文档。
2. **`SetNPMRegistryBaseForTest` 是否有非测试生产调用方** —— 已 grep 未命中外部调用，但无法排除。
3. **npm registry 流式解析无 body 上限**（`remote.go:701`/`744-807`）—— 仅受 client 30s 总超时约束，是否构成实际 DoS 面未评估。
4. **7z 列表解析依赖外部 `7z l -slt` 文本格式**（`extractor.go:420-455`）—— 格式变化时静默空转，仅靠事后 `validateExtraction` 兜底；对已逃逸目录无能为力。

---

## 七、验证记录

```
go vet ./internal/discovery/ ./internal/env/ ./internal/shim/     → EXIT=0
go test ./internal/discovery/ ./internal/env/ ./internal/shim/ ./internal/downloader/
  ok  internal/discovery   17.590s
  ok  internal/env          3.774s
  ok  internal/shim        17.292s
  ok  internal/downloader  34.081s
```

现有测试全部通过，上述问题均为**当前测试未覆盖**的场景。

---

## 八、逐条复核与解决方案（2026-10-24 追加）

### 8.1 复核方法

不只读代码：能实证的都写了**临时测试/程序**跑了一遍，跑完已删除，仓库无残留。

| 编号 | 复核手段 | 结果 |
|---|---|---|
| S-01 | 写临时测试：先占住 `.lock`，再调 `SaveCache` | 返回 `nil`，文件内容与 mtime 均被改 → **成立** |
| S-02 | 枚举全文件 12 处 `WriteUserPath` 调用点 | 仅 L2111 不检返回值 → **成立** |
| S-03 | 写 Go 程序精确复现 `exec.Command("cmd","/c",path)` | marker 文件从 `ORIGINAL` 被改为 `INJECTED` → **成立** |
| S-04/S-05 | 读 `WriteSystemPath` 实现 + 枚举调用点 | 函数内无长度检查；L1311 无锁无长度 → **成立** |
| S-06 | 读导入/导出两个函数 | 仅 `.json` + 父目录存在 → **成立** |
| S-07 | 查 Wails v2.12.0 源码 | `DevToolsEnabled` 字段**不存在**，由 `devtools` 构建标签控制，生产默认关 → **devtools 那条推翻**；导航拦截（仅 `NavigationCompletedCallback`，无 `NavigationStarting`）**成立** |
| S-08 | 写临时测试复现 `defer release()` + panic | `jobHasKillOnClose` 返回 `false` → **成立** |
| S-09 | 对比 `MigrateLocalTool` 与 `SwitchToLocalVersion` | 后者缺 `ValidateAbsPathUnder` → **成立** |
| S-10 | grep `IsSafeExternalURL` | remote 2 处、downloader **0 处** → **成立** |
| S-11 | grep `_setxSetPath` | 仅定义、无调用 → **成立** |
| S-12 | grep `maxWinRegValueLen` | L780 包级、L1332 局部重复定义 → **成立** |

### 8.2 逐条解决方案

#### S-01 `SaveCache` 抢锁失败仍写盘

`internal/discovery/scheduler.go:886-893`，改为与 `withCacheLock` 对齐：

```go
lock, lockErr := util.AcquireFileLock(lockPath, 5*time.Second)
if lockErr != nil {
    return fmt.Errorf("acquire cache lock (refusing unlocked write): %w", lockErr)
}
defer lock.Release()
```

回归测试：预占 `.lock` → 断言 `SaveCache` 返回非 nil 且文件未变。

#### S-02 `UpdateShimDirInUserPath` 吞错

`internal/env/win_path_windows.go:2084-2114`，改为返回 `error`：

```go
func UpdateShimDirInUserPath(oldDir, newDir string) error {
    return withUserPathLock(func() error { return updateShimDirInUserPathLocked(oldDir, newDir) })
}

func updateShimDirInUserPathLocked(oldDir, newDir string) error {
    current, err := ReadUserPath()
    if err != nil { return fmt.Errorf("read user PATH: %w", err) }
    ...
    if changed {
        if err := WriteUserPath(newPath); err != nil { return err }
        BroadcastEnvironmentChange()
    }
    return nil
}
```

同时修改 `win_path_other.go:249` 的非 Windows 空实现签名，并在 `config.go:1106` 处根据返回值记日志/向上传递（迁移场景应告知用户 PATH 未更新）。

#### S-03 npm `.cmd` 路径二次解析（**最优先**）

`internal/installer/npm_install.go:103-106`。两个层次：

1. **直接消除 shell**（推荐）：对 `.cmd` 不使用 `cmd /c`，改为直接调 `node <npm-cli.js>`；若拿不到 `npm-cli.js`，则拒绝安装并报错（而非降级到 `cmd /c`）。
2. **若必须保留 `cmd /c`**：复用 Round 9 junction 的字符闸，对路径做拒绝式校验：

```go
if strings.ContainsAny(tc.npmBin, "\"%&|^<>\r\n") {
    return "", nil, fmt.Errorf("npm launcher path contains unsafe characters: %q", tc.npmBin)
}
```

并改用 `cmd /d /s /c "\"" + path + "\""` 的引号形式（`/s` 会剥掉最外层引号，与 Node 的 `spawn` 处理一致）。

回归测试：在含 `&` 的临时目录建 `npm.cmd`，断言不会执行注入命令。

#### S-04 `WriteSystemPath` 缺长度闸门

把检查下沉到函数内部（单一判据），而非依赖调用点：

```go
func WriteSystemPath(path string) error {
    if err := checkPathLength(path); err != nil { return err }
    ...
}
```

这样 `cleanupSystemPathLocked:1819` 与 `SetEnvVar:1311` 自动被覆盖；`TransformSystemPath:765` 的重复检查可保留（无害）或删除。

#### S-05 `SetEnvVar` machine 分支不持锁

`internal/env/win_path_windows.go:1310-1315`，与 user 分支对齐：

```go
case "machine":
    if err := withUserPathLock(func() error { return WriteSystemPath(value) }); err != nil {
        return err
    }
```

注：代码 L1287-1297 的注释**已经声称**此处“apply the 32,767-char registry limit and the cross-process RMW lock”，但实现两样都没有。修复后注释才与代码一致。

#### S-06 Profile 导入导出任意路径

`internal/gui/app_profile.go`。建议改为**前端传目录、后端拼固定文件名**，或加受管范围校验：

```go
func (a *App) ExportProfile(dir string) error {
    cfg := a.getCfg()
    // 限定在用户目录或 XMDevDir 下
    if util.ValidateAbsPathUnder(cfg.Core.XMDevDir, dir) != nil &&
       util.ValidateAbsPathUnder(homeDir, dir) != nil {
        return fmt.Errorf("profile export dir outside allowed roots: %s", dir)
    }
    filePath := filepath.Join(dir, "xdev-profile.json")
    ...
}
```

若产品上确实需要导出到任意位置，保留现状但应在文档中明确这是**有意的信任边界**（路径来自用户原生对话框选择）。

#### S-07 Wails 导航拦截（**部分问题**）

- ❌ **devtools 那条不成立**：Wails v2.12.0 的 `options.App` 没有 `DevToolsEnabled` 字段，devtools 由 `devtools` 构建标签控制；`build.ps1:73` 的 `wails build -ldflags $ldflags` 不带该标签，故生产构建 devtools **已关闭**。原报告此处为**误报，特此更正**。
- ✅ **导航拦截确实缺失**：Wails v2.12.0 只注册 `NavigationCompletedCallback`（事后），**没有 `NavigationStarting` 拦截 API**。因此应用无法阻止顶层导航。

**可行方案**（Wails 不提供钩子，只能在应用层补）：
1. 前端在 `App.vue` 全局捕获锚点点击，把外部 URL 交给后端用 `os/exec` 开系统浏览器；
2. 后端提供 `OpenExternal(url)` 绑定，内部先 `util.IsSafeExternalURL` 校验再调 `rundll32 url.dll,FileProtocolHandler`；
3. 若担心 WebView 被导航到外部页面后仍能调绑定，可在所有绑定方法入口加来源校验（需确认 Wails 是否暴露 origin，**此项仍未确定**）。

#### S-08 shim panic 清标志

`internal/shim/run_windows.go:32-33`，把 `defer` 改为显式路径：

```go
release, ok := jobKillOnClose()
runErr := cmd.Run()
if ok {
    if runErr == nil {
        release()          // 仅正常退出时清标志，避免误杀 daemon
    } else {
        // 子进程已退出但 shim 仍存活：保留 kill-on-close 不影响任何进程
        release()
    }
}
```

更稳妥的做法是把「清标志」限定在 `cmd.Run()` 正常返回的分支，**不在 `defer` 里做**——这样 panic 时标志保留，孙进程会被回收。注意：若业务上需要 daemon 存活，得区分「shim 主动退出」与「shim 崩溃」，panic 不应归为前者。

#### S-09 `SwitchToLocalVersion` 补受管树校验

`internal/gui/app_env_vars.go:793` 之后补一行，与 `MigrateLocalTool:365-367` 对齐：

```go
if cfg0 := a.getCfg(); cfg0 != nil && util.ValidateAbsPathUnder(cfg0.Core.XMDevDir, path) == nil {
    return fmt.Errorf("local path is inside xdev home, refusing to switch: %s", path)
}
```

另建议加系统敏感目录黑名单（`C:\Windows`、`C:\Windows\System32`、`Program Files`）作为纵深防御。

#### S-10 下载 URL 补 SSRF 校验

`internal/downloader/downloader.go:205` 之后：

```go
if err := util.IsSafeExternalURL(opts.URL); err != nil {
    return fmt.Errorf("unsafe download URL: %w", err)
}
```

注意：需确认 `IsSafeExternalURL` 对**已校验的官方 CDN 域名**不会误拒（它用 `net.LookupIP` 判私网，公网域名应通过）。建议加一个回归测试，用真实 manifest 中的 URL 跑一遍。

#### S-11 删除 `_setxSetPath`

`internal/env/win_path_windows.go:2460-2469` 整块删除。若想保留历史知识，把原因留在注释里即可。

#### S-12 删除局部常量

`internal/env/win_path_windows.go:1332` 删除该行，直接使用包级 `maxWinRegValueLen`。

### 8.3 建议修复顺序

| 批次 | 内容 | 理由 |
|---|---|---|
| **B1** | S-03、S-01 | S-03 已证实可执行注入；S-01 有现成测试可验证 |
| **B2** | S-02、S-04、S-05 | 同为 `win_path_windows.go`，一次改完避免重复回归 |
| **B3** | S-08、S-09、S-10 | 各自独立，风险低 |
| **B4** | S-06、S-07、S-11、S-12 | 风格/策略性，可与 B1-B3 并行 |

每批修完跑 `go vet` + 相关包 `go test`，并为 S-01/S-03/S-08 补上本轮验证用的回归测试（本轮测试为一次性探针，已删除）。

---

## 九、修复实施记录（2026-10-24 完成）

### 9.1 方案审查中发现的 4 处缺漏（已修正后再实施）

动手前先审了方案本身，发现 4 处会引入新问题：

| 原方案 | 问题 | 实际做法 |
|---|---|---|
| S-01 「改为返回错误」 | `SaveCache` **生产代码 0 调用**（全已改用 `withCacheLock`），加错误返回等于给死代码加固 | 仍改为拒锁写盘（防御未来误用）+ 注释标「新代码勿用」+ 双向回归测试 |
| S-02 「改为返回 error」 | 唯一调用方在 `migrateLegacyPathValue` 里，该函数签名 `func(string) string` **无法向上传递 error** | 保持签名不变，失败时记日志且**不广播** |
| S-06 「限制到受管目录」 | 前端是用户通过**原生对话框选任意位置**，文件名固定；限制会**破坏「导出到 D 盘备份」这类合法用法** | **不改代码**，记为有意的信任边界 |
| S-10 「直接加 `IsSafeExternalURL`」 | 它把 DNS 失败当硬错误，离线/内网场景会**拦死正常下载**；且会误伤**回环地址**（本机服务与 httptest 服务器都是合法下载源） | 新增 `validateDownloadURL`：放行回环 + DNS 失败放行，仅拦私有网段/链路本地 |

### 9.2 实际改动

| 编号 | 文件 | 改动 |
|---|---|---|
| S-01 | `internal/discovery/scheduler.go` | `SaveCache` 抢锁失败 → 返回错误，不写盘 |
| S-02 | `internal/env/win_path_windows.go` | 检查 `WriteUserPath` 返回值；失败不广播 + 记日志 |
| S-03 | `internal/installer/npm_install.go` | 新增 `cmdShimUnsafeChar`；`.cmd` 路径含 `&\|<>^%"` 时拒绝 |
| S-04 | `internal/env/win_path_windows.go` | `checkPathLength` 下沉到 `WriteSystemPath` 内部（单一判据） |
| S-05 | `internal/env/win_path_windows.go` | `SetEnvVar` machine 分支纳入 `withUserPathLock` |
| S-08 | `internal/shim/run_windows.go` | 去掉 `defer release()`；仅 `cmd.Run()` 返回后释放 |
| S-09 | `internal/gui/app_env_vars.go` | `SwitchToLocalVersion` 补 `ValidateAbsPathUnder` 受管树校验 |
| S-10 | `internal/downloader/downloader.go` | 新增 `validateDownloadURL` + `isPrivateNetworkIP` |
| S-11 | `internal/env/win_path_windows.go` | 删除死代码 `_setxSetPath`（及不再需要的 `os/exec` 导入） |
| S-12 | `internal/env/win_path_windows.go` | 删除局部常量 shadow，用包级常量 |
| S-06/S-07 | — | **未改代码**，理由见 9.1 / 8.2 |

### 9.3 新增回归测试

| 文件 | 覆盖 |
|---|---|
| `internal/discovery/savecache_lock_test.go` | 预占锁→必须拒写；无竞争→必须写入（双向） |
| `internal/downloader/ssrf_guard_test.go` | 8 个子用例：私网/链路本地拒、回环/localhost/不可解析域名放行、非 https 拒 |
| `internal/installer/npm_install_test.go` | `TestNPMRunnerRejectsShellMetaInCmdPath`（7 种元字符）+ `TestCmdShimUnsafeChar` |
| `internal/shim/job_reap_test.go` | 修正原有源码守卫的判据 |

### 9.4 修复过程中发现的两个额外问题

1. **S-10 首版修复导致 4 个既有测试红**：`signature_gate_test.go` 用 `httptest`（127.0.0.1）模拟下载服务器，被 SSRF 拦截误伤。**这就是方案审查时预警的「误伤回环」风险的真实发生** —— 说明预先审查有效。修正为放行回环后全部恢复。
2. **`internal/shim/job_reap_test.go:311` 的守卫判据本身有缺陷**：它断言源码里必须出现 `defer release()`，把实现细节固化成契约。而 S-08 的根因正是 `defer`。已改为语义判据：release 必须被调用且必须在 `cmd.Run()` 之后，并**反向禁止** `defer release()`。

### 9.5 验证结果

```
go build ./...   → EXIT=0
go vet ./...     → EXIT=0
go test ./...    → 零失败（含 cli/config/discovery/downloader/env/extractor/
                   gui/hostscan/installer/isolator/linkaudit/manifest/mirror/
                   orchestrator/plugin/remote/resolver/shim/signer/update/util 等）
```

### 9.6 未修复项（需产品决策，非遗漏）

| 编号 | 状态 | 原因 |
|---|---|---|
| S-06 | 不改 | 前端走原生目录选择对话框，限制路径会破坏合法用法；建议以文档明确信任边界 |
| S-07 | 未改 | Wails v2.12.0 无 `NavigationStarting` 钩子，修复需新增前端拦截 + 后端 `OpenExternal` 绑定，属功能改动而非缺陷修复 |
| S-04 部分 | 部分 | `TransformSystemPath:765` 的重复检查保留（无害），可在后续清理 |

---

## 十、修复后复审（2026-10-24 第二轮审查）

对上一轮全部修复逐处复审，并重点检查「新改动引入新缺陷」。

### 10.1 复审结果：9 项修复均正确

| 项 | 复审要点 | 结论 |
|---|---|---|
| S-01 | 抢锁失败返回错误、不写盘；无竞争时仍能写 | ✅ |
| S-02 | 检查返回值、失败不广播、仅记日志 | ✅ |
| S-03 | `npmBin` 的 Windows 候选只有 `npm.cmd`，**无绕过分支** | ✅ |
| S-04 | 闸门已下沉到 `WriteSystemPath` 入口，所有调用点自动覆盖 | ✅ |
| S-05 | machine 分支已纳入 `withUserPathLock` | ✅ |
| S-08 | panic 时标志保留；`cmd.Run()` 失败时也 release 是合理语义 | ✅ |
| S-09 | 受管树校验正确，且对 `XMDevDir==""` 做了防护 | ✅ |
| S-10 | 放行回环 + DNS 失败放行，避免误伤 | ✅ |
| S-12 | 局部常量已删，使用包级常量 | ✅ |

### 10.2 新发现缺陷 S-13（严重）：fallback URL 绕过 SSRF 校验

**位置**：`internal/downloader/downloader.go:229-237`

**问题**：S-10 只校验了主 URL（`:205`），而候选 URL 列表是「主 URL + FallbackURLs」，旧实现对 fallback **只检查了 scheme**：

```go
for _, fb := range opts.FallbackURLs {
    fbParsed, err := url.Parse(fb)
    if err != nil || fbParsed.Scheme != "https" { continue }  // ← 仅 scheme
    urls = append(urls, fb)
}
```

**为何是真实绕过**：主 URL 与 fallback 都来自 manifest。攻击者只需把**主 URL 设成公网地址**（通过校验）、把 **fallback 设成内网地址**，就能让 xdev 对内网发起请求——fallback 是真正会被发请求的候选之一（主 URL 404/403/410 时自动启用）。

**这是上一轮修复的自身缺口**：S-10 加了防护，但只堵住了其中一个入口。

**修复**：fallback 也走 `validateDownloadURL`，不安全的记 WARNING 并跳过：

```go
if verr := validateDownloadURL(fbParsed); verr != nil {
    log.Printf("[download] WARNING: skipping unsafe fallback URL %s: %v", sanitizedURL(fb), verr)
    continue
}
```

**回归测试**：`TestFallbackURLsAreSSRFChecked` —— 4 个内网 fallback 必须被拦（10.x / 192.168.x / 172.16.x / 169.254.169.254），2 个回环 fallback 必须放行。

### 10.3 复审方法学教训

本轮审查中，`git diff` 输出过长进入了交互式分页器，导致终端被阻塞约 30 分钟、无法执行验证命令。教训：在 `run_command` 中查看 diff 必须用 `--no-pager`，或直接读文件 —— **不要在自动化终端里使用任何可能进入交互式分页的命令**。

### 10.4 本轮验证

```
go build ./...  → EXIT=0
go vet ./...    → EXIT=0
go test ./...   → 零失败
```
