# Issue: shim 别名剥离大小写敏感——子进程以大写 `.EXE` 调用 shim 时必然报 "alias not installed"

Status: resolved
Type: bug
Severity: high（Python/子进程生态在本机无法解析 npm/node 等被管理工具）
Discovered: 2026-09-18，BugX 宿主接入（dsh）会话实测
Resolved: 2026-09-18，代码修复+守卫测试+部署冒烟全绿（见 plan.md §7 / Comments 实施记录）

## 现象

BugX CLI（Python）在本机执行 `bugx mcp-runtime install` 时，npm 阶段以 exit 1 失败：

    npm install 失败（exit 1）：shim: shim alias "npm.EXE" not installed (run `xdev install npm.EXE` first)

同一会话里交互式 `npm -v` 却正常返回 11.19.0。`bugx doctor` 的 Node 探针同样失败
（`shim alias "node.EXE" not installed`）。

## 根因（两端不一致）

1. **触发端**：Windows 上 Python `shutil.which("npm")` 按 PATHEXT（默认全大写
   `.COM;.EXE;…`）构造候选文件名，NTFS 大小写不敏感地命中磁盘上的 `npm.exe`，
   返回**大写扩展名**路径 `E:\XDev\shims\npm.EXE`。该路径作为 argv[0] 传给子进程。
   （实测 `py -c "import shutil; print(shutil.which('npm'))"` → `E:\XDev\shims\npm.EXE`；
   node 同理。任何走 `shutil.which`/等价 PATHEXT 展开的运行时都会触发。）
2. **缺陷端**：`internal/shim/shim.go` 的 `MaybeExec`（L64/L78）用**大小写敏感**的
   `strings.TrimSuffix(selfName, exeSuffix())` 剥离扩展名得到别名——`"npm.EXE"`
   不被剥成 `"npm"`，别名带着 `.EXE` 进 `Lookup`，索引里没有 `"npm.EXE"` → L135
   报错。而索引构建端 `addExecutables`（L384）对扩展名判断是 `ToLower` 后比较的
   ——**同一条别名链，构建端大小写不敏感、消费端敏感**，不一致即缺陷。
3. 与 bf69b85 PATH 透传修复的关系：透传以**别名**查 PATH（L85 `LookupOnPath(alias)`），
   大写未剥离时 `alias="npm.EXE"`，PATHEXT 式候选展开不会命中 PATH 上真实的
   `npm.cmd`/`npm.exe`（小写文件名）→ 仍保留原始错误。**透传修复不覆盖本缺陷**，
   必须单独修别名剥离。

## 复现步骤（byte 级）

1. `& 'E:\XDev\shims\npm.exe' --version`  → `11.19.0`，exit 0（对照组，小写正常分发）
2. `& 'E:\XDev\shims\npm.EXE' --version`  → `shim: shim alias "npm.EXE" not installed
   (run `xdev install npm.EXE` first)`，exit 1（与用户报错逐字节一致）
3. `py -c "import shutil; print(shutil.which('npm'))"` → `E:\XDev\shims\npm.EXE`
   （证明 Python 生态稳定触发大写路径）

## 影响

- 本机一切经 `shutil.which`（或等价 PATHEXT 大写展开）解析再 spawn 的程序
  （bugx 全链、其他 Python 工具链）都无法命中 xdev 管理的 npm/node/npx 等。
- 部署面放大：本机 `E:\XDev` 部署仍为 **0.3.3-rc.2**（2026-08-29 构建，连 bf69b85
  透传都没有；仓库 `build/bin` 已是 0.4.0 含透传版），修复需**改代码 + 重编 + 升级部署**。
- 误报误导：错误信息让用户去 `xdev install npm.EXE`（一个不存在的工具名）。

## 修复方向

1. `MaybeExec`/别名列的扩展名剥离改为大小写不敏感（与 `addExecutables` L384 的
   `ToLower` 语义对齐），一处工具函数统一两端。
2. 守卫测试（先红后绿）：
   - 别名矩阵单测：`npm.EXE` / `NPM.exe` / `npm.exe` / `NPM.EXE` 四种 selfName
     对同一 shim 的 `Lookup` 命中一致性；
   - 端到端：以大写扩展名路径 spawn shim，断言分发成功且 argv 原样透传；
   - 透传路径：未管理工具的大写 selfName + 孤儿 shim → 断言走 `LookupOnPath`
     透传而非报 alias not installed。
3. 部署升级：重编后把新二进制替换 `E:\XDev\shims\*.exe` 与 `E:\XDev\xdev.exe`
   （本机部署 0.3.3-rc.2 → ≥0.4.0+本修复；顺带带上 bf69b85 透传）。

## Comments

- 2026-09-18（BugX 宿主接入会话）：BugX 侧已有官方覆盖通道
  `BUGX_NPM_BIN` / `BUGX_NODE_BIN`（`.bugx/core/mcp_runtime.py` 的
  `_npm_executable`/`find_node` 优先读取），实测可绕过本缺陷完成 npm 阶段；
  但 `setup_ui_env.py`（doctor 的 UI/Node 探针）不认覆盖变量、直接
  `shutil.which("node")`，探针转绿只能靠本票根治。详见
  `.scratch/bugx-host-dsh/spec.md`。
- 2026-09-18（补充，全包扫描）：修复范围收敛于 `shim.go` L64/L78 两处 TrimSuffix。
  下游 L156/160/184/188 的 `alias+exeSuffix()` 路径拼装与 `shim_fallthrough.go`
  L181 的透传候选构建均为**继承**该错误 alias 的连锁点，非独立缺陷——上游归一化后
  自动恢复。透传绕过机制已从推断升级为代码级证实（`shim_fallthrough.go` L181：
  `cands = append(cands, filepath.Join(dir, alias+strings.ToLower(ext)))`，
  alias=`npm.EXE` 时构建出 `npm.EXE.exe` 等无效候选）。

## Comments

- 2026-09-18（实施完毕，红→绿证据）：
  **红**（修复前 `go test ./internal/shim/ -run '...' -v`）：
  - G1 `TestMaybeExec_AliasCaseMatrixDispatches`：`variant "fakepy.EXE": dispatch failed: shim alias "fakepy.EXE" not installed (run 'xdev install fakepy.EXE' first)`——三个大写变体全红，错误信息与生产事故逐字节一致
  - G3 `TestLookup_FoldsCrossToolAliasCase`：`shim alias "NPM" not installed`
  - G4 `TestShimIndex_FoldsUppercaseDiskNames`：`reverse index must fold key to "npm", got keys like [NPM]` + `Lookup(npm) ... not installed`
  - G5 `TestMaybeExec_PassthroughUppercaseSelfName`：`shim alias "mytool.EXE" not installed`（.cmd 夹具，复现 npm.EXE→npm.cmd 生产形态）

  **绿**（修复后同命令）：
  - G1 四变体（fakepy.exe / fakepy.EXE / FAKEPY.EXE / FakePy.Exe）端到端分发全绿（4 个子进程 PASS）
  - G3/G4/G5 全绿；G2 helper 表测绿；G6 Unix 精确门（Windows skip，Linux CI 运行）

  **全门**：gofmt ✓ · `go vet ./...` ✓ · `GOOS=linux go build ./internal/shim/ ./internal/gui/` ✓ ·
  `go test ./... -short -count=1` 全仓 28 包全绿 · `go test ./internal/shim/ -count=5` 全绿

  **缺陷④同时落地**：`internal/shim/refresh.go::RefreshDispatchers`（幂等、只刷现存条目、
  不碰非分发器文件）+ `internal/gui/app.go::refreshShimDispatchers`（EnsureSelfInstalled 两分支
  钩入）+ 4 个守卫测试全绿（`refresh_test.go`）。暴露既有 `TestDefaultExcludedDirs` 的
  sync.Once 隔离弱点（新文件按字母序先跑后消费缓存），补 `config.ResetHomeDirForTest()` 修复。

  Status: resolved（代码修复+守卫测试全绿；部署闭环见 plan.md §3）
