# Issue: shim 别名解析失败时无 PATH 透传（发现于 2026-09-08 会话）

Status: open
Type: bug
Severity: high（破坏用户系统上已有的同名工具）
Discovered: 2026-09-08，v0.4.0 发布会话实测

## 现象

用户系统上存在 xdev shims/git.exe（目录 E:\XDev\shims），但 git 并非通过
xdev 安装（未在 install/git/ 下）。此后所有 `git` 命令（PATH 上 shims 目录
优先）都命中 shim，shim 的 MaybeExec → Lookup 找不到 current/git/ →
直接报错退出：

    shim: shim alias "git" not installed (run `xdev install git` first)

——真实 Git for Windows（D:\Env\Tools\Git）明明在 PATH 上，却被 shim
遮蔽而完全不可用。gh CLI 等内部调 git 的工具也全部连带失效。

## 根因

shim.Lookup 失败后 MaybeExec 直接报错返回，没有「本 shim 对应的工具未由
xdev 管理时，透传到 PATH 上的真实可执行文件」的降级路径。

## 建议修复方向

1. Lookup 失败时，从 PATH 上剔除 shims 目录后重新解析别名；命中则
   exec 真实工具（透传）。
2. 或：shim 安装时校验「工具必须已由 xdev 管理」，未管理的工具不落 shim
   （git shim 为何存在？——历史安装/目录扫描写入的孤儿 shim，需要
   自愈清理）。
3. 顺带：EnsureSelfInstalled / Activate 之后应校验 shims/ 下没有指向
   未安装工具的孤儿 shim 并清理。

## 复现步骤

1. 在装有系统 Git 的机器上运行 xdev，使 shims/git.exe 存在
2. 新开 shell 执行 git --version
3. 预期：显示 Git 版本；实际：shim 报错

## 影响

用户日常工具（git/node/python 等任何在目录里有 shim 但未激活的）全部失效，
且报错信息误导（让用户去 install 一个系统已有的工具）。

## 实测记录（2026-09-08，0.4.0 发布会话）

- `go build ./...`（VCS stamping）调用 git → 命中孤儿 shim → "error obtaining VCS
  status: exit status 1"，三次重试全挂，必须把 D:\Env\Tools\Git\cmd 手工前置
  到 PATH 才能构建——同一缺陷连编译工具链都被破坏，实锤 P0。
- gh CLI 同样受影响（内部 git 检测 base repo 失败）。
- 临时绕过：将真实 Git 目录前置到 PATH（或删除孤儿 shim）。

## Comments

- 2026-09-18（BugX 宿主接入会话）状态更新：代码修复（bf69b85，Spec 038
  `shim_fallthrough.go` + 测试）已在仓库（build\bin 为 0.4.0），但**三重未闭环**：
  ① 本机部署面 `E:\XDev` 仍是 0.3.3-rc.2（2026-08-29 构建），运行中的 shims 无透传
  （实测：shim 调用失败仍直接终态报错，无 PATH 兜底）；
  ② GitHub v0.4.0 tag（e72a9e1）早于本修复，无任何发布版本携带；
  ③ 新发现的别名剥离大小写缺陷（`.scratch/shim-alias-case-bug/issue.md`）会**绕过透传**：
  alias 带大写 `.EXE` 进入 `LookupOnPath`，`shim_fallthrough.go` L181 以
  `alias+ToLower(ext)` 构建候选，`npm.EXE.exe` 等永远不命中真实文件。
  两缺陷需同时落地（组合矩阵见该票）；本票在部署面与发布面闭合前保持 open。
- 2026-09-18（实施会话）**部署面已闭环**：
  - 新二进制（0.4.0+bf69b85+大小写修复，hash 30AB3AE9F50E1ABB）已替换
    `E:\XDev\xdev.exe` 并刷新全部 100 个 shims 分发器条目（同一 inode）；
  - 机器冒烟矩阵全绿：
    - `E:\XDev\shims\git.exe --version` → **git version 2.55.0.windows.3 exit 0**
      （本票核心行为——孤儿 shim 不再遮蔽真实 Git，透传正路径命中）
    - `E:\XDev\shims\npm.EXE --version` → **11.19.0 exit 0**（shim-alias-case-bug 核心用例）
    - `E:\XDev\shims\node.EXE -v` → **v26.8.1 exit 0**
    - Python `shutil.which('npm')` → spawn → **11.19.0 exit 0**（bugx 生产事故链端到端）
    - bugx doctor Node 探针 **✗ → ✓**（"UI/TUI 环境就绪"）
  - 发布面（②）：待 v0.4.1 发布时自然闭环。

  Status: partially-resolved（代码+部署面已闭环；发布面待 v0.4.1 tag）


