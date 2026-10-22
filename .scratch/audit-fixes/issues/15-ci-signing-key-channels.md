# 15 CI 三渠道签名公钥注入

Status: resolved
Type: task
批次: B3
对应修复项: P0-16（方案 §4 P0-16，含复核修订的渠道清单）

## 问题

ci.yaml 注入 `signer.rootPublicKeyB64` 的仅有 :247-250（tar/zip）与 :536（DMG）；未注入的三处：**:337（deb/rpm 循环 `go build -ldflags="-s -w"`）**、**:374（AppImage amd64 步骤——v1 误标为 deb/rpm）**、**:660（arm64 AppImage，有 GitCommit/BuildDate 无公钥）** → 三渠道产物签名验证 disabled。

## 方案

- :337 所在 deb/rpm job 与 :374 所在 AppImage job **各自补 LDFLAGS 定义块**（步骤级 shell 变量不跨 step——每个含 go build 的 step 各自补，**勿按 v1 字面只补 deb/rpm job 而漏 AppImage amd64 渠道**）；:660 补公钥注入。
- **勿重复建设**：三个 release job 已有 "Verify signing secrets (hard gate)"（:228/:522/:646-649）——v1 的「可选加固」已存在。

## 验收

发布后对 deb/AppImage 产物 `xdev doctor` 无 "verification is DISABLED" 警告（或 strings 比对公钥常量存在）。

## Comments（Batch 3 实施记录）

- 改动（仅 `.github/workflows/ci.yaml`，+6/-3）：改前基线盘点全部 12 处 `go build` —— 缺公钥的正是三处：deb/rpm（改后 :326 新增 `LDFLAGS="-s -w -X .../signer.rootPublicKeyB64=$XDEV_SIGNING_PUBLIC_KEY"`，:339 改用 `-ldflags="$LDFLAGS"`）、**AppImage amd64（:376/:377，确认审计 v1 误标处）**、arm64 AppImage（:663 在既有 -ldflags 末尾追加）。每处各自定义 LDFLAGS（遵守 shell 变量不跨 step）。
- 公钥来源核实：三个 release job 均有 job 级 env `XDEV_SIGNING_PUBLIC_KEY: ${{ secrets.XDEV_SIGNING_PUBLIC_KEY }}`（release/:164、release-macos/:469、release-arm64/:595）→ 被修 step 直接继承，不会注入空值。三个 "Verify signing secrets (hard gate)" 步骤一字未改。
- 验证：YAML 解析（node + 仓库内 yaml 库）通过 + 结构断言（5 个发布渠道构建 step 全含 rootPublicKeyB64、引号平衡）；**端到端符号实证**——用与 CI 一致的 flag 串真实链接，`go tool nm` 对比：注入后多出 `...signer.rootPublicKeyB64.str`（R 段），未注入时仅空 `D` 符号 → 证明 `-X` 符号路径正确。
- 未动项（有意）：`build` job 的 Build CLI（PR/push 常规产物，fork PR 拿不到 secrets）、`release` 的 Sign manifests（构建签名工具本身，不需根公钥）。E-4 的「发布产物实机核验」需下次发版后执行（本批无法在本地产生发行产物）。
