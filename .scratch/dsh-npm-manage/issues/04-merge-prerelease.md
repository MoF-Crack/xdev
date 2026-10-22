# 04: 发现合并的预发布处理

Status: done

mergeIntoManifest：
- 追加版本时 `Stable: !IsPrereleaseVersion(ver)`（稳定发现版本不再隐式 false）；
- latest 提升（**通道感知**）：预发布候选仅在 latest 本身为预发布且通道
  相同时可提升（rc→rc），稳定 latest 不被任何预发布抢占，alpha 通道
  不得劫持 rc-latest（真实 dsh 数据验证后细化：npm dist-tags
  latest=0.1.2-rc.1 而 alpha=0.1.5-alpha.1，纯 semver 规则会把默认安装
  引到 alpha）。

## Comments

- IsVersionNewer 对任何预发布串恒 false（parseNumeric 拒绝非数字段），
  是 rc-only 流 latest 停摆的根因；提升逻辑改用预发布感知的比较。
- 通道提取：`util.PrereleaseChannel`（后缀首个 "." 前的标识符）。
- 守卫：TestMergeIntoManifest_AlphaChannelNeverHijacksRCLatest 锁定
  真实 dsh 数据形态。
