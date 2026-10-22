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
## Comments（Batch 5 追加 #3 — A 方案风险评估，2026-09-13）

**方案定义**：A′ = ①CI 手动触发作业跑 `fill-sha256` + `sign-manifests`（用现有 `XDEV_SIGNING_PRIVATE_KEY`），
产出 `manifests/*.yaml` + `.sig`；②把 `keys/pub.key` 替换为 CI 公钥
`4Ac61lOH0N9WR6MnVr0hyLykUJL1JP5gnEgp3A31kTw=`，使本地构建 / 发布产物 / 仓库内签名共用同一信任根。

**收益**：一次解决 163 空哈希、16 个缺失签名、mingw stale sig、以及"仓库里有两把根密钥"；
对已发布客户端**零影响**（CI secret 不变、产物内自洽）；纯 `git revert` 可回滚，无密钥变更。

### 风险登记（概率 × 影响 → 缓解 / 检测）

| # | 风险 | 概率×影响 | 缓解 / 检测 |
|---|---|---|---|
| R1 | **私钥不可读回**：A′ 后 CI 是唯一签发者；secret 若丢失只能轮换（旧客户端拒绝新签名） | 中×高 | 现状已如此，A′ **不新增**该风险；需在文档写应急预案。可选"一次性轮换以获得可备份私钥"（生成新对 → 私钥入密码管理器 → 更新两个 secret），代价是破坏旧客户端兼容 → **建议暂不做** |
| R2 | **公钥替换与签名替换的顺序耦合**：先换 `keys/pub.key`、后换 `.sig` 会让"用仓库公钥验签仓库内签名"的检查立刻变红（本票新增的 `TestEmbeddedSignatures_VerifyAgainstCommittedPublicKey` 就是这种） | 中×中 | **同一次提交原子完成**（新 sigs + 公钥 + 文档）；提交前本地跑守卫。守卫本身就是检测器 |
| R3 | **CI 回写仓库**的写权限与分支保护：直接 push main 会绕过评审；`GITHUB_TOKEN` 推送不触发后续 workflow（掩盖 CI 状态）；分支保护可能拒绝 | 低-中×中 | **不让 CI 直接 push main**：上传 artifact + 开 PR（或推 `signing/*` 分支）由人复核；`workflow_dispatch` 手动触发，不做 push 自动触发 |
| R4 | **fill 并非 163 全能填**（实测）：直连探测 163 条 → 133 条 200/206、**4 条 HTTP 404**、26 条本机网络受限（CI 不受限）。404 全是 **manifest 用了上游旧命名**：`llvm@22.1.8` 的 `clang+llvm-22.1.8-{x86_64-linux-gnu-ubuntu-18.04,arm64-apple-macos11}.tar.xz`（上游现为 `LLVM-22.1.8-Linux-X64.tar.xz` / `LLVM-22.1.8-macOS-ARM64.tar.xz`，实测 302 可达）；`zig@0.15.1` 的 `zig-linux-x86_64-0.15.1.tar.xz` / `zig-macos-aarch64-…`（上游 0.14+ 改为 `zig-x86_64-linux-0.15.1.tar.xz` / `zig-aarch64-macos-0.15.1.tar.xz`，实测 200） | **高**×中 | **先修这 4 条 URL**（属 mingw 同类缺陷：URL 陈旧=不可安装），否则填充后仍留空 → 覆盖守卫必红 |
| R5 | **`fill-sha256` 失败仍 exit 0**：只在 stderr 打印 ❌ 并把 `totalFailed` 计入统计，命令仍返回 nil | 高×中 | 该命令**不可当门禁**；门禁必须是"空 sha256 == 0（或显式豁免清单）"或本票新增的覆盖守卫 |
| R6 | **哈希即信任锚，填错＝把自己签死**：`downloadAndHash` 只校验 `StatusCode==200`，不校验内容类型/魔数（200 的登录页/错误页会被当资产哈希）；缓存目录命中即复用（不校验大小/完整性） | 低-中×高 | CI 用干净缓存；**复核 diff**：同一 URL 的哈希发生变化是强信号（上游重传或投毒），必须逐条确认；后续可给 fill 增内容类型/魔数校验（新票）。旁证：mingw 旧签名里 `12.2.0`/`11.2.0` 的哈希与今日同名资产**不同** ⇒ 上游确实会"同名换内容" |
| R7 | **私有仓库 Actions 成本**：163 个归档 ≈ 数 GB 下载/次，单次 job 可能 10-30 分钟（私有仓库按分钟计费） | 中×低-中 | 仅手动触发；用 `actions/cache` 缓存归档；只对空哈希增量跑 |
| R8 | **dispatch 的 ref 与 main 竞争**：签出的 `.sig` 可能对应旧哈希 | 低×中 | dispatch 显式传 `ref` + 运行前 `git pull` + 产出必须过覆盖守卫才提交 |
| R9 | **`requires_proxy` 语义反转**：CI 直连通常无碍，但非 GitHub 源（`cangjie-lang.cn`、`downloads.cursor.com`、`update.code.visualstudio.com`、`archive.apache.org`）可能被出口 IP 挑战/限速 | 低×低-中 | 记录"CI 填不满清单"，必要时这些条目改由本机（带代理）填 |

### 建议的落地形态：A′-PR（含三条守卫）

1. **作业**：`workflow_dispatch`（输入 `ref`）→ 干净 checkout → 构建 xdev → `fill-sha256 --cache-dir`（配 `actions/cache`）
   → 输出 fill 报告（成功/失败清单）→ `sign-manifests` → 跑签名守卫 → **上传 artifact / 开 PR**（不直接 push main）。
2. **新增 CI 守卫（补 R2/R1 的根因：从来没人校验过两者一致）**：断言
   `XDEV_SIGNING_PUBLIC_KEY == $(cat keys/pub.key)`，不一致即 fail —— 防止"仓库公钥与 CI 公钥再次漂移"。
3. **门禁**：fill 后空 `sha256` 必须为 0（或白名单豁免）→ `TestManifestSignatureCoverage` 转绿 →
   内嵌验签守卫 `TestEmbeddedSignatures_VerifyAgainstCommittedPublicKey` 转绿。
4. **提交顺序（原子）**：CI 产出的 `manifests/*.yaml` + `.sig` + `keys/pub.key` 替换 + 文档更新
   （`docs/security/procedures/signing-guide.md` 的"本地重签"章节改写为"CI 签发"，并记录 CI 公钥指纹）。
5. **复核清单**：① 哈希发生变化的既有 URL 逐条确认；② fill 报告里的失败条目转工单；③ `index.yaml` 重新生成（本批已加命令）。

### 退路：A″（零 CI 写权限）

作业**只上传 artifact**，由我在本机校验后提交（CI 不需要 `contents: write`）。风险更低（R3 消失），
代价是仍需你点一次运行并把 artifact 交给我。

### 结论

A′ **风险可控、可回滚、不需私钥、不需轮换**。关键前置是**先修 4 条死链 URL**（R4）；
关键约束是**不要让 CI 直接 push main**（R3）与**必须补"secret 公钥 == 仓库公钥"守卫**（R1/R2 的根因）。
若要最保守，用 A″（CI 只产 artifact）。

Type: task
批次: B3 数据线
对应修复项: P0-02 阶段 2 + P1-12(D-07)（方案 §4 P0-02 阶段 2）
Blocked by: 13（先代码语义后数据）

## Comments（Batch 5 追加 #4 — 逐风险的解决方案与已落地实现，2026-09-13）

按上一节的风险登记逐条给出解决方案；**不需要私钥、不依赖任何用户决策的部分已实现并验证**（本批提交）。

| 风险 | 解决方案 | 状态 |
|---|---|---|
| **R1** 私钥不可读回 / CI 成为唯一签发者 | ① 作业内私钥只以 `0600` 落在临时 `keys/priv.key`，`trap ... EXIT` 立即删除，绝不进 artifact；② 仓库侧 `.gitignore` 继续排除；③ 在 `signing-guide.md` 写"secret 丢失＝必须轮换"的应急预案（随 A′ PR 落地）；④ **可选的"一次性轮换换取可备份私钥"**（生成新对→私钥入密码管理器→更新两个 secret）保持为待决策项——它会破坏旧客户端的"新版本发现+安装"链路（旧客户端内嵌旧公钥，无法验证新签名），故默认不做 | ①②已实现；③随 A′ PR；④待决策 |
| **R2** 公钥替换与签名替换的顺序耦合 | **结构性解决**：签名作业一次性产出「`.sig` + `index.yaml` + 可选对齐后的 `keys/pub.key`」，四者永不分开提交；`inputs.align_pubkey=true` 时作业自己把 `keys/pub.key` 写成 CI 公钥。守卫侧新增 `XDEV_MANIFEST_VERIFY_PUBKEY` 覆盖（专用变量名，避免被无关 secret 影响），使签名作业能"用发布公钥验证刚产出的签名"——实测：默认（仓库公钥）PASS，注入 CI 公钥后旧签名如实报错 | 已实现 |
| **R3** CI 写权限 / 绕评审 | 新工作流 `sign-manifests.yml`：默认 `mode=artifact` **只要 `contents: read`（零写权限）**；`mode=branch` 才授予 write，且只推 `signing/*` 分支 + 开 PR，不碰 main；`environment: signing` 人工批准门；`concurrency` 串行化 | 已实现 |
| **R4** fill 填不满（上游改名） | ① **已修 4 条死链**：`llvm@22.1.8` 的 `clang+llvm-22.1.8-{x86_64-linux-gnu-ubuntu-18.04,arm64-apple-macos11}.tar.xz` → `LLVM-22.1.8-Linux-X64.tar.xz` / `LLVM-22.1.8-macOS-ARM64.tar.xz`；`zig@0.15.1` 的 `zig-{linux-x86_64,macos-aarch64}-0.15.1.tar.xz` → `zig-x86_64-linux-0.15.1.tar.xz` / `zig-aarch64-macos-0.15.1.tar.xz`（并清掉实测四个路径全 404 的清华镜像 `mirror_url`）；② 新增 **URL 存活巡检**：CLI `xdev manifest check-urls`（HEAD-only、可选 `--mirrors`、`--allow-file` 豁免、`--json` 报告、dead 即非零退出）+ 每周 `url-liveness.yml` 工作流，把"上游改名"提前到发布前发现；③ 全量实测：**441 条唯一 URL，dead=0**（修好后），101 条 error 全是本机网络受限（github.com 85 / archive.apache.org 8 / go.dev 4 / services.gradle.org 4），CI 出口可达 | 已实现并实测 |
| **R5** `fill-sha256` 失败仍 exit 0 | **改代码**：默认 `totalFailed > 0` → 返回错误（非零退出）；`--allow-partial` 显式豁免；新增 `--report <json>` 逐条机读结果（tool/version/os-arch/url/status/hash/error）；工作流再加"空 `sha256` 必须清零"的兜底门禁 | 已实现（含 6 组守卫测试） |
| **R6** 哈希即信任锚，填错＝签死 | **改代码**：下载后按声明的 `format` 做**魔数校验**（zip/gzip/xz/zstd/tar/7z/exe-msi/dmg/shell），200 的错误页/登录页一律拒收并删除；缓存命中也要过同一校验（`--no-cache` 可完全忽略缓存）；`--retries` 处理瞬时失败。工作流把「哈希发生变化的既有 URL」写进 job summary 供人工复核 | 已实现（含 9 组用例，含 dmg 尾部 `koly` 特例、损坏缓存重下） |
| **R7** Actions 分钟数 | 工作流缓存归档目录（`actions/cache`，key 用 manifests hash）+ 新增 `--tool` 增量填充（只重试个别工具）；`timeout-minutes: 90` | 已实现 |
| **R8** dispatch 的 ref 竞争 | 作业开头记录 base SHA，产出前 `git rev-parse` 复核未移动，移动即报错要求重跑；artifact 名与 PR 正文都带 base SHA | 已实现 |
| **R9** 非 GitHub 源在 CI 出口可能被挑战 | `check-urls` 区分 `dead`（4xx/5xx）与 `error`（网络），**默认只有 dead 会失败**（`--fail-on-network-error` 可切换）；`manifests/url-allowlist.txt` 提供带原因的豁免机制 | 已实现 |

**下一步（需要你决策/操作）**：
1. 选 **A′-PR**（默认 artifact，零写权限）还是 **A′-branch**（自动开 PR）；两者都只需你点一次 `Run workflow`。
2. 报告默认用**仓库 `keys/pub.key`**；若勾选 `align_pubkey`，作业会把仓库公钥对齐为 CI 公钥（信任根收敛，本 PR 一并复核）——建议勾选，否则本地 keyed 构建仍无法验证 CI 产出的签名。
3. 是否要我顺带把 `signing-guide.md` 改成「CI 签发」流程（含 secret 丢失应急预案与公钥指纹）——已计划随 A′ PR 落地。

## Comments（Batch 5 追加 #5 — 落地后回归复核，2026-09-13）

**复核方法**（不只跑测试，而是把"只在 CI 才会炸"的面向也验一遍）：
① `actionlint` v1.7.12（本机 go install 构建）静态校验**全部 4 份工作流**；
② 自写探针抽出所有工作流的 `run:` 片段（46 段）交给 Git-bash `bash -n` 做 shell 语法校验（含 heredoc 缩进）；
③ 用**真实二进制** `--help` 复核工作流里写的每一条命令与 flag；
④ 全量 `go build/vet ./...` + `go test ./... -short -count=1`（29 包）+ 新增单测。

**发现并修复的 4 个真回归（全部在我本批新增的代码/工作流里）**：

1. **工作流命令路径错**（CI 会直接拒绝执行）：初稿写的是 `./xdev manifest fill-sha256` 与
   `./xdev manifest sign-manifests`，但这两个是**顶层命令**（`xdev fill-sha256` / `xdev sign-manifests`），
   只有 `manifest index` / `manifest check-urls` 在 `manifest` 子命令组下。已用真实二进制 `--help`
   逐条核对后纠正（文档 `docs/security/procedures/signing-guide.md` 里写的 `xdev fill-sha256` 才是对的）。
2. **`permissions` 不支持表达式**：actionlint 实测报错
   `"${{ inputs.mode == 'branch' && 'write' || 'read' }}" is invalid as permission of scope "contents"`
   → 工作流非法。改为**两个 job**：`sign`（固定 `contents: read`，零写权限）+ `publish`
   （`if: inputs.mode == 'branch'`，才授予 `contents/pull-requests: write`）。
3. **门禁口径错误（会必然失败）**：原稿用 `grep 'sha256: ""'` 当"空哈希必须清零"的硬门禁，但
   **npm 型条目（dsh）按设计保留空 `sha256`**（不下载归档、完整性由 npm 负责）→ 每次都会红。
   改为：以 `fill-sha256` 的退出码（失败即非零）作硬门 + summary 里**按工具列出剩余空哈希**供人工判断；
   机器判据继续由覆盖守卫承担。
4. **魔数校验误伤合法载体**：新加的 `exe` 校验起初只认 PE(MZ)/OLE(MSI)，而
   **Cursor 的 Linux 条目声明 `format: exe`、实际下发 AppImage（ELF）**（本次用"format vs URL 后缀"
   扫描发现）→ 会把原本能填的条目变成填失败。已扩为 PE/OLE/ELF/Mach-O（小端/大端 32/64 位 + fat）
   并补单测；随后单测又抓到我漏掉 **Mach-O 小端 magic**（`CF FA ED FE`，macOS 最常见形态）的笔误，一并修正。
5. 附带清理：删除对不存在文件 `--body-file .github/signing-pr-body.md` 的引用（改用 `--body`）；
   `publish` job 改为**从 artifact 落地文件后再提交**（不依赖两个 job 之间隐式共享工作区）。

**同批复核确认无问题的项**：
- 495 条归档 URL 的「声明 format vs URL 后缀」冲突 = **0**（故魔数校验不会因格式声明错误而误伤）；
- 工作流用到的 action 版本均已用 GitHub API 逐一确认存在（`download-artifact@v7`、`upload-artifact@v7`、
  `checkout@v7`、`setup-go@v7`、`cache@v4`）；
- 新增子命令与 flag 与工作流用法逐字对齐（`--report/--allow-partial/--no-cache/--retries/--tool` 全在）；
- `manifests/url-allowlist.txt` 不会被任何 `*.yaml` 过滤器（LoadCatalog / 守卫 / index 生成）误读；
- 仓库内没有断言命令清单/帮助输出的测试，故新增 `manifest check-urls`、`manifest index` 不影响既有断言；
- `bash -n` 报出的 5 处语法失败全在 `governance.yaml`（那些步骤是 `shell: pwsh`，属探针误报），与本次改动无关。

**仍只能由真实 CI 运行验证的项（本地无法证明，如实标注）**：① `environment: signing` 批准门的实际行为；
② `actions/cache` 命中率与 163 条归档在 CI 出口的填充成功率（本机网络受限：441 条探测中 101 条 GitHub URL 超时，
CI 出口不受限）；③ `mode=branch` 下 PR 创建权限；④ 填充后 `TestManifestSignatureCoverage` 由 skip 转绿的实测结果。

## Comments（Batch 5 追加 #7 — 第四轮复核：对第三轮修复再复核，2026-09-13）

用户要求"修复之后还要再复核一轮，看是否引入新问题"。第四轮只审**第三轮新写的代码**，又发现两组问题。

### H-7 文本级回填器自身的边界缺陷（第三轮的修复引入了新解析逻辑）

| 缺陷 | 后果 | 修复 |
|---|---|---|
| version 块边界只看"下一个同缩进 `- version:`" | 版本列表之后若有别的顶层段（`remote:`/`latest:`）且其中含 `- os:/arch:` 列表项，搜索会扩到文件末尾 → **跨段顶替**（把别处的同名条目当成目标改掉） | 块结束判定加入"回退到顶层键"（缩进 < 版本项缩进即停） |
| 目标字段只看 `TrimSpace` 前缀 | 归档项内部**嵌套映射**里的 `sha256:`/`url:` 被误当目标 → 哈希写到更深缩进，真正字段仍为空（语义损坏且静默） | 字段匹配限定在该归档项自身的字段缩进；注释行/空行跳过 |
| 调用方用 `changed != len(pending)` 判"没写全" | 把"值本来就一样"的幂等情形误报为失败 | 改用 `len(missed) > 0`（missed 只表示"定位不到"） |
| 死代码 `_ = idx` / `_ = eol` / `_ = filepath.Dir` | 噪音 | 清理 + 去掉无用 import |

新增 6 个边界守卫，其中两个是**真区分点**（关掉加固即红，已实测）：
`NoCrossSectionSubstitution`（红证据：`changed=1` 误改 remote 段）、`IgnoresNestedFields`（红证据：嵌套字段被改动）；
另有 `DoesNotLeakIntoLaterSections` / `IgnoresCommentedStructure` / `VersionQuoteStyles`（裸/双引号/单引号 + `1.0.0` 不匹配 `1.0.0-beta`）/ `AlreadyCorrectIsNotMissed`。
真实全量干跑（仓库 manifest 副本）：40 文件、163 请求 → 163 写入、**missed=0**、回填后 YAML 全合法、注释与行数不变。

### H-8 宿主机快照守卫两次假红 → 顺藤摸到**真实的宿主污染**（本轮最重要的发现）

现象：3 轮全量里 2 轮 `internal/testkit/hostsnapshot` 被自己的守卫判红，明细为
`[env] GOROOT before="C:\...\Temp\TestSetUserEnvVarTrusted_...\install\go\1.26.5\go" after=""`；单跑该包总是绿 → 并发触发。根因两层：

1. **守卫设计缺陷**：`plugins/` 目录按**全指纹**（含目录 mtime）跟踪，而目录 mtime 会在任何子文件创建/删除时变化（含无关进程的瞬时活动）→ 设计上必然假红。
   修复：新增 `Options.DirEntryPaths`（只比**条目集合**，不看 mtime/size），`plugins/` 改走它；`config.yaml` 仍走全指纹。守卫：`TestCapture_DirEntryPaths_MtimeChurnIgnoredButEntriesCaught`（mtime 抖动不报、条目变化必报且报告列出文件名）。
2. **真正触发者**：`internal/env/goroot_cleanup_test.go`（票 01 的守卫测试）直接写**开发者真实的 HKCU\Environment `GOROOT`**，靠 `defer` 恢复。两个后果：
   - 并发跑全量时另一个包的快照窗口必然可能撞上这次写 → 假红（实测 2/3 轮，明细里 before 值就是临时路径）；
   - 更危险：测试进程被强杀（CI 超时、管道截断）时 defer 不执行 → 用户真实 `GOROOT` **永久留在临时路径**上，正是 P0-17「package fmt is not in std」的病因。同类风险还存在于 `internal/env/win_path_test.go`（真实 PATH RMW）、`internal/gui/app_initconfig_test.go`（InitConfig 追加真实 PATH；其注释里就有"清理历史死锁测试残留 Temp 目录"的痕迹，说明残留确实发生过）、`internal/cli/profile_export_whitelist_test.go`（真实 JAVA_HOME/MY_SECRET）。
   修复（本轮只做 GOROOT 这一处，因为它已被实测触发）：为 `CleanupStaleGOROOT` 加注入接缝 `gorootGetFn`/`gorootDeleteFn`/`gorootBroadcastFn`（生产默认指向真实注册表实现，行为零变化），测试改用**内存假注册表**：判定逻辑一字未改、宿主零接触；另新增 `DeleteFailurePropagates`（删除失败必须上报且不广播）与 `TestGOROOTCleanupTests_NeverTouchRealRegistry`。
3. **守卫增加持久性判定**（结构性消除这类假红）：`classifyChanges` 对被判为变化的条目做短窗口重采样（5×1.5s）——差异自行消失 → 判为"他包在途写"、打印 WARNING 但不阻塞；仍存在 → 判为真污染、阻塞。守卫：`TestClassifyChanges_TransientVsPersistent`（两个方向都钉死）。**这不是放宽标准**：真正要拦的"留在宿主上的污染"（含历史事故里的 `config.yaml` 被覆盖、`plugins/e2eplug.lua` 被写入）仍是**持久**差异，照旧阻塞。

**验证（3 轮连续全量 + 200ms 宿主观测器 2596 次采样）**：`FAIL=0 / 阻塞报警=0 / 瞬时降级=0`（三轮一致）；观测器显示 `config.yaml` 与 `plugins` 指纹**全程零变化**；用户真实 `GOROOT` 保持为空（票 01 的设计状态）。
**遗留（新增待办，非阻塞）**：把 env PATH / gui InitConfig / cli JAVA_HOME 这几处"故意写真实环境"的测试也改成接缝注入或加崩溃日志（SIGKILL 残留风险）；本轮只修了已被实测触发的 GOROOT 一处，其余如实登记。

## Comments（Batch 5 追加 #8 — 第五轮：#7 遗留清单全量根除 + 包级防污染闸门，2026-09-14）

上一条登记的遗留清单**全部清零**，并把防护从"逐用例自律"升级为"包级不变量"。除登记的 3 处外，复扫又揪出
**4 处**漏网写点与 **2 个真实缺陷**（其中一处是我第一轮复扫被 `Select-Object -First 15` 截断输出坑掉的——闸门替我抓住了它）。

### 改造清单（7 个写点全部接缝化 / 内存化）

| # | 写点 | 改造 |
|---|---|---|
| 1 | `internal/cli/profile_export_whitelist_test.go`（真实写 JAVA_HOME/MY_SECRET） | 注入 `env.UseInMemoryUserEnvForTesting({JAVA_HOME, MY_SECRET})`；断言口径改为"注入映射里非白名单个数"（与宿主状态无关，任何机器确定值）；删除 save/restore 夹具 |
| 2 | `internal/gui/app_initconfig_test.go`（InitConfig 追加真实 PATH） | 内存 PATH 后端；断言反而变强（直接验证 current/xdev 被追加）+ 新增"真实 PATH 与切换前逐字符一致"宿主断言（第一版断言读错侧被自己的用例抓到后修正） |
| 3 | `internal/env/win_path_test.go` + `path_rmw_test.go`（真实 PATH RMW） | `saveUserPath` 只读种子 + 内存后端；`UseInMemoryUserPathForTesting` 还原语义改**还原前值**（原硬编码回真实实现——TestMain 包级后端下嵌套安装会互相撕掉） |
| 4 | `internal/env/manifest_env_validation_test.go`（X-08 值校验契约，整族写真实 HKCU） | env 变量**三操作接缝组** `getAllUserEnvVarsFn`/`userEnvWriteFn`/`userEnvDeleteFn`（此前只有枚举走接缝）；TestMain 包级后端种子=真实快照，save/restore 保留为第二道保险，`assertNoWrite` 语义不变（值若被写入会出现在内存里，一样抓住） |
| 5 | `internal/env/regenum_test.go`（P0-07/N-10 守卫，写 300 字符名 + 70KB 值） | R1/R2 默认在内存后端上验证接缝层；R3（`EnumEnvVars` 真身 RegEnumValueW 缓冲重建**必须**真实数据驱动）默认显式 skip + 文档化，`XMDEV_ALLOW_REAL_REGISTRY_TESTS=1` 全量真身（已实跑：绿 + 零残留） |
| 6 | **缺陷**：`SetEnvVar`/`DeleteEnvVar` user 级**直写注册表**（自带校验集，绕过全部接缝=闸门缺口） | user 级路由经 `userEnvWriteFn`/`userEnvDeleteFn`（落盘语义零变化）；machine/非法级保留通用路径 |
| 7 | `internal/testkit/hostsnapshot/hostsnapshot_test.go` `TestCapture_DetectsUserEnvWrite`（守卫自检写真实 GOROOT——残留=用户 Go 工具链直接坏） | 改命名空间探针 `XMDEV_HOSTSNAPSHOT_PROBE` + 内存接缝读写；Capture+Diff 检测语义逐字不变 |

### 新发现缺陷：`GetUserEnvVarsBatch` 白名单过滤（改造 7 的过程中踩中）

批量读取对**调用方显式给出的名单**做白名单过滤——非白名单名被**静默丢弃**，与"按名单读取"的契约矛盾
（非 Windows 桩从不过滤，两平台行为不一致即佐证）。hostsnapshot 自检的命名空间探针名会因此完全失明。
唯一生产调用方 `DefaultOptions` 只传白名单名 → 移除过滤对生产行为零影响。守卫：
`TestGetUserEnvVarsBatch_ReturnsExplicitlyRequestedNonWhitelistedNames`（红探针：恢复过滤即红，实测）。

### 包级防污染闸门（P0-17 事故类的结构性根治）

`internal/env/userenv_hermetic_test.go`（全包唯一 TestMain）：
- 为整个测试二进制安装**双内存后端**（PATH + 用户环境变量，种子=启动时真实注册表快照）——任何用例（含未来新增）从结构上写不到真实 HKCU；
- 每个真实注册表写/删函数入口埋 `realUserRegistryMutationCount`（**尝试即计数**，先于任何注册表访问——M-2 长度拒绝/编码失败/删不存在值都算）；TestMain 跑完断言 `增量 == expectedRealRegistryMutationCount`（守卫用零落盘尝试声明：超长 PATH、NUL 值、删确认不存在的探针）→ 任何未声明的真实写/删尝试**整包失败**，无论发生在哪个用例、是否又"恢复"了；
- 逃生口 `XMDEV_ALLOW_REAL_REGISTRY_TESTS=1`：停用内存后端与计数断言，供真实机件集成测试（regenum R3 等）——已实跑通过且零残留；CI runner 是一次性环境，未来加 CI 测试 job 可设以换取全量真身覆盖；
- K2（锁降级守卫）重写为**锁路径接缝注入**（原靠 `t.Setenv("USERPROFILE", <file>)`，包级后端下锁路径已指向临时目录，必须自己控制接缝才能确定性触发）。

### 守卫与红探针（先红后绿，全部实测）

| 守卫 | 钉死的不变量 | 红证据 |
|---|---|---|
| `TestUserEnvSeam_WriteAndDeleteNeverTouchRealRegistry` | trusted 写/删经接缝，真实注册表零接触（真实枚举里出现探针=红） | ①写接缝绕过：守卫在可见性断言红 + TestMain 计数违规 + 真实 HKCU 出现探针（已 `reg delete` 清理并枚举验证）；②删接缝绕过：守卫在"删除后探针应从内存后端消失"红 + 计数违规 + **真实注册表零残留**（探针设计的安全性同时得到实证） |
| `TestUserPathSeam_RealWriterCountsAttemptBeforeRegistry` | PATH 真身入口埋点先于注册表访问（40000 字符被 M-2 拒=零落盘尝试） | 移除埋点：delta=0 红 |
| `TestUserEnvSeam_RealWritersCountAttemptBeforeRegistry` | env 写/删真身入口埋点（NUL 值编码失败 / 删不存在值=零落盘尝试） | 移除埋点：delta=0 红 |
| `TestGenericEnvVarAPIs_RouteUserLevelThroughSeam` | SetEnvVar/DeleteEnvVar user 级经接缝 | 撤销路由：红 + 真实 HKCU 出现探针（已清理验证） |
| `TestGetUserEnvVarsBatch_...` | 批量读取返回显式名单 | 恢复过滤：`ok=false` 红 |

TEMP-RED-PROBE 标记全仓 grep 为零。

### 验证

- gofmt 干净；`go vet ./...` 干净；`GOOS=linux go build ./...` 交叉编译过（env 包大改后确认非 Windows 侧无破坏）；
- 全仓 `go test ./... -short -count=1`：hostsnapshot 收尾改造前 3 轮全绿（exit 0）；最终态共 5 轮 —— A/C/D/E 四轮 exit 0，**B 一轮 exit=1 但过滤管道未捕获任何 FAIL/--- 行**（当时未落盘全量日志；紧随其后的 C 轮全绿且宿主终态逐字节一致，无法复现）→ D/E 改为完整日志落盘复跑均绿、非 ok 行仅 3 条良性 `[no test files]`。如实记录：B 轮无日志可考，不据此下"无回归"结论，仅以可复现的 D/E 全量日志为准；
- 宿主终态（多轮后）：真实 PATH **1301 字节、sha16=8FB6AAB0087E7B4B 与批前逐字节一致**；`GOROOT` 空、`JAVA_HOME` 不变、`MY_SECRET` 从未存在；`XMDEV_*`/`XDEVTEST*` 探针零残留；
- 逃生口模式（`XMDEV_ALLOW_REAL_REGISTRY_TESTS=1`）整包绿，真实探针随 t.Cleanup 全清（枚举验证零残留）。

### 顺带实证的历史残留（本批之前的旧跑遗留，待用户决策是否清理）

真实用户 PATH 里现存**两条死条目**，指向早已不存在的 `TestInitConfig_NoDeadlock2229425286` 临时目录：
`PATH[0]=C:\Users\xinyan\AppData\Local\Temp\TestInitConfig_NoDeadlock2229425286\001\shims`（**排在最前**，拖慢每个进程启动）与
`PATH[35]=...\001\current\xdev`。这正是 #7 里"gui 测试注释有清理残留痕迹"的实锤——改造前的强杀/提前退出留下的。
批前批后 PATH 逐字节一致（1301 字节）证明**不是本批产物**；是否清理由用户定夺（动真实 PATH 属宿主手术，不随本提交）。

### 遗留记录（非阻塞）

- R1/R2（300 字符值名缓冲重建）的真身健壮性在默认防污染模式下不再被驱动——由逃生口模式覆盖；若未来加 CI 测试 job，runner 上设 `XMDEV_ALLOW_REAL_REGISTRY_TESTS=1` 即可常态全量覆盖（一次性环境无宿主安全顾虑）；
- internal/cli、internal/gui 的测试二进制**不经过** env 包的 TestMain（接缝保持生产默认值）——它们的宿主安全仍由 hostsnapshot 守卫（RunWithPinnedHome/RunWithHostSnapshot）承担；若未来需要同强度闸门，可各自加 TestMain + `UseInMemory*ForTesting`；
- **既有观察（已收口）**：`gofmt -l` 扩到全仓时第一方共 **79 个**本批未触碰文件存在手工格式漂移（早前批次遗留）。经用户确认，以独立 hygiene 提交 `9cd1102` 全部 `gofmt -w`（纯机械改动，377+/370-）；vendor/ 明确排除（.gitignore 本就忽略；曾被误 -w 已 `go mod vendor` 从模块缓存原样重建，仓库零影响）。此后 `gofmt -l` 于第一方代码为 0。另：**历史 PATH 死条目已按用户确认清理**（36→34 条目、逐字符校验、REG_EXPAND_SZ 保持、已广播；备份在 `%TEMP%\path_backup_*.txt`）。



## Comments（Batch 5 追加 #6 — 第二轮回归+安全复核，2026-09-13）

用户要求"再复核一遍是否引入新回归/安全隐患"。方法：逐行重读本批新增的三个 Go 文件与两份工作流，
对照仓库既有安全基线（`util.SecureRedirectCheck` 的使用面、票 23 的原子写承诺、`.gitignore` 的私钥规则），
再跑全量门禁。**结论：发现 3 个真实隐患（1 高 2 中），全部已修并加守卫。**

| # | 隐患 | 等级 | 修复 |
|---|---|---|---|
| H-1 | **fill 的下载没有 SecureRedirectCheck**：`downloadAndHash` 用裸 `http.Client{Timeout}`，默认跟随最多 10 跳重定向到**任意协议/主机**。而仓库其它全部下载路径（`downloader.go:335`、`update.go:171/341/578`）都强制 `util.SecureRedirectCheck(10)`（限跳数、拒 HTTPS→HTTP 降级、每跳 SSRF 校验——审计项 H-8/N-6/N-9 的产物）。更严重的是：**这个函数产出的是要被签名的哈希（信任锚）**，攻击者控制重定向即可让 CI 哈希并签署任意内容 | **高** | `downloadAndHash` → `fillHTTPClient()` 集中挂 `SecureRedirectCheck(10)`；新增测试接缝 `fillHTTPTransportHook`（与 `signer.testHookDirectTransport` 同模式：只注入 Transport，不触碰重定向策略，保证测的是生产代码路径） |
| H-2 | **check-urls 的探测同样无重定向策略**，且未限制 scheme（理论上可被 `file://` 等触发奇怪的客户端行为） | 中 | 同样挂 `SecureRedirectCheck(10)`；新增 `isHTTPScheme` 预检，非 http/https 一律判 `error` 并写明原因（不进 dead 计数） |
| H-3 | **`writeManifestYAML` 非原子写**（`os.WriteFile`）：fill 每填完一个工具就整文件回写，CI 超时取消（常态）会留下**半截 manifest**——违背票 23 已兑现的"manifest 域写盘全部原子化"承诺 | 中（回归类） | 改用 `util.AtomicWriteFile`（同目录临时文件 + rename），补 round-trip + 无 `.tmp` 残留断言 |

新增守卫：`internal/cli/fill_security_test.go`（HTTPS→HTTP 降级必须被拒 + 拒绝后不留半截文件；正常 https 不被误伤；
原子写 round-trip）；`check_urls_test.go` 增加 `file://`/`ftp://` 判 error 用例。

**复核后确认无问题的项（第二轮）**：
- 私钥处理：`umask 077` + `trap rm`（同 step 退出即删）+ 不进 artifact；`publish` job 的 `git add` 显式只加
  `manifests` 与 `keys/pub.key`（私钥在 .gitignore 且从未被 add）；
- `align_pubkey` 写入 `keys/pub.key` 的是**公钥**（本来就是公开值）；
- `publish` 只推 `signing/*` 分支并开 PR，不碰 main（分支保护不绕过）；
- fill 报告/CI 摘要只含公开 manifest 的 URL 与哈希，无任何密钥材料；
- `--retries` 负值有下限保护（`attempts < 1` → 1）；`--tool` 过滤不影响未选中工具的文件（不重写）；
- 缓存目录默认在 `os.TempDir()`（非仓库内），损坏缓存会被魔数校验拒掉并重下；
- `check-urls` 的 URL 全部来自 manifest（无用户输入 URL 参数）→ 无 SSRF 注入面；
- `verifyPublicKey` 的 env 覆盖用专用变量名 `XDEV_MANIFEST_VERIFY_PUBKEY`（release job 导出的
  `XDEV_SIGNING_PUBLIC_KEY` 不会误触发）；非法值会 fail-fast 报清晰错误而非静默通过。



## Comments（Batch 6 追加 #7 — 第六轮专项审计：数据线 5 缺陷修复 + 端态要求登记，2026-09-25）

对 fill→sign→verify 全链做第六轮对抗审计（四路并行 + 人工复核），发现数据线在
**运行时序**上有一个 P0 级正确性缺陷（此前五轮全部漏检——它只在「真实执行
build→fill→sign 工序」时发作，单测因测试二进制不嵌清单而构造不出）：

| # | 缺陷 | 等级 | 修复（commit f08ff07） |
|---|---|---|---|
| F1 | **sign-manifests 签的是构建前的嵌入快照**：`LoadCatalog` 在已构建二进制里走 embed-first 合并（`*merged = *embeddedTool`），同版本磁盘归档字段被丢弃；工序 build→fill→sign 下，fill 写好的 163 个哈希**永远进不了签名图**——sig 与同 artifact 的填充后 manifest 互不一致，合并后 keyed 构建把本线要修的每个条目拒装（"possible hash tampering"） | **P0** | sign/fill 改用 `LoadToolManifests` 磁盘专用加载（与 `xdev manifest index`、check-urls 同源），签名=磁盘实际内容 |
| F2 | `TestManifestSignatureCoverage` 永久死代码：按全集收集 unsigned 后立即 Skipf，而 dsh（npm 型）**按设计永远无 .sig**→豁免集永远非空→skip 永远先触发——覆盖断言在数据线完成前后都从未执行，恰好掩盖 F1 | P1 | 无哈希豁免集先于 skip 计算；unsigned 只从「可签」集合收集；重签后守卫真实生效 |
| F3 | fill/check-urls **初始 URL 零校验**（CheckRedirect 只管第 2..N 跳）：http 明文起点 + 私网/169.254.169.254 全裸奔，失败信息的响应前缀还进公开报告——定时跑在公网 runner 上的巡检=SSRF 探针 | P1 | 两处初始 URL 过 `util.IsSafeExternalURL`（产品统一基线：https 强制、拒私网/保留地址、localhost 例外交给测试夹具） |
| F6 | 缓存键缺 URL 维度：同 (tool,ver,os,arch,format) 换 URL 时静默哈希旧字节进签名图；工作流 `restore-keys:` 前缀回退构成跨运行投毒链 | P2 | `<dest>.url` sidecar 证明字节来源（不匹配即重下）+ 删 `restore-keys` |
| F7 | manifest 供给的 name/version/os/arch/format 未做路径安全校验——`os: "../../.."` 可把下载字节写到缓存外、.sig 写到 manifests 外 | P2 | `fillCacheFilename`/`validateManifestPathName` 逐分量校验 + Join 后包含性断言（三处消费点全覆盖） |

另修：F8（`inputs.ref` ${{ }} 内插进 run: 脚本）、F10（fill 报告把未落盘哈希记为
filled）、F11（WriteSignatureFile 原子化）、isAllZeroHex 收敛到 util.IsAllZeroHash。
**工作流信任边界（F4/P1）重构**：ref 只进 `ref/` 子目录作数据，代码一律取
github.sha 构建——任意 ref（含 fork `refs/pull/N/head`）的代码不再携带私钥运行。

**端态要求（F5 登记，数据线完成后必须回来翻转）**：两个生产调用方把
`AllowPlaceholderTOFU` 派生为「哈希为空/占位」——L-10 拒绝门在 keyed 构建下对空哈希
条目恒跳过（TOFU 兜底）。这是数据线未完成的**有意过渡策略**（否则 keyed 发布直接
拒装 17/52 工具）；已用 `TestPlaceholderTOFU_TransitionalPolicy` 钉死两端语义（过渡
放行 + 端态可达）。**本线 fill+sign 落地后，installer.go/app_install.go 两个调用方
应改为 false，让 L-10 门真正生效；届时该测试的「transitional」子测试必须同步翻转。**

新守卫：`internal/cli/fill_round6_guard_test.go`（F3×3/F6/F7×2/F10）+ 既有 4 处
PreviewCustomToolVersions 无关；`internal/downloader/placeholder_tofu_policy_test.go`。
全门：gofmt/build/vet/linux build + `go test ./... -short` 30 包 + vitest 236 +
vite build + e2e 21/21 全绿。



## 内容

1. `xdev manifest fill-sha256` 补全 **163** 个空 SHA256（复核实测精确数；go/node 的 linux/darwin 条目需真实下载计算哈希）。
2. **全部 52 个 manifest 重签**（非 50——实测 52 个工具 yaml：37 基线 + 2026-08-08 批次 14 + dsh 1；闭环 D-07 的 16 个缺失）。**顺序必须先 fill 后签**。
3. 签名图收集改造（internal/cli/sign_manifest.go:64-86）：对每个 archive 额外写 `"os/arch#mirror"` 键 = `a.MirrorSHA256`（与官方相同跳过）。
4. 无哈希工具边界：mingw（0 哈希）/dsh（npm 型）**签不了**——与票 18 的守卫豁免口径联动（「无哈希豁免」或空图签名），本票先行决策记录。

## 验收

- 52 个 yaml 每个都有对应 .yaml.sig；签名图覆盖全部非空 sha256 与 mirror_sha256（Temurin 镜像经 #mirror 键通过）。
- 阶段 1 的「mirror 未签名剔除」逻辑在重签后对 java 镜像恢复可用。
