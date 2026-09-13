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

## Comments（Batch 5 追加 — 不需要私钥的部分已完成，2026-09-13）

用户决策：**先只做不需要私钥的部分**（`keys/priv.key` 路线见文末「密钥路线三选」），因此本票拆成「代码侧（已完成）」+「数据线（仍阻塞）」。

**已完成（票面第 3 项 + 相关基建）**：

1. **签名图收集改造 — `os/arch#mirror` 键**（票面第 3 项，原计划落在 `internal/cli/sign_manifest.go:64-86`）：
   - 实现迁移到 `internal/manifest/signature_map.go` 的 `manifest.BuildSignatureHashMap(tool)`——**规则同时被守卫复用**（重签后的覆盖完整性守卫要按同一规则判断），放在 manifest 域可避免「签名侧/守卫侧各写一份而漂移」；`internal/cli/sign_manifest.go` 改为调用它。
   - 规则：`os/arch` 记官方哈希；`os/arch#mirror` 记 `mirror_sha256`（与官方相同、或为空/占位时不重复签）；只有镜像哈希而无官方哈希时也算可签内容；完全无哈希的工具 `hasHash=false`（供「无哈希豁免」口径使用）。
   - **验证侧零改动**已确认：`downloader.verifySHAInMapForVersion` 对本版本先做 `os/arch` 精确键匹配，再做**同版本值扫描**，因此额外的 `#mirror` 键天然可用。
   - 守卫：`internal/manifest/signature_map_test.go`（5 例收集规则 + `SignatureCoversArchive` 5 例）；`internal/downloader/signature_gate_test.go` 新增 `TestFilterSignatureCoveredURLs_MirrorKeyKeepsMirrorCandidate`——用**真实过滤函数**验证三段：①`#mirror` 键存在 → 镜像候选保留；②**去掉 `#mirror` 键（=修复前）→ 镜像候选必被剔除**（把契约写成可执行断言，而非注释约定）；③攻击者自带哈希 → 剔除。
2. **mingw 数据补全**（原属票 18 P2-29，与重签同属数据线，先行完成）：见票 18 Comments。
3. **重签后自动生效的覆盖守卫**（原属票 18 边界规格）：`manifests_integrity_guard_test.go::TestManifestSignatureCoverage` ——数据线未完成时 **skip 并打印缺失清单与数量**（当前：16/52 尚无 `.sig`），重签后**无需改任何配置即自动生效**，逐版本断言「manifest 里每个非空 sha256 与独立 mirror_sha256 都出现在本版本签名图中」。

**仍阻塞（不可自行完成）**：`xdev manifest fill-sha256` 补 163 个空哈希 + 全量重签 52 个 manifest。当前盘点（2026-09-13 实测）：工具 yaml 52、空 `sha256: ""` 163、现存 `.sig` 36（另 2 个孤儿签名已按票 18 边界②删除）。

**密钥路线三选（需用户决策）**：
- **A（推荐）**：私钥不出 CI——新增 `workflow_dispatch` 作业，用现有 secret `XDEV_SIGNING_PRIVATE_KEY` 跑 fill-sha256 + sign-manifests 并回传产物；本地只做校验。
- **B**：用户提供私钥材料（`keys/priv.key`），本机执行同两条命令。
- **C**：确认密钥丢失 → 轮换（更新 `keys/pub.key` + 两个 CI secrets；已发布旧版客户端将拒绝全部新签名）。

**已核实的取证**（供决策）：`keys/` 只有 `pub.key`；`.gitignore:63` 排除 `keys/priv.key`；`git log --all --diff-filter=AD -- 'keys/*'` 全历史仅出现过 `pub.key` ⇒ 私钥不可从 git 找回。但 `ci.yaml:164` 定义了 `XDEV_SIGNING_PRIVATE_KEY`，release job（第 221-240 行）含硬门且 v0.4.0 发布成功 ⇒ **私钥存在于 GitHub Actions secrets**（secret 只写不可读，故只能让 CI 代跑 = 路线 A）。另：用 `keys/pub.key` 验签现有 38 个签名得到 **36 通过 / 2 失败（即两个孤儿签名）** ⇒ `keys/pub.key` 与当年签名私钥同源。

## Comments（Batch 5 追加 #2 — 私钥取证与信任根真相，2026-09-13）

### 更正上一节的一处不准确表述

上一节写「文档称 `keys/pub.key` + `keys/priv.key` 存在于项目树内」——不准确。实际文档立场相反：
`docs/security/procedures/signing-guide.md` §2.1 用 `xdev-keygen --pub-key keys/pub.key --priv-key keys/priv.key`
生成密钥对，§2.2 明确「`keys/priv.key` 已在 `.gitignore` 中排除，不会提交到版本控制」并建议备份到
密码管理器；`docs/history/implementation-plans/git-initial-import-plan.md` 更直接写着「**永远不得暂存或
提交 `keys/priv.key`**」，并给出 `git check-ignore -v -- keys/priv.key` 等校验步骤；ADR-0001 亦只声明
「私钥用于在 release CI 中签名 manifest」。**即：私钥被设计为不入库，而不是"入库后被删"。**

### 取证结论（回答「没删过密钥，为什么找不到」）

1. **从未入库**：`.gitignore` 规则提交于 `31fce52`（2026-07-16 00:36），**早于**基线导入 `7fd7c43`
   （2026-07-16 19:58）；`git rev-list --all --objects` 全历史只出现 `keys/pub.key`。进一步对
   **全部 94 个小 blob（含 unreachable/dangling 对象）**逐个做「内容能否推出 `keys/pub.key`」的
   密码学比对 → **0 命中** ⇒ 既没提交过，也不存在「提交后被移除、可从悬空对象恢复」的痕迹。
2. **本机没有**：按内容（不看文件名）比对候选：工作树 + `~/.ssh` + `~/.xdev` + `~/.config` +
   `%APPDATA%` + `%LOCALAPPDATA%` + `E:\XDev` + 上级目录（16 个候选文件），以及三个回收站 +
   Downloads/Documents/Temp（30 万个小文件）→ **0 命中**。
3. **信任根真相（本节最重要的发现，改变了决策依据）**：仓库 `keys/pub.key` 与 CI 里那把钥匙
   **不是同一对**。用 `gh`（账号 MoF-Crack，含 `repo` 权限）下载 **CI 构建的 v0.4.0 产物**
   `xdev-linux-amd64-v0.4.0.tar.gz`，从二进制内嵌数据中提取：
   - 内嵌签名 **51** 个（= 52 工具去掉无哈希工具，与 CI 发版签名数一致）；
   - 内嵌公钥候选中 **`4Ac61lOH0N9WR6MnVr0hyLykUJL1JP5gnEgp3A31kTw=`** 可验证 **50/51** 个签名；
   - 仓库公钥 **`kwWGRFuwxx+H9G7MORTZ9/wNVC1mBxee7RvU7sw4LaM=`** 只验证 **1/51**；
   - 唯一「仓库公钥通过、CI 公钥不通过」的是 **mingw**（ts=`2026-07-08T12:35:12Z`）——CI 因 mingw
     当时无哈希而跳过重签、保留了仓库旧签名（见票 18 的 stale sig 记录）。
   推论：**发布链路（CI 密钥）从未缺私钥且自洽**（产物内公钥验证产物内签名 50/51，剩余 1 个是被跳过的
   旧签名）。缺失的那把是**生成仓库内 36 个 `.sig`（2026-07-08 同批时间戳）所用的本地私钥**，它只影响
   「本地 keyed 构建（`build.ps1` 嵌入 `keys/pub.key`）+ 仓库内签名」这条支线。旁证：`gh secret list`
   显示两个签名 secret 的**最后更新时间为 2026-07-25T01:49Z**，而 `keys/pub.key` 仍在 2026-07-08 签名的
   验证链上（36/36 通过）⇒ 07-25 那次是**给 CI 配了一对新密钥**（与仓库那把不同源），并非替换仓库公钥。
4. **对票 17 的影响**：数据线**不需要那把本地私钥，也不需要轮换**。两条可行路线：
   - **A′（推荐，且顺带收敛信任根）**：让 CI 当唯一签发者——新增 `workflow_dispatch` 作业跑
     `fill-sha256` + `sign-manifests`（用现有 `XDEV_SIGNING_PRIVATE_KEY`），把产出的
     `manifests/*.yaml` + `.sig` 回传/提交；同时把 `keys/pub.key` **替换为 CI 公钥
     `4Ac61lOH0N9WR6MnVr0hyLykUJL1JP5gnEgp3A31kTw=`**，使「本地构建 / 发布产物 / 仓库内签名」
     共用同一信任根（对已发布客户端零影响：它们本来就嵌 CI 公钥）。这是把现状「两把根密钥」收敛为
     一把的**非破坏性对齐**。
   - **B′**：若你手上仍有当年那把本地私钥，放回 `keys/priv.key` 后本地重签（不动 CI 信任根），
     但仓库内签名与发布产物仍分属两把钥匙（＝现状补齐）。
   - **C（轮换）在现有证据下不必要**：CI 私钥可用且与发布产物自洽。
5. **尚未能证实的**：那把本地私钥是否曾存在于**本机**后被删除——NTFS USN 日志需管理员权限，回收站内
   无副本，故只能记录为「无法证实」。唯一间接线索是导入计划（2026-07-16）里的校验步骤
   （`git check-ignore` / `git log --all --full-history -- keys/priv.key`）通常用于确认"确实存在但未入库"的
   文件，暗示当时工作树里可能确有该文件。
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
