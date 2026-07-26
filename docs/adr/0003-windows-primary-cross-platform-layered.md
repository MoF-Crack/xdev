# ADR-0003: Windows 主平台 + 跨平台分层验证

> **状态**：Accepted
> **日期**：2026-07-25

## 背景

XDev 用户主要在 Windows 上使用开发工具。Constitution 原则 V 要求"正式发布前
必须通过 Windows、Linux、macOS CI"，但 Windows 是主要测试和验证平台。

## 决策

采用 Windows 主平台 + 跨平台分层验证策略：
- Windows 特有功能（注册表 PATH 操作、HKCU\Environment 读写、进程隐藏）
  在 `win_path_windows.go` 中实现，非 Windows 在 `win_path_other.go` 中 stub
- GUI 进程辅助（`gui_proc_windows.go` vs `gui_proc_other.go`）通过 build tag 隔离
- CI 在三平台（ubuntu/windows/macos）并行验证
- 集成测试（`integration` build tag）覆盖三平台

## 后果

- 正面：Windows 功能完整，非 Windows 不受影响
- 正面：CI 三平台验证确保跨平台兼容性
- 负面：非 Windows 平台的部分功能为 stub（如注册表 PATH 操作）
- 负面：新增 Windows 特有功能需同步维护非 Windows stub

---

## Approval History

| approval_id | decision | date |
|---|---|---|
| 2026-07-26-adr-0003-001 | Accepted | 2026-07-26 |
