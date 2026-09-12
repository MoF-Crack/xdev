# 28 manifest 加载/resolver/shim 索引组（P1-11/14/15）

Status: open
Type: task
批次: B5
对应修复项: P1-11(D-06) + P1-14(D-09) + P1-15(C-04) + N-20 注释（方案 §5 主题 B）

## 方案要点

1. **P1-11**：loadMergedCatalog 单文件解析失败分支（manifest.go:415-416）加 `log.Printf("[manifest] WARNING: skip unparseable %s: %v")`；容错语义保留；**顺带更正 :374-378 函数注释**（「嵌入优先」实现 vs 注释「disk as override」矛盾——N-20）。
2. **P1-14**：Resolve 的 KindExact（resolver.go:80）两侧 TrimPrefix("v")，精确匹配优先（先原始串全等，miss 再双侧去 v）——对内置（无 v 前缀）与自定义工具（Pattern 捕获组含 v）均无回归。
3. **P1-15**：scanFor 磁盘命中后 `os.Stat(entry.path)` 失败 → **强制 buildShimIndexMap 现扫**并以现扫结果回答（只回落 sync.Once 内存索引会回环）；writeDiskShimIndex（shim.go:273 裸 os.WriteFile）改 AtomicWriteFile。

## 守卫测试

坏 yaml → 日志 + embedded 基线生效；`Resolve(Parse("v3.12.0"), ["3.12.0"])` == "3.12.0" 且 `Resolve(Parse("v1.2.3"), ["v1.2.3"])` == "v1.2.3"；陈旧磁盘索引 → 现扫兜底（恢复 bf69b85 透传语义）。
