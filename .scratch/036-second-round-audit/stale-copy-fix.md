# 冒烟反馈 #5：老旧 xdev 副本未检测/替换修复方案

## 现状（实测）

E:\XDev 根：`xdev.exe`（1.2.2-alpha，2026-07-10，14.5MB）+ `xdev-gui.exe`（同日，17MB 孤儿）。

## 根因（双重）

1. **版本比较在版本号断层上方向反转**：`ReplaceStaleCLICopy` 用 `CompareSemver(probed, version.Version) >= 0` 判「不旧」。
   旧副本 1.2.2-alpha vs 当前 0.3.3-rc.2：1.2.2 > 0.3.3 → 判定「副本更新」→ 跳过替换。历史版本线（旧分支 1.x / 主线 0.3.x）断层使语义比较失效。
2. **孤儿无主**：`xdev-gui.exe` 是双二进制时代残留，单二进制模型（Spec 034+）后无任何代码路径认识它——既不替换也不清理。

另外覆盖面：`EnsureSelfInstalled` 仅 GUI startup 调用（app.go:535-541），CLI/TUI 不调。CLI 场景 home 副本仅作备用（PATH 上的才是调用入口），stale 无功能影响——不扩面，聚焦修对。

## 修复（A+B）

### A. stale 判定改「镜像一致性」语义

home 根副本的职责是「当前运行二进制的镜像」（EnsureSelfInstalled 注释本意）。判定改为：
```go
probed := probe(copyPath)
if probed == "" { return nil }              // 探测失败：仍跳过（防误伤无关 exe）
if probed == version.Version && hashSame(copyPath, currentExe) { return nil } // 已是镜像
// 其余（版本不同，或同版本不同构建如本地重编）→ 替换（前后 SHA-256 校验既有逻辑不变）
```
效果：1.2.2-alpha（版本不同）→ 替换 ✅；同版本同构建 → 跳过（探测 1s 子进程仍是最小成本）✅；同版本不同构建（本地重编）→ 跟随镜像 ✅。

### B. 孤儿 xdev-gui 清理

`EnsureSelfInstalled` 在 stale 检查后追加：`xdev-gui.exe` / `xdev-gui`（非 Windows）存在即删除（单二进制残留，无任何调用方）。删除失败仅 log 不阻塞。

## 验证

- 单测：`ReplaceStaleCLICopyWithProbe` 注入用例——probed="1.2.2-alpha"（>当前）且 hash 不同 → 替换（旧逻辑此用例红，新逻辑绿）；probed==当前且 hash 同 → 跳过；probe 失败 → 跳过。
- 孤儿清理单测：临时 home 放 xdev.exe + xdev-gui.exe → EnsureSelfInstalled 后 gui 副本消失。
- 真实验证：重编后跑一次 GUI（或调 EnsureSelfInstalled 的路径）→ E:\XDev\xdev.exe 变 0.3.3-rc.2、xdev-gui.exe 消失。

## 风险

- 「镜像语义」会覆盖用户故意放的旧版本副本——home 根副本从不是版本管理入口（install/ 目录才是，stale.go 头注释明确「install/ 一概不动」），该语义正确。
- 删除 xdev-gui.exe 属破坏性动作——仅删 homeDir 根这一个确定性文件名，且它已无调用方（PATH 查找按名字 xdev，`xdev gui` 子命令进程内启动）。
