# 39 P2 远程/semver/杂项组（含两条必红/统一饱和项）

Status: open
Type: task
批次: B5
对应修复项: P2-07/08/09/11/12/13/14/23/21/22/30/31（方案 §6）

## 方案要点

1. **P2-07** GetLatestVersionForTool 先查 a.getCat()，miss 单飞 LoadCatalog 共享（等待者复用同一次结果）。
2. **P2-08** C-11 304 分页：cacheEntry 存 nextURL；304 时从缓存恢复下一页继续循环。
3. **P2-09** lookupOnPath：Unix os.Stat（非 Lstat——LookPath 语义跟随符号链接）校验执行位；Windows 按 PATHEXT 展开（hostscan.go:247 先例），裸名末位兜底。
4. **P2-11** retryDirect 包级单例 Transport——**仍须检查 testHookDirectTransport**（remote_test.go:1767/1802 换注入，漏检两测试红）；或 CloseIdleConnections。
5. **P2-12 IsAllZeroHash 严格化**：TrimPrefix "0x" 后 `len(s)==64` 才算占位。**必红**：downloader_test.go:238 `{"0", true}` 同步改 **false**；同测试 :234 `{"0x"+64零}` **保持 true**（TrimPrefix 后恰 64 零，与签发端一致）——测试注释显式说明两行口径。
6. **P2-13 + P2-14 + P2-23 统一**：parseSemver 先剥 `+...`；ParseSemverParts 与 numericValue **统一为 strconv.ParseUint + uint64 饱和**（两项分别实现会在同一比较器产生不同天花板）。
7. **P2-21** linkaudit 标签循环：`retryLoop: select { ... case <-ctx.Done(): break retryLoop }`。
8. **P2-22** hmac_key.go 注释如实声明（推荐仅改注释，不破坏兼容）。
9. **P2-30**【复核新增】ConfigureHTTPClient（remote.go:120）包级单例 Transport 或替换前 CloseIdleConnections。
10. **P2-31**【复核新增】register 双 LuaEngine 合并：一次目录扫描、每 .lua 只 DoFile 一次；vfox 加载失败记日志。

## 守卫测试

必红清单执行「先红后绿+同步更新」；semver 统一后大数字/溢出用例排序正确。
