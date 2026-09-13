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
// LoadCatalogCached() loads from the first available source and caches
// the result in memory for the process lifetime.
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
	"context"
	"crypto/sha256"
	"encoding/json"
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

	"golang.org/x/sync/errgroup"

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

// Installable reports whether this version can be installed on the given
// platform (os/arch matching follows the manifest convention: "any" matches).
func (v Version) Installable(goos, goarch string) bool {
	for _, a := range v.Archives {
		if a.OS != goos && a.OS != "any" {
			continue
		}
		if a.Arch != goarch && a.Arch != "any" {
			continue
		}
		if a.Installable() {
			return true
		}
	}
	return false
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

// LoadCatalog loads every manifests/<tool>.yaml in the given directory.
//
// When embedded manifests are available (via InitEmbedded), this function
// returns a merged catalog: embedded as the base, overlaid with on-disk
// manifests from dir.  For tools that exist in both sources, the on-disk
// version takes priority (includes discovery-merged versions).  For tools
// only in embedded, the embedded version is used.  This ensures that
// sniff-discovered versions written to the on-disk manifest are visible
// to the installer (SC-001).
//
// If the requested directory does not exist OR is empty, we
// transparently fall back to the repo-relative manifests/ directory
// (development mode).  This keeps the first-run experience working
// even before the user has populated their personal manifest cache.
func LoadCatalog(dir string) (*Catalog, error) {
	if embeddedFS != nil {
		return loadMergedCatalog(dir)
	}

	c := &Catalog{tools: map[string]*Tool{}}

	needsFallback := false
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		needsFallback = true
	} else {
		// Directory exists - is it empty?
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, fmt.Errorf("read manifest dir %s: %w", dir, err)
		}
		hasYAML := false
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".yaml") {
				hasYAML = true
				break
			}
		}
		if !hasYAML {
			needsFallback = true
		}
	}

	if needsFallback {
		if alt, ok := repoManifestDir(); ok {
			dir = alt
		} else {
			// No manifests anywhere - return an empty catalog so
			// the rest of the program still works (e.g. `available`
			// prints an empty table rather than crashing).
			return c, nil
		}
	}

	return loadFromDir(dir)
}

// loadMergedCatalog loads embedded manifests as the base, then overlays
// on-disk manifests from dir. For tools in both sources the merge is
// EMBEDDED-FIRST: the embedded VersionList is the base and only disk
// versions not already present are appended; all other fields
// (DisplayName, Shims, EnvVars, archives of existing versions, ...) come
// from the embedded copy. A disk edit of a built-in field therefore has
// NO effect — the practical attack/change surface of an on-disk override
// is a NEW version entry (audit-fixes ticket 13 note; corrected here
// because the previous wording claimed "disk as override", the opposite
// of the implementation).
func loadMergedCatalog(dir string) (*Catalog, error) {
	// Step 1: Load embedded catalog as the base.
	embedded, err := loadFromEmbedded()
	if err != nil {
		return nil, fmt.Errorf("load embedded manifests: %w", err)
	}

	// Step 2: Load on-disk manifests (Spec 024: parallel; single-file
	// failures are skipped, mirroring the original `continue` semantics).
	// 确定性保证（L3 审查修复）：并行解析到按索引保存的 slice，g.Wait()
	// 后按目录顺序串行写入 disk map（重复 Tool.Name 后胜出——与原串行一致）。
	disk := &Catalog{tools: map[string]*Tool{}}
	if _, statErr := os.Stat(dir); statErr == nil {
		if entries, readErr := os.ReadDir(dir); readErr == nil {
			var yamlNames []string
			for _, e := range entries {
				if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
					continue
				}
				yamlNames = append(yamlNames, e.Name())
			}
			n := len(yamlNames)
			results := make([]*Tool, n)
			g, _ := errgroup.WithContext(context.Background())
			sem := make(chan struct{}, parallelLoadConcurrency)
			for i, name := range yamlNames {
				i, name := i, name
				sem <- struct{}{}
				g.Go(func() error {
					defer func() { <-sem }()
					path := filepath.Join(dir, name)
					data, readErr := os.ReadFile(path)
					if readErr != nil {
						// D-06（审计 P1-11）：容错语义保留（坏的单条跳过，
						// 不影响整体加载），但必须可见——静默跳过时坏
						// manifest 完全查不出原因（用户只看到工具"不见了"）。
						// log 包自带互斥，可安全用于这里的并行 goroutine。
						log.Printf("[manifest] WARNING: skip unreadable %s: %v", name, readErr)
						return nil // 单文件读失败跳过（与原 continue 一致）
					}
					var t Tool
					if err := yaml.Unmarshal(data, &t); err != nil {
						// D-06（审计 P1-11）：同上，解析失败也只跳过该条 +
						// 告警，不回退为整体加载失败。
						log.Printf("[manifest] WARNING: skip unparseable %s: %v", name, err)
						return nil // 单文件解析失败跳过（与原 continue 一致）
					}
					if t.Name == "" {
						t.Name = strings.TrimSuffix(name, ".yaml")
					}
					t.ValidateDepends()
					t.ValidateFormFactor()
					results[i] = &t
					return nil
				})
			}
			_ = g.Wait() // 错误忽略（单文件失败跳过，与原 continue 语义一致）
			// 按目录顺序串行写入（重复名后胜出，与原串行一致）
			for i := 0; i < n; i++ {
				if results[i] != nil {
					disk.tools[results[i].Name] = results[i]
				}
			}
		}
	}

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
			SortVersionsByRelease(merged.VersionList)
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
	return embedded, nil
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

// loadFromDir reads every .yaml file in dir and returns a Catalog.
// Spec 024：用 errgroup 并行读取+解析，并发度限制到 8。
//
// 确定性保证（L3 审查修复）：
//   - 并行解析到按文件索引保存的 slice，g.Wait() 后按目录顺序串行写入
//     catalog map，重复 Tool.Name 时"后文件胜出"——与原串行实现一致。
//   - 错误按目录顺序返回第一个遇到的错误（而非最先完成的 goroutine 错误）。
//   - 不在首个错误时 cancel 其他 goroutine：manifest 为小体积本地文件，
//     全量解析成本可接受，确定性优先于提前取消。
func loadFromDir(dir string) (*Catalog, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read manifest dir %s: %w", dir, err)
	}

	var yamlNames []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		yamlNames = append(yamlNames, e.Name())
	}

	n := len(yamlNames)
	results := make([]*Tool, n)
	errs := make([]error, n)

	g, _ := errgroup.WithContext(context.Background())
	sem := make(chan struct{}, parallelLoadConcurrency)

	for i, name := range yamlNames {
		i, name := i, name
		sem <- struct{}{}
		g.Go(func() error {
			defer func() { <-sem }()
			path := filepath.Join(dir, name)
			t, perr := loadOneFile(name, func() ([]byte, error) {
				return os.ReadFile(path)
			})
			if perr != nil {
				errs[i] = perr
				return nil // 收集错误，按目录顺序处理（不在首错时 cancel）
			}
			results[i] = t
			return nil
		})
	}
	_ = g.Wait()

	// 按目录顺序处理：返回第一个遇到的错误；否则写入 map（重复名后胜出）。
	c := &Catalog{tools: map[string]*Tool{}}
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			return nil, errs[i]
		}
		if results[i] != nil {
			c.tools[results[i].Name] = results[i]
		}
	}
	c.markInstallability()
	return c, nil
}

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

// SortVersionsByRelease orders versions newest-first using the total order
// implemented by versionReleaseNewer. Used after sniff-merge so
// available-version lists are not YAML-append order.
func SortVersionsByRelease(versions []Version) {
	sort.SliceStable(versions, func(i, j int) bool {
		return versionReleaseNewer(versions[i], versions[j])
	})
}

// versionReleaseNewer is the comparator behind SortVersionsByRelease
// (audit-fixes ticket 27 / P1-10).
//
// Keys, in order:
//  1. semver (SemVer 2.0.0, pre-release aware) — the PRIMARY key;
//  2. release date — a tie-break only, with a missing date counting as the
//     zero time so a dated entry sorts before an undated one;
//  3. the raw version string — final deterministic tie-break.
//
// The pre-fix comparator put the DATE first. That relation is NOT
// transitive on real manifest data: with A(1.0.0, 2024-06), B(2.0.0,
// 2024-01) and C(1.5.0, no date) it yields A>B (date), B>C (semver), C>A
// (semver) — a cycle. Sort order then depended on input order, and the
// list top (used for `latest` and UpdatePackageManager) could resolve to an
// OLDER version merely because its release string was newer. Ordering by
// the (semver, date, string) tuple is a total order by construction.
func versionReleaseNewer(a, b Version) bool {
	if c := util.CompareSemver(a.Version, b.Version); c != 0 {
		return c > 0
	}
	ta, oka := parseReleaseDate(a.Release)
	tb, okb := parseReleaseDate(b.Release)
	if oka != okb {
		// Equal semver, only one side carries a date: the dated entry
		// sorts first (an absent date is the zero time).
		return oka
	}
	if oka && okb && !ta.Equal(tb) {
		return ta.After(tb)
	}
	return a.Version > b.Version
}

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
	return v.archiveFormAgnostic(osName, arch)
}

// archiveFormAgnostic implements the legacy form-agnostic selection:
//  1. exact os+arch
//  2. os + any arch
//  3. any os
//
// It is shared by ArchiveForCurrent and by ArchiveForCurrentWithForm
// with an empty formFilter. Spec 028: this must NOT delegate back to
// ArchiveForCurrentWithForm — that caused infinite mutual recursion
// (fatal error: stack overflow) on every archive resolution.
func (v *Version) archiveFormAgnostic(osName, arch string) (*Archive, bool) {
	// 1. exact os+arch
	for i := range v.Archives {
		a := &v.Archives[i]
		if a.OS == osName && a.Arch == arch {
			return a, true
		}
	}
	// 2. os + any arch
	for i := range v.Archives {
		a := &v.Archives[i]
		if a.OS == osName && a.Arch == "any" {
			return a, true
		}
	}
	// 3. any os
	for i := range v.Archives {
		a := &v.Archives[i]
		if a.OS == "any" {
			return a, true
		}
	}
	return nil, false
}

// ArchiveForCurrentWithForm is like ArchiveForCurrent but additionally
// filters by form factor (Spec 025). When formFilter is non-empty:
//  1. exact os+arch+form match
//  2. os+arch with Form=="" (form-agnostic archive, backward compatible)
//  3. os+any+form
//  4. os+any with Form==""
//  5. any os+form
//  6. any os with Form=="" (existing fallback)
//
// When formFilter is empty, behaves identically to ArchiveForCurrent
// (ignores Form field entirely) via archiveFormAgnostic — no recursion.
func (v *Version) ArchiveForCurrentWithForm(osName, arch, formFilter string) (*Archive, bool) {
	if formFilter == "" {
		return v.archiveFormAgnostic(osName, arch)
	}
	// formFilter != "": prefer exact form, allow Form=="" fallback
	// Pass 1: exact os+arch+form
	for i := range v.Archives {
		a := &v.Archives[i]
		if a.OS == osName && a.Arch == arch && a.Form == formFilter {
			return a, true
		}
	}
	// Pass 2: exact os+arch with Form=="" (form-agnostic)
	for i := range v.Archives {
		a := &v.Archives[i]
		if a.OS == osName && a.Arch == arch && a.Form == "" {
			return a, true
		}
	}
	// Pass 3: os+any+form
	for i := range v.Archives {
		a := &v.Archives[i]
		if a.OS == osName && a.Arch == "any" && a.Form == formFilter {
			return a, true
		}
	}
	// Pass 4: os+any with Form==""
	for i := range v.Archives {
		a := &v.Archives[i]
		if a.OS == osName && a.Arch == "any" && a.Form == "" {
			return a, true
		}
	}
	// Pass 5: any os+form
	for i := range v.Archives {
		a := &v.Archives[i]
		if a.OS == "any" && a.Form == formFilter {
			return a, true
		}
	}
	// Pass 6: any os with Form==""
	for i := range v.Archives {
		a := &v.Archives[i]
		if a.OS == "any" && a.Form == "" {
			return a, true
		}
	}
	return nil, false
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

// --- Catalog caching ---

// cachedCatalog is the JSON-serializable form of a Catalog used for
// disk-based caching.  The internal map is converted to a sorted slice
// so the serialization is deterministic.
type cachedCatalog struct {
	Tools []*Tool `json:"tools"`
}

// LoadCatalogCached works like LoadCatalog but uses a disk-based cache
// to avoid re-parsing YAML files on every invocation.
//
// If cacheDir is empty, caching is skipped and LoadCatalog is called
// directly (backward compatible).  The cache file is stored at
// <cacheDir>/manifest-cache.dat and is validated against a fingerprint
// of the manifest directory (file names + mod times + sizes).
func LoadCatalogCached(manifestDir string, cacheDir string) (*Catalog, error) {
	if cacheDir == "" {
		return LoadCatalog(manifestDir)
	}

	// When embedded manifests are available, use the executable's
	// modification time as the base fingerprint, AND include the
	// on-disk manifest directory fingerprint so that sniff-discovered
	// updates invalidate the cache (SC-001).
	var fp string
	if embeddedFS != nil {
		// Base: executable modtime (embedded manifest freshness).
		if exe, err := os.Executable(); err == nil {
			if info, err := os.Stat(exe); err == nil {
				fp = fmt.Sprintf("embedded:%d:%d", info.ModTime().UnixNano(), info.Size())
			}
		}
		if fp == "" {
			fp = "embedded:unknown"
		}
		// Extend: include on-disk manifest directory fingerprint if it exists.
		if diskFP, err := fingerprintDiskDir(manifestDir); err == nil {
			fp += "|disk:" + diskFP
		} else {
			fp += "|disk:missing"
		}
	} else {
		// Resolve the actual directory that will be used for loading,
		// so the fingerprint matches the real source of truth.
		actualDir := resolveManifestDir(manifestDir)
		fp = fingerprintDir(actualDir)
	}
	cachePath := filepath.Join(cacheDir, "manifest-cache.dat")

	// Try to read the cached catalog.
	if data, err := os.ReadFile(cachePath); err == nil {
		var entry struct {
			Fingerprint string        `json:"fingerprint"`
			Catalog     cachedCatalog `json:"catalog"`
		}
		if json.Unmarshal(data, &entry) == nil && entry.Fingerprint == fp {
			// Cache hit — reconstruct the Catalog.
			c := &Catalog{tools: map[string]*Tool{}}
			for _, t := range entry.Catalog.Tools {
				c.tools[t.Name] = t
			}
			return c, nil
		}
	}

	// Cache miss — load from disk the slow way.
	cat, err := LoadCatalog(manifestDir)
	if err != nil {
		return nil, err
	}

	// Write cache atomically.
	if err := os.MkdirAll(cacheDir, 0o755); err == nil {
		tools := cat.Tools() // already sorted by name
		entry := struct {
			Fingerprint string        `json:"fingerprint"`
			Catalog     cachedCatalog `json:"catalog"`
		}{
			Fingerprint: fp,
			Catalog:     cachedCatalog{Tools: tools},
		}
		if data, err := json.Marshal(entry); err == nil {
			if err := util.AtomicWriteFile(cachePath, data, 0o644); err != nil {
				log.Printf("[manifest] failed to write cache %s: %v", cachePath, err)
			}
		}
	}

	return cat, nil
}

// resolveManifestDir returns the actual directory from which manifests
// will be loaded, mirroring the fallback logic in LoadCatalog without
// actually reading the files.
func resolveManifestDir(dir string) string {
	needsFallback := false
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		needsFallback = true
	} else {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return dir
		}
		hasYAML := false
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".yaml") {
				hasYAML = true
				break
			}
		}
		if !hasYAML {
			needsFallback = true
		}
	}
	if needsFallback {
		if alt, ok := repoManifestDir(); ok {
			return alt
		}
	}
	return dir
}

// loadFromEmbedded reads all YAML manifests baked into the binary.
// Spec 024：用 errgroup 并行解析，并发度限制到 8。
//
// 确定性保证（L3 审查修复）：
//   - 并行解析到按文件索引保存的 slice，g.Wait() 后按 entries 顺序串行
//     写入 catalog map（重复 Tool.Name 后胜出——与原串行一致）。
//   - 错误按 entries 顺序返回第一个遇到的错误。
//   - 保留原"read embedded"与"parse embedded"两类错误前缀（L3 阻塞项3）。
func loadFromEmbedded() (*Catalog, error) {
	entries, err := fs.ReadDir(embeddedFS, ".")
	if err != nil {
		return nil, fmt.Errorf("read embedded manifests: %w", err)
	}

	var yamlNames []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		yamlNames = append(yamlNames, e.Name())
	}

	n := len(yamlNames)
	results := make([]*Tool, n)
	errs := make([]error, n)

	g, _ := errgroup.WithContext(context.Background())
	sem := make(chan struct{}, parallelLoadConcurrency)

	for i, name := range yamlNames {
		i, name := i, name
		sem <- struct{}{}
		g.Go(func() error {
			defer func() { <-sem }()
			data, readErr := fs.ReadFile(embeddedFS, name)
			if readErr != nil {
				errs[i] = fmt.Errorf("read embedded %s: %w", name, readErr)
				return nil
			}
			var t Tool
			if uerr := yaml.Unmarshal(data, &t); uerr != nil {
				errs[i] = fmt.Errorf("parse embedded %s: %w", name, uerr)
				return nil
			}
			if t.Name == "" {
				t.Name = strings.TrimSuffix(name, ".yaml")
			}
			t.ValidateDepends()
			t.ValidateFormFactor()
			results[i] = &t
			return nil
		})
	}
	_ = g.Wait()

	// 按 entries 顺序处理：返回第一个错误；否则写入 map（重复名后胜出）。
	c := &Catalog{tools: map[string]*Tool{}}
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			return nil, errs[i]
		}
		if results[i] != nil {
			c.tools[results[i].Name] = results[i]
		}
	}
	c.markInstallability()
	return c, nil
}

// fingerprintDir computes a short hash that changes whenever the set of
// .yaml files or their contents change.  It uses file name + mod time +
// size so we don't need to read every file.
func fingerprintDir(dir string) string { return computeFingerprint(dir) }

// fingerprintDiskDir is like fingerprintDir but returns an empty string
// (no error) when the directory does not exist, allowing callers to
// embed it in composite fingerprints.
func fingerprintDiskDir(dir string) (string, error) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return "", nil
	}
	return computeFingerprint(dir), nil
}

// computeFingerprint is the shared implementation.
func computeFingerprint(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		// If we can't read the dir, the fingerprint will be unique
		// each time, effectively disabling the cache.
		return fmt.Sprintf("err:%v:%d", err, time.Now().UnixNano())
	}

	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".yaml") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	var sb strings.Builder
	for _, name := range names {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			sb.WriteString(name)
			sb.WriteString("err")
			continue
		}
		sb.WriteString(name)
		sb.WriteString(fmt.Sprintf("%d:%d", info.ModTime().UnixNano(), info.Size()))
	}

	h := sha256.Sum256([]byte(sb.String()))
	return fmt.Sprintf("%x", h[:16])
}
