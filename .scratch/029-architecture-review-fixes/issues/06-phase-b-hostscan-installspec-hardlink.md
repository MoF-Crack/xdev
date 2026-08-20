# 06 Phase B hostscan / InstallSpec / hardlink

Status: resolved
Type: task

## Answer

- B1: `internal/hostscan` `Query`/`Hit`/`Scan`/`ScanPATH` 一次扫描 PATH 上全部实例，跳过 shimDir；GUI `collectPMInstances` 委托它（D: leftover + E: xdev 同时出现）。
- B2: App 仍是 Wails adapter（不增绑定）；实现按 catalog / install-session / host-scan / prefs 分组；`CONTEXT.md` 记下术语。
- B3: `InstallSpec.Kind` = tool / package-manager / sub-tool；`InstallPackageManager` / `InstallSubTool` 转发 `App.install`。
- B4: Windows CreateHardLink，失败 copy。
- B5: AppContent session composable + 删除 one-api 工作副本。
