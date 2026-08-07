# Spec 021 实施 Checklist

> 配套 `report.md`。每项 task 包含：实施内容 / 验收标准 / 验证命令。逐项打勾，全部通过才进入下一阶段。

## Phase 1 — 纯重构（零行为变化）

### P1-D1 合并 CopyDir/CopyDirProgress
- [ ] 合并为单一 `CopyDir(src, dst, progress func(...))`（progress 可 nil），删除 CopyDirProgress 独立实现
- [ ] 现有 CopyDir 调用点不变；测试覆盖两条路径
- [ ] 验证：`go test ./internal/util/ -count=1` + `go build ./...`

### P1-D2 统一归档后缀表 + cache_classify.go
- [ ] 归档后缀单一来源（建议 `internal/gui/cache_classify.go` 或 util），`isArchiveName` 引用同一表
- [ ] 缓存分类 `cacheEntryCategory(name)` 单一实现，供 GetCacheBreakdown / CleanCache / CleanCacheCategory / app_size_breakdown 共用
- [ ] 消除 `.dat` 分类分歧（GetCacheBreakdown 与 retention 不一致处，按 spec 019 既定语义统一）
- [ ] 验证：`go test ./internal/gui/ -count=1 -short`（缓存分类相关测试全部通过）

### P1-D3 统一伴生文件删除
- [ ] `removeArchiveCompanions(path)` 单实现，供 DeleteCacheFile / CleanupExpiredArchives / applyArchiveRetention 共用
- [ ] 验证：`go test ./internal/gui/ -count=1 -short -run "CacheDelete|Retention|CleanupExpired"`

### P1-D4 util.IsProxyError + util.SecureRedirectCheck
- [ ] `util.IsProxyError(err) bool`（proxyconnect/connection refused/actively refused/no connection could be made）
- [ ] `util.SecureRedirectCheck(maxRedirects int) func(...)` 供 downloader/remote/gui/update 5 处使用
- [ ] 各调用点行为不变（redirect 上限 3 vs 10 保留为参数）
- [ ] 验证：`go test ./internal/util/ ./internal/downloader/ ./internal/remote/ ./internal/gui/ -count=1 -short`

### P1-D5 前端 formatBytes/分类常量统一
- [ ] `frontend/src/utils/format.js` 导出 formatBytes（B/KB/MB/GB/TB 自适应），DashboardPage/InstallProgressDialog/SetupWizard/CacheSettings 统一引用
- [ ] 缓存分类 label map 提取共享常量（DashboardPage + CacheSettings 引用同一份）
- [ ] 验证：`npx vite build` + `npx vitest run test/pages/DashboardPage.spec.js`

## Phase 2 — 行为统一（含安全漂移修复）

### P2-D6 GUI 安装复用签名加载（F-02）
- [ ] 导出/共用签名加载（磁盘 → third-party → embedded 同 CLI 顺序），GUI `InstallToolWithOptions` 的 `downloader.Options` 填充 `ManifestSignature`
- [ ] GUI 与 CLI 下载选项构造收敛（复用 Installer.DownloadArchiveWithProgress 或等价）
- [ ] 新增：签名 GUI 安装集成测试（embedded key 场景）
- [ ] 验证：`go test ./internal/gui/ ./internal/installer/ -count=1 -short`

### P2-D7 TOFU/proxy-warning 共用 helper
- [ ] 导出 `Installer.ToFUArchiveIfNeeded`（或迁移 manifest 包），GUI app_install.go TOFU 块改为调用
- [ ] `CheckProxyConfigured(cfg, tool, ver)` 供 installer + GUI 共用
- [ ] 验证：`go test ./internal/installer/ ./internal/gui/ -count=1 -short`

### P2-D8 InstallMany 委托 InstallWithProgress
- [ ] InstallMany 对已装版本改走 `VerifyInstallIntegrity` 判定；IncompleteInstallError → Status "already_installed" 或新状态
- [ ] 批次模式不再把不完整安装当已装
- [ ] 验证：`go test ./internal/installer/ -count=1 -short -run "InstallMany|Incomplete"`

### P2-D9 版本比较收敛
- [ ] 删除 installer.CompareVersions→compareVersions 包装链，调用点直调 util.CompareSemver（确认语义一致）
- [ ] util.IsVersionNewer 与 CompareSemver 语义核对（保留 IsVersionNewer 保守语义或合并，需测试保护）
- [ ] 验证：`go test ./internal/... -count=1 -short`

### P2-D10 InitConfig 死锁修复（F-18）
- [ ] `InitConfig` 持 cfgMu.Lock 期间不再调用 getCfg()/RLock（锁前取指针或锁内用局部）
- [ ] 新增回归测试：并发调用 InitConfig + getCfg 不 deadlock（可用 `go test -race` 或超时断言）
- [ ] 验证：`go test ./internal/gui/ -count=1 -short -run "InitConfig"` + 手动首次启动

## Phase 3 — 安全加固

### P3-D11 shim 名校验（F-08）
- [ ] shim 名统一过 `util.SafeName`；InstallShim 目标路径 `ValidateSubPath` 校验
- [ ] 拒绝分隔符/设备名/`..`/空名；Windows 后缀策略保持
- [ ] 新增负向测试：`..\outside`、含 `/`、空名被拒
- [ ] 验证：`go test ./internal/shim/ ./internal/installer/ -count=1 -short`

### P3-D12 envVar 白名单强制（F-09/F-16）
- [ ] 自定义工具 AddCustomTool 校验 shim/envVar 名（IsValidEnvVarName + 白名单）
- [ ] 最终注册表写入边界（SetUserEnvVar/win_path 底层）强制白名单 + 保护名（PATH/Path/COMSPEC/SystemRoot 变体）
- [ ] 新增负向测试：Path 变体、非法字符、超长被拒
- [ ] 验证：`go test ./internal/env/ ./internal/gui/ -count=1 -short`

### P3-D13 Wails 破坏性方法加固（F-10/F-11）
- [ ] MigrateLegacyDir/MigrateLocalToolByJunction/Move：校验工具名/版本/路径包含、拒绝 symlink/reparse 源、目标路径规范化后校验
- [ ] ExportProfile/ImportProfile：路径限定专用目录或校验扩展/大小；导入前确认
- [ ] 新增负向测试：任意路径/穿越被拒
- [ ] 验证：`go test ./internal/gui/ -count=1 -short -run "Migrate|Profile"`

### P3-D14 Linux cron 严格解析（F-12）
- [ ] 新增 cron 语法解析器（5 字段、字符集、范围、无换行/控制字符）；非法 schedule 返回错误
- [ ] 可执行路径引号包裹或 wrapper 脚本
- [ ] 新增解析测试
- [ ] 验证：`go test ./internal/discovery/ -count=1 -short`

### P3-D15 缓存 sidecar 校验（F-04/F-05）
- [ ] 缓存命中 + 空 SHA：用既有 sidecar pin 校验，不一致则隔离/删除并失败
- [ ] 已存在 sidecar 时不再自动覆盖 pin
- [ ] 新增测试：篡改缓存后命中被拒
- [ ] 验证：`go test ./internal/downloader/ ./internal/installer/ -count=1 -short`

### P3-D16 日志 URL 全量 sanitize（F-15）
- [ ] downloader/remote/audit 所有 URL 日志点统一 sanitize（userinfo/query/fragment 去除）
- [ ] 代理错误消息去凭据
- [ ] 验证：grep 确认无原始 URL 直记；`go test ./internal/downloader/ ./internal/remote/ -count=1 -short`

## Phase 4 — 架构与治理

### P4-D17 配置快照化（F-19）
- [ ] getCfg 返回快照或全部变更走同步方法；消除锁外可变指针读写
- [ ] 验证：`go test -race ./internal/gui/ -count=1 -short`（如工具可用）

### P4-D18 上下文绑定/事件化（F-20）
- [ ] 安装轮询、启动延迟任务、远程操作统一派生自 app ctx；关闭可取消
- [ ] 轮询可改事件/通道（dlMgr 完成通知）
- [ ] 验证：`go test ./internal/gui/ -count=1 -short` + 手动关闭无泄漏

### P4-D19 签名负载扩展（F-03，大改需单独评估）
- [ ] 设计规范：签名 payload 含 version/os/arch/URL/镜像 hash/format/strip/kind/args
- [ ] 校验精确 tuple 而非 map 成员
- [ ] 所有 manifest 重签流程（toolsHashFullCheck + keygen + CI）
- [ ] 验证：全量签名校验测试 + 重签产物构建

### P4-D20 平台凭据存储（F-14）
- [ ] keystore 实现（Windows DPAPI/Keychain/Secret Service）替代明文 YAML
- [ ] sshpass -p 移除（stdin/受控 channel）
- [ ] 日志/错误去除 secret 值
- [ ] 验证：凭据读写测试 + ssh 连接测试

### P4-D21 自更新签名（F-06）
- [ ] 发布签名校验和文件（release key/Authenticode）
- [ ] 验证：自更新测试

### P4-D22 依赖治理（F-21）
- [ ] 评估 one-api/vendor/toolsHashFullCheck 是否产品范围；更新 xi2/xz
- [ ] CI 增加 govulncheck/staticcheck/race（如工具可用）
- [ ] 验证：`go mod verify` + govulncheck 输出

---

## 执行记录
| 日期 | 阶段 | 完成项 | 验证结果 | 备注 |
|------|------|--------|----------|------|
| 2026-08-01 | P1 | D1-D5 | 全量 go test -short 通过；vite build 通过；DashboardPage 2/2、Settings/SetupWizard 24/24 | `.dat` 分类统一（parseCacheName helper-only）；TestProxy 增加 HTTPS 降级阻断 |
| 2026-08-02 | P2 | D6-D10 | 全量 go test -short 通过（27 包）；wails build 通过（1:08:36） | GUI 安装现加载 ManifestSignature（F-02 修复）；InitConfig 死锁回归测试通过（补 CloseLogger 清理）；InstallMany 完整性判定统一 |
| 2026-08-02 | P3 | D11-D16 | 全量 go test -short 通过（26 包，无 FAIL） | D11 shim 名校验（F-08）；D12 envVar 白名单（F-09/F-16）；D13 迁移/Profile 路径加固（F-10/F-11）；D14 cron 严格解析（F-12）；D15 缓存 sidecar pin 强制（F-04）；D16 日志/审计/错误 URL sanitize（F-15） |
| 2026-08-02 | P4 | D17+D18 | 全量 go test -short 通过（26 包，无 FAIL）；wails build 通过（2:20:28） | D17 配置快照化（getCfg→Clone 快照 + mutateCfg 写路径，F-19）；D18 root context 绑定安装轮询/延迟任务、shutdown 取消（F-20）。**D19-D22 经用户确认暂缓**（签名负载扩展需重签 manifest、平台凭据存储、自更新签名、依赖治理，建议单独评估） |
| | P2 | D6-D10 | （待填） | |
| | P3 | D11-D16 | （待填） | |
| | P4 | D17-D22 | （待填） | |
