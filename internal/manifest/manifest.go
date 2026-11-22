// Package manifest describes the metadata of supported tools and provides
// loading, caching, and version enumeration capabilities.
//
// A manifest is a YAML file shipped in this repository under
// /manifests/<tool>.yaml, describing:
//
//   - how to enumerate remote versions (e.g. scrape a JSON API)
//   - what artefacts (archives / installers) each version ships
//   - what shims the tool needs (e.g. python, python3)
//   - what environment variables to set on activation
//
// # Loading
//
// Manifests can be loaded from three sources (in priority order):
//  1. Embedded FS — manifests baked into the binary via go:embed, so
//     the tool works even without the manifests/ directory on disk
//  2. Disk directory — $XMDEV_HOME/manifests/ for user-customized manifests
//  3. Repository-relative — manifests/ in the source tree (dev mode)
//
// LoadCatalogCached caches the merged catalog on disk.
//
// # Remote Version Discovery
//
// When a manifest has a RemoteSource configured (e.g. type: "github",
// url: "golang/go"), the refresh command can fetch the latest version
// list from the upstream source and merge it into the local manifest.
//
// # Template Variables
//
// The EnvVars map supports "{{install}}" template variables that are
// replaced at activation time with the actual installation path.
// For example: {"GOROOT": "{{install}}"} becomes {"GOROOT": "E:/XDev/install/go/1.22.0"}.
//
// # SHA-256 Placeholders
//
// Archive SHA-256 fields that are all zeros (e.g. "000000...") are
// treated as placeholders — verification is skipped for these entries.
// This allows shipping manifests before all hashes have been computed.
// Use `xdev fill-sha256` to populate them before release.
package manifest

import (
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/xmdemt/xdev/internal/util"
	"gopkg.in/yaml.v3"
)

// embeddedFS is set by InitEmbedded to provide manifests baked
// into the binary so the tool works even when the manifests/
// directory is not present on disk.
var embeddedFS fs.FS

// Tool describes one supported tool (python, node, go, ...).
type Tool struct {
	Name           string            `yaml:"name"`
	DisplayName    string            `yaml:"display_name"`
	Description    string            `yaml:"description"`
	Homepage       string            `yaml:"homepage"`
	Category       string            `yaml:"category"` // language | mobile | build | system
	Shims          []string          `yaml:"shims"`
	EnvVars        map[string]string `yaml:"env_vars"`
	VersionList    []Version         `yaml:"versions"`
	Remote         RemoteSource      `yaml:"remote"`
	Latest         string            `yaml:"latest"`
	LTS            string            `yaml:"lts"`
	InstallRoot    string            `yaml:"install_root"`
	RequiredPlugin []string          `yaml:"required_plugins,omitempty"` // required plugin name list
	AutoSniff      *bool             `yaml:"auto_sniff,omitempty"`       // nil=true (default allow sniff), explicit false=disable
	// Depends 声明工具的可选依赖（平台/工具版本约束），用于安装前依赖提示（Spec 023）。
	Depends []Dependency `yaml:"depends,omitempty"`
	// FormFactor 声明工具的使用形态（gui/cli/tui，可组合）。缺省由 Shims 推断。
	FormFactor []string `yaml:"form_factor,omitempty"`
	// GUIBinary GUI 主二进制（相对 install 根），form_factor 含 gui 时使用。
	GUIBinary string `yaml:"gui_binary,omitempty"`
	// PathAppend 声明激活时需追加到 PATH 的目录（P1，2026-10-25）。
	//
	// 背景：很多语言的包管理器会把可执行文件装到自己的 bin 下，
	// 而这些目录**不在**安装根，靠 shim 只能找到工具主程序：
	//   - Go：  `go install` → $GOPATH/bin（官方文档明确要求加入 PATH）
	//   - Rust：`cargo install` → $CARGO_HOME/bin
	//   - Bun： 全局包 → $BUN_INSTALL/bin
	// 不追加这些目录，用户装完工具会得到 "command not found"。
	//
	// 值支持与 EnvVars 相同的模板：{{install}} / {{version}} / {{tool}}，
	// 另外可用 {{<ENVNAME>}} 引用**本 manifest 内已定义**的 env_var，
	// 例如 GOPATH 定义后写 `{{GOPATH}}\bin`（避免重复推导路径）。
	//
	// 注意：PATH 本身**不能**写进 EnvVars —— setUserEnvVars 会跳过 PATH，
	// 且写注册表 PATH 会与 shim 机制打架。追加必须走本字段。
	PathAppend []string `yaml:"path_append,omitempty"`
	// Installable 是**派生**标记（不来自 YAML）：本平台上是否至少有一个可安装
	// 版本（见 Version.Installable）。LoadCatalog 会为每个工具计算它，供
	// GUI/CLI/doctor 把「看着有版本、其实装不了」的工具显式标出来（票 18 / P2-29）。
	Installable bool `yaml:"-"`
}

// Dependency 描述一个工具的可选依赖约束。
// InstallPath 限定依赖仅在特定安装路径需要（如 npm 安装需 Node，native 免）。
type Dependency struct {
	Tool        string   `yaml:"tool"`
	MinVersion  string   `yaml:"min_version,omitempty"`
	Platforms   []string `yaml:"platforms,omitempty"`
	InstallPath string   `yaml:"install_path,omitempty"`
}

// ValidateDepends 校验 depends 声明（宽松模式，仅警告不报错，保证向后兼容）：
//   - tool 名称为空 → 警告
//   - min_version 语法非法 → 警告并清空该约束（Depends[i].MinVersion = ""），
//     避免 Phase B 依赖检查读到畸形版本串
//   - platforms 含非法值（非 windows|darwin|linux|any）→ 警告
//
// 在目录加载时对每个 Tool 调用。
func (t *Tool) ValidateDepends() {
	for i := range t.Depends {
		d := &t.Depends[i]
		if strings.TrimSpace(d.Tool) == "" {
			log.Printf("[manifest] WARNING: tool %q has a dependency with empty tool name", t.Name)
		}
		if mv := strings.TrimSpace(d.MinVersion); mv != "" && !isValidVersionGrammar(mv) {
			log.Printf("[manifest] WARNING: tool %q dependency %q has invalid min_version %q; version constraint cleared", t.Name, d.Tool, d.MinVersion)
			d.MinVersion = ""
		}
		for _, p := range d.Platforms {
			switch p {
			case "windows", "darwin", "linux", "any":
			default:
				log.Printf("[manifest] WARNING: tool %q dependency %q has invalid platform %q (want windows|darwin|linux|any)", t.Name, d.Tool, p)
			}
		}
	}
}

// versionGrammar 是 min_version 支持的全部语法：可选 "v" 前缀 + 1~3 个严格
// 数字点分段（如 22、22.0、v22.0.0）。契约（023-R5）：
//   - 仅数字比较语义（与 util.ParseSemverParts 的 3 段比较一致）；
//   - 拒绝 4 段及以上（如 "22.0.0.1"，ParseSemverParts 会静默截断比较）、
//     拒绝 prerelease/build 后缀（如 "22.0.0-beta.1"）、拒绝数字前缀垃圾
//     （如 "22oops"）。
//
// 严格语法避免 ParseSemverParts 的隐式容错掩盖笔误。
var versionGrammar = regexp.MustCompile(`^v?\d+(\.\d+){0,2}$`)

// isValidVersionGrammar 报告 s 是否匹配完整版本语法（versionGrammar）。
func isValidVersionGrammar(s string) bool {
	return versionGrammar.MatchString(strings.TrimSpace(s))
}

// EffectiveFormFactor 返回工具的有效形态列表：
// 显式声明时原样返回（包括空列表 form_factor: []，表示禁用所有形态）；
// 未声明（nil）时按 Shims 推断（有 shims → [cli]，否则 nil）。
// 向后兼容：现有 39 个 manifest（无 form_factor 字段 → nil）推断为 [cli]。
func (t *Tool) EffectiveFormFactor() []string {
	if t.FormFactor != nil {
		return t.FormFactor
	}
	if len(t.Shims) > 0 {
		return []string{"cli"}
	}
	return nil
}

// HasFormSpecificArchives 报告该工具是否有按形态区分的独立 archive
// （Spec 025）。遍历所有 version 的所有 archive，若任一 archive 的 Form
// 字段非空（如 "gui" / "cli"），返回 true——表示安装时应显示形态选择器，
// 让用户选择下载哪个形态对应的 archive。
//
// 缺省（无 Form 标注，现有 50 个 manifest）返回 false：单包多形态工具
// （如 VS Code 的 code.exe 既是 GUI 又是 CLI）不显示选择器，直接安装。
func (t *Tool) HasFormSpecificArchives() bool {
	return len(t.AvailableArchiveForms()) > 0
}

// AvailableArchiveForms 返回所有 version 的 archive.Form 去重列表（小写，
// 保持首次出现顺序）。形态选择器应使用本列表，而不是 form_factor 声明。
func (t *Tool) AvailableArchiveForms() []string {
	if t == nil {
		return nil
	}
	seen := make(map[string]bool)
	var forms []string
	for vi := range t.VersionList {
		for ai := range t.VersionList[vi].Archives {
			form := strings.ToLower(strings.TrimSpace(t.VersionList[vi].Archives[ai].Form))
			if form == "" || seen[form] {
				continue
			}
			seen[form] = true
			forms = append(forms, form)
		}
	}
	return forms
}

// ValidateFormFactor 校验 form_factor 声明（宽松模式，仅警告不报错）：
// 合法值 ∈ {gui, cli, tui}；非法值仅警告（字段原样保留，前端可展示原始值）。
func (t *Tool) ValidateFormFactor() {
	for _, f := range t.FormFactor {
		switch f {
		case "gui", "cli", "tui":
		default:
			log.Printf("[manifest] WARNING: tool %q has invalid form_factor %q (want gui|cli|tui)", t.Name, f)
		}
	}
}

// ShouldAutoSniff returns true if the tool permits automatic sniff
// discovery. A nil (unset) AutoSniff is treated as true (opt-out model).
func (t *Tool) ShouldAutoSniff() bool {
	return t.AutoSniff == nil || *t.AutoSniff
}

// Version describes a single release of a tool.
type Version struct {
	Version    string    `yaml:"version"` // e.g. "3.12.0"
	Stable     bool      `yaml:"stable"`
	Release    string    `yaml:"release"` // ISO date
	Archives   []Archive `yaml:"archives"`
	Discovered bool      `yaml:"discovered,omitempty"` // sniff-discovered (not built-in)
	Unverified bool      `yaml:"unverified,omitempty"` // archive URL not yet verified
}

// Install kind constants. An Archive's InstallKind field controls how the
// installer treats the downloaded file:
//
//   - "archive"    (default): extract the file (zip/tar.gz/...) into the
//     install directory. The file is never executed.
//   - "executable": the downloaded file is a self-installing binary
//     (e.g. rustup-init.exe). After SHA-256 verification it is
//     run with InstallArgs; it is expected to install itself.
//   - "script":    the downloaded file is a shell script (e.g. sh.rustup.rs).
//     After SHA-256 verification it is executed via "sh".
//   - "npm":       the archive entry is metadata only — no file is
//     downloaded. The tool is an npm package (e.g. @deepseek-ai/dsh)
//     installed by running the npm CLI against the version directory:
//     npm resolves the dependency tree, verifies registry integrity,
//     and generates its launcher shims (dsh.cmd / bin/dsh). Requires
//     Node.js + npm (checked before install, Spec 039).
//
// Unknown values always fall back to "archive" so that a typo or a future
// kind string can never accidentally cause code execution (trust model:
// running untrusted code requires explicit opt-in).
const (
	InstallKindArchive    = "archive"
	InstallKindExecutable = "executable"
	InstallKindScript     = "script"
	InstallKindNPM        = "npm"
)

// Archive is one downloadable file (the manifest can list several
// archives per version, one per OS/arch).
type Archive struct {
	OS            string   `yaml:"os"`     // windows | darwin | linux | any
	Arch          string   `yaml:"arch"`   // amd64 | arm64 | 386 | any
	Format        string   `yaml:"format"` // zip | tar.xz | tar.gz | msi | pkg | dmg | exe | deb
	URL           string   `yaml:"url"`
	MirrorURL     string   `yaml:"mirror_url"`  // optional preferred mirror (singular, legacy)
	MirrorURLs    []string `yaml:"mirror_urls"` // optional multiple mirror candidates (plural, preferred)
	SHA256        string   `yaml:"sha256"`
	MirrorSHA256  string   `yaml:"mirror_sha256,omitempty"` // SHA-256 for mirror_url (if different from official)
	Size          int64    `yaml:"size"`
	Strip         int      `yaml:"strip"`
	InstallArgs   []string `yaml:"install_args"`
	InstallKind   string   `yaml:"install_kind,omitempty"`   // archive|executable|script; empty => archive
	RequiresProxy bool     `yaml:"requires_proxy,omitempty"` // true if the download URL is unreachable without a proxy (e.g. GitHub releases in mainland China)
	// Spec 025: Form 标注该 archive 对应的形态（gui/cli/tui）。
	// 缺省空 = 该 archive 适用于所有形态（向后兼容现有 50 个 manifest）。
	// 仅当工具的某些 archives 有 Form != "" 时（如 OpenCode 的 Desktop App
	// 与 CLI 是独立 archive），安装时才显示形态选择器（HasFormSpecificArchives）。
	Form string `yaml:"form,omitempty"`
}

// Installable reports whether this archive can actually be installed.
//
// 票 18 / P2-29：manifest 里可以存在「有 os/arch 但没有 url/format」的占位条目
// （历史遗留、或嗅探模板尚未补全）。这类条目过去会被 `xdev available` 照常展示，
// 直到用户点安装才失败——实测 mingw 的三个版本全部如此。安装侧要求：
//
//   - npm 型（install_kind: npm）：条目本身只是元数据，由 npm CLI 安装，
//     因此不需要 url/format；
//   - archive / executable / script：必须有 url 与 format。
func (a Archive) Installable() bool {
	if a.EffectiveKind() == InstallKindNPM {
		return true
	}
	return strings.TrimSpace(a.URL) != "" && strings.TrimSpace(a.Format) != ""
}

// CanonicalArch normalizes a CPU-arch spelling so that comparisons survive
// both conventions in play. Manifests normally declare "x86_64" for
// Linux/macOS and "amd64" for Windows (the same convention plugin.HostArch
// implements), while runtime.GOARCH is always "amd64". Comparing verbatim made
// every linux/amd64 archive look foreign: only 1 of 54 tools was reported
// installable on Linux instead of 48. Normalizing both sides also keeps
// fixtures/manifests that spell linux as "amd64" working.
func CanonicalArch(goos, goarch string) string {
	if goarch == "x86_64" && goos == "windows" {
		return "amd64"
	}
	if goarch == "amd64" && (goos == "linux" || goos == "darwin") {
		return "x86_64"
	}
	return goarch
}

// Installable reports whether this version can be installed on the given
// platform (os/arch matching follows the manifest convention: "any" matches).
func (v Version) Installable(goos, goarch string) bool {
	_, ok := v.ArchiveForCurrent(goos, goarch)
	return ok
}

// HasInstallableVersion reports whether at least one version is installable on
// the given platform. LoadCatalog marks such tools via Tool.Installable so the
// GUI/CLI/doctor can flag entries that look available but cannot be installed.
func (t *Tool) HasInstallableVersion(goos, goarch string) bool {
	if t == nil {
		return false
	}
	for _, v := range t.VersionList {
		if v.Installable(goos, goarch) {
			return true
		}
	}
	return false
}

// HasCompleteArchive reports whether ANY version carries a structurally
// complete archive (url + format, or npm kind) on ANY declared platform.
//
// 与 HasInstallableVersion 的区别：后者问「本平台能否安装」（供 UI/doctor 提示），
// 本方法问「这个工具是不是纯占位」（供 manifests 守卫）——守卫必须与宿主平台无关，
// 否则 Windows-only 工具在 Linux CI 上会被误判。实测（2026-09-13）52 个工具里只有
// mingw 曾整体缺失 url/format（票 18 / P2-29）。
func (t *Tool) HasCompleteArchive() bool {
	if t == nil {
		return false
	}
	for _, v := range t.VersionList {
		for _, a := range v.Archives {
			if a.Installable() {
				return true
			}
		}
	}
	return false
}

// EffectiveKind returns the resolved install kind, defaulting to
// InstallKindArchive when the field is empty or holds an unrecognised
// value. Only "executable", "script", and "npm" are returned as-is;
// everything else collapses to the safe "archive" default so that
// execution of downloaded code (or delegation to a package manager)
// always requires an explicit manifest declaration.
func (a Archive) EffectiveKind() string {
	switch a.InstallKind {
	case InstallKindExecutable, InstallKindScript, InstallKindNPM:
		return a.InstallKind
	default:
		return InstallKindArchive
	}
}

// EffectiveMirrorURLs returns the list of candidate mirror URLs for this
// archive.
//
// Priority:
//  1. mirror_urls (plural) — the new preferred field
//  2. mirror_url (singular) — legacy field, wrapped in a single-element slice
//  3. nil — no mirror configured
//
// This ensures backward compatibility: manifests that only set mirror_url
// (singular) continue to work without modification.
func (a Archive) EffectiveMirrorURLs() []string {
	if len(a.MirrorURLs) > 0 {
		return a.MirrorURLs
	}
	if a.MirrorURL != "" {
		return []string{a.MirrorURL}
	}
	return nil
}

// RemoteSource describes how to discover new versions dynamically.
type RemoteSource struct {
	Type    string `yaml:"type"`    // github | html | json | npm | none
	URL     string `yaml:"url"`     // GitHub "owner/repo" or full URL
	Path    string `yaml:"path"`    // JSON dot-separated field path or HTML selector
	Pattern string `yaml:"pattern"` // regex to extract version from tag/text
	// Package 为 npm 远程类型的 npm 包名（type: npm 时使用）。
	Package string `yaml:"package,omitempty"`
}

// Catalog is the full collection of tools, loaded from disk.
type Catalog struct {
	tools map[string]*Tool
}

// LoadCatalog loads embedded and on-disk manifests. Without an embedded base,
// an absent or empty disk collection falls back to the development checkout.
func LoadCatalog(dir string) (*Catalog, error) { return LoadCatalogCached(dir, "") }

func mergeCatalogs(embedded, disk *Catalog) *Catalog {
	// Step 3: Merge.  Start with embedded, then apply disk overrides.
	for name, diskTool := range disk.tools {
		if embeddedTool, exists := embedded.tools[name]; exists {
			// Merge VersionList: embedded versions first, then append
			// disk versions not already in embedded.
			existingVersions := make(map[string]bool, len(embeddedTool.VersionList))
			for _, v := range embeddedTool.VersionList {
				existingVersions[v.Version] = true
			}
			merged := &Tool{}
			*merged = *embeddedTool // shallow copy embedded
			// Deep-copy VersionList so we don't mutate the embedded.
			merged.VersionList = make([]Version, len(embeddedTool.VersionList))
			copy(merged.VersionList, embeddedTool.VersionList)

			// Append disk versions not in embedded, then sort newest first.
			for _, dv := range diskTool.VersionList {
				if !existingVersions[dv.Version] {
					merged.VersionList = append(merged.VersionList, dv)
					existingVersions[dv.Version] = true
				}
			}
			SortToolVersionsByRelease(name, merged.VersionList)
			// Preserve the trusted effective default before appended versions can
			// change the first-entry fallback. Only an accepted disk latest moves it.
			current := embeddedTool.DefaultVersion()
			merged.Latest = current
			if CanPromoteToolLatest(name, merged.VersionList, diskTool.Latest, current, true) {
				merged.Latest = diskTool.Latest
			}
			// 形态字段按 disk-override 策略合并（Spec 023 T-A6，B2-A6-04）：
			// 运行时覆盖内置 manifest 时，磁盘声明的 form_factor / gui_binary
			// 必须生效（与 DisplayName 等其他字段的覆盖语义一致）。
			if diskTool.FormFactor != nil {
				merged.FormFactor = diskTool.FormFactor
			}
			if diskTool.GUIBinary != "" {
				merged.GUIBinary = diskTool.GUIBinary
			}
			embedded.tools[name] = merged
		} else {
			// Tool only in disk — add it.
			embedded.tools[name] = diskTool
		}
	}

	embedded.markInstallability()
	return embedded
}

// loadOneFile 封装"读字节 + 解析 + 校验"的单文件处理逻辑，
// 供并行加载复用。read 函数返回文件字节（os.ReadFile 或 fs.ReadFile）。
// 返回解析后的 *Tool；filename 用于空 name 时回填（从文件名推导）。
func loadOneFile(filename string, read func() ([]byte, error)) (*Tool, error) {
	data, err := read()
	if err != nil {
		return nil, err
	}
	var t Tool
	if err := yaml.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("parse %s: %w", filename, err)
	}
	if t.Name == "" {
		t.Name = strings.TrimSuffix(filename, ".yaml")
	}
	t.ValidateDepends()
	t.ValidateFormFactor()
	return &t, nil
}

// parallelLoadConcurrency 限制 manifest 并行加载的并发度，避免在
// 大目录下同时打开过多文件描述符。
const parallelLoadConcurrency = 8

// loadFromDir is the strict, disk-only loader (no fallback).
func loadFromDir(dir string) (*Catalog, error) {
	files, err := collectSource(os.DirFS(dir), "disk")
	if err != nil {
		return nil, err
	}
	catalog, _, err := parseSource(files, false)
	return catalog, err
}

// thirdPartySubdir 是第三方清单的约定子目录名。
//
// 必须与 cli.thirdPartyManifestDir() 保持一致 —— 那里是写入侧，
// 这里是读取侧，两边分叉就会重现「报成功但不生效」的缺陷。
const thirdPartySubdir = "third-party"

// markInstallability 计算**派生**标记 Tool.Installable：当前平台上是否至少有一个
// 可安装版本（票 18 / P2-29）。
//
// 三个加载出口都必须调用——磁盘加载、内嵌加载、内嵌+磁盘合并：合并在 base 上追加
// 磁盘版本，因此合并结果必须重新计算，否则「磁盘补上了 url/format」的工具仍被标记为
// 不可安装（反之亦然）。
func (c *Catalog) markInstallability() {
	if c == nil {
		return
	}
	goos, goarch := runtime.GOOS, runtime.GOARCH
	for _, t := range c.tools {
		if t == nil {
			continue
		}
		t.Installable = t.HasInstallableVersion(goos, goarch)
	}
}

// Tool returns a tool by name.
func (c *Catalog) Tool(name string) (*Tool, bool) {
	t, ok := c.tools[name]
	return t, ok
}

// Tools returns every tool in the catalog, sorted by name.
func (c *Catalog) Tools() []*Tool {
	out := make([]*Tool, 0, len(c.tools))
	for _, t := range c.tools {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Search returns tools whose name, display name or description match
// the query (case-insensitive substring).
func (c *Catalog) Search(q string) []*Tool {
	q = strings.ToLower(q)
	out := make([]*Tool, 0)
	for _, t := range c.tools {
		if strings.Contains(strings.ToLower(t.Name), q) ||
			strings.Contains(strings.ToLower(t.DisplayName), q) ||
			strings.Contains(strings.ToLower(t.Description), q) {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Versions returns the sorted list of known versions for a tool.
func (t *Tool) Versions() []string {
	out := make([]string, 0, len(t.VersionList))
	for _, v := range t.VersionList {
		out = append(out, v.Version)
	}
	return out
}

// SortVersionsByRelease applies the ordinary numeric release scheme.
func SortVersionsByRelease(versions []Version) { SortToolVersionsByRelease("", versions) }

// VersionReleaseNewer compares display order with the ordinary release scheme.
func VersionReleaseNewer(a, b Version) bool { return versionReleaseNewer(a, b) }
func versionReleaseNewer(a, b Version) bool { return toolReleaseNewer("", a, b) }

func parseReleaseDate(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02", "2006-01-02T15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// FindVersion looks up a concrete version struct.
func (t *Tool) FindVersion(v string) (*Version, bool) {
	for i := range t.VersionList {
		if t.VersionList[i].Version == v {
			return &t.VersionList[i], true
		}
	}
	return nil, false
}

// ArchiveForCurrent picks the archive whose OS/Arch matches the
// current platform, falling back to "any" if no exact match exists.
// The Form field is ignored (legacy form-agnostic selection).
func (v *Version) ArchiveForCurrent(osName, arch string) (*Archive, bool) {
	return v.ArchiveForCurrentWithForm(osName, arch, "")
}

// ArchiveForCurrentWithForm ranks installable candidates by platform first
// (exact OS/arch, exact OS/any arch, any OS), then exact form before generic.
// An empty form accepts all forms. Equal ranks retain manifest order.
func (v *Version) ArchiveForCurrentWithForm(osName, arch, form string) (*Archive, bool) {
	var best *Archive
	bestRank := 6
	wantArch := CanonicalArch(osName, arch)
	for i := range v.Archives {
		a := &v.Archives[i]
		if !a.Installable() || (a.OS != osName && a.OS != "any") || (a.Arch != "any" && CanonicalArch(osName, a.Arch) != wantArch) {
			continue
		}
		rank := 0
		if a.OS != osName {
			rank = 4
		} else if a.Arch == "any" {
			rank = 2
		}
		if form != "" && a.Form != form {
			if a.Form != "" {
				continue
			}
			rank++
		}
		if rank < bestRank {
			best, bestRank = a, rank
		}
	}
	return best, best != nil
}

// DefaultVersion returns Latest, falling back to LTS, then the first
// listed version, then "".
func (t *Tool) DefaultVersion() string {
	if t.Latest != "" {
		return t.Latest
	}
	if t.LTS != "" {
		return t.LTS
	}
	if len(t.VersionList) > 0 {
		return t.VersionList[0].Version
	}
	return ""
}

// MarshalTool serialises a Tool back to YAML bytes.
func MarshalTool(t *Tool) ([]byte, error) {
	return yaml.Marshal(t)
}

// manifestMu protects concurrent writes to on-disk manifest files.
// Using a sync.Mutex is simpler and more portable than file locks
// for in-process concurrency (multiple goroutines in the same binary).
var manifestMu sync.Mutex

// UpdateArchiveSHA256 updates the sha256 field of a specific archive entry
// in the on-disk manifest YAML.
//
// Concurrency (audit-fixes ticket 23 / D-03): the read-modify-write cycle
// is protected by TWO locks — the in-process manifestMu (goroutines inside
// one binary) and a cross-process file lock on <tool>.yaml.lock (this
// process vs another xdev process, e.g. the sniff scheduler appending a
// newly discovered version). Both this function and the scheduler's
// mergeIntoManifest take them in the same order (manifestMu → file lock),
// so there is no lock-order inversion.
//
// Every write in this function (including the initial baseline copy) is
// atomic (temp file + rename) — the function previously claimed atomicity
// while writing the baseline with a bare os.WriteFile.
//
// Tool name is validated against existing catalog — rejects unknown tools
// to prevent path traversal.
//
// If the on-disk manifest does not exist, it is created from the embedded
// manifest (if available) before applying the update.
func UpdateArchiveSHA256(manifestDir, toolName, version, osName, arch, sha256 string) error {
	// --- Validation ---
	if toolName == "" {
		return fmt.Errorf("tool name is empty")
	}
	if strings.Contains(toolName, "..") || strings.ContainsAny(toolName, "/\\") {
		return fmt.Errorf("tool name %q contains path traversal characters", toolName)
	}
	if version == "" || osName == "" || arch == "" {
		return fmt.Errorf("version, osName, and arch are required")
	}
	if sha256 == "" {
		return fmt.Errorf("sha256 is required")
	}

	manifestPath := filepath.Join(manifestDir, toolName+".yaml")

	// --- Lock ---
	manifestMu.Lock()
	defer manifestMu.Unlock()

	// Ticket 23 (D-03): cross-process lock — the in-process mutex alone let
	// another xdev process (sniff scheduler / a second installer) lose this
	// hash update. Stale-safe: OS byte-range locks are released by the
	// kernel when a holder dies.
	lockPath := manifestPath + ".lock"
	fileLock, lockErr := util.AcquireFileLock(lockPath, 30*time.Second)
	if lockErr != nil {
		return fmt.Errorf("manifest lock %s: %w", lockPath, lockErr)
	}
	defer fileLock.Release()

	// --- Read or create from embedded ---
	var tool Tool
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("read manifest %s: %w", manifestPath, err)
		}
		// On-disk manifest does not exist — try embedded.
		if embeddedFS == nil {
			return fmt.Errorf("manifest %s not found on disk and no embedded manifests available", manifestPath)
		}
		embData, embErr := fs.ReadFile(embeddedFS, toolName+".yaml")
		if embErr != nil {
			return fmt.Errorf("manifest %s not found on disk or embedded", manifestPath)
		}
		// Create on-disk copy from embedded — atomic (ticket 23: was a bare
		// os.WriteFile that could leave a truncated manifest on a crash).
		if err := os.MkdirAll(manifestDir, 0o755); err != nil {
			return fmt.Errorf("create manifest dir: %w", err)
		}
		if err := util.AtomicWriteFile(manifestPath, embData, 0o644); err != nil {
			return fmt.Errorf("write initial manifest %s: %w", manifestPath, err)
		}
		data = embData
	}

	if err := yaml.Unmarshal(data, &tool); err != nil {
		return fmt.Errorf("parse manifest %s: %w", manifestPath, err)
	}
	if tool.Name == "" {
		tool.Name = toolName
	}

	// --- Find version and archive ---
	var found bool
	for vi := range tool.VersionList {
		v := &tool.VersionList[vi]
		if v.Version != version {
			continue
		}
		for ai := range v.Archives {
			a := &v.Archives[ai]
			if a.OS == osName && a.Arch == arch {
				a.SHA256 = sha256
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		return fmt.Errorf("archive %s/%s for %s@%s not found in manifest %s",
			osName, arch, toolName, version, manifestPath)
	}

	// --- Atomic write back ---
	out, err := yaml.Marshal(&tool)
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	if err := util.AtomicWriteFile(manifestPath, out, 0o644); err != nil {
		return fmt.Errorf("write manifest %s: %w", manifestPath, err)
	}
	return nil
}

// repoManifestDir tries to find a sibling /manifests directory when
// running from a development checkout.  This is best-effort and only
// used as a fallback when the user's $XMDEV_HOME/manifests is empty.
func repoManifestDir() (string, bool) {
	// Try the executable path first (works in production where the
	// binary is shipped as <root>/bin/xdev).  Walk up to 6
	// parents to cover common layouts (bin/, cmd/, ...).
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		for i := 0; i < 6; i++ {
			candidate := filepath.Join(dir, "manifests")
			if info, err := os.Stat(candidate); err == nil && info.IsDir() {
				return candidate, true
			}
			dir = filepath.Dir(dir)
		}
	}
	// Fallback: walk up from the current working directory.  This
	// makes `go test` (whose binary lives in the build cache) and
	// `xdev run --manifest=...` work without extra wiring.
	if cwd, err := os.Getwd(); err == nil {
		dir := cwd
		for i := 0; i < 6; i++ {
			candidate := filepath.Join(dir, "manifests")
			if info, err := os.Stat(candidate); err == nil && info.IsDir() {
				return candidate, true
			}
			dir = filepath.Dir(dir)
		}
	}
	return "", false
}

// Time helpers - mostly so manifest code can express "released on".

// ParseRelease parses t.Release into time.Time, returning ok=false
// when empty.
func (v *Version) ParseRelease() (time.Time, bool) {
	if v.Release == "" {
		return time.Time{}, false
	}
	t, err := time.Parse("2006-01-02", v.Release)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// ---------------------------------------------------------------------------
// Custom tool manifest generation
// ---------------------------------------------------------------------------

// DiscoveredVersion holds one version discovered from a remote source,
// used by GenerateCustomManifest.
type DiscoveredVersion struct {
	Version  string              `json:"version"`
	Archives []DiscoveredArchive `json:"archives"`
}

// DiscoveredArchive represents an archive file for a specific platform.
type DiscoveredArchive struct {
	URL  string `json:"url"`
	OS   string `json:"os"`
	Arch string `json:"arch"`
}

// GenerateCustomManifest generates a YAML manifest for a user-defined
// custom tool.  The returned YAML includes name, display_name, shims,
// env_vars, versions (with auto-detected archive format, SHA256 left
// empty for TOFU), latest, and remote block.
//
// If shim is empty, name is used as the default shim name.
// If envVar is empty, strings.ToUpper(name) + "_HOME" is used.
// The latest version is set to the first entry in versions.
// Archive format is inferred from the URL suffix (.zip→zip, .tar.gz→tar.gz, .tgz→tar.gz).
func GenerateCustomManifest(name, displayName, shim, envVar string, versions []DiscoveredVersion) ([]byte, error) {
	if shim == "" {
		shim = name
	}
	if envVar == "" {
		envVar = strings.ToUpper(name) + "_HOME"
	}

	tool := Tool{
		Name:        name,
		DisplayName: displayName,
		Shims:       []string{shim},
		EnvVars:     map[string]string{envVar: "{{install}}"},
	}

	for _, dv := range versions {
		v := Version{
			Version:    dv.Version,
			Stable:     true,
			Discovered: true,
			Unverified: true,
		}
		for _, da := range dv.Archives {
			v.Archives = append(v.Archives, Archive{
				URL:    da.URL,
				OS:     da.OS,
				Arch:   da.Arch,
				Format: detectFormat(da.URL),
				SHA256: "",
			})
		}
		tool.VersionList = append(tool.VersionList, v)
	}

	if len(versions) > 0 {
		tool.Latest = versions[0].Version
	}

	return yaml.Marshal(tool)
}

// detectFormat infers the archive format from a URL suffix.
func detectFormat(url string) string {
	lower := strings.ToLower(url)
	switch {
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return "tar.gz"
	case strings.HasSuffix(lower, ".tar.xz"):
		return "tar.xz"
	case strings.HasSuffix(lower, ".tar.bz2"):
		return "tar.bz2"
	case strings.HasSuffix(lower, ".zip"):
		return "zip"
	case strings.HasSuffix(lower, ".7z"):
		return "7z"
	default:
		return "zip"
	}
}
