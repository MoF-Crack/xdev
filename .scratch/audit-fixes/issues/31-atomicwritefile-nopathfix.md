# 31 AtomicWriteFile 改造 + no-path-fix 一致化（P1-23/24）

Status: open
Type: task
批次: B5
对应修复项: P1-23(E-04) + P1-24(E-05=F-07) + N-15（方案 §5 主题 C/D）

## 方案要点

1. **P1-24**：AtomicWriteFile（fs.go:215-225）改 `os.CreateTemp(dir, base+".tmp-*")` → 写入 + `Sync()` + `Chmod(perm)`（CreateTemp 0600 需还原；Windows 0600 不触发只读）→ rename → 失败清理。**同型固定 tmp 克隆 4 处一并迁移**（v1 清单 8+2，复核扩为 8+4）：writeHomeCache（config.go:1206-1213）、MirrorHealthCache.Save（installer/mirror_selector.go:103-130）、**update/policy.go writeJSONAtomic（:141-159——无 Sync 无 perm 还原恒 0600）**、**toolsHashFullCheck progress.go Progress.Save（:99）**。
2. **P1-23**：删除 init.go:73 本地 --no-path-fix flag（复用 root.go:109 持久 flag）；:169 与 :219/:254 一致化用同一来源（现状「部分生效」——自装门有效、仅向导安装阶段失效——测试覆盖该现状后统一）。断言 `xdev init --no-path-fix` 后 NoPathFix()==true。

## 守卫测试

100 goroutine 并发写同一路径循环读取校验（P1-24）；CI e2e 的 --no-path-fix 用例语义不变（P1-23）。
