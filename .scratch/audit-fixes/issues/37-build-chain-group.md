# 37 构建链组（P1-39/40/41）

Status: resolved
Type: task
批次: B5
对应修复项: P1-39(F-10) + P1-40(F-11) + P1-41(F-13)（方案 §5 主题 G）

## 方案要点

1. **P1-39 appimagetool**：固定版本下载（指定 release tag）+ SHA-256 校验；或发行版包管理器安装（ci.yaml:411-416 与 :693-696 两 job——现 wget "continuous" 构建直接执行且持 GITHUB_TOKEN）。
2. **P1-40 build.sh**：`VERSION=$(sed -n 's/.*Version = "\([^"]*\)".*/\1/p' internal/version/version.go)`（:5 的 grep -P 在 macOS BSD grep 首行即死）。
3. **P1-41 build.ps1**：`$buildDate = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")`（:25 现为本地时间拼字面 "Z"）。

## 验收

CI release job 通过（appimagetool 固定+校验）；macOS 上 build.sh 可跑；build.ps1 BuildDate 为真 UTC。

## Comments（Batch 5 实施记录）

- 改动（仅 `.github/workflows/ci.yaml` +28/-3、`build.sh` +12/-1、`build.ps1` +6/-1）：
  - **F-10**（ci.yaml 两处 AppImage job）— appimagetool 从可变 "continuous" 改为**固定 release tag 1.9.1 + SHA-256 校验**（`echo "<sha>  appimagetool" | sha256sum -c -`，`bash -e` 下不匹配即终止步骤）。amd64 与 arm64 两处架构不同（`release` job=ubuntu-latest x86_64；`release-arm64` job=ubuntu-24.04-arm 原生 arm64），**哈希各自独立实测**：x86_64 `ed4ce84f…4eb0`（15092216 B）、aarch64 `f0837e74…9158`（14678536 B），并与 GitHub API 的 asset `digest` 字段逐字节交叉校验一致。
  - **F-11**（build.sh）— 实际代码是 `grep -oP`（票面写 sed；同属 GNU 专属），改为 POSIX 子集 `sed -n 's/^[[:space:]]*const Version = "\([^"]*\)".*$/\1/p' | head -n 1`，并加空值守卫（防产出 `xdev-` 静默错误产物）。BSD 依据：FreeBSD 14.0 `grep(1)` SYNOPSIS 无 `-P`（BSD grep 遇未知选项即退出 → 配合 `set -e` 使 macOS 上 build.sh 首行即死，正是本缺陷）。
  - **F-13**（build.ps1）— `(Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")`。
- 证据：YAML 解析 OK（node + yaml 2.9.0）+ 两个 run 块抽出后 `bash -n` 通过；旧命令与新命令等价性实测（Git-Bash GNU sed，输出同为 `0.4.0`；`sed --posix` 关扩展后同样通过）；F-13 实机输出 `2026-09-12T08:31:16Z`（真 UTC，本地 +08:00 为 16:31）对比旧式 `2026-09-12T16:31:16Z`（假 UTC）；`sha256sum -c` 正例 exit 0 / 翻转一字节 exit 1 实测。
- **旧 URL 可变性硬证据**（支撑「必须固定」）：AppImageKit `continuous` 资产 `created 2023-03-08` 但 `updated_at 2025-07-26`（同 URL 内容被上游替换过）；旧 URL 当前实际内容 8.8MB / `b90f4a8b…`，与固定的 1.9.1（15.1MB）完全不同构建。
- 票 15 保护项复核：三个 release job 的 "Verify signing secrets (hard gate)" 与 4 处 `LDFLAGS` 定义步骤**均未受影响**（node 回读解析确认）。
- **未决/存疑（非阻塞）**：①`release-arm64` 的 AppImage 步骤带 `continue-on-error: true` → 该处哈希门的强制力弱于 amd64 侧（既有策略，未改）；②同类「未固定可执行输入」仍在：`ci.yaml:323-324` 的 `apt-get install` 与 `gem install fpm --no-document`（fpm 恒取最新且直接产 deb/rpm）——建议单独出票；③appimagetool 1.9.1 属新工具链，首次真实 release 前的兼容性无法本地证伪（唯一实质残余风险）；④「CI 三平台绿 / macOS 上 build.sh 可跑」需 CI 与 macOS 环境（本机仅完成等价替代验证）。
