# ADR-0005: hostsnapshot 套件级测试隔离

> **状态**：Accepted
> **日期**：2026-07-25

## 背景

Constitution 原则 II 要求"测试必须隔离宿主状态"。传统方案如 t.Cleanup 或
t.Setenv 只覆盖单测试函数。需要套件级检测来捕获跨测试的状态泄漏。

## 决策

在 `internal/testkit/hostsnapshot` 包中实现套件级宿主状态快照：
- `RunWithHostSnapshot(m, opts)` 包装 `testing.M` 的 `Run`
- before/after 捕获用户持久环境变量和文件路径
- `Diff` 检测变化，有变化则测试失败
- `GetUserEnvVarsBatch` 批量读取注册表（SC-004 优化：70%→2.1% 开销）
- 通过 `XDEV_HOST_SNAPSHOT=0` 可禁用（CI 不应设置此变量）

## 后果

- 正面：测试套件级别的宿主状态泄漏自动检测
- 正面：批量注册表读取将开销降至 2.1%（≤5% 阈值）
- 负面：import cycle 限制导致 env/util 包不能接入 TestMain
- 负面：增加约 2% 测试耗时（可接受）

---

## Approval History

| approval_id | decision | date |
|---|---|---|
| 2026-07-26-adr-0005-001 | Accepted | 2026-07-26 |
