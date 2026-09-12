# 17 fill-sha256 163 + 全量 52 manifest 重签

Status: open
Type: task
批次: B3 数据线
对应修复项: P0-02 阶段 2 + P1-12(D-07)（方案 §4 P0-02 阶段 2）
Blocked by: 13（先代码语义后数据）

## 内容

1. `xdev manifest fill-sha256` 补全 **163** 个空 SHA256（复核实测精确数；go/node 的 linux/darwin 条目需真实下载计算哈希）。
2. **全部 52 个 manifest 重签**（非 50——实测 52 个工具 yaml：37 基线 + 2026-08-08 批次 14 + dsh 1；闭环 D-07 的 16 个缺失）。**顺序必须先 fill 后签**。
3. 签名图收集改造（internal/cli/sign_manifest.go:64-86）：对每个 archive 额外写 `"os/arch#mirror"` 键 = `a.MirrorSHA256`（与官方相同跳过）。
4. 无哈希工具边界：mingw（0 哈希）/dsh（npm 型）**签不了**——与票 18 的守卫豁免口径联动（「无哈希豁免」或空图签名），本票先行决策记录。

## 验收

- 52 个 yaml 每个都有对应 .yaml.sig；签名图覆盖全部非空 sha256 与 mirror_sha256（Temurin 镜像经 #mirror 键通过）。
- 阶段 1 的「mirror 未签名剔除」逻辑在重签后对 java 镜像恢复可用。
