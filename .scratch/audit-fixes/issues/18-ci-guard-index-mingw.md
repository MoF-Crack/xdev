# 18 CI 守卫 job 四边界 + index 重建 + mingw 处置

Status: blocked（后置于票 17；票 17 因私钥缺失阻塞）
Type: task
批次: B3 数据线
对应修复项: P2-15(D-15) + P2-29 + P0-02 阶段 2 守卫规格（方案 §6 P2-15/P2-29）
Blocked by: 17

## 内容

1. **CI 守卫 job 规格**（P1-12/P0-02 阶段2/P2-15 三处需求合并为一份）：`yaml↔sig 一一对应` + 签名图覆盖全部非空 sha256/mirror_sha256 + index.yaml 差集。**四个边界不定义则守卫第一天就红**：
   - ① index.yaml 无 sig → 豁免；
   - ② 孤儿 sig：placeholder.yaml.sig 与 .placeholder.yaml.sig 无对应 yaml，前者被 main.go:30-31 go:embed 打进二进制——先从 embed 清单移除或补 yaml，再上守卫；
   - ③ sign-manifests 对无哈希工具跳过（:82-86）→ mingw/dsh 需「无哈希豁免」或空图签名，否则差集恒红；
   - ④ mingw 陈旧 sig 处置随 P2-29 分支同步。
2. **D-15**：index.yaml 重建（实测收录 37、缺 15 工具、日期 2026-06-05 过期）——构建脚本生成或守卫差集断言。
3. **P2-29 mingw 处置**：补全（url/format/sha256/strip——remote 指向 niXman 仓库，产物为 **.7z**，引入运行时 7z 依赖需评估 A-03 欠账）或下线（`xdev available` 不再展示）；**archives 缺 url/format 的条目被 LoadCatalog/doctor 标记「不可安装」**（标记先于/后于下线决策——先定分支再动标记）；旧 sig 处置写入分支方案。

## 验收

CI 守卫 job 在当前树 + 重签后全绿；`xdev available` 不再展示不可安装条目（或 mingw 可正常安装）。

## Comments（Batch 5 实施记录 — 不需要私钥的部分已全部完成，2026-09-13）

用户决策：mingw **补全**（而非下线）；票 17/18 先做不需要私钥的部分。

### 1. CI 守卫规格 → 落成「Go 测试 + 自激活」形态

守卫实现为 `manifests_integrity_guard_test.go`（仓库根，package main，可访问 `main.go` 的 `go:embed` 文件系统与工作树）。之所以不新开 CI job：仓库既有的三个平台 job 都跑 `go test ./...`，守卫随测试自动进入 CI，无需维护第二处清单；同时避免与 `xdev manifest index --check` 重复。

- **始终生效（当前树即通过）**：
  - `TestManifestsTree_NoOrphanSignatures`（边界②）——**先红后绿**：红 `[.placeholder.yaml.sig placeholder.yaml.sig]`；两个文件实测为 `{}`（可解析但 tool 为空）与 **0 字节**（解析失败），且**都被 `main.go:31` 的 `go:embed manifests/*.yaml.sig` 打进二进制**（实测：embed 的 `*` 会匹配点开头文件，且 `embed.FS` 路径必须用斜杠——Windows 上 `filepath.Join` 读不到，已在测试里踩过并修正）。处置：删除两个孤儿签名（`go:embed` 为 glob，删单个文件不影响匹配）。
  - `TestEmbeddedSignatures_VerifyAgainstCommittedPublicKey`——每个内嵌 `.sig` 必须可解析、内嵌存在同名 `.yaml`、且用 `keys/pub.key` 验签通过。修复后 **36/36 通过**（红态即上面两个孤儿：0 字节无法解析、`{}` 验签失败且无对应 yaml）。
  - `TestIndexYaml_CoversAllToolManifests`（P2-15/D-15）——索引必须覆盖全部工具 manifest、条目 `name` 等于 manifest 声明的 `name`、文件名符合 `<name>.yaml`、无重复收录、无指向不存在文件、`generated` 为合法且非未来的日期。
  - `TestManifests_EveryToolHasCompleteArchive`（P2-29）——每个工具至少有一个结构完整的归档（`url`+`format`，或 npm 型）。**平台无关**（Windows-only 工具在 Linux CI 同样通过），否则会误伤。
- **自激活（重签后自动生效）**：`TestManifestSignatureCoverage` —— 数据线未完成时 skip 并打印缺口（当前 `16/52 尚无 .sig`，列出示例）；完成后逐版本断言「非空 `sha256` 与独立 `mirror_sha256` 均在本版本签名图中」，且签名可验签、`tool` 字段与 manifest 一致、无哈希工具（`dsh` 型）若存在 `.sig` 则报错（边界③口径）。

### 2. D-15 index.yaml 重建（并给出可持续的再生成手段）

- 实测缺口：索引只收录 **37** 个工具（磁盘 52）、`generated` 停在 2026-06-05；缺的 15 个是 `cangjie` 与 14 个 `development_tools`（该分类在旧索引里**整类缺失**）。
- 新增生成端（纯函数 + 命令，**生成端与守卫端共用同一实现**，防漂移）：
  - `internal/manifest/index.go`：`LoadToolManifests(dir)`、`BuildIndexYAML(tools, generated)`（确定性：分类按规范顺序、分类内按名字排序、`<name>.yaml` 命名、日期可重现；已知分类即使为空也保留分组，以免抹掉手工占位）、`ParseIndexYAML`、`Index.ManifestNames`。
  - `xdev manifest index [--manifests-dir] [--out] [--date] [--check]`（`internal/cli/index_manifest.go`；`--check` 忽略日期差异、检测工具/分类漂移）。
- 重建结果：**52 工具 / 6 分类 / 5138 字节**，`generated: 2026-09-13`；守卫转绿。守卫**不比对字节**（不冻结格式），只断言覆盖与引用完整性——比 `--check` 的字节级比较更耐手工微调。

### 3. P2-29 mingw 补全（含 A-03 评估与不可安装标记）

- **实测缺口**：mingw 三个版本的 archives **只有 `os/arch/requires_proxy`**，无 `url`/`format`/`sha256` ⇒ 完全不可安装；全仓审计（52 工具）显示**只有 mingw 整体缺归档**（`no-installable=1, partially-broken=0`）。
- **归档选择与哈希来源**（remote 指向 `niXman/mingw-builds-binaries`，产物 `.7z`）：
  | 版本 | tag | 资产 | size | sha256 |
  |---|---|---|---|---|
  | 16.2.0（新增，最新） | 16.2.0-rt_v14-rev1 | x86_64-16.2.0-release-posix-seh-ucrt-rt_v14-rev1.7z | 107890935 | 26788998…15c0（**API digest 与本地下载计算一致**） |
  | 16.1.0 | 16.1.0-rt_v14-rev1 | x86_64-16.1.0-release-posix-seh-ucrt-rt_v14-rev1.7z | 107056212 | 820bed24…7284（**双向交叉验证一致**） |
  | 12.2.0 | 12.2.0-rt_v10-rev2 | x86_64-12.2.0-release-posix-seh-ucrt-rt_v10-rev2.7z | 71301998 | dc1f299e…8c4e（本地计算；该 release 早于 GitHub digest 记录） |
  | 11.2.0 | 11.2.0-rt_v9-rev1 | x86_64-11.2.0-release-posix-seh-rt_v9-rev1.7z | 62408601 | 2c943b1a…0ae8（本地计算；上游无 ucrt 变体） |
  版本日期按 GitHub API 的 `published_at` 校正（16.1.0 原写 2026-07-29 → 实为 2026-05-23；12.2.0 原写 2022-09-22 → 实为 2022-12-11）；`latest` 提升到 16.2.0。
- **shims 修正（本票附带发现的真实缺陷）**：原清单含 `x86_64-w64-mingw32-{ld,ar,strip}`，但**本上游从未提供这三个带前缀的二进制**（16.2.0 实测 bin 下只有裸 `ar/ld/strip` + 前缀 `gcc/g++/c++/gcc-ar/gcc-nm/gcc-ranlib/gfortran`；本机既有 8.1.0 同样如此）⇒ 这三个 shim 自建立起就是坏的（实测报 `shim alias "x86_64-w64-mingw32-ld" not installed`）。现改为实际存在的等价物：`gcc`/`g++`/`c++`/`gcc-ar`/`gcc-ranlib`；**不为裸名 `ar/ld/strip` 建 shim**（会在用户 PATH 上遮蔽系统同名工具，属审计已知的 shim 遮蔽问题）。
- **端到端验收（沙箱 home + `--no-path-fix`，宿主零写入）**：`xdev install mingw@16.2.0` → 下载 107,890,935 字节、SHA-256 校验通过（`Install-by-kind complete (integrity OK)`）→ 7z 解压 13,878 文件 → 激活 + 5 个 shim；**逐个 shim 实测**：gcc/g++/c++ 输出 `16.2.0`、gcc-ar/gcc-ranlib 输出 `GNU Binutils 2.47.20260726`；**并用该 shim 真交叉编译**：`hello.c → hello.exe`（54,907 字节，magic `MZ`，PE machine `0x8664`）✓。
- **A-03 评估结论（运行时 7z 依赖）**：解压 `.7z`/`.exe` 需要系统 `7z`/`7za`（`internal/extractor`），但这不是本次新增——`ruby.yaml` 已用 `format: 7z`（4 条），而全部 `format: exe`（NSIS/Inno：clion/cursor/goland/jetbrains-*/llvm/pycharm/rubymine/rust/webstorm/zed）同样经 7z 解压。**属既有欠账，本次只登记、不扩大**；本机 `7z/7za/7zr` 均不在 PATH，因此端到端验证时用官方 7-Zip 独立版（7zr.exe，602KB）临时置于 PATH（未改系统）。
- **strip 现状（实测记录）**：`internal/extractor` 只在 zip/tar 路径实现 strip，`extract7z` 不接收该参数 ⇒ mingw 的 `strip: 1` 目前是**声明意图**、实际不生效，归档按原样落为 `<install>/mingw64/bin/...`；工具仍可用，因为解析器会向下搜索嵌套 bin（上面 gcc 实测即走该路径）。与 `ruby.yaml`（7z + strip: 1）保持同口径；若后续实现 7z 的 strip，条目会自然变为 `<install>/bin/...`，对解析器同样有效。
- **不可安装标记（票面第 2 项）**：`manifest.Archive.Installable()`（npm 型不要求 url/format）、`Version.Installable(goos,goarch)`、`Tool.HasInstallableVersion`、`Tool.HasCompleteArchive`（平台无关，供守卫）；`Catalog.markInstallability()` 在**三个加载出口**（磁盘/内嵌/合并）计算派生字段 `Tool.Installable`（不来自 YAML）；`xdev doctor` 新增「Not installable on this platform (N): …」提示行——**只列有版本却装不了的工具**（零版本伪条目不列），且**不算检查失败**（平台差异属正常）。实测：合成一个占位工具后 doctor 输出 `Not installable on this platform (1): placeholder-tool`。
- 守卫测试：`internal/manifest/archive_installable_test.go`（4 例：`Installable` 9 个判据、平台匹配、`HasInstallableVersion`/`HasCompleteArchive`、`LoadCatalog` 标记生效）、`internal/cli/doctor_installable_test.go`、根守卫 `TestManifests_EveryToolHasCompleteArchive`（红态证据：补全前的探针输出 `NO-INSTALLABLE mingw.yaml versions=3 archives=3 missingURL=3 missingFormat=3`；补全后 `no-installable=0 partially-broken=0`）。

### 4. 未做 / 留待后续（白名单外，仅记录）

- `manifests/index.yaml` 会被 `LoadCatalog` 解析成名为 `index` 的**零版本伪条目**（`register.go` 在插件注册时跳过它，`xdev available` 也不展示）；建议后续在加载层就排除非工具 YAML。
- 7z/exe 路径的 `strip` 语义（本票只记录，不改行为——改动会影响 ruby/rust/zed 等既有条目）。
- 数据线重签（票 17）仍阻塞于私钥路线决策。
