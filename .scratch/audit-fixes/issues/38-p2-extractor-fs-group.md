# 38 P2 抽取器与文件系统组

Status: open
Type: task
批次: B5
对应修复项: P2-01(A-12) + P2-02(A-13) + P2-03(A-14) + P2-04(B-10) + P2-05(B-11) + P2-06(B-14) + P2-26(X-02) + P2-32(N-17)（方案 §6）

## 方案要点

1. **P2-01 tar TypeLink**：switch 增加 `case tar.TypeLink:`——Linkname 相对**归档根**解析 `Join(dst, linkname)`（不能照搬 TypeSymlink 的 Join(Dir(out)) 语义）；**linkname 须同 stripPath 归一**（strip≥1 归档的硬链 linkname 含顶层前缀，不归一则指向未提取路径）；目标侧显式 `isSafePath(dst, target)`；目标已提取后 os.Link，失败 warning 跳过。
2. **P2-02 UNC**：`\\` 前缀拒绝只作用于 rel 部分（Join 前），允许 UNC dst；或明确报「UNC home not supported」。
3. **P2-03 runInstaller SHA**：复检接受 `{a.SHA256, a.MirrorSHA256}` 集合。
4. **P2-04 loadFavorites**：赋值前 favMu.Lock()（两行）。
5. **P2-05 autoAppendBinToPath**：补实现 `env.AppendToUserPath(filepath.Join(value, "bin"))` 或删函数（确认产品意图二选一）。
6. **P2-06 FixLocalVersions**：每工具 AcquireToolLock（20s，失败跳过下次再修）。
7. **P2-26 HideWindow**：extractor.go:370/:395/:478 三处 exec.Command 补 `util.HideWindow(cmd)`。
8. **P2-32 CopyFile 权限**【复核新增】：fs.go:32 落盘改源文件 `Stat().Mode().Perm()`（Unix 迁移 0644 不再变 0755）。

## 守卫测试

strip≥1 归档含硬链条目正确落盘；UNC home 明确报错；其余各一例。
