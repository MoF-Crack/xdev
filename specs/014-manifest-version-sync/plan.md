# Plan 014: Manifest 版本同步——数据更新 + sniff 自动更新 latest + TOFU

> **状态**：待用户批准
> **依据**：`specs/014-manifest-version-sync/spec.md`

## 执行策略

三阶段串行，B 和 C 可部分并行。

```
Phase 1: A 数据修复（manifest YAML 更新 + SHA256 获取）
    ↓
Phase 2: B 架构修复（sniff 自动更新 latest）+ C TOFU 机制（并行）
    ↓
Phase 3: 构建验证
```

## Phase 1: A 数据修复

### 1.1 编写 SHA256 获取工具

创建 `cmd/fetch-sha256/main.go`（或 `tools/fetch_shasum.go`），一个一次性 Go 工具：
- 读取每个需要更新的 manifest YAML
- 对每个新增版本条目，按 spec 中的 SHA256 获取策略从上游获取
- 支持的获取方式：
  - Go: `https://go.dev/dl/?mode=json` → files[].sha256
  - Node: `https://nodejs.org/dist/v<ver>/SHASUMS256.txt`
  - Python: 下载页 HTML 解析
  - Flutter: `releases_<os>.json` → sha256 字段
  - Rust: `<file>.sha256` 文件
  - Bun: `SHASUMS256.txt` release asset
  - Deno: `<file>.sha256sum` release asset
  - CMake: `cmake-<ver>-SHA-256.txt` release asset
  - Kotlin: `<download-url>.sha256`
  - PHP: `https://www.php.net/distributions/<file>.sha256`
  - Ruby: 下载页 HTML 解析
  - sbt: `<download-url>.sha256`
  - Julia: `julia-<ver>.sha256` 文件
  - uv: `<download-url>.sha256`
  - Haskell: `SHA256SUMS` 文件
  - Dart: `<file>.sha256` 文件
- 无法获取的工具（git, gradle, zig, erlang, llvm, ninja, android-ndk, mingw, perl, dotnet, maven）：sha256 留空
- 输出更新后的 YAML 或直接写入 manifest 文件

### 1.2 更新 24 个 manifest YAML

对每个落后工具：
- 更新 `latest` 字段为目标版本
- 在 `versions` 列表头部插入新版本条目（保留现有版本）
- 每个新版本条目包含三个平台（windows/amd64, linux/x86_64, darwin/arm64）
- `url`/`mirror_url` 按现有模式替换版本号
- `sha256` 从 1.1 获取，无法获取的留空
- `format`/`strip`/`size` 与现有版本一致

### 1.3 补全 remote 配置

- 7 个工具补全 `remote.url`
- java 新增完整 `remote` 块

### 1.4 验证
- `go build ./...` 通过（manifest 被 embed 到二进制）
- `go test ./internal/manifest/...` 通过

### 1.5 测试用例与预期结果

| 用例 | 操作 | 预期结果 |
|---|---|---|
| T1.5.1 | 遍历所有 manifest YAML，检查 `latest` 字段是否非空 | 37 个工具全部非空 |
| T1.5.2 | 检查每个工具的 `latest` 值是否在 `versions` 列表中存在 | 37/37 一致 |
| T1.5.3 | 对比更新后的 `latest` 与 spec 中目标版本 | 24 个更新工具全部匹配目标版本 |
| T1.5.4 | 检查 7 个工具的 `remote.url` 是否非空 | erlang/haskell/julia/llvm/lua/mingw/perl 全部非空 |
| T1.5.5 | 检查 java 的 `remote` 块是否存在且 type=github | 存在且配置正确 |
| T1.5.6 | 检查可获取 SHA256 的工具的 `sha256` 字段 | go/node/python/rust/flutter/bun/deno/cmake/kotlin/php/ruby/sbt/julia/uv/haskell/dart 的新版本条目 sha256 非空 |
| T1.5.7 | 检查无法获取 SHA256 的工具的 `sha256` 字段 | git/gradle/zig/erlang/llvm/ninja/android-ndk/mingw/perl/dotnet/maven 的新版本条目 sha256 为空字符串 |
| T1.5.8 | `go test ./internal/manifest/...` | 通过，无 manifest 解析错误 |

## Phase 2: B 架构修复 + C TOFU 机制（并行）

### B: sniff 自动更新 latest

**`internal/discovery/scheduler.go` — `mergeIntoManifest()`**

在追加新版本后，添加 latest 更新逻辑：
```go
// 比较发现版本与当前 latest
for _, dv := range discovered {
    if isVersionNewer(dv.Version, existing.Latest) {
        existing.Latest = dv.Version
    }
}
```

新增 `isVersionNewer(a, b string) bool` 函数：
- 按 `.` 分割，逐段比较数字
- 非数字段（如 `r29`）返回 false（跳过更新）
- 放在 `internal/manifest/manifest.go` 或 `internal/util/` 中

**测试**：`mergeIntoManifest` 发现更新版本后 latest 被更新；非 semver 版本号不触发更新

### C: TOFU 机制

**`internal/installer/installer.go` — 下载后校验逻辑**

修改 `if a.SHA256 == ""` 分支：
```go
if a.SHA256 == "" || util.IsAllZeroHash(a.SHA256) {
    // TOFU: 计算实际 SHA256
    actualSHA, err := util.FileSHA256(src)
    if err == nil {
        // 写回 on-disk manifest
        manifest.UpdateArchiveSHA256(toolName, version, actualSHA)
        // 通过 Wails event 通知前端（GUI 模式）
        // CLI 模式打印 WARNING
    }
    // 继续安装，不阻拦
}
```

**`internal/manifest/manifest.go` — 新增 `UpdateArchiveSHA256`**

读取 on-disk manifest YAML，找到对应 archive 条目，更新 sha256 字段，原子写回。需要文件锁防止并发写。

**`internal/gui/` — 告警弹窗**

安装器通过 Wails event（`runtime.EmitEvent`）通知前端。前端监听事件，弹出 `n-dialog` 告警弹窗：
- 标题："版本校验提示"
- 内容："{tool} {version} 未经预校验，已下载并计算校验值，后续安装将以此校验"
- 确认按钮：继续安装

### 验证
- `go build ./...` 通过
- `go test ./internal/discovery/... ./internal/installer/... ./internal/manifest/...` 通过
- `npm run build` 通过

### B 测试用例与预期结果

| 用例 | 操作 | 预期结果 |
|---|---|---|
| T2B.1 | `mergeIntoManifest` 传入 discovered=[{version:"2.55.0"}]，现有 latest="2.47.0" | 返回后 manifest 的 `latest` 字段更新为 "2.55.0" |
| T2B.2 | `mergeIntoManifest` 传入 discovered=[{version:"2.47.0"}]，现有 latest="2.55.0" | `latest` 不变，仍为 "2.55.0"（不降级） |
| T2B.3 | `mergeIntoManifest` 传入 discovered=[{version:"1.26.5"},{version:"1.25.3"}]，现有 latest="1.23.2" | `latest` 更新为 "1.26.5"（取最大） |
| T2B.4 | `mergeIntoManifest` 传入 discovered=[{version:"r29"}]，现有 latest="27.2.12479018" | `latest` 不变（非 semver，跳过） |
| T2B.5 | `mergeIntoManifest` 传入 discovered=[{version:"26"}]，现有 latest="25" | `latest` 更新为 "26"（纯数字可比较） |
| T2B.6 | `isVersionNewer("2.55.0", "2.47.0")` | 返回 true |
| T2B.7 | `isVersionNewer("2.47.0", "2.55.0")` | 返回 false |
| T2B.8 | `isVersionNewer("r29", "27.2.12479018")` | 返回 false（非 semver） |
| T2B.9 | `isVersionNewer("4.4.1", "3.31.0")` | 返回 true（跨大版本） |
| T2B.10 | `isVersionNewer("7.0.2", "5.7.3")` | 返回 true（跨大版本） |
| T2B.11 | `isVersionNewer("3.14.6", "3.14.6")` | 返回 false（相等） |
| T2B.12 | `go test ./internal/discovery/...` | 全部通过 |

### C 测试用例与预期结果

| 用例 | 操作 | 预期结果 |
|---|---|---|
| T2C.1 | 安装一个 sha256 为空的新版本（如 git 2.55.0），下载完成后 | 安装器计算实际 SHA256，写入 on-disk manifest 对应条目，安装继续不阻拦 |
| T2C.2 | 重复 T2C.1 的安装（第二次安装同版本） | on-disk manifest 中 sha256 已非空，安装器校验下载文件与缓存 SHA256 一致后继续 |
| T2C.3 | 第二次安装时篡改下载文件（hash 不匹配） | 安装器返回 `SHA-256 mismatch` 错误，阻拦安装 |
| T2C.4 | GUI 模式下首次安装无 SHA256 版本 | 前端弹出告警弹窗，显示工具名/版本/提示信息，用户确认后继续 |
| T2C.5 | CLI 模式下首次安装无 SHA256 版本 | 日志输出 WARNING，不阻塞，安装继续 |
| T2C.6 | `UpdateArchiveSHA256("git", "2.55.0", "windows", "amd64", "abc123...")` | on-disk manifest YAML 中对应条目 sha256 字段更新为 "abc123..." |
| T2C.7 | 并发调用 `UpdateArchiveSHA256` 两个不同工具 | 两次写入都成功，无数据损坏（文件锁/原子写） |
| T2C.8 | `UpdateArchiveSHA256` 调用时 on-disk manifest 不存在 | 函数不报错，从 embedded manifest 创建 on-disk 副本后更新 |
| T2C.9 | `go test ./internal/installer/...` | 全部通过 |
| T2C.10 | `npm run build` 通过，前端告警弹窗组件正确渲染 | 构建无错误 |

## Phase 3: 构建验证

- `go build ./...` 通过
- `go test ./...` 通过
- `npm run build` 通过
- `wails build` 通过
- 确认 manifest latest 与上游一致

### Phase 3 测试用例与预期结果

| 用例 | 操作 | 预期结果 |
|---|---|---|
| T3.1 | `go build ./...` | 编译通过，无错误 |
| T3.2 | `go test ./internal/manifest/...` | 全部通过，manifest 解析无异常 |
| T3.3 | `go test ./internal/discovery/...` | 全部通过，含 isVersionNewer 和 mergeIntoManifest latest 更新测试 |
| T3.4 | `go test ./internal/installer/...` | 全部通过，含 TOFU 首次计算和二次校验测试 |
| T3.5 | `go test ./...` | 全部通过，无回归 |
| T3.6 | `npm run build` | 前端构建通过，告警弹窗组件正确打包 |
| T3.7 | `npm run test:unit -- --run` | 全部通过（pre-existing failures 可接受） |
| T3.8 | `wails build` | GUI 构建通过，bindings 正常生成 |
| T3.9 | 启动 GUI，查看工具目录页 | 24 个更新工具的最新版本与 spec 目标版本一致 |
| T3.10 | 安装一个有 SHA256 的新版本（如 go 1.26.5） | 正常安装，校验通过 |
| T3.11 | 安装一个无 SHA256 的新版本（如 git 2.55.0） | 弹出告警弹窗，确认后安装成功，on-disk manifest 中 sha256 已缓存 |
| T3.12 | 再次安装同一个无 SHA256 版本 | 不再弹窗，校验缓存的 SHA256，安装成功 |
| T3.13 | 手动运行 `xdev sniff` | sniff 发现新版本后，manifest 的 latest 字段自动更新为最新发现版本 |

### Phase 3 性能约束测试

| 用例 | 操作 | 预期结果 | 性能指标 |
|---|---|---|---|
| T3.P1 | `isVersionNewer` 对 1000 对版本号比较 | 全部返回正确结果 | 总耗时 < 10ms（单次 < 10μs） |
| T3.P2 | `mergeIntoManifest` 对一个含 50 个版本的工具进行合并（追加 10 个新版本 + 更新 latest） | 正确追加并更新 latest | 总耗时 < 50ms（含 YAML 反序列化+序列化+文件写） |
| T3.P3 | `UpdateArchiveSHA256` 对一个 200KB 的 on-disk manifest YAML 进行单条 sha256 更新 | 正确写入，原子替换 | 总耗时 < 30ms（含读+改+写+fsync） |
| T3.P4 | 并发 10 个 `UpdateArchiveSHA256` 调用（不同工具） | 全部成功，无数据损坏 | 总耗时 < 300ms（文件锁串行化，每次 < 30ms） |
| T3.P5 | SHA256 获取工具对 17 个可获取工具的全部平台（3 平台 × 17 = 51 个请求）发起网络请求 | 成功获取 ≥ 40 个 SHA256（允许少量网络失败） | 总耗时 < 60s（单个请求超时 5s，并发 5 个） |
| T3.P6 | GUI 启动后加载合并后的 catalog（embedded + on-disk，含新增版本条目） | catalog 加载成功 | 加载耗时 < 200ms（与当前 37 工具×3 版本的基线相比增量 < 50ms） |
| T3.P7 | TOFU 首次安装：下载完成后计算 SHA256（对 100MB 文件） | 计算成功，写入 on-disk manifest | SHA256 计算耗时 < 2s（SSD 上 100MB 文件） |
| T3.P8 | TOFU 二次安装：读取 on-disk manifest 缓存的 SHA256 + 校验下载文件 | 校验通过 | 读取 manifest < 30ms + SHA256 计算 < 2s |

### Phase 3 回归安全审计

| 用例 | 审计维度 | 操作 | 预期结果（安全不变式） |
|---|---|---|---|
| T3.S1 | **SHA256 绕过不可扩大化** | 安装一个 sha256 为空的版本，检查安装器是否对 sha256 非空的版本仍强制校验 | sha256 非空的版本仍走完整校验流程，`SHA-256 mismatch` 错误仍阻拦安装。TOFU 仅对空 sha256 生效，不影响已有校验逻辑 |
| T3.S2 | **TOFU 写回不可注入恶意 hash** | 模拟 on-disk manifest 被篡改（sha256 字段被替换为恶意文件的 hash），再次安装 | 安装器下载真实文件后计算的 SHA256 与被篡改的缓存值不匹配，返回 `SHA-256 mismatch` 错误，阻拦安装。篡改的 hash 不能被"信任" |
| T3.S3 | **TOFU 不可绕过 PATH/系统变量保护** | TOFU 流程执行后，检查是否触发任何 PATH 或环境变量写入 | TOFU 仅涉及 sha256 字段写回 manifest YAML，不触碰 PATH、注册表、环境变量白名单。原有白名单防护不变 |
| T3.S4 | **mergeIntoManifest 不可降级 latest** | 现有 latest="2.55.0"，sniff 返回 discovered=[{version:"2.47.0"}] | `latest` 不被降级为 "2.47.0"。`isVersionNewer` 返回 false，latest 保持不变 |
| T3.S5 | **mergeIntoManifest 不可覆盖已有版本** | 现有 versions 含 "2.47.0"（带 sha256），sniff 返回 discovered=[{version:"2.47.0"}] | 已有版本条目不被覆盖，sha256 不丢失。append-only 语义不变 |
| T3.S6 | **isVersionNewer 不可被注入伪造版本号** | `isVersionNewer("2.55.0\x00malicious", "2.47.0")` | 不 panic、不返回 true。含非数字/非点字符的版本号返回 false 或安全处理 |
| T3.S7 | **UpdateArchiveSHA256 不可路径穿越** | 调用 `UpdateArchiveSHA256("../../../etc/passwd", "1.0", ...)` | 工具名被限制为 manifest 中已存在的工具名，不存在则返回 error，不创建任意文件 |
| T3.S8 | **UpdateArchiveSHA256 原子写不可导致数据丢失** | 在 manifest 写回过程中模拟断电（写一半中断） | 原子写（写临时文件 + rename）保证原文件完整。中断后 manifest 仍可正常解析 |
| T3.S9 | **并发 TOFU 写回不可损坏 manifest** | 10 个 goroutine 同时安装不同工具（均无 sha256），各自调用 UpdateArchiveSHA256 | 全部成功，manifest 无数据损坏、无 YAML 解析错误。文件锁保证串行化 |
| T3.S10 | **sniff 自动更新 latest 不可被恶意上游利用** | 模拟恶意 GitHub release 返回极高版本号 "999.999.999" | `isVersionNewer("999.999.999", "2.55.0")` 返回 true，latest 被更新为 "999.999.999"。但该版本无 archive URL，用户无法安装。`DefaultVersion()` 返回该版本但安装时报错"no archive found"。安全风险可接受——sniff 本身就是信任上游 releases |
| T3.S11 | **空 sha256 不可绕过下载来源验证** | 安装一个 sha256 为空的版本，但 URL 指向非 HTTPS 源 | 下载器仍执行 HTTPS 强制校验（`util.ShouldApplyMirror` 和 URL 安全校验逻辑不变）。TOFU 不影响下载来源安全 |
| T3.S12 | **GUI 告警弹窗不可被静默跳过** | GUI 模式下首次安装无 SHA256 版本，检查弹窗是否强制用户确认 | 弹窗为模态对话框，用户必须点击"确认"才能继续安装。不可被自动关闭或跳过 |
| T3.S13 | **回归测试：现有安装流程不受影响** | 安装一个 sha256 非空的已有版本（如 python 3.12.7） | 安装流程与改动前完全一致：下载→校验 SHA256→解压→激活。无行为变化 |
| T3.S14 | **回归测试：现有 uninstall 流程不受影响** | 卸载一个已安装版本 | 卸载流程不变：删除 shims→清理 env vars→删除目录→重定向 current。TOFU 缓存的 sha256 不影响卸载 |
| T3.S15 | **回归测试：现有 sniff 流程不受影响** | 运行 `xdev sniff` 对一个有 remote 配置的工具 | sniff 流程不变：发现版本→mergeIntoManifest 追加。唯一新增行为是 latest 自动更新（如果发现版本更新） |

## 并行分配

Phase 1 完成后，Phase 2 的 B 和 C 可并行：
- @fixer A：Phase 2B（scheduler.go + manifest.go + 测试）
- @fixer B：Phase 2C（installer.go + manifest.go UpdateArchiveSHA256 + GUI 告警弹窗）

两者写入范围有重叠（manifest.go），需要协调：B 写 `isVersionNewer`，C 写 `UpdateArchiveSHA256`，分属不同函数，可并行。

## 风险缓解

| 风险 | 缓解 |
|---|---|
| SHA256 获取工具网络请求失败 | 失败的工具 sha256 留空，走 TOFU 流程 |
| URL 模式随版本变化 | 逐一检查新版本 URL 是否有效 |
| TOFU 写回竞态 | 原子写 + 文件锁 |
| semver 比较边界 | 非数字段跳过，保守策略 |

## 批准记录 (Approval History)

> **批准ID**: `2026-08-08-plan-014-001`
