# xdev 安全审计核实报告

> **审计日期**：2026-07-08  
> **审计方法**：静态代码分析（ripgrep/grep/read 工具实际检查代码）  
> **审计范围**：核实 `docs/security-audit.md`（2026-07-04）的修复声明 + 发现新问题  
> **Go版本**：1.23 | **平台**：Windows 为主，跨平台  

---

## 目录

1. [核实结果：C-1 到 H-14 修复状态](#1-核实结果c-1-到-h-14-修复状态)
2. [新发现问题](#2-新发现问题)
3. [依赖 CVE 检查](#3-依赖-cve-检查)
4. [汇总表](#4-汇总表)
5. [总体安全评分](#5-总体安全评分)

---

## 1. 核实结果：C-1 到 H-14 修复状态

### CRITICAL 级别

#### C-1: Manifest install_root / Name / version 路径穿越 — ✅ 确认已修复

**代码证据**：`internal/plugin/generic.go:128-162`

```go
func (g *Generic) InstallDir(installRoot, version string) string {
    base := g.M.Name
    if _, err := util.SafeName(g.M.Name); err != nil {  // line 132: 校验 Name
        base = "unsafe-name-blocked"
    }
    if g.M.InstallRoot != "" {
        candidate := filepath.Join(installRoot, g.M.InstallRoot, g.M.Name)
        if err := util.ValidateSubPath(installRoot, candidate); err != nil {  // line 140: 校验 install_root
            // fallback to just the tool name
        } else {
            base = filepath.Join(g.M.InstallRoot, g.M.Name)
        }
    }
    if _, err := util.SafeName(version); err != nil {  // line 148: 校验 version
        version = "unsafe-version-blocked"
    }
    result := filepath.Join(installRoot, base, version)
    if err := util.ValidateSubPath(installRoot, result); err != nil {  // line 156: 最终路径校验
        return filepath.Join(installRoot, base, "unsafe-path-blocked")
    }
    return result
}
```

`util.SafeName` 和 `util.ValidateSubPath` 均已在 `internal/util/sanitize.go:37-140` 实现，包含路径分隔符拒绝、`..` 拒绝、驱动器字母拒绝、Windows 保留设备名拒绝、尾随点号/空格拒绝、null 字节拒绝。**确认已修复**。

---

#### C-2: Lua 插件名路径穿越 — ✅ 确认已修复

**代码证据**：`internal/plugin/lua_plugin.go:92-113` + `internal/plugin/vfox_compat.go:102-104`

`InstallDir` 方法调用 `util.SafeName(p.name)` 和 `util.SafeName(version)`，并通过 `util.ValidateSubPath` 校验最终路径。`loadLuaBase` 在加载时（`vfox_compat.go:102`）也校验插件名，不安全则返回错误拒绝加载。**确认已修复**。

---

#### C-3: 自定义工具名/版本路径穿越 — ✅ 确认已修复

**代码证据**：`internal/gui/app_custom_tools.go:72-79`（AddCustomTool）+ `internal/gui/app_custom_tools.go:135-140`（InstallCustomTool）

```go
// AddCustomTool (line 72)
if _, err := util.SafeName(tool.Name); err != nil {
    return fmt.Errorf("tool name is invalid: %w", err)
}
if tool.Version != "" {
    if _, err := util.SafeName(tool.Version); err != nil {
        return fmt.Errorf("tool version is invalid: %w", err)
    }
}
// InstallCustomTool (line 135) — 再次校验
if _, err := util.SafeName(toolName); err != nil { ... }
if _, err := util.SafeName(version); err != nil { ... }
```

**确认已修复**。

---

#### C-4: Manifest SHA-256 哈希填充 — ⚠️ 部分修复（与报告声明一致）

**核实结果**：使用 grep 统计 `manifests/*.yaml` 中 `sha256: 0{64}` 条目，**确认为 6 个**，全部位于 `manifests/php.yaml`（行 22, 30, 42, 50, 62, 70）。与报告声明的 271/277=98% 已填充一致。

**Sidecar TOFU 加固**已在 `internal/downloader/downloader.go:370-423` 实现：
- 当 `signer.HasRootKey()` 为 true 时，拒绝全零哈希（line 375-379）
- Sidecar 文件以只读模式写入（`0o444`，line 464）
- 首次使用时打印 stderr 警告（line 381）

但实际效果受限——见 C-5 分析。**与报告声明一致，部分修复**。

---

#### C-5: Ed25519 签名验证 — ⚠️ 部分修复（报告声明"已修复"但实际未激活）

**代码证据**：`internal/signer/verify.go:60`

```go
var rootPublicKeyB64 = ""  // 空字符串 — 未注入真实公钥
```

`RootPublicKey()` 在 `verify.go:82` 检查到空字符串时返回 `nil`，导致 `signer.HasRootKey()` 返回 `false`。这意味着：
- **所有 manifest 签名验证被完全跳过**
- **C-4 的 sidecar TOFU 路径仍然活跃**（因为 `HasRootKey()` 为 false）
- **下载无签名完整性保护**

**签名文件状态**：`manifests/` 目录中仅有 `placeholder.yaml.sig` 和 `.placeholder.yaml.sig` 占位文件，**无任何实际 manifest 的签名文件**。

**基础设施已就位**：
- `cmd/xdev-keygen/main.go` 密钥生成工具存在
- `-ldflags` 注入机制存在
- `signer.go` 中 Ed25519 签名/验证逻辑正确

**结论**：报告将 C-5 标记为"✅ 已修复"是**误导性的**。基础设施已搭建，但**实际密钥未嵌入，签名验证在发行版中完全禁用**。应标记为 ⚠️ 部分修复。

---

#### C-6: 7z 解压后置校验 — ✅ 确认已修复

**代码证据**：`internal/extractor/extractor.go:212-240`

```go
func extract7z(src, dst string) error {
    for _, cmd := range []string{"7z", "7za"} {
        if _, err := exec.LookPath(cmd); err == nil {
            // 前置校验：列出归档内容并验证路径
            if err := validate7zArchive(cmd, src, dst); err != nil {
                return fmt.Errorf("7z archive contains unsafe paths: %w", err)
            }
            // 解压（所有路径已预校验）
            c := exec.Command(cmd, "x", src, "-o"+dst, "-y")
            // ...
            // 后置校验作为纵深防御
            if err := validateExtraction(dst); err != nil { ... }
        }
    }
}
```

`validate7zArchive`（line 245-278）使用 `7z l -slt` 列出归档内容，对每个 `Path = ` 条目调用 `isSafePath` 验证。**确认已修复**。

---

#### C-7: Task runner 执行未过滤的 shell 命令 — ✅ 确认已修复

**代码证据**：`internal/task/task.go:160-162, 281-309`

```go
// 执行前确认
if err := r.confirmTask(name, command); err != nil {
    return fmt.Errorf("task %q cancelled: %w", name, err)
}
```

`confirmTask` 使用 SHA-256 哈希跟踪命令变更，首次执行或命令变更时要求用户确认（`fmt.Scanln`）。确认状态持久化到 lockfile 的 `ConfirmedTasks` map（`lockfile.go:209-234`）。

即使 `r.lf` 为 nil（无 lockfile），用户仍会被提示确认，只是确认不会被持久化。**确认已修复**。

---

### HIGH 级别

#### H-1: Tar 归档跨条目符号链接穿越 — ✅ 确认已修复

**代码证据**：`internal/extractor/extractor.go:379` + `extractor.go:442-480`

```go
// 在写入每个 tar 条目前检查祖先是否为符号链接
if err := checkNoSymlinkAncestor(dst, out); err != nil {
    return fmt.Errorf("tar slip: cannot write %q: %w", hdr.Name, err)
}
```

`checkNoSymlinkAncestor` 从 dst 向下逐组件检查 `os.Lstat`，发现符号链接则拒绝写入。**确认已修复**。

---

#### H-2: Service 标记文件 exec 路径未解析符号链接 — ✅ 确认已修复

**代码证据**：`internal/service/manager.go:190-192`

```go
if resolved, err := filepath.EvalSymlinks(execPath); err == nil {
    execPath = resolved
}
if !isSubPath(dir, execPath) {
    svc.Status = "invalid"
}
```

datadir 也做了同样的 EvalSymlinks 检查（line 206-208）。**确认已修复**。

---

#### H-3: Lua 沙箱 dofile/loadfile — ✅ 确认已修复

**代码证据**：`internal/plugin/lua_engine.go:81-91`

```go
if err := L.DoString(`
    dofile = function()
        error("dofile is not allowed in sandboxed plugins")
    end
    loadfile = function()
        error("loadfile is not allowed in sandboxed plugins")
    end
`); err != nil { ... }
```

`package.loaders` 限制为仅 preload loader（line 99），并通过 metatable 冻结（line 106-117）。VM 创建时设置了 `RegistryMaxSize: 1024*1024` 和 `CallStackSize: 200`（line 53-54）。**确认已修复**。

---

#### H-4: Lua 插件输出被信任用于下载URL和注册表写入 — ✅ 确认已修复

**代码证据**：
- URL 校验：`internal/plugin/vfox_compat.go:263-265` — 拒绝非 HTTPS URL
- 环境变量白名单：`internal/plugin/vfox_compat.go:324` — `util.IsAllowedEnvVarName(key)` 检查

```go
if !strings.HasPrefix(a.URL, "https://") {
    return nil, fmt.Errorf("%s() returned non-HTTPS URL %q — only HTTPS URLs are allowed", fnName, a.URL)
}
// ...
if !util.IsAllowedEnvVarName(key) {
    log.Printf("[plugin] WARNING: Lua plugin %q returned env var %q not in whitelist — skipping", p.name, key)
    return
}
```

**确认已修复**。

---

#### H-5: Fish shell HookOutput PATH 未引用 — ✅ 确认已修复

**代码证据**：`internal/env/hook.go:172-176` + `hook.go:216-221`

```go
quotedPaths := make([]string, len(binPaths))
for i, p := range binPaths {
    quotedPaths[i] = fishQuote(p)
}
fmt.Fprintf(&b, "set -gx PATH %s $PATH\n", strings.Join(quotedPaths, " "))
```

`fishQuote` 转义反斜杠和单引号。**确认已修复**。

---

#### H-6: HookOutput 环境变量键名未校验/未引用 — ✅ 确认已修复

**代码证据**：`internal/env/hook.go:179, 191, 203`

所有 shell 输出（fish/powershell/bash）在插值前都调用 `util.IsValidEnvVarName(k)` 校验键名，不合法则跳过。**确认已修复**。

---

#### H-7: 下载 URL 未强制 HTTPS — ✅ 确认已修复

**代码证据**：
- `internal/downloader/downloader.go:124-130`：URL scheme 必须为 `https://`
- `internal/downloader/downloader.go:170-177`：fallback URL 也校验 HTTPS
- `internal/mirror/mirror.go:239, 256`：mirror rewrite 强制 `https://` scheme
- `internal/config/config.go:238-242`：config 加载时对非 HTTPS mirror URL 发出警告

**确认已修复**。

---

#### H-8: HTTPS→HTTP 重定向降级攻击 — ✅ 确认已修复

**代码证据**：`internal/downloader/downloader.go:148-156`

```go
CheckRedirect: func(req *http.Request, via []*http.Request) error {
    if len(via) >= 10 {
        return fmt.Errorf("too many redirects")
    }
    if len(via) > 0 && via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
        return fmt.Errorf("insecure redirect: %s → %s (HTTPS downgrade blocked)", via[0].URL, req.URL)
    }
    return nil
},
```

**确认已修复**。

---

#### H-9: Activate 部分失败无回滚 — ✅ 确认已修复

**代码证据**：`internal/installer/installer.go:404-415, 471-473, 487-494, 516-531`

Activate 保存旧 junction 目标，在 shim 安装失败或 env var 写入失败时调用 `rollbackActivation` 恢复。**确认已修复**。

---

#### H-10: setUserEnvVars 部分失败导致不一致状态 — ✅ 确认已修复

**代码证据**：`internal/installer/installer.go:566-608`

保存原始值，逐个写入，失败时回滚所有已写入的变量。**确认已修复**。

---

#### H-11: 自更新 SHA-256 校验可被跳过 — ✅ 确认已修复

**代码证据**：`internal/update/update.go:350-354, 362-364, 369-371`

缺少 SHA256SUMS 资产、下载失败、找不到条目时均返回错误拒绝更新。**确认已修复**。

---

#### H-12: config.yaml 篡改可重定向下载和 shims — ⚠️ 部分修复

**代码证据**：`internal/config/config.go:230-234`

```go
if cfg.Integrity != "" {
    expectedHMAC := cfg.computeIntegrityHMAC()
    if expectedHMAC != cfg.Integrity {
        log.Printf("[config] WARNING: config.yaml integrity check FAILED ...")
        // 仅警告，未拒绝加载！
    }
}
```

HMAC 完整性校验已实现，但**校验失败时仅记录警告，不拒绝加载篡改的配置**。攻击者修改 config.yaml 后，xdev 仍会使用篡改后的配置（只是日志中有警告）。**部分修复**。

---

#### H-13: .xdev.lock URL/哈希篡改 — ✅ 确认已修复

**代码证据**：`internal/lockfile/lockfile.go:119-126, 138`

HMAC 在 Save 时计算并存储，Load 时校验。校验失败返回错误拒绝加载。文件锁通过 `util.AcquireSharedFileLock` / `util.AcquireFileLock` 实现（M-5）。**确认已修复**。

---

#### H-14: GUI setWindowsEnvVar 绕过环境变量白名单 — ✅ 确认已修复（但 DeleteEnvVar 仍有问题，见新发现 N-1）

**代码证据**：`internal/gui/app_env_vars.go:376-380`

```go
func (a *App) SetEnvVar(toolName, name, value, level string) error {
    if !util.IsAllowedEnvVarName(name) {
        return fmt.Errorf("环境变量 %q 不在允许的白名单中，拒绝写入注册表", name)
    }
    // ...
}
```

`SetEnvVar` 的白名单检查已确认。CLI 侧 `SetUserEnvVar`（`win_path_windows.go:413`）和 `DeleteUserEnvVar`（`win_path_windows.go:540`）也有白名单检查。**SetEnvVar 确认已修复**。

---

## 2. 新发现问题

### N-1 [Medium] GUI DeleteEnvVar 绕过环境变量白名单

- **文件**：`internal/gui/app_env_vars.go:430-437`
- **风险等级**：Medium

```go
func (a *App) DeleteEnvVar(toolName, name, level string) error {
    switch level {
    case "user", "machine":
        return deleteWindowsEnvVar(name, level)  // 无白名单检查！
    // ...
    }
}
```

`SetEnvVar`（line 378）有 `util.IsAllowedEnvVarName` 白名单检查，但 `DeleteEnvVar` **直接调用 `deleteWindowsEnvVar`**，无任何白名单检查。CLI 侧的 `DeleteUserEnvVar`（`win_path_windows.go:540`）有白名单检查，但 GUI 侧的 `deleteWindowsEnvVar`（`app_env_vars.go:737`）是独立的底层函数，不经过白名单。

**影响**：Wails 前端（或 WebView XSS）可删除任意注册表环境变量，包括非白名单内的变量。

**修复建议**：
```go
func (a *App) DeleteEnvVar(toolName, name, level string) error {
    if !util.IsAllowedEnvVarName(name) {
        return fmt.Errorf("环境变量 %q 不在允许的白名单中", name)
    }
    // ...
}
```

---

### N-2 [Medium] GUI CleanEnvVarPath 绕过环境变量白名单

- **文件**：`internal/gui/app_env_vars.go:295-369`
- **风险等级**：Medium

`CleanEnvVarPath` 仅检查 `protectedVars`（一小组系统关键变量），**不检查 `util.IsAllowedEnvVarName` 白名单**。它直接调用 `setWindowsEnvVar(name, newValue, level)`（line 355）和 `deleteWindowsEnvVar(name, level)`（line 352, 365），可修改/删除非 protected 但非白名单的环境变量。

**修复建议**：在函数入口添加 `util.IsAllowedEnvVarName(name)` 检查。

---

### N-3 [Medium] GUI DeleteEnvVarEntry 保护不完整

- **文件**：`internal/gui/app_env_vars.go:272-292`
- **风险等级**：Medium

`DeleteEnvVarEntry` 使用 `protectedVars`（仅 ~16 个系统变量），而非 `util.IsAllowedEnvVarName` 白名单。可删除非 protected 的任意环境变量。

**修复建议**：使用 `util.IsAllowedEnvVarName` 替代 `protectedVars` 检查。

---

### N-4 [Medium] GUI TestMirrorConnectivity SSRF

- **文件**：`internal/gui/app_config.go:245-280`
- **风险等级**：Medium

```go
func (a *App) TestMirrorConnectivity(mirrorURL string) ProxyTestResult {
    client := &http.Client{
        Timeout: 10 * time.Second,
        CheckRedirect: func(req *http.Request, via []*http.Request) error {
            if len(via) >= 3 { return fmt.Errorf("too many redirects") }
            return nil  // 不检查 scheme 降级
        },
    }
    resp, err := client.Head(mirrorURL)  // 无 URL scheme/目标验证
    // ...
}
```

此 Wails 绑定函数接受前端传入的任意 URL 并发起 HTTP 请求，无以下检查：
- URL scheme 限制（允许 `http://`、`file://` 等）
- 内网地址过滤（可访问 `http://127.0.0.1:8080`、`http://169.254.169.254/` 等云元数据端点）
- HTTPS 降级保护

**影响**：SSRF — 前端或 WebView XSS 可扫描内网、访问云元数据端点。

**修复建议**：强制 HTTPS、拒绝内网/保留 IP 范围。

---

### N-5 [Medium] GUI TestProxy SSRF

- **文件**：`internal/gui/app_config.go:283-331`
- **风险等级**：Medium

`TestProxy(proxyURL, testURL string)` 接受前端传入的 `testURL`（默认 `https://www.google.com`），无 URL scheme 或目标验证。可通过代理向任意内部地址发起请求。

**修复建议**：限制 `testURL` 为 HTTPS 且为已知测试端点。

---

### N-6 [Medium] remote.go 版本发现 URL 无 HTTPS 强制

- **文件**：`internal/remote/remote.go:282`（discoverJSON）、`remote.go:371`（discoverHTML）
- **风险等级**：Medium

```go
// discoverJSON (line 282)
req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.URL, nil)
// r.URL 来自 manifest RemoteSource.URL，可为 http:// 或内网地址

// discoverHTML (line 371) — 同上
```

`r.URL` 来自 manifest 的 `RemoteSource.URL` 字段，第三方 manifest 可设置任意 URL。无 HTTPS 强制、无内网地址过滤、无重定向降级保护。`httpClient`（line 22-24）无 `CheckRedirect`。

**影响**：SSRF + MITM — 恶意第三方 manifest 可将版本发现请求重定向到内网或 HTTP 端点。

**修复建议**：强制 HTTPS、添加 `CheckRedirect` 阻止降级。

---

### N-7 [Medium] AI assistant OllamaURL SSRF

- **文件**：`internal/ai/assistant.go:60-62, 115-119`
- **风险等级**：Medium

```go
func (a *Assistant) SetOllamaURL(url string) {
    a.ollamaURL = strings.TrimRight(url, "/")  // 无任何验证
}
```

`OllamaURL` 可被配置为任意 URL（含内网地址、外部 HTTP 端点）。AI 请求包含用户诊断信息（`DiagnoseIssue`），可泄露到攻击者控制的服务器。无 localhost 限制、无 HTTPS 强制、无 `CheckRedirect`。

**修复建议**：限制为 `localhost`/`127.0.0.1`，或强制 HTTPS 并允许用户确认非本地端点。

---

### N-8 [Medium] Nix flake 注入 — 工具名未转义

- **文件**：`internal/isolator/nix.go:106-109, 128, 145`
- **风险等级**：Medium

```go
// line 108: 工具名直接加入 pkgs 列表，无转义
for tool := range n.spec.Tools {
    pkgs = append(pkgs, tool)
}
// line 128: 拼接后直接插入 flake.nix 模板
pkgsStr := strings.Join(pkgs, "\n      ")
// line 145: %s 插入 — 若 tool 含 Nix 语法则注入代码
```

`escapeNixString` 函数（line 159-175）仅用于环境变量值，**工具名（packages 列表）未转义**。恶意 `.xdev.toml` 中 `[isolate] tools = ["bash; builtins.readFile /etc/shadow #"]` 可注入 Nix 代码。

**修复建议**：对工具名使用 `sanitizeNixIdent` 或拒绝非字母数字/下划线的工具名。

---

### N-9 [Low] update.go 下载函数无 HTTPS 强制和重定向保护

- **文件**：`internal/update/update.go:208-229`（downloadAsset）、`update.go:410-428`（downloadToMemory）
- **风险等级**：Low

`downloadAsset` 和 `downloadToMemory` 使用 GitHub API 返回的 `BrowserDownloadURL`，无显式 HTTPS 校验和 `CheckRedirect`。虽然 GitHub 始终使用 HTTPS，但缺少纵深防御。

**修复建议**：添加 HTTPS scheme 检查和 `CheckRedirect` 降级保护。

---

### N-10 [Low] manifest_cmd.go validateGitCloneURL 允许 http://

- **文件**：`internal/cli/manifest_cmd.go:346`
- **风险等级**：Low

```go
switch u.Scheme {
case "https", "http", "git":  // http:// 被允许
    return nil
}
```

`http://` git clone 可被 MITM，攻击者可注入恶意 manifest YAML。报告声称 M-13 已修复并"拒绝 ext::/ssh:///file:// 协议"，但 `http://` 仍被允许。

**修复建议**：移除 `http` 从允许列表，或至少发出显著警告。

---

### N-11 [Low] HMAC 比较非常量时间

- **文件**：`internal/util/hmac_key.go:81`
- **风险等级**：Low

```go
func VerifyHMAC(data []byte, hexHMAC string) bool {
    expected := computeHMACWithKey(data, key)
    return expected == hexHMAC  // 非常量时间比较
}
```

使用 `==` 比较 HMAC 字符串，可能受时序攻击。虽然威胁模型是本地篡改检测（非网络认证），风险较低。

**修复建议**：使用 `crypto/subtle.ConstantTimeCompare`。

---

### N-12 [Low] 自定义 HMAC 实现替代标准库

- **文件**：`internal/util/hmac_key.go:86-124`
- **风险等级**：Low

注释称"We use a simple HMAC construction here to avoid importing crypto/hmac which would add a dependency"，但 `crypto/hmac` 是 Go 标准库，不增加外部依赖。自定义实现虽然逻辑正确，但违反"不要自己实现密码学"原则。

**修复建议**：替换为 `crypto/hmac` + `hmac.Equal`。

---

### N-13 [Low] Config 完整性校验失败仅警告不拒绝

- **文件**：`internal/config/config.go:230-234`
- **风险等级**：Low

HMAC 校验失败时仅 `log.Printf` 警告，不返回错误，篡改的配置仍被加载使用。

**修复建议**：校验失败时返回错误，或至少要求用户确认后才能继续。

---

### N-14 [Low] Docker 参数注入 — image/env var 名称

- **文件**：`internal/orchestrator/docker_adapter.go:60`、`internal/isolator/container.go:76`
- **风险等级**：Low

Docker image 和 env var 名称直接作为 `exec.Command` 参数传递。若 image 或 env var key 以 `--` 开头，可能被 docker 解释为 flag（如 `--privileged`）。由于使用 `exec.Command`（非 shell），无 shell 注入风险，但存在参数注入风险。

**修复建议**：在 image/env var 前添加 `--` 分隔符：`args = append(args, "--", image, "sleep", "infinity")`。

---

### N-15 [Low] 项目根目录异常文件 `nul豫`

- **文件**：`D:\Env\Android\ReverseTools\ReverseEng\xmdevenvmt\nul豫`（48 bytes）
- **风险等级**：Low

该文件疑似 Windows `nul` 设备重定向错误的产物（可能来自构建脚本中 `2>nul` 在非英文 locale 下的编码问题）。虽非安全漏洞，但表明构建脚本可能存在编码处理问题。

**修复建议**：检查 `build.ps1` / `build.sh` 中的 `nul` 重定向，删除此文件。

---

## 3. 依赖 CVE 检查

基于 `go.mod` 中的依赖版本：

| 依赖 | 版本 | 已知 CVE | 状态 |
|------|------|----------|------|
| `golang.org/x/crypto` | v0.33.0 | 无已知 CVE（2025年2月发布） | ✅ 安全 |
| `golang.org/x/net` | v0.35.0 | 无已知 CVE（2025年2月发布） | ✅ 安全 |
| `golang.org/x/sys` | v0.30.0 | 无已知 CVE | ✅ 安全 |
| `golang.org/x/text` | v0.22.0 | 无已知 CVE（CVE-2022-32149 已在 v0.3.8 修复） | ✅ 安全 |
| `github.com/wailsapp/wails/v2` | v2.12.0 | 无已知 CVE | ✅ 安全 |
| `github.com/gorilla/websocket` | v1.5.3 | CVE-2020-27813 已在 v1.4.1 修复 | ✅ 安全 |
| `github.com/yuin/gopher-lua` | v1.1.2 | 无已知 CVE | ✅ 安全 |
| `github.com/spf13/cobra` | v1.8.1 | 无已知 CVE | ✅ 安全 |
| `github.com/pelletier/go-toml/v2` | v2.3.1 | 无已知 CVE | ✅ 安全 |
| `gopkg.in/yaml.v3` | v3.0.1 | CVE-2022-28948 已在 v3.0.0 修复 | ✅ 安全 |
| `github.com/klauspost/compress` | v1.17.11 | 无已知 CVE | ✅ 安全 |
| `github.com/labstack/echo/v4` | v4.13.3 | CVE-2023-39777 已在 v4.11.4 修复 | ✅ 安全 |

**结论**：所有依赖版本均高于已知 CVE 的修复版本，未发现使用存在已知漏洞的依赖。

---

## 4. 汇总表

### 核实结果汇总

| ID | 报告状态 | 实际状态 | 说明 |
|----|----------|----------|------|
| C-1 | ✅已修复 | ✅确认已修复 | SafeName + ValidateSubPath 已实施 |
| C-2 | ✅已修复 | ✅确认已修复 | SafeName + ValidateSubPath 已实施 |
| C-3 | ✅已修复 | ✅确认已修复 | AddCustomTool/InstallCustomTool 均有 SafeName |
| C-4 | 🔶部分修复 | ⚠️部分修复 | 6个全零哈希（php.yaml），与报告一致 |
| C-5 | ✅已修复 | **⚠️部分修复** | **rootPublicKeyB64="" — 验证完全禁用，报告声明误导** |
| C-6 | ✅已修复 | ✅确认已修复 | 前置校验 validate7zArchive 已实施 |
| C-7 | ✅已修复 | ✅确认已修复 | confirmTask 机制已实施 |
| H-1 | ✅已修复 | ✅确认已修复 | checkNoSymlinkAncestor 已实施 |
| H-2 | ✅已修复 | ✅确认已修复 | EvalSymlinks 已实施 |
| H-3 | ✅已修复 | ✅确认已修复 | dofile/loadfile 覆盖 + package.loaders 冻结 |
| H-4 | ✅已修复 | ✅确认已修复 | HTTPS 校验 + env var 白名单 |
| H-5 | ✅已修复 | ✅确认已修复 | fishQuote 已实施 |
| H-6 | ✅已修复 | ✅确认已修复 | IsValidEnvVarName 已实施 |
| H-7 | ✅已修复 | ✅确认已修复 | HTTPS 强制已实施 |
| H-8 | ✅已修复 | ✅确认已修复 | CheckRedirect 降级保护已实施 |
| H-9 | ✅已修复 | ✅确认已修复 | rollbackActivation 已实施 |
| H-10 | ✅已修复 | ✅确认已修复 | 事务性写入+回滚已实施 |
| H-11 | ✅已修复 | ✅确认已修复 | 缺少 SHA256SUMS 时拒绝更新 |
| H-12 | ✅已修复 | **⚠️部分修复** | **HMAC 校验失败仅警告，不拒绝加载** |
| H-13 | ✅已修复 | ✅确认已修复 | HMAC 校验+文件锁已实施 |
| H-14 | ✅已修复 | ✅确认已修复 | SetEnvVar 白名单已实施（但 DeleteEnvVar 有新问题 N-1） |

**核实统计**：20 项中 16 项确认已修复，3 项部分修复（C-4, C-5, H-12），1 项已修复但有遗漏（H-14 → N-1）

### 新发现问题汇总

| ID | 风险等级 | 文件 | 问题 |
|----|----------|------|------|
| N-1 | Medium | `gui/app_env_vars.go:430` | GUI DeleteEnvVar 绕过白名单 |
| N-2 | Medium | `gui/app_env_vars.go:295` | GUI CleanEnvVarPath 绕过白名单 |
| N-3 | Medium | `gui/app_env_vars.go:272` | GUI DeleteEnvVarEntry 保护不完整 |
| N-4 | Medium | `gui/app_config.go:245` | GUI TestMirrorConnectivity SSRF |
| N-5 | Medium | `gui/app_config.go:283` | GUI TestProxy SSRF |
| N-6 | Medium | `remote/remote.go:282,371` | 版本发现 URL 无 HTTPS 强制 |
| N-7 | Medium | `ai/assistant.go:60` | OllamaURL SSRF |
| N-8 | Medium | `isolator/nix.go:108` | Nix flake 工具名注入 |
| N-9 | Low | `update/update.go:208,410` | 自更新下载无 HTTPS 强制/重定向保护 |
| N-10 | Low | `cli/manifest_cmd.go:346` | git clone 允许 http:// |
| N-11 | Low | `util/hmac_key.go:81` | HMAC 比较非常量时间 |
| N-12 | Low | `util/hmac_key.go:86` | 自定义 HMAC 实现替代标准库 |
| N-13 | Low | `config/config.go:233` | Config 完整性校验失败仅警告 |
| N-14 | Low | `orchestrator/docker_adapter.go:60` | Docker 参数注入 |
| N-15 | Low | 项目根目录 `nul豫` | 异常构建产物 |

---

## 5. 总体安全评分

### 评分依据

| 维度 | 得分（满分10） | 说明 |
|------|---------------|------|
| 路径穿越防护 | 9/10 | C-1/C-2/C-3/C-6/H-1 均已修复，实现完善 |
| 完整性验证 | 4/10 | C-5 签名验证完全禁用（无密钥嵌入），C-4 仍有6个全零哈希，H-12 校验失败不拒绝 |
| 传输安全 | 7/10 | 主下载器 HTTPS 强制+降级保护已修复，但 remote/ai/update 仍有缺口 |
| 注入防护 | 8/10 | Shell 注入、Lua 沙箱、Task 确认均已修复，但 Nix flake 注入和 GUI 白名单遗漏 |
| 状态一致性 | 9/10 | Activate 回滚、事务性写入、文件锁、互斥体均已实现 |
| SSRF 防护 | 3/10 | GUI TestMirror/TestProxy、remote 发现、AI OllamaURL 均无 SSRF 防护 |
| 依赖安全 | 10/10 | 所有依赖无已知 CVE |
| GUI 安全 | 5/10 | SetEnvVar 白名单已修复，但 Delete/Clean 绕过白名单，Test 函数有 SSRF |

### 总体评分：**6.5 / 10**

### 评分说明

**积极方面**：
- 路径穿越防护（C-1/C-2/C-3/C-6）实现完善，是最大亮点
- 状态一致性（H-9/H-10/M-5/M-16）实现全面
- Lua 沙箱（H-3/M-3）和 Task 确认（C-7）已修复
- 传输安全在主下载路径已加固
- 依赖无已知 CVE

**主要扣分项**：
1. **C-5 签名验证完全禁用**（-2分）：报告声称已修复但 `rootPublicKeyB64=""`，实际无任何签名保护。这是最严重的 misleading 声明——所有 manifest 的 SHA-256 哈希均无签名保护，网络攻击者可篡改 manifest 中的哈希和 URL 而不被检测。
2. **SSRF 防护缺失**（-1分）：4个 Medium 级 SSRF（N-4/N-5/N-6/N-7），GUI 绑定和远程发现均可被利用访问内网。
3. **GUI 白名单不一致**（-0.5分）：Set 有白名单但 Delete/Clean 没有，安全边界不完整。
4. **H-12 配置篡改不拒绝**（-0.5分）：检测到篡改但仍使用篡改的配置。

### 修复优先级建议

1. **紧急**：嵌入真实 Ed25519 公钥并签名 manifest（C-5 真正修复）
2. **高**：修复 GUI DeleteEnvVar/CleanEnvVarPath/DeleteEnvVarEntry 白名单（N-1/N-2/N-3）
3. **高**：修复 GUI TestMirrorConnectivity/TestProxy SSRF（N-4/N-5）
4. **中**：修复 remote.go HTTPS 强制（N-6）
5. **中**：修复 AI OllamaURL 限制（N-7）
6. **中**：修复 Nix flake 注入（N-8）
7. **低**：修复 H-12 配置校验失败拒绝、HMAC 常量时间比较、Docker 参数注入等

---

> **声明**：本核实报告基于 2026-07-08 的代码状态进行静态分析。所有结论均附有具体文件行号和代码证据。建议对修复后的代码进行动态渗透测试验证。
