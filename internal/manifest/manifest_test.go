package manifest

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"gopkg.in/yaml.v3"
)

func TestLoadCatalogFallsBackToRepoWhenEmpty(t *testing.T) {
	tmp := t.TempDir()
	// tmp/manifests exists but is empty.
	empty := filepath.Join(tmp, "manifests")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	cat, err := LoadCatalog(empty)
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	// The fallback path is the in-repo manifests/ directory; in
	// this dev checkout it must contain at least python, node, go,
	// java, rust, flutter.
	want := []string{"flutter", "go", "java", "node", "python", "rust"}
	for _, name := range want {
		if _, ok := cat.Tool(name); !ok {
			t.Errorf("missing tool %q in fallback catalog: %v", name, toolNames(cat))
		}
	}
}

func TestLoadCatalogFallsBackWhenDirMissing(t *testing.T) {
	// dir does not exist at all - same fallback.
	cat, err := LoadCatalog(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	if _, ok := cat.Tool("python"); !ok {
		t.Errorf("expected fallback to repo manifests, got %v", toolNames(cat))
	}
}

func TestLoadCatalogFromExplicitDir(t *testing.T) {
	// User supplies a directory with a single hand-written manifest.
	dir := t.TempDir()
	yaml := `name: mytool
display_name: My Tool
description: For testing
homepage: https://example.com
category: language
shims:
  - mytool
versions:
  - version: "1.0.0"
    stable: true
    archives:
      - os: windows
        arch: amd64
        format: zip
        url: https://example.com/mytool-1.0.0.zip
        sha256: 0000000000000000000000000000000000000000000000000000000000000000
        size: 1000
        strip: 0
`
	if err := os.WriteFile(filepath.Join(dir, "mytool.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cat, err := LoadCatalog(dir)
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	t1, ok := cat.Tool("mytool")
	if !ok {
		t.Fatalf("expected mytool in catalog, got %v", toolNames(cat))
	}
	if t1.DisplayName != "My Tool" {
		t.Errorf("DisplayName = %q, want %q", t1.DisplayName, "My Tool")
	}
}

func toolNames(c *Catalog) []string {
	tools := c.Tools()
	out := make([]string, 0, len(tools))
	for _, t := range tools {
		out = append(out, t.Name)
	}
	sort.Strings(out)
	return out
}

// ---------- Depends (T-A1) ----------

func TestDepends_Parsed(t *testing.T) {
	yamlStr := `name: demo
display_name: Demo
category: language
depends:
  - tool: node
    min_version: "22.0.0"
    platforms: [windows, darwin]
    install_path: npm
  - tool: git
    platforms: [any]
`
	var tool Tool
	if err := yaml.Unmarshal([]byte(yamlStr), &tool); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(tool.Depends) != 2 {
		t.Fatalf("Depends len = %d, want 2", len(tool.Depends))
	}
	d := tool.Depends[0]
	if d.Tool != "node" || d.MinVersion != "22.0.0" || d.InstallPath != "npm" {
		t.Errorf("depends[0] = %+v, want node/22.0.0/npm", d)
	}
	if len(d.Platforms) != 2 || d.Platforms[0] != "windows" || d.Platforms[1] != "darwin" {
		t.Errorf("depends[0].Platforms = %v, want [windows darwin]", d.Platforms)
	}
	if tool.Depends[1].Tool != "git" || tool.Depends[1].MinVersion != "" || tool.Depends[1].InstallPath != "" {
		t.Errorf("depends[1] = %+v (omitempty fields must stay empty)", tool.Depends[1])
	}
}

func TestDepends_InvalidTolerated(t *testing.T) {
	// 非法值（空 tool、非法 platform、非法 min_version 语法）只警告不报错，
	// manifest 仍正常加载（向后兼容）；非法 min_version 被清空（023-A1-01）。
	var buf bytes.Buffer
	oldWriter := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(oldWriter)

	dir := t.TempDir()
	yamlStr := `name: badtool
display_name: Bad Tool
category: language
depends:
  - tool: ""
    min_version: "not-a-version"
    platforms: [windows, solarium]
  - tool: node
    min_version: "v22.1.0"
    platforms: [any]
  - tool: node
    min_version: "22oops"
  - tool: node
    min_version: "22.0.0.1"
  - tool: node
    min_version: "22.0.0-beta.1"
  - tool: node
    min_version: "22"
`
	if err := os.WriteFile(filepath.Join(dir, "badtool.yaml"), []byte(yamlStr), 0o644); err != nil {
		t.Fatal(err)
	}
	cat, err := LoadCatalog(dir)
	if err != nil {
		t.Fatalf("LoadCatalog with invalid depends must not fail: %v", err)
	}
	tool, ok := cat.Tool("badtool")
	if !ok {
		t.Fatalf("badtool missing from catalog: %v", toolNames(cat))
	}
	if len(tool.Depends) != 6 {
		t.Errorf("Depends len = %d, want 6", len(tool.Depends))
	}
	// 非法 min_version 必须被清空（Phase B 依赖检查不应看到畸形版本串）。
	for i, wantReason := range map[int]string{
		0: "invalid grammar",
		2: "digit-prefix junk '22oops'",
		3: "4-component '22.0.0.1' (beyond 3-part compare)",
		4: "prerelease '22.0.0-beta.1'",
	} {
		if tool.Depends[i].MinVersion != "" {
			t.Errorf("Depends[%d].MinVersion = %q, want cleared (%s)", i, tool.Depends[i].MinVersion, wantReason)
		}
	}
	// 合法语法（v 前缀 + 数字点分，1~3 段）必须保留。
	if tool.Depends[1].MinVersion != "v22.1.0" {
		t.Errorf("Depends[1].MinVersion = %q, want v22.1.0 preserved", tool.Depends[1].MinVersion)
	}
	if tool.Depends[5].MinVersion != "22" {
		t.Errorf("Depends[5].MinVersion = %q, want single-component '22' preserved", tool.Depends[5].MinVersion)
	}
	// 警告必须实际发出（证明校验执行了，而非静默吞掉）。
	out := buf.String()
	for _, want := range []string{"empty tool name", "invalid min_version", "invalid platform"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected warning containing %q in log output:\n%s", want, out)
		}
	}
}

// TestDepends_InvalidCleared_MergedLoad exercises the embedded+disk merged
// load path (loadMergedCatalog): invalid min_version is cleared in both
// embedded and on-disk tools.
func TestDepends_InvalidCleared_MergedLoad(t *testing.T) {
	var buf bytes.Buffer
	oldWriter := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(oldWriter)

	oldFS := embeddedFS
	defer func() { embeddedFS = oldFS }()
	InitEmbedded(fstest.MapFS{
		"embtool.yaml": &fstest.MapFile{Data: []byte(`name: embtool
display_name: Emb Tool
category: language
depends:
  - tool: node
    min_version: "abc"
`)},
	})

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "disktool.yaml"), []byte(`name: disktool
display_name: Disk Tool
category: language
depends:
  - tool: node
    min_version: "22oops"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cat, err := LoadCatalog(dir)
	if err != nil {
		t.Fatalf("LoadCatalog merged: %v", err)
	}
	emb, ok := cat.Tool("embtool")
	if !ok {
		t.Fatalf("embtool missing from merged catalog: %v", toolNames(cat))
	}
	if len(emb.Depends) != 1 || emb.Depends[0].MinVersion != "" {
		t.Errorf("embedded tool invalid min_version not cleared: %+v", emb.Depends)
	}
	disk, ok := cat.Tool("disktool")
	if !ok {
		t.Fatalf("disktool missing from merged catalog: %v", toolNames(cat))
	}
	if len(disk.Depends) != 1 || disk.Depends[0].MinVersion != "" {
		t.Errorf("disk tool invalid min_version not cleared: %+v", disk.Depends)
	}
	out := buf.String()
	if !strings.Contains(out, "invalid min_version") {
		t.Errorf("expected merged-load warning, log:\n%s", out)
	}
}

func TestDepends_MarshalOmitEmpty(t *testing.T) {
	// 无 depends 的 manifest 序列化时不应输出 depends 字段（omitempty 回归）。
	var tool Tool
	out, err := yaml.Marshal(&tool)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "depends") {
		t.Errorf("empty Depends must be omitted from YAML output:\n%s", out)
	}
}

// ---------- FormFactor / GUIBinary (Spec 023 T-A6) ----------

func TestFormFactor_Parsed(t *testing.T) {
	yamlStr := `name: demo
display_name: Demo
category: language
form_factor: [gui, cli]
gui_binary: bin/demo.exe
shims:
  - demo
`
	var tool Tool
	if err := yaml.Unmarshal([]byte(yamlStr), &tool); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(tool.FormFactor) != 2 || tool.FormFactor[0] != "gui" || tool.FormFactor[1] != "cli" {
		t.Errorf("FormFactor = %v, want [gui cli]", tool.FormFactor)
	}
	if tool.GUIBinary != "bin/demo.exe" {
		t.Errorf("GUIBinary = %q, want bin/demo.exe", tool.GUIBinary)
	}
}

func TestEffectiveFormFactor_Explicit(t *testing.T) {
	tool := &Tool{Name: "demo", Shims: []string{"demo"}, FormFactor: []string{"tui"}}
	if got := tool.EffectiveFormFactor(); len(got) != 1 || got[0] != "tui" {
		t.Errorf("EffectiveFormFactor = %v, want [tui] (explicit wins over shims)", got)
	}
}

func TestEffectiveFormFactor_InferredFromShims(t *testing.T) {
	// 有 shims 无 form_factor → [cli]（现有 39 manifest 缺省行为）。
	tool := &Tool{Name: "go", Shims: []string{"go", "gofmt"}}
	got := tool.EffectiveFormFactor()
	if len(got) != 1 || got[0] != "cli" {
		t.Errorf("EffectiveFormFactor = %v, want [cli] (inferred from shims)", got)
	}
}

func TestEffectiveFormFactor_NoShimsNoForm(t *testing.T) {
	tool := &Tool{Name: "bare"}
	if got := tool.EffectiveFormFactor(); got != nil {
		t.Errorf("EffectiveFormFactor = %v, want nil", got)
	}
}

func TestFormFactor_InvalidTolerated(t *testing.T) {
	// 非法 form_factor 只警告不报错，manifest 仍正常加载。
	var buf bytes.Buffer
	oldWriter := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(oldWriter)

	dir := t.TempDir()
	yamlStr := `name: badform
display_name: Bad Form
category: language
form_factor: [gui, windows, exotic]
shims: [badform]
`
	if err := os.WriteFile(filepath.Join(dir, "badform.yaml"), []byte(yamlStr), 0o644); err != nil {
		t.Fatal(err)
	}
	cat, err := LoadCatalog(dir)
	if err != nil {
		t.Fatalf("LoadCatalog with invalid form_factor must not fail: %v", err)
	}
	tool, ok := cat.Tool("badform")
	if !ok {
		t.Fatalf("badform missing from catalog: %v", toolNames(cat))
	}
	if len(tool.FormFactor) != 3 {
		t.Errorf("FormFactor len = %d, want 3 (values preserved)", len(tool.FormFactor))
	}
	if !strings.Contains(buf.String(), "invalid form_factor") {
		t.Errorf("expected warning about invalid form_factor, log:\n%s", buf.String())
	}
}

// TestMergedCatalog_DiskFormFactorOverride verifies that a disk manifest
// overriding a built-in tool can set form_factor / gui_binary (B2-A6-04):
// merged result must carry the disk values even when the embedded tool
// declares none.
func TestMergedCatalog_DiskFormFactorOverride(t *testing.T) {
	oldFS := embeddedFS
	defer func() { embeddedFS = oldFS }()
	// 内置工具：无 form_factor / gui_binary（模拟现有 39 个内置 manifest）。
	InitEmbedded(fstest.MapFS{
		"builtin.yaml": &fstest.MapFile{Data: []byte(`name: builtin
display_name: Builtin
category: language
shims: [builtin]
`)},
	})

	dir := t.TempDir()
	// 磁盘覆盖：声明 gui 形态与 gui_binary。
	if err := os.WriteFile(filepath.Join(dir, "builtin.yaml"), []byte(`name: builtin
display_name: Builtin
category: language
shims: [builtin]
form_factor: [gui, cli]
gui_binary: bin/builtin.exe
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cat, err := LoadCatalog(dir)
	if err != nil {
		t.Fatalf("LoadCatalog merged: %v", err)
	}
	tool, ok := cat.Tool("builtin")
	if !ok {
		t.Fatalf("builtin missing from merged catalog: %v", toolNames(cat))
	}
	if len(tool.FormFactor) != 2 || tool.FormFactor[0] != "gui" || tool.FormFactor[1] != "cli" {
		t.Errorf("merged FormFactor = %v, want [gui cli] (disk override must survive merge)", tool.FormFactor)
	}
	if tool.GUIBinary != "bin/builtin.exe" {
		t.Errorf("merged GUIBinary = %q, want bin/builtin.exe (disk override must survive merge)", tool.GUIBinary)
	}
}

// TestMergedCatalog_DiskNoFormKeepsEmbedded verifies a disk manifest
// WITHOUT form fields does not clobber embedded values (nil/empty no-op).
func TestMergedCatalog_DiskNoFormKeepsEmbedded(t *testing.T) {
	oldFS := embeddedFS
	defer func() { embeddedFS = oldFS }()
	InitEmbedded(fstest.MapFS{
		"builtin.yaml": &fstest.MapFile{Data: []byte(`name: builtin
display_name: Builtin
category: language
shims: [builtin]
form_factor: [gui]
gui_binary: bin/builtin.exe
`)},
	})

	dir := t.TempDir()
	// 磁盘覆盖：未声明形态字段。
	if err := os.WriteFile(filepath.Join(dir, "builtin.yaml"), []byte(`name: builtin
display_name: Builtin
category: language
shims: [builtin]
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cat, err := LoadCatalog(dir)
	if err != nil {
		t.Fatalf("LoadCatalog merged: %v", err)
	}
	tool, ok := cat.Tool("builtin")
	if !ok {
		t.Fatalf("builtin missing from merged catalog: %v", toolNames(cat))
	}
	if len(tool.FormFactor) != 1 || tool.FormFactor[0] != "gui" {
		t.Errorf("merged FormFactor = %v, want embedded [gui] preserved", tool.FormFactor)
	}
	if tool.GUIBinary != "bin/builtin.exe" {
		t.Errorf("merged GUIBinary = %q, want embedded value preserved", tool.GUIBinary)
	}
}

func TestDepends_ExistingManifestsLoadWithoutDepends(t *testing.T) {
	// 回归：现有 manifest（无 depends 字段）加载不受影响。
	dir := t.TempDir()
	yamlStr := `name: plain
display_name: Plain
category: system
shims: [plain]
versions:
  - version: "1.0.0"
    stable: true
    archives:
      - os: windows
        arch: amd64
        format: zip
        url: https://example.com/plain-1.0.0.zip
        sha256: 0000000000000000000000000000000000000000000000000000000000000000
`
	if err := os.WriteFile(filepath.Join(dir, "plain.yaml"), []byte(yamlStr), 0o644); err != nil {
		t.Fatal(err)
	}
	cat, err := LoadCatalog(dir)
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	tool, ok := cat.Tool("plain")
	if !ok {
		t.Fatalf("plain missing from catalog")
	}
	if len(tool.Depends) != 0 {
		t.Errorf("Depends = %+v, want none", tool.Depends)
	}
}

func TestArchiveForCurrentPicksExactOS(t *testing.T) {
	v := &Version{
		Version: "1.0.0",
		Archives: []Archive{
			{OS: "linux", Arch: "x86_64", URL: "linux-x64", Format: "tar.gz"},
			{OS: "windows", Arch: "amd64", URL: "win-amd64", Format: "zip"},
			{OS: "darwin", Arch: "arm64", URL: "mac-arm64", Format: "tar.gz"},
		},
	}
	a, ok := v.ArchiveForCurrent("windows", "amd64")
	if !ok {
		t.Fatal("expected to find windows/amd64 archive")
	}
	if a.URL != "win-amd64" {
		t.Errorf("URL = %q, want win-amd64", a.URL)
	}
	a, ok = v.ArchiveForCurrent("darwin", "arm64")
	if !ok {
		t.Fatal("expected to find darwin/arm64 archive")
	}
	if a.URL != "mac-arm64" {
		t.Errorf("URL = %q, want mac-arm64", a.URL)
	}
}

func TestArchiveForCurrentFallsBackToAnyOS(t *testing.T) {
	v := &Version{
		Version: "1.0.0",
		Archives: []Archive{
			{OS: "any", Arch: "any", URL: "universal", Format: "tar.gz"},
		},
	}
	for _, plat := range [][2]string{{"windows", "amd64"}, {"linux", "x86_64"}, {"darwin", "arm64"}} {
		a, ok := v.ArchiveForCurrent(plat[0], plat[1])
		if !ok {
			t.Errorf("expected to find 'any' archive for %s/%s", plat[0], plat[1])
			continue
		}
		if a.URL != "universal" {
			t.Errorf("%s/%s picked %q", plat[0], plat[1], a.URL)
		}
	}
}

// --- Spec 028 死递归回归（TC-A1~A6）---
// 背景：ArchiveForCurrent ↔ ArchiveForCurrentWithForm("") 曾互相委托，
// 导致任何 archive 解析触发 fatal error: stack overflow。以下测试锁定
// 修复后的语义：空 formFilter 走 legacy form-agnostic 选择，非空走 6-pass。

// TestArchiveForCurrent_NoInfiniteRecursion (TC-A1) 直接入口回归：
// 死递归版本会耗尽 1GB goroutine 栈后 fatal（>1s），正常应在毫秒级返回。
func TestArchiveForCurrent_NoInfiniteRecursion(t *testing.T) {
	v := &Version{
		Version: "1.0.0",
		Archives: []Archive{
			{OS: "windows", Arch: "amd64", URL: "win-amd64", Format: "zip"},
		},
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = v.ArchiveForCurrent("windows", "amd64")
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ArchiveForCurrent did not return within 5s — recursion regression")
	}
}

// TestArchiveForCurrentWithForm_EmptyFilterMatchesLegacy (TC-A2)
// 空 formFilter 与 ArchiveForCurrent 行为完全一致（同一结果指针）。
func TestArchiveForCurrentWithForm_EmptyFilterMatchesLegacy(t *testing.T) {
	v := &Version{
		Version: "1.0.0",
		Archives: []Archive{
			{OS: "windows", Arch: "amd64", URL: "win-amd64", Format: "zip"},
		},
	}
	a1, ok1 := v.ArchiveForCurrent("windows", "amd64")
	a2, ok2 := v.ArchiveForCurrentWithForm("windows", "amd64", "")
	if !ok1 || !ok2 {
		t.Fatalf("both lookups should succeed (legacy=%v, withForm=%v)", ok1, ok2)
	}
	if a1 != a2 {
		t.Error("empty formFilter must behave identically to ArchiveForCurrent")
	}
}

// TestArchiveForCurrent_IgnoresFormField (TC-A3)
// legacy 选择完全忽略 Form 字段：exact os+arch 带 Form 的 archive 仍被选中。
func TestArchiveForCurrent_IgnoresFormField(t *testing.T) {
	v := &Version{
		Version: "1.0.0",
		Archives: []Archive{
			{OS: "windows", Arch: "amd64", URL: "win-gui", Format: "zip", Form: "gui"},
		},
	}
	a, ok := v.ArchiveForCurrent("windows", "amd64")
	if !ok {
		t.Fatal("expected exact os+arch archive even though Form is set")
	}
	if a.URL != "win-gui" {
		t.Errorf("URL = %q, want win-gui (legacy ignores Form)", a.URL)
	}
}

// TestArchiveForCurrent_FallbackOrder (TC-A4)
// legacy 回退顺序：exact os+arch → os+any → any-os。
func TestArchiveForCurrent_FallbackOrder(t *testing.T) {
	v := &Version{
		Version: "1.0.0",
		Archives: []Archive{
			{OS: "linux", Arch: "any", URL: "linux-any", Format: "tar.gz"},
			{OS: "any", Arch: "any", URL: "any-any", Format: "tar.gz"},
		},
	}
	a, ok := v.ArchiveForCurrent("linux", "x86_64")
	if !ok || a.URL != "linux-any" {
		t.Errorf("linux/x86_64 = %q (ok=%v), want linux-any (os+any before any-os)", a.URL, ok)
	}
	a, ok = v.ArchiveForCurrent("windows", "amd64")
	if !ok || a.URL != "any-any" {
		t.Errorf("windows/amd64 = %q (ok=%v), want any-any", a.URL, ok)
	}
}

// TestArchiveForCurrentWithForm_PreferenceAndFallback (TC-A5)
// 非空 formFilter 的 6-pass 语义不回归：Pass 1 精确 Form 优先，
// 匹配不到时 Pass 2 降级到同 os+arch 的 Form=="" archive。
func TestArchiveForCurrentWithForm_PreferenceAndFallback(t *testing.T) {
	v := &Version{
		Version: "1.0.0",
		Archives: []Archive{
			{OS: "windows", Arch: "amd64", URL: "win-gui", Format: "zip", Form: "gui"},
			{OS: "windows", Arch: "amd64", URL: "win-agnostic", Format: "zip"},
		},
	}
	a, ok := v.ArchiveForCurrentWithForm("windows", "amd64", "gui")
	if !ok || a.URL != "win-gui" {
		t.Errorf("form=gui = %q (ok=%v), want win-gui (exact form first)", a.URL, ok)
	}
	a, ok = v.ArchiveForCurrentWithForm("windows", "amd64", "tui")
	if !ok || a.URL != "win-agnostic" {
		t.Errorf("form=tui = %q (ok=%v), want win-agnostic (Form==\"\" fallback)", a.URL, ok)
	}
}

// TestArchiveForCurrentWithForm_EmptyFilterDoesNotRequireEmptyForm (TC-A6)
// 空 formFilter 不进入 6-pass（Pass 1 要求 Form==formFilter=="" 会漏选带
// Form 的 archive）。legacy 语义应选中第一个 exact os+arch，Form 忽略。
func TestArchiveForCurrentWithForm_EmptyFilterDoesNotRequireEmptyForm(t *testing.T) {
	v := &Version{
		Version: "1.0.0",
		Archives: []Archive{
			{OS: "windows", Arch: "amd64", URL: "win-gui", Format: "zip", Form: "gui"},
			{OS: "windows", Arch: "amd64", URL: "win-tui", Format: "zip", Form: "tui"},
		},
	}
	a, ok := v.ArchiveForCurrentWithForm("windows", "amd64", "")
	if !ok {
		t.Fatal("empty formFilter must pick exact os+arch regardless of Form")
	}
	if a.URL != "win-gui" {
		t.Errorf("URL = %q, want win-gui (legacy first exact os+arch match)", a.URL)
	}
}

func TestAvailableArchiveForms_DedupAndOrder(t *testing.T) {
	tool := &Tool{
		FormFactor: []string{"gui", "cli", "tui"},
		VersionList: []Version{
			{Archives: []Archive{
				{Form: "cli"},
				{Form: "CLI"},
				{Form: ""},
			}},
		},
	}
	got := tool.AvailableArchiveForms()
	if len(got) != 1 || got[0] != "cli" {
		t.Errorf("AvailableArchiveForms = %v, want [cli]", got)
	}
	if !tool.HasFormSpecificArchives() {
		t.Error("HasFormSpecificArchives should be true when any archive has Form")
	}
}

func TestDefaultVersionPicksLatest(t *testing.T) {
	t1 := &Tool{
		Latest: "2.0.0",
		LTS:    "1.0.0",
		VersionList: []Version{
			{Version: "1.0.0"},
			{Version: "2.0.0"},
		},
	}
	if got := t1.DefaultVersion(); got != "2.0.0" {
		t.Errorf("DefaultVersion = %q, want 2.0.0", got)
	}
	t2 := &Tool{
		Latest: "",
		LTS:    "1.0.0",
		VersionList: []Version{
			{Version: "1.0.0"},
		},
	}
	if got := t2.DefaultVersion(); got != "1.0.0" {
		t.Errorf("DefaultVersion = %q, want 1.0.0", got)
	}
}

// --- EffectiveKind ---

// TestEffectiveKind_DefaultIsArchive asserts that an Archive with no
// explicit InstallKind is treated as a plain extractable archive.
func TestEffectiveKind_DefaultIsArchive(t *testing.T) {
	a := Archive{}
	if got := a.EffectiveKind(); got != InstallKindArchive {
		t.Errorf("empty InstallKind: EffectiveKind() = %q, want %q", got, InstallKindArchive)
	}
}

func TestEffectiveKind_ExplicitArchive(t *testing.T) {
	a := Archive{InstallKind: InstallKindArchive}
	if got := a.EffectiveKind(); got != InstallKindArchive {
		t.Errorf("explicit archive: EffectiveKind() = %q, want %q", got, InstallKindArchive)
	}
}

func TestEffectiveKind_Executable(t *testing.T) {
	a := Archive{InstallKind: InstallKindExecutable}
	if got := a.EffectiveKind(); got != InstallKindExecutable {
		t.Errorf("executable: EffectiveKind() = %q, want %q", got, InstallKindExecutable)
	}
}

func TestEffectiveKind_Script(t *testing.T) {
	a := Archive{InstallKind: InstallKindScript}
	if got := a.EffectiveKind(); got != InstallKindScript {
		t.Errorf("script: EffectiveKind() = %q, want %q", got, InstallKindScript)
	}
}

// TestEffectiveKind_UnknownFallsBackToArchive enforces the trust model:
// an unrecognised kind must never silently become executable/script
// (those run untrusted code). The safe default is always "archive"
// which only extracts, never executes.
func TestEffectiveKind_UnknownFallsBackToArchive(t *testing.T) {
	a := Archive{InstallKind: "bogus"}
	if got := a.EffectiveKind(); got != InstallKindArchive {
		t.Errorf("unknown kind %q: EffectiveKind() = %q, want %q (safe default)", a.InstallKind, got, InstallKindArchive)
	}
}

// --- AutoSniff ---

func TestShouldAutoSniffNil(t *testing.T) {
	t1 := &Tool{Name: "test", AutoSniff: nil}
	if !t1.ShouldAutoSniff() {
		t.Error("nil AutoSniff should return true (default allow)")
	}
}

func TestShouldAutoSniffExplicitTrue(t *testing.T) {
	v := true
	t1 := &Tool{Name: "test", AutoSniff: &v}
	if !t1.ShouldAutoSniff() {
		t.Error("explicit true AutoSniff should return true")
	}
}

func TestShouldAutoSniffExplicitFalse(t *testing.T) {
	v := false
	t1 := &Tool{Name: "test", AutoSniff: &v}
	if t1.ShouldAutoSniff() {
		t.Error("explicit false AutoSniff should return false")
	}
}

// --- Discovered / Unverified ---

func TestVersionDiscoveredUnverifiedDefaults(t *testing.T) {
	// Manifests without discovered/unverified fields should parse with false defaults
	yamlData := `name: mytool
display_name: My Tool
description: A test tool
version: "1.0.0"
versions:
  - version: "1.0.0"
    stable: true
    release: "2026-01-01"
    archives:
      - os: windows
        arch: amd64
        format: zip
        url: https://example.com/mytool-1.0.0.zip
        sha256: 0000000000000000000000000000000000000000000000000000000000000000
        size: 1000
        strip: 0
`
	var t1 Tool
	if err := yaml.Unmarshal([]byte(yamlData), &t1); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	if len(t1.VersionList) == 0 {
		t.Fatal("expected at least one version")
	}
	v := t1.VersionList[0]
	if v.Discovered {
		t.Error("Discovered should be false by default")
	}
	if v.Unverified {
		t.Error("Unverified should be false by default")
	}
}

func TestVersionDiscoveredUnverifiedExplicit(t *testing.T) {
	yamlData := `name: mytool
versions:
  - version: "2.0.0"
    stable: true
    discovered: true
    unverified: true
    archives:
      - os: any
        arch: any
        format: zip
        url: https://example.com/mytool-2.0.0.zip
        sha256: 0000000000000000000000000000000000000000000000000000000000000000
        size: 500
        strip: 0
`
	var t1 Tool
	if err := yaml.Unmarshal([]byte(yamlData), &t1); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	if len(t1.VersionList) == 0 {
		t.Fatal("expected at least one version")
	}
	v := t1.VersionList[0]
	if !v.Discovered {
		t.Error("Discovered should be true")
	}
	if !v.Unverified {
		t.Error("Unverified should be true")
	}
}

func TestAutoSniffOmitEmpty(t *testing.T) {
	// Tool YAML without auto_sniff should parse with nil AutoSniff
	yamlData := `name: mytool
display_name: My Tool
description: No auto_sniff field
`
	var t1 Tool
	if err := yaml.Unmarshal([]byte(yamlData), &t1); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	if t1.AutoSniff != nil {
		t.Error("AutoSniff should be nil when not present in YAML")
	}
	if !t1.ShouldAutoSniff() {
		t.Error("ShouldAutoSniff should return true for nil AutoSniff")
	}
}

func TestAutoSniffExplicitFalseInYAML(t *testing.T) {
	yamlData := `name: mytool
auto_sniff: false
`
	var t1 Tool
	if err := yaml.Unmarshal([]byte(yamlData), &t1); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	if t1.AutoSniff == nil {
		t.Fatal("AutoSniff should not be nil when explicitly set")
	}
	if *t1.AutoSniff {
		t.Error("AutoSniff should be false")
	}
	if t1.ShouldAutoSniff() {
		t.Error("ShouldAutoSniff should return false for explicit false")
	}
}

// --- UpdateArchiveSHA256 ---

func TestUpdateArchiveSHA256_UpdatesSHA256(t *testing.T) {
	dir := t.TempDir()

	// Write an initial manifest
	yamlData := `name: mytool
display_name: My Tool
description: For testing
homepage: https://example.com
category: language
shims:
  - mytool
versions:
  - version: "1.0.0"
    stable: true
    archives:
      - os: windows
        arch: amd64
        format: zip
        url: https://example.com/mytool-1.0.0-win.zip
        sha256: ""
        size: 1000
        strip: 0
      - os: linux
        arch: x86_64
        format: tar.gz
        url: https://example.com/mytool-1.0.0-linux.tar.gz
        sha256: ""
        size: 500
        strip: 0
`
	manifestPath := filepath.Join(dir, "mytool.yaml")
	if err := os.WriteFile(manifestPath, []byte(yamlData), 0o644); err != nil {
		t.Fatal(err)
	}

	expectedSHA := "abc123def456abc123def456abc123def456abc123def456abc123def456abc1"
	if err := UpdateArchiveSHA256(dir, "mytool", "1.0.0", "windows", "amd64", expectedSHA); err != nil {
		t.Fatalf("UpdateArchiveSHA256 failed: %v", err)
	}

	// Verify the update
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var tool Tool
	if err := yaml.Unmarshal(data, &tool); err != nil {
		t.Fatal(err)
	}

	v, ok := tool.FindVersion("1.0.0")
	if !ok {
		t.Fatal("version 1.0.0 not found")
	}
	a, ok := v.ArchiveForCurrent("windows", "amd64")
	if !ok {
		t.Fatal("windows/amd64 archive not found")
	}
	if a.SHA256 != expectedSHA {
		t.Errorf("sha256 = %q, want %q", a.SHA256, expectedSHA)
	}

	// Verify other archive was not touched
	a2, ok := v.ArchiveForCurrent("linux", "x86_64")
	if !ok {
		t.Fatal("linux/x86_64 archive not found")
	}
	if a2.SHA256 != "" {
		t.Errorf("linux archive sha256 should remain empty, got %q", a2.SHA256)
	}
}

func TestUpdateArchiveSHA256_RejectsPathTraversal(t *testing.T) {
	err := UpdateArchiveSHA256(t.TempDir(), "../../etc/passwd", "1.0", "windows", "amd64", "abcd")
	if err == nil {
		t.Fatal("expected error for path traversal in tool name")
	}
	t.Logf("Got expected error: %v", err)
}

func TestUpdateArchiveSHA256_RejectsUnknownTool(t *testing.T) {
	dir := t.TempDir()
	err := UpdateArchiveSHA256(dir, "nonexistent_tool_xyz", "1.0", "windows", "amd64", "abcd")
	if err == nil {
		t.Fatal("expected error for nonexistent tool")
	}
	t.Logf("Got expected error: %v", err)
}

func TestUpdateArchiveSHA256_ArchiveNotFound(t *testing.T) {
	dir := t.TempDir()
	yamlData := `name: mytool
versions:
  - version: "1.0.0"
    stable: true
    archives:
      - os: windows
        arch: amd64
        format: zip
        url: https://example.com/mytool-1.0.0.zip
        sha256: ""
        size: 1000
        strip: 0
`
	if err := os.WriteFile(filepath.Join(dir, "mytool.yaml"), []byte(yamlData), 0o644); err != nil {
		t.Fatal(err)
	}

	err := UpdateArchiveSHA256(dir, "mytool", "1.0.0", "darwin", "arm64", "abcd")
	if err == nil {
		t.Fatal("expected error for non-matching archive")
	}
}

func TestUpdateArchiveSHA256_ConcurrentDifferentTools(t *testing.T) {
	dir := t.TempDir()

	// Create two manifests
	for _, name := range []string{"tool_a", "tool_b"} {
		yamlData := `name: ` + name + `
versions:
  - version: "1.0.0"
    archives:
      - os: windows
        arch: amd64
        format: zip
        url: https://example.com/` + name + `.zip
        sha256: ""
        size: 100
        strip: 0
`
		if err := os.WriteFile(filepath.Join(dir, name+".yaml"), []byte(yamlData), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Run 10 concurrent updates
	errs := make(chan error, 10)
	for i := 0; i < 5; i++ {
		go func() {
			errs <- UpdateArchiveSHA256(dir, "tool_a", "1.0.0", "windows", "amd64", "aaaa")
		}()
		go func() {
			errs <- UpdateArchiveSHA256(dir, "tool_b", "1.0.0", "windows", "amd64", "bbbb")
		}()
	}

	for i := 0; i < 10; i++ {
		if err := <-errs; err != nil {
			t.Errorf("concurrent update failed: %v", err)
		}
	}

	// Verify both tools have correct sha256
	for _, tc := range []struct {
		tool, sha string
	}{
		{"tool_a", "aaaa"},
		{"tool_b", "bbbb"},
	} {
		data, err := os.ReadFile(filepath.Join(dir, tc.tool+".yaml"))
		if err != nil {
			t.Fatal(err)
		}
		var tool Tool
		if err := yaml.Unmarshal(data, &tool); err != nil {
			t.Fatal(err)
		}
		a, ok := tool.VersionList[0].ArchiveForCurrent("windows", "amd64")
		if !ok {
			t.Fatalf("%s: archive not found", tc.tool)
		}
		if a.SHA256 != tc.sha {
			t.Errorf("%s: sha256 = %q, want %q", tc.tool, a.SHA256, tc.sha)
		}
	}
}

// ──────────────────────────────────────────────────────
// GenerateCustomManifest tests (T1.5.1 – T1.5.3)
// ──────────────────────────────────────────────────────

func TestGenerateCustomManifest_ValidYAML(t *testing.T) {
	versions := []DiscoveredVersion{
		{
			Version: "2.0.0",
			Archives: []DiscoveredArchive{
				{URL: "https://example.com/tool-2.0.0-windows-amd64.zip", OS: "windows", Arch: "amd64"},
				{URL: "https://example.com/tool-2.0.0-linux-amd64.tar.gz", OS: "linux", Arch: "amd64"},
			},
		},
		{
			Version: "1.0.0",
			Archives: []DiscoveredArchive{
				{URL: "https://example.com/tool-1.0.0-windows-amd64.zip", OS: "windows", Arch: "amd64"},
			},
		},
	}

	data, err := GenerateCustomManifest("cangjie", "Cangjie", "cjc", "CANGJIE_HOME", versions)
	if err != nil {
		t.Fatalf("GenerateCustomManifest failed: %v", err)
	}

	var tool Tool
	if err := yaml.Unmarshal(data, &tool); err != nil {
		t.Fatalf("unmarshal generated YAML: %v", err)
	}

	// Check required fields
	if tool.Name != "cangjie" {
		t.Errorf("Name = %q, want %q", tool.Name, "cangjie")
	}
	if tool.DisplayName != "Cangjie" {
		t.Errorf("DisplayName = %q, want %q", tool.DisplayName, "Cangjie")
	}
	if len(tool.Shims) != 1 || tool.Shims[0] != "cjc" {
		t.Errorf("Shims = %v, want [cjc]", tool.Shims)
	}
	if tool.EnvVars["CANGJIE_HOME"] != "{{install}}" {
		t.Errorf("EnvVars[CANGJIE_HOME] = %q, want {{install}}", tool.EnvVars["CANGJIE_HOME"])
	}
	if tool.Latest != "2.0.0" {
		t.Errorf("Latest = %q, want 2.0.0", tool.Latest)
	}
	if len(tool.VersionList) != 2 {
		t.Fatalf("VersionList length = %d, want 2", len(tool.VersionList))
	}

	// First version (latest) should be "2.0.0"
	v := tool.VersionList[0]
	if v.Version != "2.0.0" {
		t.Errorf("VersionList[0].Version = %q, want 2.0.0", v.Version)
	}
	if !v.Discovered {
		t.Error("VersionList[0].Discovered should be true")
	}
	if !v.Unverified {
		t.Error("VersionList[0].Unverified should be true (TOFU)")
	}

	// Check SHA256 is empty (TOFU)
	for _, ver := range tool.VersionList {
		for _, a := range ver.Archives {
			if a.SHA256 != "" {
				t.Errorf("archive %s sha256 should be empty for TOFU, got %q", a.URL, a.SHA256)
			}
		}
	}
}

func TestGenerateCustomManifest_DetectFormatZip(t *testing.T) {
	versions := []DiscoveredVersion{
		{
			Version: "1.0.0",
			Archives: []DiscoveredArchive{
				{URL: "https://example.com/tool-1.0.0.zip", OS: "windows", Arch: "amd64"},
			},
		},
	}

	data, err := GenerateCustomManifest("testtool", "Test Tool", "", "", versions)
	if err != nil {
		t.Fatalf("GenerateCustomManifest failed: %v", err)
	}

	var tool Tool
	if err := yaml.Unmarshal(data, &tool); err != nil {
		t.Fatalf("unmarshal generated YAML: %v", err)
	}

	if len(tool.VersionList) != 1 {
		t.Fatalf("expected 1 version, got %d", len(tool.VersionList))
	}
	if len(tool.VersionList[0].Archives) != 1 {
		t.Fatalf("expected 1 archive, got %d", len(tool.VersionList[0].Archives))
	}

	arch := tool.VersionList[0].Archives[0]
	if arch.Format != "zip" {
		t.Errorf("Format = %q, want %q", arch.Format, "zip")
	}
}

func TestGenerateCustomManifest_DetectFormatTarGz(t *testing.T) {
	versions := []DiscoveredVersion{
		{
			Version: "1.0.0",
			Archives: []DiscoveredArchive{
				{URL: "https://example.com/tool-1.0.0.tar.gz", OS: "linux", Arch: "amd64"},
			},
		},
	}

	data, err := GenerateCustomManifest("testtool", "Test Tool", "", "", versions)
	if err != nil {
		t.Fatalf("GenerateCustomManifest failed: %v", err)
	}

	var tool Tool
	if err := yaml.Unmarshal(data, &tool); err != nil {
		t.Fatalf("unmarshal generated YAML: %v", err)
	}

	if len(tool.VersionList) != 1 {
		t.Fatalf("expected 1 version, got %d", len(tool.VersionList))
	}
	if len(tool.VersionList[0].Archives) != 1 {
		t.Fatalf("expected 1 archive, got %d", len(tool.VersionList[0].Archives))
	}

	arch := tool.VersionList[0].Archives[0]
	if arch.Format != "tar.gz" {
		t.Errorf("Format = %q, want %q", arch.Format, "tar.gz")
	}
}

func TestGenerateCustomManifest_Defaults(t *testing.T) {
	// Test default shim and envVar when empty
	versions := []DiscoveredVersion{
		{Version: "1.0.0"},
	}

	data, err := GenerateCustomManifest("mycustomtool", "My Custom Tool", "", "", versions)
	if err != nil {
		t.Fatalf("GenerateCustomManifest failed: %v", err)
	}

	var tool Tool
	if err := yaml.Unmarshal(data, &tool); err != nil {
		t.Fatalf("unmarshal generated YAML: %v", err)
	}

	// Default shim = name
	if len(tool.Shims) != 1 || tool.Shims[0] != "mycustomtool" {
		t.Errorf("Shims = %v, want [mycustomtool]", tool.Shims)
	}
	// Default envVar = strings.ToUpper(name) + "_HOME"
	if tool.EnvVars["MYCUSTOMTOOL_HOME"] != "{{install}}" {
		t.Errorf("EnvVars = %v, want MYCUSTOMTOOL_HOME", tool.EnvVars)
	}
}

func TestGenerateCustomManifest_TgzDetect(t *testing.T) {
	versions := []DiscoveredVersion{
		{
			Version: "1.0.0",
			Archives: []DiscoveredArchive{
				{URL: "https://example.com/tool-1.0.0.tgz", OS: "linux", Arch: "arm64"},
			},
		},
	}

	data, err := GenerateCustomManifest("testtgz", "Test TGZ", "", "", versions)
	if err != nil {
		t.Fatalf("GenerateCustomManifest failed: %v", err)
	}

	var tool Tool
	if err := yaml.Unmarshal(data, &tool); err != nil {
		t.Fatalf("unmarshal generated YAML: %v", err)
	}

	arch := tool.VersionList[0].Archives[0]
	if arch.Format != "tar.gz" {
		t.Errorf("Format = %q, want %q (tgz should map to tar.gz)", arch.Format, "tar.gz")
	}
}

func TestSortVersionsByRelease_NewestFirst(t *testing.T) {
	versions := []Version{
		{Version: "1.18.11", Release: "2026-08-01"},
		{Version: "1.18.10", Release: "2026-07-30"},
		{Version: "1.18.18"}, // sniffed, no date — still newest by semver
		{Version: "1.1.0", Release: "2025-01-01"},
	}
	SortVersionsByRelease(versions)
	want := []string{"1.18.18", "1.18.11", "1.18.10", "1.1.0"}
	for i, v := range want {
		if versions[i].Version != v {
			t.Errorf("sorted[%d] = %q, want %q", i, versions[i].Version, v)
		}
	}
}

func TestSortVersionsByRelease_DatedNewestBeatsOlderSemver(t *testing.T) {
	versions := []Version{
		{Version: "2.0.0", Release: "2025-01-01"},
		{Version: "1.9.0", Release: "2026-08-01"},
	}
	SortVersionsByRelease(versions)
	if versions[0].Version != "1.9.0" {
		t.Errorf("got %q first, want dated-newer 1.9.0", versions[0].Version)
	}
}
