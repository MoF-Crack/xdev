# 34 startup 嗅探三入口

Status: open
Type: task
批次: B5
对应修复项: P1-30（E-11）（方案 §5 主题 D——复核完全核实）

## 方案要点

1. main.go:79 固定 100ms sleep 改为仅当 `cli.HasStartupSniff()`——**须把 interval 策略 TTL 到期的后台嗅探也算入**（onStartupInterval 同样 spawn，否则 sleep 条件不完整）。
2. strategy=startup 的 OnStartup 仅挂 interactive 入口（root RunE / tui / gui——selfcheck_test.go:93-107 三入口 grep 守卫直接复制）；cd-hook 的 `xdev hook`/`env` 不触发。
3. **别顺手给 gui/app.go 加 OnStartup**（双击零参 GUI main.go:47-62 不走 Cobra，现状就没有 startup 嗅探——加了会双嗅探）。
4. OnInitialize 仍建 sniffScheduler 供 Shutdown 刷缓存（startupCancel==nil 时 Shutdown 秒退 ✓）。

## 守卫测试

三入口 grep 守卫（复制 selfcheck 模式）；单命令耗时 -100ms；startup 用户只在交互入口付出嗅探。
