# 26 安装器正确性组（P1-03/04/05/06/07）

Status: open
Type: task
批次: B5
对应修复项: P1-03(A-07) + P1-04(A-08) + P1-05(A-09) + P1-06(A-11=D-10) + P1-07(A-15/A-15b)（方案 §5 主题 A）

## 方案要点

1. **P1-03 Force 先删后下载**：次序反转——Force 分支先下载+SHA 校验成功，删除移入锁内 extract 前（推荐）；守卫：Force+不可达 URL → 原安装仍在。
2. **P1-04 zip mode**：unzipFile（extractor.go:293）`f.Mode()` → `f.Mode().Perm()`（与 tar 路径对齐）；symlink 条目维持降级 + 注释。
3. **P1-05 glob 兜底**：删除 installer.go:987 glob `*-{ver}.{fmt}` 兜底改直接报 cache miss（或保守版：前缀限定 `tool+"-"` + 命中跑 sidecar/SHA 校验——**本机已有受害者**：E:\XDev\install\llvm\lld_is_a_generic_driver._ 装 android-build-tools 内容）。
4. **P1-06 TOFU any**：TOFUArchiveIfNeeded 不代换 os/arch=any（传原始值）；UpdateArchiveSHA256 匹配先精确、再回退「条目 any 且请求键为宿主」双向兜底。
5. **P1-07 npm PATH**：prependEnvPath（npm_install.go:404-419）EqualFold 匹配（前 5 字符）；前置目录改 `filepath.Dir(tc.nodeBin)`（Unix bin 布局）。

## 守卫测试

Force+断网原安装保留；zip setuid 位剥离；glob 移除后异常竞争场景报错可重装；any 条目 pin 写回成功；Windows "Path=" 命中改造。
