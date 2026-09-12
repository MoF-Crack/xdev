# 07 SSH adapter Create 消费 spec

Status: resolved
Type: task
批次: B1
对应修复项: P0-10（方案 §4 P0-10）
依赖: 02（同文件域，先落 sshPassArgv）

## 问题

orchestrator.go:80-81 NewAdapter(AdapterSSH) 返回零值 SSHAdapter；ssh_adapter.go:86-102 Create 只调 TestConnection 用 s.target、不消费 spec.SSHTarget → GUI「测试连接能过（app_project.go:233-261 走 NewSSHAdapter）、创建必败（:194-215 target 被丢弃）」。Exec 腿（:294-298 每次新建零值 adapter）挂接 C-08/P1-35，不在本票。

## 方案

`SSHAdapter.Create` 开头：
```go
if spec != nil && spec.SSHTarget.Host != "" && s.target.Host == "" {
    s.target = spec.SSHTarget
    if err := validateSSHTarget(s.target); err != nil { return nil, err }
}
```
**实施要点**：复用 NewSSHAdapter 的 user/port 默认化（:31-42 三步链 ""→$USER→$USERNAME→root、port 0→22）——adapter 侧无兜底时零 user 的 spec 拼出 "@host"；默认化链与 GUI/CLI 预填并存问题一并决策（两套默认化二选一，死代码删除或保留需注明）。

## 守卫测试

`NewAdapter(AdapterSSH)` + `Create(ctx, &EnvironmentSpec{SSHTarget:{Host:"h"}})` 后断言 adapter.target 生效；零 user spec 默认化正确。

## Comments（Batch 1 实施记录）

- 改动：`internal/orchestrator/ssh_adapter.go` — NewSSHAdapter 的 user 三步链（""→$USER→$USERNAME→root）+ port 0→22 抽成 `normalizeTarget(t)`；Create 开头守卫式消费 spec：`spec != nil && spec.SSHTarget.Host != "" && s.target.Host == ""` 时取 `normalizeTarget(spec.SSHTarget)` → `validateSSHTarget`（校验失败不采用）→ 赋值；守卫保留确保 CLI 路径/GUI 预填不被覆盖（两套默认化不冲突：normalize 只填空值）。Exec 腿（每次新建零值 adapter）挂 C-08/P1-35 不在本票。
- 守卫测试：`internal/orchestrator/ssh_adapter_create_test.go` — TestSSHAdapterCreate_ConsumesSpecTarget（A1）/ TestSSHAdapterCreate_KeepsExistingTarget（A2：显式 target 不被 spec 覆盖）/ TestSSHAdapterCreate_DefaultsUserAndPort（A3：默认化链 + Port 22）/ TestSSHAdapterCreate_InvalidTargetRejected（A4：`..` host 拒绝且不被采用）/ TestNormalizeTarget_FillsOnlyEmpties（normalizeTarget 单元：只填空值）。A1-A3 用预取消 ctx 使 TestConnection 立即失败——断言的是 target 消费状态（发生在连接测试之前），无网络依赖。
