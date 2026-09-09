# Spec 039: dsh 安装管理（npm 托管）

## Status: in-progress

## 背景

dsh（DeepSeek Harness，npm 包 `@deepseek-ai/dsh`）是一个纯 JS 的 npm CLI（`bin: lib/bin.js`，
运行时依赖 180+ npm 包）。与 opencode/claude-code 的「npm 平台二进制包 + tgz 解压」形态不同：

- 平台 tgz 不存在——`@deepseek-ai/dsh` 的 tgz 只含 `lib/*.js`，不含依赖树；
  直接解压得到的是一个无法运行的残缺安装。
- 依赖树必须由 npm 在安装时解析（registry 完整性由 npm 自身校验）。

### 版本现实（2026-09 实测，考虑因素：版本频繁更新 + alpha）

- 一个月内发布 17 个版本，**全部为预发布**（`X.Y.Z-rc.N` / `X.Y.Z-alpha.N`）。
- dist-tags：`latest=0.1.2-rc.1`、`alpha=0.1.5-alpha.1`——**latest 本身就是 rc**。
- 官方 package.json 无 `engines` 声明（依赖树也无）；按同类工具（claude-code）先例声明 node>=22。

## 需求（用户原始诉求）

1. **分类与 opencode 一致**：`development_tools` 分类。
2. **默认通过 npm 管理**：安装即 `npm install -g --prefix <installDir>/<version>`，
   npm 负责依赖解析、下载完整性校验与启动器 shim（.cmd/.ps1/bin/）生成。
3. **安装前检查 npm 依赖**：node + npm 必须可用（xdev 管理的 Node 优先），
   缺失/版本不足时在安装前给出可执行的错误（`xdev install node`）。
4. **考虑版本频繁更新 + alpha**：
   - remote pattern 接受 `X.Y.Z(-(alpha|beta|rc).N)?`（否则 0 个版本可发现）；
   - `stable` 标志正确标注预发布（merge 时按版本串推导）；
   - `latest` 提升规则（**通道感知**，真实数据验证后细化）：
     - 稳定 latest 永不被任何预发布版本抢占；
     - 预发布仅在 latest 本身是预发布且**通道相同**时可提升
       （rc→rc ✓；alpha 通道不得劫持 rc-latest——dsh 实测 npm
       dist-tags：latest=0.1.2-rc.1 而 alpha=0.1.5-alpha.1，
       若按纯 semver 提升会让默认安装装到 registry 不认作
       latest 的 alpha）；
     - 比较用预发布感知的 semver 降序（SemVer 2.0.0）；
   - 比较器（util.CompareSemver）升级为 SemVer 2.0.0 预发布语义。

## 方案

### 安装路径（新增 install_kind: npm）

- CLI：`InstallWithProgress` →（npm kind 跳过归档下载）→ lock →
  `installByKind → installViaNPM` → 完整性硬门 → PostInstall → Activate。
- GUI：`installWithOptions` 在解析 archive 后提前分派到 `InstallNPMPackage`
  （异步 goroutine + installProgress/installError 事件，与 dlMgr 管线事件契约一致）。
- `installViaNPM`：
  1. 包名取自 `manifest.Remote`（type: npm），回退工具名；校验包名/版本串字符集；
  2. 预检 `resolveNodeToolchain`：`<XMDEV_HOME>/current/node` 优先（npm-cli.js 直调 node），
     否则 PATH 上的 node/npm（剔除 xdev shim 目录）；
  3. node 版本预检（manifest `depends: node` min_version，硬失败）；
  4. `node <npm-cli.js> install --global --prefix <installPath> <pkg>@<ver> --no-fund --no-audit`，
     PATH 前置 node 目录 + 代理注入（对齐 runInstaller）；
  5. 完整性门：npm 全局布局（win: `<install>/dsh.cmd`；unix: `<install>/bin/dsh`）
     命中现有 binaryCandidates/FindInDir，shim 分发零改动。
- npm-kind 的 manifest archive 条目为元数据/嗅探模板（URL 指向 registry tgz，
  sha256 留空——npm 自带 registry 完整性校验，不落 TOFU）；
  GUI 的「未验证版本」确认弹窗对 npm-kind 豁免（无归档下载可失败）。

### 预发布语义（SemVer 2.0.0）

- `util.CompareSemver`：核心段相同 → 无预发布 > 有预发布；预发布标识符
  逐段比较（数字段按数值、字母段按 ASCII、数字段 < 字母段、字段多者高）。
  对齐 resolver 包既有实现。npm 列表降序排序的 rc/alpha 次序随之修正。
- `util.IsPrereleaseVersion`：trim `v`、剥 build metadata、含 `-` 即预发布。

## 验收

- `xdev install dsh` / GUI 安装：走 npm，缺 Node 时报可执行错误；
- 嗅探发现 alpha/rc，latest 提升/抢占保护行为符合上述规则；
- go test ./... -short 全绿；vitest 全绿；vite build 通过。

## Tickets

见 `issues/`。
