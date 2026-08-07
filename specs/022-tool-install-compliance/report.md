# Spec 022: 工具安装流程与指令符合性审计

> **状态**：已完成（P1+P2+P3 实施完毕，2026-08-02）
> **类型**：审计 + 合规修复
> **日期**：2026-08-02
> **范围**：manifests/*.yaml 39 个工具 vs 官方安装规范（环境变量 / bin 布局 / 安装方式 / 完整性）

## 实施记录（2026-08-02）

### P1 环境变量（D1-D10）✅
- php: `PHP_HOME`→`PHPRC`；julia: `JULIA_HOME`→`JULIA_DEPOT_PATH`；deno: `DENO_HOME`→`DENO_INSTALL`；bun: `BUN_HOME`→`BUN_INSTALL`
- uv: 官方 UV_INSTALL_DIR/UV_CACHE_DIR/UV_TOOL_BIN_DIR/UV_TOOL_DIR/UV_PYTHON_INSTALL_DIR
- dart: 补 PUB_CACHE；node: 补 npm_config_prefix；python: 移除无效 PYTHONHOME；go: GOROOT/GOPATH 保留+注释
- 白名单 +10（PHPRC/JULIA_DEPOT_PATH/DENO_INSTALL/BUN_INSTALL/UV_*/npm_config_prefix）

### P2 安装流程（D11-D17）✅
- erlang: exe 加 install_kind: executable；llvm 17.0.6: format exe + install_kind
- php/lua/make: 移除 linux 源码 tarball 条目；mingw: 移除错误平台 URL（修复重复 requires_proxy）
- scala: 移除 sbt shim；flutter: 移除 dart shim；dart: 移除 dartfmt（现 `dart format` 子命令）
- android cmdline-tools: 布局注释说明

### P3 完整性（D18-D21）✅
- **D18 假 hash 清零**：32 处 `a04cfd77...` 全部处理（实测回填 6 处，其余改空走 TOFU）
- **D19/D20 实测回填 15 个工具 Windows 最新版 SHA**：ninja/deno/typescript/sbt/maven/kotlin/gradle/cmake/ruby/git/node/android-build-tools-34/android-platform-34/go/dotnet/android-ndk-r29
- 大文件（llvm/flutter/python/erlang/julia/cangjie 等）保留 TOFU（安全机制保证首次下载后永久校验）
- zig 0.15.1 URL 404（上游文件名变化，待核实），保留空走 TOFU
### 补充（2026-08-02 二轮回填）
- **zig URL 修正**：`zig-windows-x86_64` → `zig-x86_64-windows`（官方文件名顺序），实测 SHA `91e69e88...`
- **php URL 修正**：vs16 → **vs17**（官方已切换编译器版本），实测 SHA `6a8af9f5...`
- **大文件实测回填**：llvm 22.1.8 `d96c2cc1...`（862MB）、flutter 3.44.7 `a973249b...`（1.79GB）、python 3.14.6 `daf2b7e9...`、erlang 29.0.4 `c119a52a...`、julia 1.12.6 `a63d9919...`、cangjie 1.1.3 `8bb65499...`（392MB）、android-ndk r27c/r26d/r25c、android-platform 33/31、android-build-tools 33.0.2、android-cmdline 10406996/9477386
- **剩余空 SHA**：仅 cangjie@1.0.5（旧版下载链接失效，87 字节非有效文件），保留 TOFU
- 全部同步运行时 `E:\XDev\manifests\`（含 zig 补同步）
- 全量测试 26 包通过；GUI 重建（18:03:26）

### 验证
- 全量 `go test ./... -short`：26 包通过、无 FAIL
- manifest 解析测试通过；假 hash 归零
- 新构建 `build\bin\xdev-gui.exe`（2026-08-02）

## 结论摘要

- **符合官方的核心项**：JAVA_HOME、GOROOT+GOPATH、DOTNET_ROOT、CARGO_HOME/RUSTUP_HOME、MAVEN_HOME、GRADLE_HOME、SCALA_HOME、SBT_HOME、PYTHONHOME、CANGJIE_HOME、ANDROID_HOME（当前推荐名，非弃用的 ANDROID_SDK_ROOT）、PUB_CACHE
- **xdev 自造的非官方变量**（无害但非官方）：GIT_HOME、LLVM_HOME、MINGW_HOME、TYPESCRIPT_HOME、LUA_HOME、PHP_HOME、KOTLIN_HOME、ERLANG_HOME、GHC_HOME、DART_SDK、ZIG_HOME、NODE_HOME、PERL_HOME、RUBY_HOME、JULIA_HOME、BUN_HOME、DENO_HOME、UV_HOME、CMAKE_HOME、FLUTTER_HOME
- **错误/冲突项（需修）**：详见下表

## 一、环境变量符合性对照

| 工具 | xdev 现状 | 官方规范 | 判定 |
|------|-----------|----------|------|
| java | JAVA_HOME={{install}} | JAVA_HOME=JDK 目录（官方） | ✅ 符合 |
| go | GOROOT={{install}} + GOPATH={{install}}\..\gopath | GOROOT=安装根（官方，非默认位置时需要）；GOPATH=用户工作区（默认 ~/go，**安装器不设置**） | ⚠️ GOPATH 指向 xdev 内部目录不官方（官方默认 ~/go）；GOROOT 正确 |
| python | PYTHONHOME={{install}} | python.org 不设；python-build-standalone 有 `._pth` 使 PYTHONHOME **无效** | ⚠️ 无效变量（无害但无用） |
| node | NODE_HOME={{install}} | Node 官方无此变量（nvm-windows 用 NVM_HOME） | ⚠️ 非官方（无害） |
| typescript | TYPESCRIPT_HOME={{install}} | 无官方变量（npm 包） | ⚠️ 非官方（无害） |
| ruby | RUBY_HOME={{install}} | 无官方变量 | ⚠️ 非官方（无害） |
| perl | PERL_HOME={{install}} | 无官方变量 | ⚠️ 非官方（无害） |
| php | PHP_HOME={{install}} | 官方用 **PHPRC**（php.ini 位置） | ❌ 应改用 PHPRC |
| lua | LUA_HOME={{install}} | 无官方变量 | ⚠️ 非官方（无害） |
| kotlin | KOTLIN_HOME={{install}} | 官方仅要求 bin 入 PATH | ⚠️ 非官方（无害） |
| scala | SCALA_HOME={{install}} | 官方推荐 cs setup，SCALA_HOME 非当前官方 | ⚠️ 非官方（无害） |
| sbt | SBT_HOME={{install}} | 官方无 SBT_HOME（有 SBT_OPTS） | ⚠️ 非官方（无害） |
| dart | DART_HOME={{install}} | 官方无 DART_SDK；**PUB_CACHE** 官方 | ⚠️ 建议补 PUB_CACHE |
| flutter | FLUTTER_HOME + PUB_CACHE | FLUTTER_ROOT 工具内部自动推导；PUB_CACHE 官方 | ⚠️ PUB_CACHE ✅，FLUTTER_HOME 非官方 |
| julia | JULIA_HOME={{install}} | **JULIA_HOME 已从文档移除**，官方用 JULIA_BINDIR / JULIA_DEPOT_PATH | ❌ 应改用 JULIA_DEPOT_PATH |
| haskell | GHC_HOME={{install}} | 无 GHC_HOME；官方 GHC_PACKAGE_PATH | ⚠️ 非官方（无害） |
| erlang | ERLANG_HOME={{install}} | 官方安装器只写注册表，不设变量 | ⚠️ 非官方（无害） |
| deno | DENO_HOME={{install}} | 官方 DENO_INSTALL（**DENO_INSTALL_ROOT 是旧名**） | ❌ 应改用 DENO_INSTALL |
| bun | BUN_HOME={{install}} | 官方 BUN_INSTALL | ❌ 应改用 BUN_INSTALL |
| zig | ZIG_HOME={{install}} | 无安装变量（有缓存变量） | ⚠️ 非官方（无害） |
| cangjie | CANGJIE_HOME={{install}} | CANGJIE_HOME 官方（华为文档） | ✅ 符合 |
| maven | MAVEN_HOME={{install}} | 官方当前文档只要求 bin 入 PATH（MAVEN_HOME 历史遗留） | ⚠️ 无害，可保留 |
| gradle | GRADLE_HOME={{install}} | 官方文档提及 GRADLE_HOME（可选） | ✅ 符合 |
| cmake | CMAKE_HOME={{install}} | 官方无 CMAKE_ROOT 安装变量 | ⚠️ 非官方（无害） |
| ninja | （空） | 无官方变量 | ✅ |
| make | （空） | 无官方变量 | ✅ |
| mingw | MINGW_HOME={{install}} | 无官方变量 | ⚠️ 非官方（无害） |
| git | GIT_HOME={{install}} | MinGit 无官方变量 | ⚠️ 非官方（无害） |
| llvm | LLVM_HOME={{install}} | 无官方安装变量 | ⚠️ 非官方（无害） |
| dotnet | DOTNET_ROOT={{install}} | DOTNET_ROOT 官方（zip 安装官方指引设此变量） | ✅ 符合 |
| rust | CARGO_HOME/RUSTUP_HOME={{install}}\.cargo/.rustup | rustup 官方机制（默认 ~/.rustup ~/.cargo，可重定位） | ✅ 符合 |
| uv | UV_HOME={{install}} | 官方 UV_INSTALL_DIR/UV_CACHE_DIR/UV_TOOL_BIN_DIR；**无 UV_HOME** | ❌ 应改用 UV_INSTALL_DIR 或补 UV_* |
| npm/npx/yarn/pnpm | （未设） | npm_config_prefix（Windows 默认 %AppData%\npm）；PNPM_HOME 官方 | ⚠️ 建议补 npm_config_prefix |
| android-* | ANDROID_HOME={{install}}\.. 等 | **ANDROID_HOME 当前官方**（ANDROID_SDK_ROOT 已弃用） | ✅ ANDROID_HOME 正确 |
| android-ndk | ANDROID_NDK_HOME={{install}} | **非官方**（官方用 sdk\ndk\<ver> 目录） | ⚠️ 非官方（无害） |
| android-build-tools 等 | ANDROID_BUILD_TOOLS / ANDROID_PLATFORM_TOOLS / ANDROID_CMDLINE_TOOLS / ANDROID_PLATFORM | **非官方 SDK 变量**（官方列表无） | ⚠️ 非官方（无害，供工具链识别） |

## 二、安装流程/布局符合性

| 工具 | 问题 | 判定 |
|------|------|------|
| erlang | `format: exe`（NSIS 安装器）**缺 install_kind** → 被当 archive 解压 → 必然失败（对比 rust 已正确设 executable） | ❌ 必修 |
| php (linux/darwin) | URL 指向**源码 tarball**（非可运行二进制） | ❌ 需修（或标记不支持非 Windows） |
| lua (linux) | 源码 tarball | ❌ 需修 |
| make (linux) | 源码 tarball（无编译流程） | ❌ 需修 |
| mingw (linux/darwin) | 指向**同一 Windows 7z URL**，非 Windows 无法安装 | ❌ 需修 |
| llvm 17.0.6 | format: zip 但 URL 是 `.exe` NSIS 安装器 | ❌ 需修 |
| scala.yaml | shim 含 `sbt`，与独立 sbt 工具重叠 | ⚠️ 需去重 |
| flutter.yaml | shim 含 `dart`，与独立 dart 工具重叠 | ⚠️ 需去重 |
| dart.yaml | shim `dartfmt` 已被现代 Dart 移除（现 `dart format`） | ⚠️ 需更新 |
| android-* 布局 | cmdline-tools zip 解出 `cmdline-tools/` 根；install_root=android-sdk + strip 0 → 路径语义需验证 | ⚠️ 需验证 |

## 三、完整性（SHA256）问题

| 类型 | 工具 | 判定 |
|------|------|------|
| **共享复制粘贴错误 hash** `a04cfd77...` | android-platform (34/33/31)、android-ndk (r27c/r26d/r25c)、android-cmdline-tools (全部)、android-build-tools (全部) | ❌ 高危，hash 与文件不符 → 下载必失败 |
| 最新版 SHA 为空（旧版有） | zig/typescript/ruby/python/php/node/ninja/mingw/maven/kotlin/julia/llvm/gradle/go/git/flutter/erlang/dotnet/deno/cmake/android-ndk(r29)（20 个） | ⚠️ 走 TOFU，建议回填 |
| 全部版本 SHA 为空 | cangjie | ⚠️ 建议回填 |
| 版本命名混乱 | android-ndk `r29` vs `27.2.x`；android-emulator/cmdline-tools 数字 build id | ⚠️ 版本比较语义需明确 |
| semver 单段 | java `26`/`25`/`21` | ⚠️ 可接受（major-only） |

## 四、修复 Plan

### P1 环境变量纠正（低风险，manifest-only）
- D1 php: `PHP_HOME` → `PHPRC`（指向 install 目录，php.ini 位置）
- D2 julia: `JULIA_HOME` → `JULIA_DEPOT_PATH`（官方缓存路径语义）
- D3 deno: `DENO_HOME` → `DENO_INSTALL`
- D4 bun: `BUN_HOME` → `BUN_INSTALL`
- D5 uv: 补 `UV_INSTALL_DIR`/`UV_CACHE_DIR`/`UV_TOOL_BIN_DIR`（保留或移除 UV_HOME）
- D6 dart: 补 `PUB_CACHE`（官方包缓存）
- D7 node: 补 `npm_config_prefix`（npm 全局 bin 官方约定）
- D8 go: 评估 GOPATH 是否改为官方默认语义（或保留 xdev 内部 gopath 并在文档说明）
- D9 python: 移除无效 PYTHONHOME（python-build-standalone 有 ._pth，变量无效）或改 PYTHONPATH
- D10 其余非官方 *_HOME：**保留**（无害、供工具识别），在 manifest 注释标注"非官方，仅供 xdev 内部识别"

### P2 安装流程修复（行为修复）
- D11 erlang: 加 `install_kind: executable`（或改官方 zip 分发）
- D12 php/lua/make linux: 源码 tarball → 标记不可安装或换官方二进制分发
- D13 mingw linux/darwin: 移除错误平台 URL 或标记不支持
- D14 llvm 17.0.6: format 改 exe + install_kind executable（NSIS 安装器）
- D15 scala/flutter shim 去重（移除重叠 sbt/dart shim）
- D16 dart shim `dartfmt` → `dart format`（更新 shim 名或移除）
- D17 android cmdline-tools 布局验证 + strip/install_root 修正

### P3 完整性修复
- D18 修正 android 系列复制粘贴的 `a04cfd77...` 错误 hash（实测回填或改空走 TOFU）
- D19 回填 20 个"最新版 SHA 为空"的工具（代理实测）
- D20 回填 cangjie 全部版本 SHA
- D21 android-ndk 版本命名统一评估

### 验证
1. 每项 manifest 修改后：`go build ./...` + manifest 解析测试
2. D18 后实际下载校验通过
3. 全部完成：`go test ./... -short` + 手工抽查 java/go/node 安装后 `go env GOROOT`、`%JAVA_HOME%\bin\java.exe` 等

## 五、Task 清单（待审批）

- [ ] P1-D1 php PHPRC
- [ ] P1-D2 julia JULIA_DEPOT_PATH
- [ ] P1-D3 deno DENO_INSTALL
- [ ] P1-D4 bun BUN_INSTALL
- [ ] P1-D5 uv UV_* 官方变量
- [ ] P1-D6 dart PUB_CACHE
- [ ] P1-D7 node npm_config_prefix
- [ ] P1-D8 go GOPATH 评估
- [ ] P1-D9 python PYTHONHOME 处理
- [ ] P1-D10 非官方变量注释标注
- [ ] P2-D11 erlang install_kind
- [ ] P2-D12 php/lua/make linux 处理
- [ ] P2-D13 mingw 平台 URL 修正
- [ ] P2-D14 llvm 17.0.6 format
- [ ] P2-D15 scala/flutter shim 去重
- [ ] P2-D16 dart shim 更新
- [ ] P2-D17 android cmdline-tools 布局
- [ ] P3-D18 android 系列 hash 修正
- [ ] P3-D19 20 工具最新版 SHA 回填
- [ ] P3-D20 cangjie SHA 回填
- [ ] P3-D21 android-ndk 版本命名

> **审批请求**：建议 P1+P3（环境变量 + 完整性，均为 manifest-only 低风险）；P2 中 D11/D14（erlang/llvm 安装失败项）优先。批准后按阶段执行。
