# Map — 029 architecture review fixes

## Notes

审查报告：`C:\Users\xinyan\AppData\Local\Temp\architecture-review-2026-08-20.html`

Phase A 落地。Phase B 全部落地：hostscan 全量扫描 + LookPath 走过前置 shim、InstallSpec Kind 分发、hardlink、App-as-adapter、AppContent session composable、one-api 工作副本删除。

## Decisions-so-far

- Phase A 五张票已落地（2026-08-20）：UpdatePackageManager 拒附属工具；localReady+立即扫描；detectLocalToolPath 注入 shimDir；shim size+mtime；architecture.md / query 注释
- HasUpdate 仅 detectCmd == toolName 的独立 PM（uv 等）；yarn/pnpm 不显示更新（启用走 InstallSubTool）
- Shim 本轮不改 hardlink（B4），只修 size 误判 → 后一轮 B4 已落地 CreateHardLink
- detectLocalToolPath 增加 shimDir 参数，删除 resolveShimDir
- Phase B deepening（2026-08-21）：hostscan.Scan 返回全部 PATH Hit；InstallSpec.Kind 收口 PM/SubTool；App 不增 Wails 绑定；CONTEXT.md 增加 Host-scan / InstallSpec / Desktop adapter
- LookPath 原先在 shim 排 PATH 最前时直接放弃 → 改为 Scan，走过 shim 取后续真实二进制
- B5：AppContent 拆 usePackageManagers / useSniffSession / useNotifications / useWindowChrome；one-api 工作副本删除（本就不跟踪）

## Fog

- AppContent 仍持有 env / isolate / remote / settings handler；再拆 page-level session 是后续 deepening，不是 B5 出口
