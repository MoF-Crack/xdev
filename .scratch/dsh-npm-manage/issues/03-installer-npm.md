# 03: installer npm 安装路径

Status: done

- `internal/installer/npm_install.go`：
  - resolveNodeToolchain：current/node 优先（nodeBin + npm-cli.js + npmBin 候选），
    回退 PATH（剔除 xdev shim/install 目录）；
  - installViaNPM：包名/版本校验 → node/npm 预检（硬失败，错误含
    `xdev install node` 指引）→ node 版本预检（depends min_version）→
    `node <npm-cli.js> install --global --prefix <installPath> <pkg>@<ver>
    --no-fund --no-audit`（PATH 前置 nodeDir + 代理注入 + HideWindow + 15min 超时）；
  - InstallNPMPackage：GUI 自包含入口（tool lock → installViaNPM → 完整性门 →
    PostInstall → Activate，ActivationError 语义与 ExtractAndActivate 一致）。
- installer.go：InstallWithProgress 对 npm kind 跳过 downloadWithProgress；
  installByKind 分派 npm case；完整性硬门覆盖 npm kind（安装同步完成）。

## Comments

- npm 启动器布局命中既有 FindInDir/binaryCandidates（win `<install>/dsh.cmd`、
  unix `<install>/bin/dsh`），shim 分发零改动。
