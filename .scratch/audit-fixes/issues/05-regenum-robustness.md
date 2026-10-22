# 05 RegEnumValueW 健壮性（含 EnumEnvVars 同族）

Status: resolved
Type: task
批次: B1
对应修复项: P0-07 + N-10（方案 §4 P0-07）

## 问题

1. win_path_windows.go:1370-1394 GetAllUserEnvVars：nameBuf 固定 256；ERROR_MORE_DATA 时 nameBufSize 被改写 >256 直接 `nameBuf[:nameBufSize]` 越界 panic（阈值 ≥257 字符值名；恰 256 为截断错名）；其它非零码静默 break。每次 Windows shim 执行热路径。
2. 【N-10】EnumEnvVars（:920-935）同款：值数据 >64KB 时 ERROR_MORE_DATA 被吞，连重试都没有。

## 方案

1. ERROR_MORE_DATA 分支：**先把 nameBufSize 重置为新 buf 长度**（lpcchValueName 为 in/out，沿用 API 写回值会系统性二次失败）→ 按 required+1（上限 16384）重新分配 → 重试同一 index 一次；二次失败 log + index++ 跳过。
2. 其它非零码 break 前 log.Printf。
3. EnumEnvVars 按同一模式修复（buf 重试 + 日志）。

## 守卫测试

单测难以注入真实注册表异常值名——代码评审 + 手工 `reg add`（≥257 字符名 + >64KB 值）复现步骤记录；EnumEnvVars 可用超大 REG_SZ 注入测试。

## Comments（Batch 1 实施记录）

- 改动：`internal/env/win_path_windows.go` — GetAllUserEnvVars 的 ERROR_MORE_DATA 分支改为几何增长重试（512→…→16384，上限 16383+1 字符）；其它非零码 break 前 log.Printf WARN（热路径可诊断）。EnumEnvVars（N-10）同族修复：修字节尺寸 bug（原传 len(buf) 而非 len(buf)*2）、MORE_DATA 时按 lpcbData 回写值重分配重试一次 + 日志。
- **实证发现（推翻方案内假设）**：RegEnumValueW 对**名字**超长返回 ERROR_MORE_DATA 时**不回写所需长度**（lpcchValueName 保持输入值——实测 ret=234 且 nameBufSize 仍为 256），因此「按 API 写回值重分配」会系统性二次失败；名字缓冲必须几何增长。值数据侧（lpcbData）的回写语义正常（R3 实测 70KB 值按回写值重试成功）。
- 守卫测试：`internal/env/regenum_test.go` — TestGetAllUserEnvVars_LongValueName_NoPanic（R1：300 字符名，真实 HKCU 写入-断言-清理，原实现必 panic）/ TestGetAllUserEnvVars_Exactly256_TruncatedNotPanic（R2：恰 256 字符名完整返回）/ TestEnumEnvVars_LargeValueData（R3：70KB REG_SZ 完整返回 + 枚举不中断）。
- R4 手工复现步骤（已被 R1-R3 真实注册表单测超覆盖，实机核验可选）：`reg add HKCU\Environment /v XDEVTEST...<300字符名> /t REG_SZ /d longname-value /f` 后任意 shim 调用不 panic；`reg add HKCU\Environment /v XDEVTESTBIG /t REG_SZ /d <70KB串> /f` 后 GUI 环境变量页枚举完整。
