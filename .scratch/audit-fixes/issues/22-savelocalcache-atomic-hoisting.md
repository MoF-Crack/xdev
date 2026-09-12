# 22 saveLocalCache 原子化 + 迁移刷新上提

Status: open
Type: task
批次: B4
对应修复项: P1-08（方案 §5 主题 A，复核修订版）

## 问题（复核修正的口径）

app_migration.go 实际 **4 个** refreshAndSaveLocalCache 调用点（:93/:495/:608/:696；:495/:608 为**批量迁移复用**的 worker——v1「被单工具迁移复用」方向写反）；GUI 包全域共 **15 个调用点/7 文件**汇入 app.go:601 的同一非原子 os.WriteFile——仅迁移层上提修不完。

## 方案（两层）

1. **核心修复落本体**：saveLocalCache 改 `util.AtomicWriteFile` + 进程内 `sync.Mutex` 串行化（排队写，不「进行中跳过」）——15 个调用点一次全收敛。
2. **刷新上提**：把逐项刷新从 worker（:495/:608）移除，上提到调用方层——批量版 :696 尾部统一刷新保留、MigrateLegacyDir(:93) 单工具场景保留一次——恢复「统一刷新一次」契约。

## 守卫测试

并发迁移多个工具 + 并发安装完成（app_install.go 各刷新点）同写 local cache：`-race` 下无竞争、文件永不撕裂、终态含全部最新状态。
