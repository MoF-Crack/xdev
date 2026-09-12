# 17 fill-sha256 163 + 全量 52 manifest 重签

Status: blocked（签名私钥 keys/priv.key 缺失——无法执行重签；见 Comments）

## Comments（Batch 3 实施记录 — 阻塞）

盘点头（2026-09-12 实测）：
- `manifests/*.yaml` = **53**（含 index.yaml）→ 工具 yaml 52 ✓ 与票面一致
- 空 `sha256: ""` = **163** ✓ 与票面一致
- 现存 `manifests/*.yaml.sig` = **38** → 差 14-15，与 D-07/D-15 吻合
- `xdev manifest fill-sha256` 命令存在 ✓

**硬阻塞：签名私钥不可得**。逐项核实：
1. `keys/` 只有 `pub.key`；`keys/priv.key` 不存在（`.gitignore` 有 `keys/priv.key` 规则——私钥从未提交，且本地也未留存）；
2. 环境变量无 `XDEV_SIGNING_PRIVATE_KEY` / 任何 `*SIGN*` 变量；
3. `E:\XDev\keys`、`~/.xdev`、`.github/` 下均无 `*priv*` 文件；
4. 文档 `docs/*.md` 称「keys/pub.key + keys/priv.key 存在于项目树内」——**该前提已不成立**（文档与现实漂移，本身应记录）。

**为什么不能绕过**（这是必须由掌握私钥者执行的原因，不是可以自行决定的事项）：
- 重签必须用**与内嵌 `keys/pub.key` 配对的私钥**；新生成密钥对意味着**密钥轮换**：需同时更新 `keys/pub.key`、CI secret `XDEV_SIGNING_PUBLIC_KEY`，且**已在用户手里的旧版 xdev 会拒绝全部新签名**（旧公钥无法验证新私钥签名）——属破坏性变更，需发布策略决策。
- 半途执行（只 fill 不签）会使签名图与新哈希**不一致**：阶段 1 的逐 URL 过滤会把原本放行的 mirror/官方候选判为「未入签」而剔除，反而比现在更坏（票 18 的守卫也会红）。

**可先行的部分（待私钥到位后一并执行）**：
- 票面第 3 项「签名图收集改造（sign_manifest.go:64-86 增 `os/arch#mirror` 键 = `a.MirrorSHA256`，与官方相同则跳过）」是**纯代码改动，不依赖私钥**，可与票 18 部分内容一起实现；本次未动（避免与数据线交付割裂，且改造后未重签的树上 `#mirror` 键无消费者）。
- 票 18 的 index.yaml 重建、mingw 分支标记、CI 守卫 job 骨架同样不需要私钥，但按票面「守卫依赖重签后状态」不可单独上线（第一天必红）。

**解除条件**：提供 `keys/priv.key`（或 CI 私钥 secret 注入本机方式），并确认「沿用现有密钥、不做轮换」；若确认轮换，需要额外的发布策略决策（旧版兼容窗口处理）。
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
