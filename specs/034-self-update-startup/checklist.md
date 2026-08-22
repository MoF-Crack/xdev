# Spec 034 Checklist

> **状态**：✅ 完成（Phase A-E 全部收敛，LOOP 出口，2026-08-22）
> **前置**：Spec 034（Approved）+ Task 清单
> **日期**：2026-08-22

## 一、实施前基线（现状证据，先行核对）

- [ ] G4 确认 `core.self_update` 无消费者：`grep -r "SelfUpdate" internal/` 命中仅 config 定义 + cli set/get（无检查逻辑读取）
- [ ] G5 确认 GUI `startup()`（app.go:479）/ `domReady`（:511）无 update check；CLI `cobra.OnInitialize`（root.go:109）仅有 initContext + sniff scheduler
- [ ] G7 确认 `EnsureSelfInstalled`（app.go:844-932）只处理缺失副本，无版本比对
- [ ] G2/G3 手动入口复盘点：`xdev self update`（extra.go:47-73）、GUI `CheckForUpdate` 绑定 + 前端 checkUpdate（AppContent.vue:1060）

## 二、测试用例设计（TC-S 系列）

### 2.1 配置（TC-S1，`internal/config` + `internal/cli`）

| 编号 | 用例 | 输入 | 预期 |
|---|---|---|---|
| TC-S1-1 | 默认值 | 全新 config 加载 | `self_update=true`、`self_update_mode="prompt"`、`self_update_stale=true` |
| TC-S1-2 | roundtrip | set mode=auto + set stale=false → Load | 两字段持久化回读一致 |
| TC-S1-3 | 非法枚举拒绝 | `xdev config set core.self_update_mode bogus` | 报错，不写盘 |
| TC-S1-4 | 旧文件回填 | 既有 config.yaml 缺新字段 | Load 后回填默认值，Save 不丢失旧字段 |

### 2.2 决策层（TC-S2，`internal/update/policy_test.go`，注入 fake Check/Now）

| 编号 | 用例 | 条件 | 预期 Action |
|---|---|---|---|
| TC-S2-1 | 开关关闭 | self_update=false，即使有更新 | Skip（无网络调用——fake 计数为 0） |
| TC-S2-2 | 未到期限频 | 上次检查 <24h | Skip |
| TC-S2-3 | 已到期检查 | >24h 且无更新 | Skip |
| TC-S2-4 | prompt × 有更新 | mode=prompt、HasUpdate、未忽略 | Ask |
| TC-S2-5 | auto × 有更新 | mode=auto、HasUpdate | Update |
| TC-S2-6 | notify × 有更新 | mode=notify、HasUpdate | Notify |
| TC-S2-7 | 检查失败 | fake Check 返回 err | Skip + 记录 last_check（不阻塞） |
| TC-S2-8 | 已忽略版本 | ignore.json 版本 == Latest | Skip（不打扰） |
| TC-S2-9 | 新版本解除忽略 | ignore.json 版本 < Latest | Ask（重新提示） |

### 2.3 旧版本副本替换（TC-S5，`internal/update/stale_test.go`）

| 编号 | 用例 | 构造 | 预期 |
|---|---|---|---|
| TC-S5-1 | 旧副本被替换 | homeDir/xdev.exe 为旧版 stub（`self version` 输出旧版） | 替换后 hash == 当前 exe hash |
| TC-S5-2 | 同版本跳过 | stub 版本 == 当前 | 文件不变（mtime/hash 未动） |
| TC-S5-3 | 更新版本跳过 | stub 版本 > 当前 | 文件不变 |
| TC-S5-4 | exec 失败跳过 | stub 不可执行/超时 | 返回 nil、文件不变 |
| TC-S5-5 | 自指跳过 | 副本路径 == currentExe | 不动 |
| TC-S5-6 | 关闭开关 | stale=false 参数/配置 | 不动 |
| TC-S5-7 | 替换后校验 | 强制校验 | hash 匹配否则报错 |

### 2.4 CLI 交互（TC-S4，`internal/cli`）

| 编号 | 用例 | 场景 | 预期 |
|---|---|---|---|
| TC-S4-1 | TTY 选 U | stdin="U"、Ask、fake Apply | 执行更新流程、exit 0 |
| TC-S4-2 | TTY 选 I | stdin="I"、Ask | 写忽略记录、继续（exit 0） |
| TC-S4-3 | TTY 选 Q | stdin="Q"、Ask | os.Exit(0)（提前退出） |
| TC-S4-4 | 非 TTY 降级 | 非交互、Ask、mode=prompt | 打印一行提示、不阻塞、exit 0 |
| TC-S4-5 | auto 非 TTY | mode=auto | 自动更新（fake）、exit 0 |
| TC-S4-6 | 挂载点守卫 | 子命令 list/install/use/which 与 shim 形态 | 无检查调用（守卫：`runSelfCheck(` 仅 3 个交互入口） |

### 2.5 GUI 前端（TC-S3，vitest `SelfUpdateDialog.spec.js`）

| 编号 | 用例 | 操作 | 预期 |
|---|---|---|---|
| TC-S3-1 | 弹出 | 收到 self-update-prompt 事件 | 显示 v{current}→v{latest} 三按钮 |
| TC-S3-2 | 更新 | 点「更新」 | emit update → 调 ApplySelfUpdate |
| TC-S3-3 | 忽略 | 点「忽略并继续」 | emit ignore → 写忽略 + 关闭 |
| TC-S3-4 | 退出 | 点「退出」 | emit quit → 触发关闭 |
| TC-S3-5 | notify 提示 | ActionNotify | info 提示、无对话框 |
| TC-S3-6 | auto 静默 | ActionUpdate | 无对话框（成功后 message） |

### 2.6 集成（TC-S6）

| 编号 | 用例 | 说明 |
|---|---|---|
| TC-S6-1 | GUI 启动链路 | startup→2s goroutine→CheckPolicy→（fake Check）ActionAsk→事件→对话框 |
| TC-S6-2 | EnsureSelfInstalled stale 集成 | 预置旧版 stub → domReady 后副本被替换（调 ReplaceStaleCLICopy） |
| TC-S6-3 | 手动入口不回归 | `xdev self update` / GUI 手动 check 按钮行为不变 |

## 三、验收标准（Spec 四、1-10 映射）

| Spec 条目 | 检查方式 | 验收判定 |
|---|---|---|
| 1. 新字段可配置/校验/回填 | TC-S1-1~4 | 绿 |
| 2. GUI 启动按配置自检 + 24h 限频 | TC-S2-1/2 + TC-S6-1 | 绿 |
| 3. prompt 三选三动作 | TC-S2-4 + TC-S3-1~4 | 绿 |
| 4. auto 自动更新 | TC-S2-5 + TC-S3-6 | 绿 |
| 5. notify 仅提示 | TC-S2-6 + TC-S3-5 | 绿 |
| 6. 非 TTY 降级不阻塞 exit 0 | TC-S4-4 | 绿 |
| 7. 挂载点仅 3 个交互入口 | TC-S4-6（守卫） | 绿 |
| 8. stale 替换全分支 | TC-S5-1~7 | 绿 |
| 9. 忽略记录生效/解除 | TC-S2-8/9 + TC-S3-3 | 绿 |
| 10. 全量回归 + 构建 | T-E1 | 全绿 |

## 四、执行记录

| 日期 | Task | 验收结果 | 备注 |
|------|------|----------|------|
| 2026-08-22 | 规划 | 已批准（task-034-001：三决策点按默认执行） | |
| 2026-08-22 | T-A1~A3 | 通过（config 2 字段 + CLI set/get + TC-S1 6 用例） | config/cli 测试全绿 |
| 2026-08-22 | T-B1 | 通过（policy.go CheckPolicy + 限频 + 忽略；TC-S2 11 用例） | update 测试全绿 |
| 2026-08-22 | T-B2 | 通过（stale.go ReplaceStaleCLICopy 注入式探测；TC-S5 8 用例） | update 测试全绿 |
| 2026-08-22 | T-C1 | 通过（startup 2s 接线 + ApplyUpdate 改目标为同目录 CLI + IgnoreSelfUpdate 绑定 + self-update 事件） | gui 编译全绿 |
| 2026-08-22 | T-C2 | 通过（SelfUpdateDialog.vue + AppContent 监听 + i18n + vitest TC-S3 5/5） | 新增前端组件 |
| 2026-08-22 | T-C3 | 通过（EnsureSelfInstalled stale 感知） | app.go |
| 2026-08-22 | T-D1 | 通过（runSelfCheck 三入口挂载 + TTY 三选 + 非 TTY 降级 + 守卫 TC-S4） | cli 测试全绿 |
| 2026-08-22 | T-E1 | 通过（go test ./... -short EXIT=0 全绿；vitest 28 文件 161 测试全绿；build.ps1 -SkipAudit 产出 0.3.3 双产物；绑定已生成 IgnoreSelfUpdate；CLI 冒烟：config set/get 新字段 + 非法枚举拒绝） | |