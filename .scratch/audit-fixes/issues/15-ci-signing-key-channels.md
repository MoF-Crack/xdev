# 15 CI 三渠道签名公钥注入

Status: open
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
