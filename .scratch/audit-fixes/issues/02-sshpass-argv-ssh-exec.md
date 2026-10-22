# 02 sshpass argv 修复 + SSH Exec 错误传播（P0-01 + P1-32 同 PR）

Status: resolved
Type: task
批次: B1
对应修复项: P0-01（方案 §4 P0-01，含 P1-32 与 N-05 并入）
耦合: 本票内 P0-01 与 P1-32 不可拆（sshPassArgv 单一来源；分离实施极易复活 X-01）

## 问题

1. ssh_adapter.go:63/:146 构造 `sshpass -e SSHPASS ssh ...`——裸 "SSHPASS" 被当命令名，密码登录 100% 失败（sshpass 1.06/1.09/1.10 源码实证）。
2. Exec 密码路径（:126-129）硬编码 ExitCode=0 且丢弃 stderr。
3. 【N-05】Exec 无密码路径（:130-131）`result, _ := util.RunCommand(...); return &result, nil` 整段吞错——密钥登录远程失败对调用方表现为「无错误+空输出」。

## 方案

1. 抽取 `sshPassArgv(sshBin string, args []string) []string` 返回 `["-e", sshBin] + args`；runSSH 与 ConnectInteractive 复用；`cmd.Env` 注入 SSHPASS 不变。
2. Exec 密码路径：手动 `exec.Command` + `SSHPASS` env + **复用 sshPassArgv**；ExitCode 用 `util.ExitCode(err)`（exec.go:18）；stderr 收集返回。
3. Exec 无密码路径：`result, err := util.RunCommand(...)`，err 传播给调用方。

## 涉及文件

internal/orchestrator/ssh_adapter.go、internal/orchestrator/ssh_connect_test.go（或新增 sshpass_argv_test.go）

## 守卫测试

- `sshPassArgv("ssh", ["-p","22","u@h"])` == `["-e","ssh","-p","22","u@h"]`，全串不含裸 "SSHPASS"。
- Exec 双路径错误传播断言（mock ssh 失败 → err 非 nil、ExitCode 正确）。
- 现有 `TestConnectInteractive_PasswordNeverOnArgv` 继续通过（v1 误写 TestSSHConnect_RequiresSSHPass——该名不存在）。

## Comments（Batch 1 实施记录）

- 改动：`internal/orchestrator/ssh_adapter.go` — 新增 `sshPassArgv(sshBin, args) []string`（唯一出口，grep 实证无手抄字面量）；`runSSH` 签名改 `(stdout, stderr []byte, err error)`（手动双 Buffer，TestConnection 只用 stdout）；`ConnectInteractive` 经 sshPassArgv；`Exec` 密码路径返回真实 ExitCode（util.ExitCode）+ stderr 并保留 err 传播，无密码路径 `result, err := util.RunCommand(...)` err 不再丢弃（N-05）。
- 守卫测试：`internal/orchestrator/sshpass_argv_test.go` — TestSSHPassArgv_ExactOrderNoBareSSHPASS / TestSSHPassArgv_EmptyArgs / TestExec_PasswordPath_ExitCodeAndStderr（假 sshpass 注入 PATH：exit 7 + stderr "boom"）/ TestExec_KeyPath_ErrorPropagates（预取消 ctx 确定性注入，先红后绿）；既有 TestConnectInteractive_PasswordNeverOnArgv 继续 green。
- S6 手工项：本机未装 sshpass（无法做密码登录端到端实机验证）。Skip-on-sshpass-host 逻辑保留。遗留：需在装有 sshpass 的主机（如 Linux）上执行 `xdev remote connect` 密码路径验证 `echo ok` 往返（原 100% 失败路径）。
