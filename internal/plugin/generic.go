// generic.go provides the Generic plugin type and shared helpers for
// manifest-driven tools.
//
// The Generic type is a Plugin implementation whose behavior is entirely
// driven by a manifest.Tool YAML struct. Most tools (python, node, go,
// rust, etc.) don't need custom install logic and can simply use Generic
// or embed it and override individual methods.
//
// # Usage
//
// Simple tools (no custom logic):
//
//	plugin.Register(plugin.NewGeneric(manifestTool))
//
// Tools with custom ArchiveFor (e.g. Java with dynamic URL generation):
//
//	type JavaPlugin struct { *plugin.Generic }
//	func (j *JavaPlugin) ArchiveFor(version string) (*manifest.Archive, error) { ... }
//
// # Version Resolution
//
// Generic.ResolveVersion delegates to the resolver package, which supports:
//   - Exact: "3.12.0"
//   - Latest: "latest"
//   - Caret: "^3.12" (compatible, >=3.12.0 <4.0.0)
//   - Tilde: "~3.12" (patch-level, >=3.12.0 <3.13.0)
//   - Prefix: "3.12" (matches highest 3.12.x)
//   - Range: ">=1.20 <2.0"
package plugin

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/resolver"
	"github.com/xmdemt/xdev/internal/util"
)

// HostOS returns the canonical OS name used in manifests
// (windows / darwin / linux).
func HostOS() string {
	switch runtime.GOOS {
	case "windows":
		return "windows"
	case "darwin":
		return "darwin"
	case "linux":
		return "linux"
	}
	return runtime.GOOS
}

// HostArch returns the canonical CPU arch used in manifests.
// Manifests use x86_64 on Linux/macOS and amd64 on Windows.
func HostArch() string {
	switch runtime.GOARCH {
	case "amd64":
		if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
			return "x86_64"
		}
		return "amd64"
	case "arm64":
		return "arm64"
	case "386":
		return "386"
	}
	return runtime.GOARCH
}

// Generic is a Plugin implementation driven entirely by a manifest.Tool.
// Every M2 plugin that does not need custom Install/PostInstall logic
// can simply embed a *Generic and override individual methods when
// necessary.
//
// 票 24（P1-16 / P2-07）：mu 保护 M 指针。reloadCatalog 在嗅探合并后调用
// SetManifest 换掉整个 manifest，而 GUI/installer 的读方（Name/Shims/
// ResolveVersion/ArchiveFor/InstallDir/EnvVars 等 13 个只读方法）可能同时在
// 读同一个 *Generic —— 修复前是无锁的指针写/读竞争。
//
// 注意：加入 mu 后 Generic 成为不可拷贝类型（含 sync.RWMutex）。Generic
// 只以 *Generic 形式使用（NewGeneric / 嵌入 *plugin.Generic），不要按值
// 传递或拷贝；go vet 的 copylocks 检查会把这类拷贝报出来。
type Generic struct {
	mu sync.RWMutex
	M  *manifest.Tool
}

// NewGeneric wraps a manifest.Tool in a Plugin.
func NewGeneric(m *manifest.Tool) *Generic {
	return &Generic{M: m}
}

func (g *Generic) Name() string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.M.Name
}

func (g *Generic) DisplayName() string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.M.DisplayName
}

func (g *Generic) Description() string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.M.Description
}

func (g *Generic) Homepage() string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.M.Homepage
}

func (g *Generic) Category() string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.M.Category
}

// Manifest returns the current manifest pointer. 读方持有该指针期间不受
// SetManifest 的指针替换影响（拿到的是替换前/后的某一个完整快照）；
// 其他路径若就地修改同一个 *manifest.Tool，本锁无法覆盖——请改为调用
// SetManifest 替换指针。
func (g *Generic) Manifest() *manifest.Tool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.M
}

func (g *Generic) Shims() []string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return append([]string{}, g.M.Shims...)
}

func (g *Generic) DefaultVersion() string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.M.DefaultVersion()
}

// SetManifest replaces the in-memory tool manifest after a sniff merge
// so ResolveVersion / ArchiveFor see newly discovered versions without
// restarting the process.
func (g *Generic) SetManifest(m *manifest.Tool) {
	if m == nil {
		return
	}
	// 票 24：与 13 个只读方法的 RLock 配对，整体替换指针。
	g.mu.Lock()
	defer g.mu.Unlock()
	g.M = m
}

// ResolveVersion translates a user spec ("latest", "3.12", "^3.11")
// into a concrete version string that exists in the manifest.
//
// Round 15：**字面版本兜底**。`xdev install <tool>` 不传 @version 时调用方把
// `DefaultVersion()`（即 manifest 的 latest 字面值）当作 spec 传给这里，
// 而 resolver.Parse 只认 semver/约束/latest/lts。semver 形态的字面值碰巧也能
// 被 Parse 认成 KindExact，所以 52 个工具都"刚好"能用；android-ndk 的字面值
// 是 `r29`（字母开头，见随包 manifests/android-ndk.yaml），Parse 直接
// `unrecognised version spec: "r29"` —— 那个工具**根本装不上**（实测纯净 home
// 上 `xdev install android-ndk` exit 1）。
//
// 语义边界：只有在清单里**确实存在**同名字面版本时才放行（顺带兼容带/不带
// "v" 前缀两侧）；未知字面值仍返回 Parse 的原始错误，垃圾输入不被放宽。
func (g *Generic) ResolveVersion(spec string) (string, error) {
	parsed, err := resolver.Parse(spec)
	if err != nil {
		g.mu.RLock()
		defer g.mu.RUnlock()
		if g.M != nil {
			want := strings.TrimPrefix(spec, "v")
			for _, v := range g.M.Versions() {
				if v == spec || strings.TrimPrefix(v, "v") == want {
					return v, nil
				}
			}
		}
		return "", err
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	available := g.M.Versions()
	return resolver.Resolve(parsed, available, g.M.Latest, g.M.LTS)
}

// ArchiveFor picks the right Archive for the current OS/Arch.
func (g *Generic) ArchiveFor(version string) (*manifest.Archive, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	v, ok := g.M.FindVersion(version)
	if !ok {
		return nil, fmt.Errorf("version %s not in manifest", version)
	}
	a, ok := v.ArchiveForCurrent(HostOS(), HostArch())
	if !ok {
		if len(v.Archives) == 0 {
			// 注意：持锁期间不调用 g.Name()（它自己也要 RLock，嵌套 RLock
			// 在写方等待时会死锁），直接读 g.M.Name。
			return nil, fmt.Errorf("no download URL for %s@%s on %s/%s — sniff found the version but could not derive an archive URL from the existing manifest (re-sniff after updating the tool, or install a pinned version)", g.M.Name, version, HostOS(), HostArch())
		}
		return nil, fmt.Errorf("no archive for %s/%s in %s", HostOS(), HostArch(), version)
	}
	return a, nil
}

// InstallDir returns the canonical per-tool install directory:
//
//	<install_dir>/<tool>/<version>
//
// Security: The manifest's Name, InstallRoot, and the version string
// are validated with util.SafeName and the final path is checked with
// util.ValidateSubPath to prevent path traversal attacks (C-1).
//
// 20261021 审计 P1-5（阶段 2）：校验失败返回 ErrUnsafeInstallPath，而不是
// 替换成 `unsafe-version-blocked` 继续（旧行为会在 install/ 下留下伪版本目录）。
func (g *Generic) InstallDir(installRoot, version string) (string, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if _, err := util.SafeName(g.M.Name); err != nil {
		return "", fmt.Errorf("%w: tool name %q: %v", ErrUnsafeInstallPath, g.M.Name, err)
	}
	base := g.M.Name

	if g.M.InstallRoot != "" {
		// Validate install_root doesn't escape the install directory.
		candidate := filepath.Join(installRoot, g.M.InstallRoot, g.M.Name)
		if err := util.ValidateSubPath(installRoot, candidate); err != nil {
			return "", fmt.Errorf("%w: install_root %q escapes %q: %v",
				ErrUnsafeInstallPath, g.M.InstallRoot, installRoot, err)
		}
		base = filepath.Join(g.M.InstallRoot, g.M.Name)
	}

	if _, err := util.SafeName(version); err != nil {
		return "", fmt.Errorf("%w: version %q: %v", ErrUnsafeInstallPath, version, err)
	}

	result := filepath.Join(installRoot, base, version)

	// Final safety check: the resolved path must be under installRoot.
	if err := util.ValidateSubPath(installRoot, result); err != nil {
		return "", fmt.Errorf("%w: %q escapes %q: %v", ErrUnsafeInstallPath, result, installRoot, err)
	}

	return result, nil
}

// EnvVars interpolates {{install}} placeholders in the manifest's
// env_vars map. Backslashes in manifest values are treated as path
// separators and converted to the OS separator (Spec 032): manifests
// are authored on Windows and write `{{install}}\..\gopath`, which
// would otherwise leak literal backslashes into GOPATH on Unix.
func (g *Generic) EnvVars(installPath string) map[string]string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := map[string]string{}
	for k, v := range g.M.EnvVars {
		v = strings.ReplaceAll(v, "{{install}}", installPath)
		if strings.ContainsRune(v, '\\') {
			v = filepath.FromSlash(strings.ReplaceAll(v, "\\", "/"))
		}
		out[k] = v
	}
	return out
}

// PathAppend 返回激活时应追加到 PATH 的目录列表（P1，2026-10-25）。
//
// 为什么需要它：很多语言的包管理器把可执行文件装到自己的 bin 下，
// 而那个目录**不在**安装根，靠 shim 只能找到工具主程序：
//   - Go：  `go install`   → $GOPATH/bin
//   - Rust：`cargo install`→ $CARGO_HOME/bin
//   - Bun： 全局包         → $BUN_INSTALL/bin
// 不追加，用户装完工具会得到 "command not found"。
//
// 模板支持：
//   - {{install}} / {{version}} / {{tool}} —— 与 InstallArgs 同一套
//   - {{<ENVNAME>}} —— 引用**本 manifest 内已定义**的 env_var（先按
//     EnvVars 规则插值后再替换），例如 GOPATH 定义后写 `{{GOPATH}}\bin`，
//     避免两处重复推导同一路径（也就避免了两处写错）
//
// 返回的每一项都保证是绝对路径（installPath 本身是绝对的），
// 且去重 —— 同一目录被多个 manifest 条目引用时不重复追加。
func (g *Generic) PathAppend(installPath string) []string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if len(g.M.PathAppend) == 0 {
		return nil
	}

	// 先算出本 manifest 的 env 变量（已插值），供 {{ENVNAME}} 引用。
	resolvedEnv := make(map[string]string, len(g.M.EnvVars))
	for k, v := range g.M.EnvVars {
		v = strings.ReplaceAll(v, "{{install}}", installPath)
		if strings.ContainsRune(v, '\\') {
			v = filepath.FromSlash(strings.ReplaceAll(v, "\\", "/"))
		}
		resolvedEnv[k] = v
	}

	seen := make(map[string]bool, len(g.M.PathAppend))
	out := make([]string, 0, len(g.M.PathAppend))
	for _, raw := range g.M.PathAppend {
		p := strings.TrimSpace(raw)
		if p == "" {
			continue
		}
		p = strings.ReplaceAll(p, "{{install}}", installPath)
		p = strings.ReplaceAll(p, "{{tool}}", g.M.Name)
		// {{ENVNAME}}：遍历本 manifest 已定义的变量逐个替换。
		// 只支持单层引用（不递归），避免 {{A}} 引用 {{B}} 再引用 {{A}}
		// 这种循环导致死循环。
		for k, v := range resolvedEnv {
			p = strings.ReplaceAll(p, "{{"+k+"}}", v)
		}
		// 剩余反斜杠按路径分隔符处理（与 EnvVars 同一约定）。
		if strings.ContainsRune(p, '\\') {
			p = filepath.FromSlash(strings.ReplaceAll(p, "\\", "/"))
		}
		p = filepath.Clean(p)
		if !filepath.IsAbs(p) {
			// 非绝对路径没有意义（PATH 里相对路径会随 CWD 变化），
			// 跳过而不报错：一个写错的条目不应阻断整个安装。
			continue
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// Ensure *Generic satisfies the Plugin interface at compile time.
var _ Plugin = (*Generic)(nil)

// PostInstall is a no-op for Generic plugins.  Tools that need
// post-install setup (e.g. npm bootstrap) should override this.
func (g *Generic) PostInstall(_ context.Context, _ string) error { return nil }

// Deactivate is a no-op for Generic plugins.  Tools that need
// cleanup on deactivation should override this.
func (g *Generic) Deactivate(_ string) error { return nil }

// RequiredPlugin returns the plugin names that this tool depends on,
// as declared in the manifest's required_plugins field.
func (g *Generic) RequiredPlugin() []string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.M.RequiredPlugin
}
