# Issue 47: isolate/remote exec 的空白切分破坏引号语义 + 杂项前端遗留

Status: open
Type: bug
Severity: low（P3；可用性/正确性，非注入面——Go 侧已是 argv 数组无 shell）
Discovered: 2026-09-25，第六轮审计（前端分路 F-4 / F-9 建议 / F-6 验证缺口）

## F-4：`command.split(/\s+/)` 丢失引号结构

- `frontend/src/AppContent.vue:572`（IsolateExec）与 `:668`
  （ExecRemoteInstance）。
- `git commit -m "fix:  double  space"` → `['git','commit','-m','"fix:','double','space"']`：
  ① Go 的 ssh 适配器把 argv 重新拼接给远端 shell（ssh_adapter.go:169-179），
  引号内连续空格被静默折叠；② isolate 侧的命令白名单
  （app_project.go:76-82，仅 shim 名 + --version 探针）可能因切分形态不符
  而误拒合法命令，报错令用户困惑。
- 修复方向：把**原始命令串**透传给 Go 绑定，Go 侧做 shell-words 解析
  （或前端实现等价的最小解析器）；白名单匹配基于解析后的 argv。

## F-6 验证缺口：依赖 CVE 扫描未进 CI

- 第六轮审计时 `web_search` 不可用（API 401），vue 3.5.35 / naive-ui 2.44.1 /
  vite 6.4.3 / vitest 3.2.7 / jsdom 26.1.0 / playwright 1.61.1 / esbuild
  0.25.12 / wails 2.12.0 的**实时 CVE 状态无法核对**（版本线均为当前主流）。
- 已修（本轮）：package.json 的 vue 地板 `^3.4.0 → ^3.5.0`、naive-ui
  `^2.38.0 → ^2.44.0`（与 lockfile 实际钉定一致，防无锁新装回退旧 minor）。
- 修复方向：CI 增加定时 job：`npm audit --production` + `osv-scanner`
  （Go + npm + lockfile 全量），对 known-exploited 断言为硬门禁，其余
  advisory 落 summary。

## F-9：check-urls allowlist 的 '#' 注释剥离不可豁免含片段 URL

- `internal/cli/check_urls.go:256-258` 从首个 '#' 起当注释剥离——含片段
  （或路径含 #）的 manifest URL 永远无法进 allowlist，存活巡检反复误报。
- 修复方向：仅当行首前缀解析为 http/https URL 时才剥离 '#'，或要求
  allowlist 裸 URL、放弃行中剥离。

## F-12：archive_sha_text.go 边缘损失

1. sha256 行替换会丢该行的行内注释（`sha256: ""  # TODO upstream`——契约
   承诺只外科手术 sha256 值，url 行的保注释有测试钉、sha256 行没有）。
   修复：携带旧行尾 `# ...` 片段。
2. 同 (version,os,arch) 重复条目：locateArchiveEntry 恒取第一个匹配——
   两条 pending 更新写同一行，第二条静默胜出，changed 计数虚高。
   修复：检测重复更新并报错/合并。

## F-1 的 Go 侧纵深（可选）

`MigratePackageManager` 的 Go 端（app_local_detect.go:1206）无工具锁、无
幂等门——本轮已在前端补 G-06 入口守卫；如需纵深，Go 端可比照
UpdatePackageManager 的锁/防重入语义。
