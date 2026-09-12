# 37 构建链组（P1-39/40/41）

Status: open
Type: task
批次: B5
对应修复项: P1-39(F-10) + P1-40(F-11) + P1-41(F-13)（方案 §5 主题 G）

## 方案要点

1. **P1-39 appimagetool**：固定版本下载（指定 release tag）+ SHA-256 校验；或发行版包管理器安装（ci.yaml:411-416 与 :693-696 两 job——现 wget "continuous" 构建直接执行且持 GITHUB_TOKEN）。
2. **P1-40 build.sh**：`VERSION=$(sed -n 's/.*Version = "\([^"]*\)".*/\1/p' internal/version/version.go)`（:5 的 grep -P 在 macOS BSD grep 首行即死）。
3. **P1-41 build.ps1**：`$buildDate = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")`（:25 现为本地时间拼字面 "Z"）。

## 验收

CI release job 通过（appimagetool 固定+校验）；macOS 上 build.sh 可跑；build.ps1 BuildDate 为真 UTC。
