# 代码审查与安全审计报告

> **状态**：待审批（文档与计划已完成，实施待批准）
> **类型**：审查/审计 + 重构计划
> **日期**：2026-08-01
> **审查范围**：`internal/`（22 包）、`cmd/`、`frontend/src/`、`go.mod`、`manifests/`

## 一、代码量统计

| 类别 | 文件数 | 行数 |
|------|-------:|-----:|
| Go 源码（internal+cmd） | 171 | 34,127 |
| Go 测试 | 74 | 14,461 |
| **Go 合计** | **245** | **48,588** |
| 前端 Vue | 32 | 3,564 |
| 前端 JS | 11 | 284 |
| 前端 TS | 9 | 1,127 |
| **前端合计** | **52** | **4,975** |
| wailsjs 生成绑定 | — | 1,822 |

Go 主要包分布（行数降序）：gui 9,092 / cli 5,195 / installer 3,787 / env 3,163 / discovery 2,878 / remote 2,085 / manifest 1,790 / tui 1,769 / util 1,621 / config 1,491 / downloader 1,388 / linkaudit 1,288 / plugin 1,253 / extractor 1,085 / shim 716 / isolator 627 / update 571 / resolver 548 / orchestrator 544 / integration 518。

**总计约 55,385 行（不含 wailsjs 生成与第三方目录 one-api/vendor）。**

## 二、安全审计发现（oracle 审查，OWASP 对齐）

### 高严重度（发布前必修）

| ID | 问题 | 位置 |
|----|------|------|
| F-01 | 签名校验 fail-open：root key 为空时接受未签名 manifest，build.ps1 仅警告不阻止 | signer/verify.go:60,81-99; build.ps1:26-35 |
| F-02 | **GUI 安装路径不加载 ManifestSignature**（CLI 有），真实 key 下 GUI 安装被拒/降级 TOFU | gui/app_install.go:188-197 vs installer/installer.go:550-577 |
| F-03 | 签名负载只含 tool+sha256_map+timestamp，未绑定 URL/镜像/格式/strip/install kind/args；校验只查 hash 是否在 map 中 | signer/signer.go:31-35; downloader.go:529-537,683-711 |
| F-08 | **shim 名未做 SafeName/路径包含校验**，恶意 manifest 可写 `..\outside` | shim/shim.go:367-382; installer.go:1023-1027 |
| F-09 | **自定义工具 envVar 未校验**，经 SetUserEnvVarTrusted 绕过白名单写任意 HKCU 变量（如 Path 变体） | gui/app_custom_tools.go; manifest.go:610-648; win_path_windows.go:422-479 |
| F-10 | **Wails 暴露任意路径破坏性操作**：MigrateLegacyDir/MigrateLocalToolByJunction/Move 接受任意路径并 RemoveAll | gui/app_migration.go; env/migrate.go |
| F-12 | **Linux crontab 注入**：schedule 未校验直接拼入 crontab | discovery/service_install.go:69-93 |

### 中严重度

| ID | 问题 | 位置 |
|----|------|------|
| F-04 | 缓存命中不验证 sidecar pin，同用户篡改缓存可被解压；0o444 可被改写 | downloader.go:539-553; installer.go:495-545 |
| F-05 | TOFU 写回 on-disk manifest 未签名，发现版本/URL 直接落盘 | manifest.go:440-531; scheduler.go:685-774 |
| F-06 | 自更新校验和文件本身未认证（HTTPS 下载 SHA256SUMS） | update/update.go:317-366 |
| F-07 | 配置 HMAC key 由用户名/主机名/SID 派生（可预测），且未覆盖 proxy/mirror/token 等敏感字段 | util/hmac_key.go; config.go:391-421 |
| F-11 | ExportProfile/ImportProfile 接受任意路径，可覆盖任意文件/读取任意文件 | gui/app_profile.go |
| F-13 | SSRF/DNS rebinding：IsSafeExternalURL 解析一次后实际请求再解析；重定向未复查内网地址 | util/url_safety.go; gui/app_config.go |
| F-14 | SSH 密码经 `sshpass -p` 命令行泄露；token 明文 YAML；安装日志打印环境变量值 | orchestrator/ssh_adapter.go; config.go; installer.go:1144-1150 |
| F-15 | 部分日志/审计在 sanitize 前记录原始 URL（可含 userinfo/query） | downloader.go:125,183,220,230; audit.go |
| F-16 | 环境变量最终写入边界只查保护名，不强制白名单 | gui/app_env_vars.go; win_path_windows.go:580-589 |
| F-17 | 解压 zip 写入 O_TRUNC 无 no-follow，同用户预置 symlink 可重定向写入 | extractor/extractor.go:100-158 |
| F-18 | **InitConfig 死锁**：持 cfgMu.Lock 后调 getCfg() 再 RLock（RWMutex 不可重入）——首次运行可用性阻塞 | gui/app_migration.go:201-202,262; app.go:614-619 |
| F-19 | getCfg() 返回可变指针，调用方在锁外读写嵌套字段，与 Config.mu 语义重叠 | gui/app.go:614-625 |

### 低

| ID | 问题 |
|----|------|
| F-20 | 安装轮询 goroutine 1 小时 sleep 未绑 app 关闭；多处理用 context.Background() |
| F-21 | 依赖治理：root 有 2017 年 xi2/xz；嵌套 one-api 模块（jwt v3.2.2）；vendor/ 树未审查 |

### 静态检查结果
- `go vet ./...`：通过（无输出）
- `go mod verify`：通过
- 前端 `npm audit`：0 漏洞
- 未发现非测试代码使用 md5/sha1/DES/RC4；未发现 v-html/innerHTML

## 三、重复实现与封装机会（explorer 审查）

| # | 重复点 | 位置 | 建议抽象 | 省行 | 优先级 |
|---|--------|------|---------|-----:|--------|
| 1 | isProxyError 3 份相同 | downloader.go:292 / remote.go:255 / app_config.go:394 | `util.IsProxyError` | ~30 | 高 |
| 2 | HTTPS 降级 CheckRedirect 5 份 | downloader/remote/app_config/update | `util.SecureRedirectCheck(max)` | ~55 | 高 |
| 3 | 归档后缀表 3 份 | app_cache_retention / app_size_breakdown / app_config | 统一 `IsArchiveName` | ~40 | 高 |
| 4 | 缓存分类规则 Go 4 处 | app_config(×2)/app_size_breakdown/app_cache_delete | `gui/cache_classify.go` | ~60 | 高 |
| 5 | .sha256/.part 伴生删除 3 处 | app_cache_delete / app_cache_retention(×2) | `removeArchiveCompanions` | ~20 | 高 |
| 6 | CopyDir vs CopyDirProgress 同体 | util/fs.go:47-117,168-244 | 合并为一个 | ~90 | 高 |
| 7 | TOFU 写回 GUI/installer 重复 | gui/app_install.go:264-288 / installer.go:641-667 | 导出 Installer.ToFUArchiveIfNeeded | ~25 | 高 |
| 8 | requires_proxy 警告 2 份 | installer.go:581-594 / app_install.go:166-177 | `CheckProxyConfigured` | ~15 | 高 |
| 9 | 版本比较 3 实现 + 包装链 | util/semver.go / util/version.go / installer 包装 / cli 直调 | 收敛到 util，删包装链 | ~30 | 中 |
| 10 | GUI 安装流水线整体重复 installer | gui/app_install.go:76-356 | 导出 `DownloadArchiveWithProgress` | ~50 | 中/高 |
| 11 | 探测用 HTTP client 3 份 | app_config(×3)/remote/mirror | `internal/httpx` 共享 | ~25 | 中 |
| 12 | InstallMany 与 InstallWithProgress 已装判定不一致 | installer.go:273-283 vs :364-404 | 委托统一 | ~10 | 中 |
| 13 | 前端 formatBytes 4 份 | DashboardPage/InstallProgressDialog/SetupWizard/CacheSettings | `utils/format.js` | ~40 | 中 |
| 14 | 仪表盘两个大小抽屉近乎相同 | DashboardPage.vue:13-30 vs :31-48 | `SizeDrawer.vue` + composable | ~80 | 中 |
| 15 | 前端缓存分类 label map 2 份 | DashboardPage / CacheSettings | 共享常量 | ~10 | 中 |
| 16 | AppContent ~26 个重复异步加载器 | AppContent.vue | `useAsyncState` composable | ~80 | 低 |
| 17 | ActiveVersion 包装链 3 处 | cli/env.go / installer.go:1376 | 直调 util | ~12 | 低 |
| 18 | GUI 方法前导重复 | app_install.go | `withInstaller` helper | ~15 | 低 |
| 19 | 路径包含校验 3 份手写 | extractor / app_cache_delete / util.sanitize | 复用 ValidateSubPath | ~10 | 低 |

**预计可省约 690 行 + 消除 6 处行为漂移**（尤其 F-02：GUI 安装漏签名校验）。

## 四、实施 Plan

按依赖与风险分 4 个阶段，每阶段独立验证、可单独审批：

### Phase 1 — 纯重构零行为变化（低风险，先做）
- D1 合并 CopyDir/CopyDirProgress（#6）
- D2 归档后缀表统一（#3）→ 缓存分类统一 cache_classify.go（#4，一并消除 .dat 分类分歧）
- D3 伴生文件删除统一（#5）
- D4 util.IsProxyError + util.SecureRedirectCheck（#1/#2，5 处调用点）
- D5 前端 formatBytes 统一 utils/format.js（#13）+ 分类 label 常量（#15）
- 验证：go test ./... -short + vite build

### Phase 2 — 行为统一（消除漂移，修 bug）
- D6 GUI 安装复用签名加载（F-02，核心安全项）
- D7 TOFU 写回/requires_proxy 警告共用 installer helper（#7/#8）
- D8 InstallMany 委托 InstallWithProgress（#12，修复批次模式完整性漏检）
- D9 版本比较收敛（#9）+ ActiveVersion 包装链删除（#17）
- D10 InitConfig 死锁修复（F-18）
- 验证：go test + 新增签名 GUI 集成测试 + 死锁回归测试

### Phase 3 — 安全加固（发布前）
- D11 shim 名 SafeName/ValidateSubPath（F-08）
- D12 自定义工具 envVar 白名单 + 最终写入边界强制（F-09/F-16）
- D13 Wails 破坏性方法路径校验/不透明 ID（F-10/F-11）
- D14 Linux cron 严格解析（F-12）
- D15 缓存命中 sidecar 校验 + pin 不可覆盖（F-04/F-05）
- D16 日志/审计 URL 全量 sanitize（F-15）
- 验证：新增负向测试（穿越/删除/非法 shim/env）+ go vet

### Phase 4 — 架构与治理（长期）
- D17 配置访问快照化/单一锁（F-19）
- D18 操作绑定 app 上下文、轮询改事件（F-20）
- D19 签名负载扩展（F-03，需重签所有 manifest——大改，单独评估）
- D20 平台凭据存储 + sshpass 移除（F-14）
- D21 自更新签名认证（F-06）
- D22 依赖治理/one-api 评估（F-21）
- 验证：race 测试 + govulncheck（如工具可用）

## 五、Task 清单（待审批）

- [ ] P1-D1 合并 CopyDir/CopyDirProgress
- [ ] P1-D2 统一归档后缀表 + cache_classify.go
- [ ] P1-D3 统一伴生文件删除
- [ ] P1-D4 util.IsProxyError + SecureRedirectCheck
- [ ] P1-D5 前端 formatBytes/分类常量统一
- [ ] P2-D6 GUI 安装复用签名加载（F-02）
- [ ] P2-D7 TOFU/proxy-warning 共用 helper
- [ ] P2-D8 InstallMany 委托（#12）
- [ ] P2-D9 版本比较收敛（#9）
- [ ] P2-D10 InitConfig 死锁修复（F-18）
- [ ] P3-D11 shim 名校验（F-08）
- [ ] P3-D12 envVar 白名单强制（F-09/F-16）
- [ ] P3-D13 Wails 破坏性方法加固（F-10/F-11）
- [ ] P3-D14 Linux cron 严格解析（F-12）
- [ ] P3-D15 缓存 sidecar 校验（F-04/F-05）
- [ ] P3-D16 日志 URL 全量 sanitize（F-15）
- [ ] P4-D17 配置快照化（F-19）
- [ ] P4-D18 上下文绑定/事件化（F-20）
- [ ] P4-D19 签名负载扩展（F-03，需重签）
- [ ] P4-D20 平台凭据存储（F-14）
- [ ] P4-D21 自更新签名（F-06）
- [ ] P4-D22 依赖治理（F-21）

> **审批请求**：请选择实施范围（建议至少 P1+P2，安全项 P3 的 D11-D14 次优先）。批准后按阶段执行，每阶段完成后汇报验证结果。
