# WSL / Linux 编译与功能测试报告（2026-10-30，含第二轮修复）

- 目标：在 **WSL2 Ubuntu 26.04** 中真实编译 `xdev` 并做功能测试，找出并修复"Windows 上测不出来、Linux 上才暴露"的缺陷。
- 被测代码：`main` @ `921e35a`（`1.0.0-alpha.2`）+ 本轮改动（**未提交**）。
- 结论：Linux 侧可完整构建（CLI/TUI + GUI），全部单元/集成/竞态/功能测试通过；共确认 **18 项问题**（8 项产品缺陷 + 10 项 P3 改进项），**已全部修复并加回归测试**。

---

## 一、环境（可复现）

| 项 | 值 |
|---|---|
| 发行版 | Ubuntu 26.04.1 LTS (`resolute`)，WSL2，内核 `6.18.54.1-microsoft-standard-WSL2` |
| 资源 | 4 vCPU / 7 GB RAM / 97 GB 空闲 |
| 图形 | WSLg 可用（`DISPLAY=:0`、`WAYLAND_DISPLAY=wayland-0`） |
| Go | `go1.27.3 linux/amd64`（`~/.local/go`，来自 `mirrors.aliyun.com/golang`；**go.dev / proxy.golang.org / github.com 在 WSL 内不可达**） |
| 系统依赖 | `build-essential`(gcc 15.2.0) / `pkg-config` / `libgtk-3-dev` 3.24.52 / `libwebkit2gtk-4.1-dev` 2.52.6（apt 走 `mirrors.aliyun.com/ubuntu`，root 通过 `wsl -u root` 获得） |
| 源码 | Windows 工作区 rsync 到 WSL 原生盘 `~/xdev-src`（60 MB，避开 /mnt/d 的 9p 延迟） |

复现脚本全部在 `.scratch/wsl/`：`install-go.sh`、`install-apt-deps.sh`、`sync-src.sh`、`phase3-test.sh`、`phase4-fullbuild.sh`、`test-all-fixes.sh`、`race-and-probe.sh`、`integration-test.sh`、`deep-functional.sh`、`verify-new-fixes.sh`、`functest/run-functional.sh`。

---

## 二、编译结果

| 目标 | 命令 | 结果 |
|---|---|---|
| CLI/TUI（无 cgo） | `CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" .` | ✅ 15,270,048 B，ELF 64-bit **静态链接** |
| GUI+TUI+CLI（CI 等价） | `CGO_ENABLED=1 go build -tags desktop,production,webkit2_41 -ldflags ... .` | ✅ 17.4 MB，~22 s；动态链接 `libgtk-3.so.0` / `libwebkit2gtk-4.1.so.0` / `libsoup-3.0.so.0`；`ldd` 无缺失 |

要点：
- **必须带 `-tags desktop,production,webkit2_41`**：Ubuntu 24.04+ 只有 webkit2gtk-4.1，漏 `webkit2_41` 会去找 `webkit2gtk-4.0` 而失败。
- `frontend/dist` 必须先存在（`frontend/embed.go` 的 `//go:embed all:dist`）。
- WSLg 下 GUI 进程能进入事件循环（WebKit/EGL 打印 `DRI3`/`/dev/dri` 权限警告，属 WSLg 图形栈限制），未做像素级验证。

---

## 三、测试结果（全部在 Linux 实跑）

| 套件 | 命令 | 结果 |
|---|---|---|
| 全量单元/集成（CGO=0） | `go test ./... -count=1` | ✅ **33 包全绿**，0 FAIL |
| 全量单元/集成（CGO=1 + GTK/WebKit） | `go test ./... -count=1` | ✅ **33 包全绿**，0 FAIL |
| 竞态 | `go test -race ./internal/... -count=1` | ✅ **31 包全绿**，0 FAIL / 0 DATA RACE |
| integration 标签 | `go test -tags=integration ./... -count=1` | ✅ **34 包全绿**，0 FAIL |
| 跨平台编译门禁 | `scripts/ci/check-cross-compile.sh` | ✅ linux/amd64 + darwin/arm64 `go vet` OK |
| 功能矩阵（94 例） | `.scratch/wsl/functest/run-functional.sh` | ✅ **PASS 86 / FAIL 0 / SKIP 8**（SKIP=无 docker、非 TTY、被墙网络） |
| Windows 回归 | `go build ./...` / `go vet ./...` / 受影响 9 包测试 | ✅ 全绿（确认修复未破坏主平台） |

功能矩阵覆盖：命令面/退出码/错误路径、`init`（XMDEV_HOME / `--xmdevenv-dir` / XDG / `XMDEV_FORCE_HOME` 优先级）、config 读写与损坏恢复、profile 导入导出、plugin、pm、isolate、shell、并发 use/current、悬空 current 链接、uninstall 清理、真实 `$HOME` 零污染。

---

## 四、确认的缺陷与修复（18 项，全部已修）

### 第一轮（4 项，P1/P2）

| # | 级别 | 缺陷 | 修复 |
|---|---|---|---|
| L1 | **P1** | RC 末行**无换行**时 `activate`/`init` 把 marker 粘到上一行：`alias ll=ls# >>> xdev hook >>>`，`bash -n` 失败（**写坏用户 shell 配置**） | `internal/env/env.go` `InjectShellRC` / `internal/cli/hook.go` `injectHookRC`：追加前补分隔换行 |
| L2 | **P2** | `removeBlock` 只删**第一处**块且不吞块尾换行 → 重复 `activate` 每次 +1 字节、双块永不收敛、`deactivate` 留空行 | `env.go` / `hook.go` 的 `removeBlock`：循环删除全部块并吞掉块尾单个换行 → **字节级还原** |
| L3 | **P2** | `HOME` 缺失 + `XMDEV_HOME` 已设 → `filepath.Join("", ".bashrc")` 变相对路径，把 `.bashrc` 写进当前目录且报 `✔` | `env.go` `detectUnixShell`：HOME 缺失 → `RCFile=""` fail-closed |
| L4 | **P2** | `Installable` 用 `runtime.GOARCH`(`amd64`) 直比 manifest 的 `x86_64` → linux/amd64 仅 **1/54** 工具被判可安装（实为 48/54），doctor 误报 51 个 | `internal/manifest/manifest.go` `CanonicalArch` 归一化两边；`internal/plugin/generic.go` `HostArch` 复用 |

### 第二轮（14 项，含 1 项 P1）

| # | 级别 | 缺陷 | 修复 |
|---|---|---|---|
| L5 | **P1** | `xdev init` 只写 `~/.bashrc`，而 **`bash -lc`（WSL 默认、CI 脚本、ssh）不读 `.bashrc`** → shims 不在 PATH，工具"装了但用不了" | `ShellInfo` 新增 `LoginRCFile`：Linux bash 同时写 `~/.bashrc` 与登录 profile（`~/.bash_profile`/`~/.bash_login`/`~/.profile` 取存在的第一个）；`InjectShellRC`/`RemoveShellRC` 处理两个文件；`doctor` 新增 `login shell rc injected` 检查 |
| L6 | **P2** | cd-hook 每次 cd 无条件前插 PATH、无去重 → PATH 无界增长 | `env.HookOutput` 先按**当前进程 PATH** 过滤缺失目录（hook 子进程继承 shell 的 PATH）；init 块改用 `case ":$PATH:"` 守卫，重复 source 不增长 |
| L7 | **P2** | zip 解压不补执行位（FAT/Windows 创建者返回 0666）→ Linux 上解出的二进制 `EACCES`；内置 manifest 有 **40 处 linux+zip** | `extractor.Extract` 成功后调用 `ensureExecutableBits`：`bin/`、无扩展名、`.sh/.bash/.run/.bin/.appimage` 补 `0755` |
| L8 | **P2** | shim 分发到 644 文件时报 `permission denied`，而不是按 Spec 038 回退系统 PATH 同名工具 | `shim.FindInDir` 优先选可执行候选；`MaybeExec` 遇 `os.ErrPermission` 时回退 `LookupOnPath` |
| L9 | P3 | fish 用 POSIX 单引号转义，路径含 `'` 时写坏配置 | fish 分支统一用 `fishQuote`（RC 块 + hook 输出） |
| L10 | P3 | doctor 的 PATH-shadow 建议是 PowerShell 语法（Linux 无法照做）；Unix 也 `ToLower` 比较 | 按 `runtime.GOOS` 输出 `export PATH=...` 或 `$env:PATH`；新增 `normalizePathForCompare`（仅 Windows 折叠大小写） |
| L11 | P3 | `config.yaml` 0644（含 proxy 凭据） | `internal/config/config.go` 改 0600（Windows ACL 不受影响） |
| L12 | P3 | 安装目录包含检查 `ToLower+HasPrefix` → `/home/u/xdev-evil` 被当成 `/home/u/xdev` 子目录 | `installer.isWithinDir`：`filepath.Rel` + 路径分量边界，仅 Windows 折叠大小写 |
| L13 | P3 | 磁盘 shim 索引 TAB 分隔不转义（Linux 文件名可含 TAB/换行） | 索引升 v2：字段转义 `\\`/`\t`/`\n`/`\r`，读取兼容 v1 |
| L14 | P3 | `xdev init` 在 Linux 仍交互；`--yes` 只跳过 Windows 盘符选择，stdin 有内容会被误消费 | `init.go`：`--yes` 完全跳过语言向导与 RC 询问（自动注入） |
| L15 | P3 | Unix 无进程组：安装器取消只杀直接子进程，孙进程可能孤儿 | `util.TrackProcessGroup`（Setpgid + Cancel=kill(-pgid)），`RunCommand` 与 `osExecRunner` 接入；Windows no-op（Job Object） |
| L16 | P3 | `manifests/cursor.yaml` 把 Linux AppImage 标成 `format: exe`（走 7z 必失败）；7z 缺失时错误不可操作 | 新增 `format: appimage`（单文件落盘 + 0755）；`FindInDir` 支持单文件前缀匹配；7z 缺失错误改为给出 p7zip 安装命令 |
| L17 | P3 | help 说"无参数启动 TUI"，实际启动 GUI | `internal/cli/root.go` 文案改为 GUI（`xdev tui` 才是 TUI） |
| L18 | P3 | `self install` 不创建 `shims/xdev`，PATH 只有 shims 目录时 `xdev` 自身不可用 | `extra.go` `runSelfInstall` 安装后调用 `shim.InstallShim(shimDir, "xdev", <install>/xdev[.exe])` |

---

## 五、修复验证证据（Linux 实测）

| 项 | 证据 |
|---|---|
| L1 | `printf 'alias ll=ls' > ~/.bashrc` → `activate` → `bash -n` rc=0，首行 `alias ll=ls$`（无粘连） |
| L2 | 10 B RC → activate×3 → 393 B → deactivate → **10 B（精确还原）**；预置双块 → activate 后 1 块 → deactivate 后 0 块；`init` 连跑两次 148 B→148 B、块数 1 |
| L3 | `env -u HOME xdev activate bash` → rc=1，CWD 无 `.bashrc` |
| L4 | `HasInstallableVersion(linux,amd64)` **1/54 → 48/54**；`doctor` "Not installable" **51 → 4**（剩余 lua/make/mingw/php 确为 Windows-only） |
| L5 | init 后：`.bashrc` 1 块、`.profile` 1 块；`bash -lc` 中 `XMDEV_HOME` 有值、shim 计数=1；`bash -ic`=1；`bash -lic`=1（**无重复**）；doctor 两行均 ✔ |
| L6 | 项目目录内 `eval "$(xdev hook --shell bash)"` ×3：PATH 中 go 相关条目 1 次后=2、3 次后=2（不增长） |
| L7 | 单测：FAT 风格 zip（0666）解出 `bin/tool` 权限含执行位、`README.md` 保持非执行 |
| L8 | `current/go/bin/go` chmod 644 后运行 `shims/go version` → 输出 `go version go1.27.3`（回退系统 go），无 `permission denied` |
| L11 | `stat -c %a config.yaml` → **600** |
| L12 | 单测：`/home/u/xdev-evil`、`/home/u/xdev` 同级均判 false，子路径 true |
| L13 | 单测：含 TAB/换行/反斜杠的字段 encode/decode 往返一致；v1 旧索引仍可解析 |
| L14 | `printf '1\n' \| xdev init --yes` → 无 "Select a language" 提示 |
| L15 | 单测：Unix 下 `SysProcAttr.Setpgid=true` 且 `Cancel != nil` |
| L16 | 单测：`Extract(..., "appimage", ...)` 落盘且 0755；`FindInDir(dir,"cursor")` 命中 `Cursor-3.14.7-x86_64.AppImage` |
| L17 | `xdev --help` 文案已改为 GUI |
| L18 | `self install` 后 `shims/xdev` 符号链接存在，执行 `shims/xdev version` → `xdev 1.0.0-alpha.2` |
| 全量回归 | `go test ./...` CGO=0/1 各 33 包、`-race` 31 包、`-tags=integration` 34 包、功能矩阵 94 例（86 PASS/0 FAIL/8 SKIP）全部通过 |

---

## 六、不是缺陷（避免误判）

| 现象 | 判定 |
|---|---|
| 隔离 HOME 下 `doctor` 报 `✘ shim dir in PATH` 并 exit 1 | 正确行为：测试环境未把 shims 放 PATH；加入后 `All checks passed ✔`、rc=0 |
| `self install` 早先 `shims/` 为空 | 旧设计如此；L18 已改为创建 `shims/xdev` |
| 假 java 的 PATH 未被前插 | 正常：仅声明 `path_append` 的工具（bun/deno/go/rust）前插 PATH；java 用 env_vars + shim |
| 无参数 `xdev` 在无 TTY 下"挂住" | 实际是 **GUI 窗口**在事件循环（WSLg 有 DISPLAY）；help 文案已修正（L17） |
| `xdev tui` 在 `</dev/null` 下不退出 | 非 TTY 不是 TUI 支持场景；P3 观察项 |
| docker 相关 isolate 用例失败 | 环境无 docker，SKIP 正确 |
| `install <tool>` 网络用例失败 | WSL 内 github/go.dev 不可达（网络限制），非产品逻辑 |
| `list` 空态只打印表头 | 文案期望差异（functest 曾误判 PRODUCT），功能正确 |

---

## 七、复现指引（节选）

```bash
# 1) 环境
wsl -d Ubuntu -u root -- bash .scratch/wsl/install-apt-deps.sh
wsl -d Ubuntu        -- bash .scratch/wsl/install-go.sh
wsl -d Ubuntu        -- bash .scratch/wsl/sync-src.sh

# 2) 构建（CI 等价）
wsl -d Ubuntu -- bash .scratch/wsl/phase4-fullbuild.sh

# 3) 全量测试
wsl -d Ubuntu -- bash .scratch/wsl/test-all-fixes.sh        # CGO=0/1 两轮
wsl -d Ubuntu -- bash .scratch/wsl/race-and-probe.sh        # -race
wsl -d Ubuntu -- bash .scratch/wsl/integration-test.sh      # -tags=integration

# 4) 功能矩阵
wsl -d Ubuntu -- bash -lc 'BIN=$HOME/xdev-src/build/bin/xdev-linux-full bash /mnt/d/.../.scratch/wsl/functest/run-functional.sh'

# 5) 第二轮修复 E2E
wsl -d Ubuntu -- bash .scratch/wsl/verify-new-fixes.sh
```

---

## 八、本轮代码改动清单（未提交）

生产代码（17 个文件）：

```
M internal/env/env.go            # LoginRCFile / 双文件注入 / 守卫 PATH / fish 引号 / HOME fail-closed / removeBlock
M internal/env/hook.go           # HookOutput 去重 + fish env 引号
M internal/cli/hook.go           # 分隔换行 + removeBlock
M internal/cli/doctor.go         # 平台化建议文案 / Unix 大小写语义 / login shell rc 检查
M internal/cli/init.go           # --yes 完全非交互 / 打印双目标
M internal/cli/root.go           # 帮助文案（GUI）
M internal/cli/extra.go          # self install 创建 shims/xdev
M internal/manifest/manifest.go  # CanonicalArch
M internal/plugin/generic.go     # HostArch 复用 CanonicalArch
M internal/config/config.go      # config.yaml 0600
M internal/installer/installer.go# isWithinDir + TrackProcessGroup
M internal/extractor/extractor.go# ensureExecutableBits + appimage + 7z 错误文案
M internal/shim/shim.go          # 可执行候选/权限回退/索引 v2/单文件前缀
M internal/util/exec.go          # TrackProcessGroup
M internal/util/process_other.go # Setpgid + Cancel
M internal/util/process_windows.go # no-op
M manifests/cursor.yaml          # Linux AppImage -> format: appimage
```

新增/修改测试（12 个文件）：

```
+ internal/env/rc_inject_linux_regression_test.go
+ internal/env/rc_login_profile_test.go
+ internal/env/hook_dedup_test.go
+ internal/cli/hook_rc_linux_regression_test.go
+ internal/manifest/installable_arch_test.go
+ internal/extractor/exec_bits_test.go
+ internal/shim/find_exec_test.go
+ internal/installer/within_test.go
+ internal/config/config_perm_test.go
+ internal/util/process_group_test.go
M internal/env/hook_test.go          # 固定 PATH，避免 /usr/local/bin 被去重误伤
M internal/shim/shim_index_stale_test.go # 断言 v2 头
```

验证：`gofmt`（改动文件）clean；`go build ./...`、`go vet ./...`、`go test ./...`（CGO=0/1）、`-race ./internal/...`、`-tags=integration ./...` 全绿；`xdev-linux-full` 重新构建通过；Windows 侧同套构建/测试全绿。
