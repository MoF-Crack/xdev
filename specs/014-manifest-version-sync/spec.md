# Spec 014: Manifest 版本同步——数据更新 + sniff 自动更新 latest

> **文档类型**：Feature Spec
> **状态**：Approved
> **文档版本**：1.0.0
> **创建日期**：2026-08-08
> **最后更新**：2026-08-20
> **所有者**：xinyan <zuijiuhjd@foxmail.com>
> **权威范围**：manifest latest 同步与 sniff 写回的目标、范围和验收
> **替代文档**：无

## 背景与动机

xdev 的 manifest YAML 文件中 `latest` 字段和 `versions` 列表是静态编写的，与上游真实最新版本严重偏差。32 个工具中 24 个落后，部分跨大版本落后（如 node 20→26、rust 1.82→1.97、cmake 3.31→4.4、typescript 5.7→7.0）。

同时，sniff 机制只追加版本到 `versions` 列表，不更新 `latest` 字段，导致即使 sniff 发现了新版本，`DefaultVersion()` 仍返回旧版本。

## 范围

### A. 数据修复——更新 manifest YAML

更新所有落后工具的 `latest` 字段和 `versions` 列表，对齐上游真实最新版本。

### B. 架构修复——sniff 自动更新 latest

修改 `mergeIntoManifest()` 让 sniff 发现新版本后自动更新 `latest` 字段。

### 不在范围

- 不修改下载 URL 的 SHA256 校验值（新版本的 archive URL 由 sniff 的 ArchivePatternSniffer 自动生成模板，或手动补全 URL 模式）
- 不修改 GUI 版本列表展示逻辑（数据源不变，仍是 VersionList）
- 不修改 catalog 缓存机制（进程内缓存刷新是独立问题）

## A. 数据修复

### 需要更新的工具清单

| 工具 | 当前 latest | 目标 latest | 变更 |
|---|---|---|---|
| git | 2.47.0 | 2.55.0 | 更新 latest + 新增版本条目 |
| go | 1.23.2 | 1.26.5 | 更新 latest + 新增版本条目 |
| node | 20.18.0 | 26.5.0 | 更新 latest + 新增版本条目 |
| python | 3.12.7 | 3.14.6 | 更新 latest + 新增版本条目 |
| rust | 1.82.0 | 1.97.1 | 更新 latest + 新增版本条目 |
| cmake | 3.31.0 | 4.4.1 | 更新 latest + 新增版本条目 |
| deno | 1.46.3 | 2.9.4 | 更新 latest + 新增版本条目 |
| kotlin | 2.0.21 | 2.4.10 | 更新 latest + 新增版本条目 |
| gradle | 8.10.2 | 9.6.1 | 更新 latest + 新增版本条目 |
| typescript | 5.7.3 | 7.0.2 | 更新 latest + 新增版本条目 |
| zig | 0.13.0 | 0.15.1 | 更新 latest + 新增版本条目 |
| php | 8.3.13 | 8.5.9 | 更新 latest + 新增版本条目 |
| ruby | 3.3.5 | 4.0.6 | 更新 latest + 新增版本条目 |
| dotnet | 8.0.403 | 9.0.119 | 更新 latest + 新增版本条目 |
| maven | 3.9.9 | 3.9.16 | 更新 latest + 新增版本条目 |
| llvm | 18.1.8 | 22.1.8 | 更新 latest + 新增版本条目 |
| julia | 1.11.1 | 1.12.6 | 更新 latest + 新增版本条目 |
| ninja | 1.12.1 | 1.13.2 | 更新 latest + 新增版本条目 |
| sbt | 1.10.2 | 2.0.4 | 更新 latest + 新增版本条目 |
| erlang | 27.1.2 | 29.0.4 | 更新 latest + 新增版本条目 |
| uv | 0.7.6 | 0.12.0 | 更新 latest + 新增版本条目 |
| android-ndk | 27.2.12479018 | r29 | 更新 latest + 新增版本条目 |
| mingw | 12.2.0 | 16.1.0 | 更新 latest + 新增版本条目 |
| flutter | 3.44.1 | 3.44.7 | 更新 latest（小版本） |

### 新增版本条目的 archive 规则

- 每个新增版本条目需要包含 `os: windows/amd64`、`os: linux/x86_64`、`os: darwin/arm64` 三个平台
- `format`：与该工具现有版本条目一致
- `url`：按现有 URL 模式替换版本号（如 `typescript-5.7.3.tgz` → `typescript-7.0.2.tgz`）
- `mirror_url`：同理替换
- `sha256`：从上游源获取真实 SHA256（见下方"SHA256 获取策略"），无法获取的设为空字符串
- `strip`：与现有版本一致
- `size`：估算值，与同工具相近版本的 size 一致

### SHA256 获取策略

更新 manifest 时，对每个新增版本条目，按以下优先级获取 SHA256：

| 获取方式 | 适用工具 | URL 模式 |
|---|---|---|
| Go JSON API | go | `https://go.dev/dl/?mode=json` → files[].sha256 |
| Node SHASUMS256 | node | `https://nodejs.org/dist/v<ver>/SHASUMS256.txt` |
| Python 下载页 | python | `https://www.python.org/downloads/release/python-<ver>/` HTML 表格 |
| Flutter releases JSON | flutter | `https://storage.googleapis.com/flutter_infra_release/releases/releases_<os>.json` → sha256 字段 |
| Rust .sha256 文件 | rust | `https://static.rust-lang.org/dist/<date>/<file>.sha256` |
| Bun SHASUMS256 | bun | `https://github.com/oven-sh/bun/releases/download/<tag>/SHASUMS256.txt` |
| Deno .sha256sum | deno | `https://github.com/denoland/deno/releases/download/<tag>/<file>.sha256sum` |
| CMake SHA-256 文件 | cmake | release asset `cmake-<ver>-SHA-256.txt` |
| Kotlin .sha256 | kotlin | `<download-url>.sha256` |
| PHP .sha256 | php | `https://www.php.net/distributions/<file>.sha256` |
| Ruby 下载页 | ruby | `https://www.ruby-lang.org/en/downloads/` HTML |
| sbt .sha256 | sbt | `<download-url>.sha256` |
| Julia .sha256 | julia | `https://julialang-s3.julialang.org/bin/checksums/julia-<ver>.sha256` |
| uv .sha256 | uv | `<download-url>.sha256` |
| Haskell SHA256SUMS | haskell | `https://downloads.haskell.org/ghc/<ver>/SHA256SUMS` |
| Dart .sha256 | dart | `https://storage.googleapis.com/dart-archive/channels/stable/release/<ver>/sdk/<file>.sha256` |
| npm integrity (SHA512) | typescript | `https://registry.npmjs.org/typescript/<ver>` → dist.integrity（SHA512，需转换或跳过） |
| **无法获取 SHA256 的工具** | git, gradle, zig, erlang, llvm, ninja, android-ndk, mingw, perl, dotnet(SHA512), maven(SHA512) | sha256 设为空字符串 |

**实现方式**：编写一个 Go 脚本/工具，遍历需要更新的 manifest，对每个新版本按上述策略获取 SHA256 并写入 YAML。对于无法获取的工具，sha256 留空（安装器会跳过校验，不阻拦安装）。

### 补全 remote.url 为空的工具

| 工具 | remote.type | 需要补全的 remote.url |
|---|---|---|
| erlang | github | `https://api.github.com/repos/erlang/otp/releases` |
| haskell | github | `https://api.github.com/repos/ghc/ghc/releases` |
| julia | github | `https://api.github.com/repos/JuliaLang/julia/releases` |
| llvm | github | `https://api.github.com/repos/llvm/llvm-project/releases` |
| lua | github | 空（lua.org 无 API，用 html sniffer 指向 `https://www.lua.org/ftp/`） |
| mingw | github | `https://api.github.com/repos/niXman/mingw-builds-binaries/releases` |
| perl | github | `https://api.github.com/repos/Perl/perl5/releases` |
| java | 无 | 新增 remote: `type: github, url: https://api.github.com/repos/openjdk/jdk/releases, pattern: "^jdk-(\\d+)$"` |

## B. 架构修复——sniff 自动更新 latest

### 修改点

**`internal/discovery/scheduler.go` — `mergeIntoManifest()` 函数**

在追加新版本后，比较发现版本与当前 `latest`：
- 如果发现的版本比当前 `latest` 更新（按 semver 比较），更新 `latest` 字段
- 同时更新 `lts` 字段（如果发现的版本是 LTS 且比当前 `lts` 更新）

### 版本比较规则

- 使用简单的 semver 比较：按 `.` 分割，逐段比较数字
- 对于非 semver 版本号（如 android-ndk 的 `r29`、java 的 `26`），跳过自动更新 latest，保留原值
- 对于 `Discovered=true && Unverified=true` 的版本，仍可更新 latest（因为这些版本已经被上游确认存在）

### 验收标准

1. `go build ./...` 通过
2. `go test ./internal/discovery/... ./internal/manifest/... ./internal/installer/...` 通过
3. 所有 32 个工具的 manifest `latest` 字段与上游真实最新版本一致
4. `mergeIntoManifest` 在发现比当前 latest 更新的版本时自动更新 latest
5. 新增版本条目的 archive URL 按现有模式生成
6. 7 个 remote.url 为空的工具补全了 URL
7. java 新增了 remote 配置
8. 可获取 SHA256 的工具已从上游获取并写入 manifest
9. 无法获取 SHA256 的工具采用 TOFU 机制：首次安装时计算 SHA256 并缓存到 on-disk manifest，后续安装校验缓存值
10. GUI 安装无 SHA256 的新版本时弹出告警弹窗，告知用户该版本未经校验、首次安装后将自动计算并缓存校验值

## C. TOFU 机制——无 SHA256 版本的信任首次使用

### 机制设计

对于 manifest 中 `sha256` 为空的新版本条目，安装器采用 TOFU（Trust On First Use）策略：

1. **首次安装**：
   - 下载文件后，计算实际 SHA256
   - 将计算出的 SHA256 写回 on-disk manifest 的对应 archive 条目
   - GUI 弹出告警弹窗："此版本未经预校验，已下载并计算校验值 `<hash>`，后续安装将以此校验"
   - 继续安装流程

2. **后续安装**：
   - 读取 on-disk manifest 中已缓存的 SHA256
   - 下载后校验，不匹配则阻拦

3. **告警弹窗**：
   - 仅在 GUI 模式下弹出（CLI 模式打印 WARNING 日志）
   - 弹窗内容包含：工具名、版本号、提示信息
   - 用户确认后继续安装

### 修改点

**`internal/installer/installer.go` — `verifyArchive()` 或下载后校验逻辑**

当前逻辑（line 711）：
```go
if a.SHA256 == "" || util.IsAllZeroHash(a.SHA256) {
    // 跳过校验，继续安装
}
```

改为：
```go
if a.SHA256 == "" || util.IsAllZeroHash(a.SHA256) {
    // TOFU: 计算实际 SHA256 并缓存
    actualSHA, err := util.FileSHA256(src)
    if err == nil {
        // 写回 on-disk manifest
        updateArchiveSHA256(toolName, version, os, arch, actualSHA)
        // GUI: 发送事件触发告警弹窗
        // CLI: 打印 WARNING 日志
    }
    // 继续安装（不阻拦）
}
```

**`internal/gui/` — 告警弹窗 binding**

新增 Wails 事件或 binding，让安装器在 TOFU 首次使用时通知前端弹出告警弹窗。

**on-disk manifest 更新**

新增辅助函数 `updateArchiveSHA256`，读取 on-disk manifest YAML，更新对应 archive 条目的 sha256 字段，原子写回。

## 风险

- **URL 模式不匹配**：部分工具的下载 URL 模式可能随版本变化（如 cmake 从 3.x 到 4.x 可能改变了命名规则），需要逐一确认
- **semver 比较边界**：非标准版本号（如 android 的纯数字、NDK 的 r29）需要跳过自动更新
- **TOFU 写回竞态**：多线程安装同一工具时可能并发写 on-disk manifest，需要文件锁或原子写
- **工作量**：24 个 YAML 文件需要更新，每个需要生成三个平台的 archive 条目

## 批准记录 (Approval History)

| 阶段 | 状态 | 批准者 | 日期 | 文档版本 | 批准范围 |
|---|---|---|---|---|---|
| Spec 批准 | Approved | `xinyan <zuijiuhjd@foxmail.com>` | 2026-08-08 | `1.0.0` | `2026-08-20-spec-014-approved-001` manifest latest 同步与 sniff 写回的目标、范围和验收 |
