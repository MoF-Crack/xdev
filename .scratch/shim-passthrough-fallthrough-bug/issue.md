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
