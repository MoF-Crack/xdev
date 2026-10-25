# XDev 多语言环境变量配置诊断（2026-10-25）

> **状态**：Snapshot
> **诊断日期**：2026-10-25
> **代码基线**：`f9c7b67`
> **范围**：`manifests/*.yaml` 的 `env_vars` 配置、`internal/util/sanitize.go` 白名单、`internal/env/` 写入机制
> **方法**：全量 manifest 横向对比 + 官方标准变量对照 + 实机写入状态核验

## 一、结论摘要

| 编号 | 级别 | 问题 | 影响 |
|---|---|---|---|
| E-01 | 🔴 严重 | `ANDROID_HOME` 在各 Android 组件间层级不一致（1 层 vs 2 层），且**都不是标准 SDK 根** | Android 工具链互相找不到 SDK |
| E-02 | 🔴 严重 | 6 个 Android 组件各自写 `ANDROID_HOME`，**后装的覆盖先装的** | 最终值取决于安装顺序，不确定 |
| E-03 | 🟡 建议 | Python 完全不设环境变量，`PYTHONHOME` 白名单存在但未用 | 无（见 3.1 说明，属合理取舍但缺 `PYTHONPATH`） |
| E-04 | 🟡 建议 | 缺少 `JAVA_HOME` 的配套变量（`JDK_HOME`、`MAVEN_OPTS` 等生态变量） | Maven/Gradle 部分场景需手工配 |
| E-05 | 🟡 建议 | `go.yaml` 的 `GOPATH` 指向 `<install>\..\gopath`，与版本无关的共享路径 | 多版本 Go 共用 GOPATH（设计如此，但需文档化） |
| E-06 | 🟢 风格 | 白名单有 20+ 个「仅占位」变量（`RUST_HOME`、`DOTNET_HOME`、`PHP_HOME`、`HASKELL_HOME`、`JULIA_HOME` 等）实际无 manifest 使用 | 白名单与真实配置漂移 |
| E-07 | 🟢 风格 | 部分语言用非官方变量名（`LUA_HOME`、`KOTLIN_HOME`、`ZIG_HOME` 等），官方并无此变量 | 第三方工具不会识别 |

## 二、全量环境变量现状对照

### 2.1 当前配置（manifest 声明）

| 工具 | 配置的环境变量 | 官方标准变量 | 差异 |
|---|---|---|---|
| go | `GOPATH` | `GOROOT`、`GOPATH` | 见 3.2（GOROOT 有意不设） |
| node | `NODE_HOME`、`npm_config_prefix` | `NODE_PATH` | `NODE_HOME` 非官方名 |
| python | **（空）** | `PYTHONHOME`、`PYTHONPATH` | 见 3.1 |
| java | `JAVA_HOME` | `JAVA_HOME` ✅ | 缺 `JDK_HOME` |
| rust | `CARGO_HOME`、`RUSTUP_HOME` | 同名 ✅ | 正确 |
| dart | `DART_HOME`、`PUB_CACHE` | `PUB_CACHE` ✅ | `DART_HOME` 非官方 |
| flutter | `FLUTTER_HOME`、`PUB_CACHE` | `FLUTTER_ROOT` | **应为 `FLUTTER_ROOT`** |
| dotnet | `DOTNET_ROOT` | 同名 ✅ | 正确 |
| erlang | `ERLANG_HOME` | 同名 ✅ | 正确 |
| haskell | `GHC_HOME` | `GHC_PACKAGE_PATH` | 非官方 |
| julia | `JULIA_DEPOT_PATH` | 同名 ✅ | 正确 |
| kotlin | `KOTLIN_HOME` | 无官方 | 非官方 |
| lua | `LUA_HOME` | 无官方 | 非官方 |
| perl | `PERL_HOME` | `PERL5LIB` | 非官方 |
| php | `PHPRC` | 同名 ✅ | 正确（但缺 `PHP_HOME`） |
| ruby | `RUBY_HOME` | `GEM_HOME`、`RUBYOPT` | 非官方 |
| scala | `SCALA_HOME` | 同名 ✅ | 正确 |
| zig | `ZIG_HOME` | 无官方 | 非官方 |
| deno | `DENO_INSTALL` | 同名 ✅ | 正确 |
| bun | `BUN_INSTALL` | 同名 ✅ | 正确 |
| cangjie | `CANGJIE_HOME` | 同名 ✅ | 正确 |
| llvm | `LLVM_HOME` | 无官方 | 非官方 |
| mingw | `MINGW_HOME` | 无官方 | 非官方 |
| cmake | `CMAKE_HOME` | 无官方 | 非官方 |
| ninja | **（空）** | 无 | 合理（单文件工具） |
| gradle | `GRADLE_HOME` | 同名 ✅ | 正确（缺 `GRADLE_USER_HOME`） |
| maven | `MAVEN_HOME` | `M2_HOME`（旧）/`MAVEN_HOME` | 正确 |
| sbt | `SBT_HOME` | 同名 ✅ | 正确 |
| uv | 5 个 `UV_*` | 同名 ✅ | 正确 |
| typescript | `TYPESCRIPT_HOME` | 无官方 | 非官方 |
| make | **（空）** | 无 | 合理 |
| git | `GIT_HOME` | 无官方 | 非官方 |
| android-* | 见 2.2 | `ANDROID_HOME`/`ANDROID_SDK_ROOT` | **见 E-01/E-02** |

### 2.2 Android 组件的 `ANDROID_HOME` 层级不一致（E-01）

| manifest | `ANDROID_HOME` 值 | 相对 `{{install}}` |
|---|---|---|
| android-ndk | `{{install}}\..` | 上 1 层 |
| android-platform-tools | `{{install}}\..` | 上 1 层 |
| android-platform | `{{install}}\..` | 上 1 层 |
| android-cmdline-tools | `{{install}}\..\..` | 上 2 层 |
| android-emulator | `{{install}}\..\..` | 上 2 层 |
| android-build-tools | `{{install}}\..\..` | 上 2 层 |

**问题**：`{{install}}` 展开为 `install/<tool>/<version>`。

- `\..` → `install/android-ndk`
- `\..\..` → `install`

**两者都不是 Android SDK 标准根目录**。标准布局应为：

```
<SDK_ROOT>/
├── ndk/<ver>/
├── platform-tools/
├── platforms/android-<api>/
├── build-tools/<ver>/
├── cmdline-tools/<ver>/
└── emulator/
```

而 xdev 把每个组件装成独立工具（`install/android-ndk/<ver>`），彼此平级，**无法组成一个合法 SDK 根**。这导致：

- `ANDROID_HOME` 无论指 `install/android-ndk` 还是 `install`，`sdkmanager`/`avdmanager`/Gradle 的 Android 插件都找不到预期的子目录结构
- 各组件互相不知道对方位置（NDK 找不到 SDK，SDK 找不到 NDK）

### 2.3 六个组件抢写同一个 `ANDROID_HOME`（E-02）

`ANDROID_HOME` 被 6 个 manifest 同时声明。按 `Activate` 的写入语义，**后激活的覆盖先激活的**，最终值取决于安装顺序：

```
装 ndk → ANDROID_HOME=install/android-ndk
装 build-tools → ANDROID_HOME=install          （覆盖）
装 emulator → ANDROID_HOME=install             （覆盖）
```

同一台机器上，安装顺序不同会导致 `ANDROID_HOME` 落在不同位置，**不可预测且不可复现**。

## 三、需要说明的两个「看似缺失实则有意」的点

### 3.1 Python 不设 `PYTHONHOME` 是正确取舍，但缺 `PYTHONPATH`

`manifests/python.yaml:11-13` 明确记录：

```yaml
env_vars:
    # python-build-standalone 自带 python3xx._pth，PYTHONHOME 对其无效
    # （._pth 机制使该变量被忽略），因此不再设置 PYTHONHOME。
    {}
```

**这个判断是对的**：python-build-standalone 的 `._pth` 会屏蔽 `PYTHONHOME`，硬设反而可能破坏隔离。

**但仍有缺口**：xdev 装的是**多版本** Python（3.14.6 / 3.12.7 / 3.11.10 / 3.10.15），切换版本时若用户已 `pip install` 到 site-packages，这些包随版本隔离——这是预期行为。真正缺的是：

- 无 `PYTHONPATH` 供用户追加自己的模块目录
- 无 `PIP_CACHE_DIR`/`PIP_TARGET` 托管（uv 有，pip 没有）

### 3.2 Go 不设 `GOROOT` 是修复过的有意行为

`manifests/go.yaml:11-15` 记录了原因：

```
GOROOT：v2 起不再设置（审计 X-07 / 票 01）。Go 1.26 的 findGOROOT 从
go.exe 向上两级自推导 + isGOROOT 校验（pkg/tool 探测），shim 分发的
go\bin\go.exe 自推导结果正确；而本 manifest 的归档 strip:0（顶层 go/
目录保留）使 {{install}} 恰比真实 GOROOT 高一层——写它会得到一个没有
src/ 的假根，导致 `go build` 报 "package fmt is not in std"。
```

**这是已修复的缺陷，不是遗漏**。记录在案避免重复报。

## 四、白名单与实际配置的漂移（E-06）

`internal/util/sanitize.go:287-344` 的白名单有 58 项，但实际 manifest 只用了约 30 项。以下**白名单允许但无任何 manifest 使用**：

```
RUST_HOME, DOTNET_HOME, PHP_HOME, HASKELL_HOME, JULIA_HOME,
NINJA_HOME, BUN_HOME, DENO_HOME, UV_HOME, PYTHONHOME,
ANDROID_SDK_ROOT, ANDROID_PLATFORM, GOROOT, FLUTTER_ROOT
```

**风险**：白名单是「允许写注册表」的授权表。保留大量未用项意味着**授权面大于实际需要**——若将来某 manifest 被篡改加入 `PYTHONHOME: C:\Windows`，白名单不会拦。

**建议**：白名单应与 manifest 实际用到的变量集对齐，或至少对「路径型变量」额外校验其值落在受管树内（`validateManagedPathValue` 已有此逻辑，但 `$XMDEV_HOME` 未设置时会整组跳过——见 `win_path_windows.go:1146-1151`）。

## 五、修复建议

### E-01 + E-02：Android 统一 SDK 根（优先）

**方案 A（推荐）**：引入 `android-sdk` 聚合工具，把各组件装到统一根下：

```yaml
# manifests/android-sdk.yaml（新增）
name: android-sdk
env_vars:
  ANDROID_HOME: '{{install}}'
  ANDROID_SDK_ROOT: '{{install}}'
```

各组件改为 `install/android-sdk/<component>/<version>` 布局，或由 `android-sdk` 的 PostInstall 把子组件 junction 进来。

**方案 B（最小改动）**：所有 Android manifest 的 `ANDROID_HOME` 统一为同一表达式，并保证该表达式指向一个**真实存在的、含标准子目录的根**。这需要同时调整 `strip` 与安装路径策略。

**无论哪个方案，必须先消除「6 处各写一个值」的竞争**——`ANDROID_HOME` 只应有一个 owner。

### E-04：补 Java 生态变量

```yaml
# java.yaml
env_vars:
  JAVA_HOME: "{{install}}"
  JDK_HOME: "{{install}}"      # 部分工具读这个
```

Maven/Gradle 的 `*_OPTS` 属用户偏好，不宜硬编码。

### E-03：Python 补 `PYTHONPATH` 与 pip 托管

```yaml
# python.yaml
env_vars:
  PYTHONPATH: '{{install}}\site-packages'   # 可选，需确认不破坏 _pth 隔离
```

**注意**：必须先实测 `PYTHONPATH` 在 python-build-standalone 下是否生效，避免重蹈 `PYTHONHOME` 的覆辙。

### E-07：非官方变量名

`LUA_HOME`/`KOTLIN_HOME`/`ZIG_HOME`/`PERL_HOME`/`RUBY_HOME` 等第三方工具**不识别**。它们目前只是「给人看的路径标记」。建议：

- 保留（无害，且 GUI 展示有用），但在 manifest 注释中标注「非官方，仅供 xdev 内部使用」
- 或改为官方变量：`PERL5LIB`、`GEM_HOME`、`GHC_PACKAGE_PATH`

## 六、未验证项

以下需实机验证才能定性，本次未做（环境未安装对应工具）：

1. Android 组件真实安装后的目录结构，是否与 `strip: 0` 推断一致
2. `PYTHONPATH` 在 python-build-standalone 下是否生效
3. `FLUTTER_HOME` vs `FLUTTER_ROOT` 的实际读取方（Flutter 工具链读哪个）
4. 多版本切换时，环境变量是否随 `current` 正确更新（已有 `Deactivate` 逻辑，但未测多版本并发切换）
