// manifests_integrity_guard_test.go — 票 18（P2-15 / P2-29 / P0-02 阶段 2 守卫规格）。
//
// 守卫分三层：
//
//  1. 始终生效（当前树即须通过）
//     a. manifests/ 里不得有孤儿 .sig（没有同名 .yaml 的签名文件）；
//     b. 打进二进制的每个 .sig 必须能解析、且用 keys/pub.key 验签通过，
//     并存在对应的内嵌 .yaml；
//     c. index.yaml 必须覆盖全部工具 manifest、引用存在的文件、日期不是未来。
//
//  2. 自激活（重签数据线完成后自动生效，无需改配置）
//     d. 每个工具 manifest 都有自己的 .sig，且签名图覆盖该 manifest 里全部
//     非空 sha256 与 mirror_sha256（含 `os/arch#mirror` 键）。
//     数据线未完成时跳过，并在输出里说明缺哪些——避免「第一天就红」，
//     也避免静默假绿（skip 理由带缺失清单）。
//
// 这些断言读的是仓库工作树（package main 的测试以仓库根为工作目录）与
// main.go 的 go:embed 文件系统，因此不依赖任何环境变量或私有密钥。
package main

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/signer"
)

const (
	manifestsDir = "manifests"
	publicKeyRel = "keys/pub.key"
)

// toolManifestFiles 返回工作树里全部工具 manifest 文件名（跳过 index.yaml 等非工具 YAML）。
func toolManifestFiles(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(manifestsDir)
	if err != nil {
		t.Fatalf("read %s: %v", manifestsDir, err)
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") || e.Name() == "index.yaml" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(manifestsDir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		var probe struct {
			Name string `yaml:"name"`
		}
		if err := yaml.Unmarshal(data, &probe); err != nil {
			t.Fatalf("parse %s: %v", e.Name(), err)
		}
		if strings.TrimSpace(probe.Name) == "" {
			continue // 非工具 YAML
		}
		files = append(files, e.Name())
	}
	sort.Strings(files)
	return files
}

// signatureFilesOnDisk 返回 manifests/ 里全部 *.yaml.sig（含点开头的隐藏文件，
// 普通 glob 会漏掉它们——这正是历史孤儿签名藏身处）。
func signatureFilesOnDisk(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(manifestsDir)
	if err != nil {
		t.Fatalf("read %s: %v", manifestsDir, err)
	}
	var sigs []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml.sig") {
			continue
		}
		sigs = append(sigs, e.Name())
	}
	sort.Strings(sigs)
	return sigs
}

// listEmbedded 列出内嵌文件系统里匹配后缀的名字。
func listEmbedded(t *testing.T, fsys fs.FS, suffix string) []string {
	t.Helper()
	var out []string
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, suffix) {
			out = append(out, filepath.Base(p))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk embedded fs: %v", err)
	}
	sort.Strings(out)
	return out
}

// TestManifestsTree_NoOrphanSignatures（边界②）：每个 .sig 必须有同名 .yaml。
// 历史遗留：placeholder.yaml.sig（内容 {}）与 .placeholder.yaml.sig（0 字节）
// 都没有对应的 manifest，前者还会被 go:embed 打进二进制。
func TestManifestsTree_NoOrphanSignatures(t *testing.T) {
	var orphans []string
	for _, sig := range signatureFilesOnDisk(t) {
		yamlName := strings.TrimSuffix(sig, ".sig")
		if _, err := os.Stat(filepath.Join(manifestsDir, yamlName)); err != nil {
			orphans = append(orphans, sig)
		}
	}
	if len(orphans) > 0 {
		t.Errorf("孤儿签名（没有同名 .yaml）：%v —— 请删除或补上对应的 manifest（票 18 边界②）", orphans)
	}
}

// TestEmbeddedSignatures_VerifyAgainstCommittedPublicKey：内嵌签名必须可解析、
// 可验签，且每个内嵌 .sig 都有对应的内嵌 .yaml。
func TestEmbeddedSignatures_VerifyAgainstCommittedPublicKey(t *testing.T) {
	pub, err := signer.LoadPublicKey(publicKeyRel)
	if err != nil {
		t.Fatalf("load %s: %v", publicKeyRel, err)
	}
	embeddedYAML := map[string]bool{}
	for _, name := range listEmbedded(t, manifestFS, ".yaml") {
		embeddedYAML[name] = true
	}
	sigs := listEmbedded(t, sigFS, ".yaml.sig")
	if len(sigs) == 0 {
		t.Fatal("no embedded signatures — sigFS embed pattern is broken")
	}
	for _, name := range sigs {
		// embed.FS 一律使用斜杠路径（Windows 上 filepath.Join 会给出反斜杠而读不到）。
		data, err := fs.ReadFile(sigFS, manifestsDir+"/"+name)
		if err != nil {
			t.Errorf("read embedded %s: %v", name, err)
			continue
		}
		var ms signer.ManifestSignature
		if err := json.Unmarshal(data, &ms); err != nil {
			t.Errorf("embedded %s is not a parseable signature (size=%d): %v", name, len(data), err)
			continue
		}
		yamlName := strings.TrimSuffix(name, ".sig")
		if !embeddedYAML[yamlName] {
			t.Errorf("embedded %s has no matching embedded %s", name, yamlName)
		}
		if !signer.VerifySignature(pub, &ms) {
			t.Errorf("embedded %s does not verify against %s (tool=%q ts=%s)", name, publicKeyRel, ms.Tool, ms.Timestamp)
		}
	}
}

// TestIndexYaml_CoversAllToolManifests（P2-15 / D-15）：索引必须与工具清单同步。
func TestIndexYaml_CoversAllToolManifests(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(manifestsDir, "index.yaml"))
	if err != nil {
		t.Fatalf("read index.yaml: %v", err)
	}
	idx, err := manifest.ParseIndexYAML(data)
	if err != nil {
		t.Fatalf("parse index.yaml: %v", err)
	}

	listed := map[string]string{} // manifest 文件名 → 条目 name
	for _, c := range idx.Categories {
		if strings.TrimSpace(c.Name) == "" {
			t.Errorf("index.yaml 有空的分类名")
		}
		if strings.TrimSpace(c.DisplayName) == "" {
			t.Errorf("index.yaml 分类 %s 缺 display_name", c.Name)
		}
		for _, tool := range c.Tools {
			if tool.Manifest == "" {
				t.Errorf("index.yaml 分类 %s 的条目 %s 缺 manifest 字段", c.Name, tool.Name)
				continue
			}
			if _, dup := listed[tool.Manifest]; dup {
				t.Errorf("index.yaml 重复收录 %s", tool.Manifest)
			}
			listed[tool.Manifest] = tool.Name
		}
	}

	files := toolManifestFiles(t)
	disk := map[string]bool{}
	for _, f := range files {
		disk[f] = true
		if _, ok := listed[f]; !ok {
			t.Errorf("工具 %s 未出现在 index.yaml —— 请运行 `xdev manifest index` 重新生成 (P2-15)", f)
		}
	}
	for manifestFile, name := range listed {
		if !disk[manifestFile] {
			t.Errorf("index.yaml 收录了不存在的 manifest %s（条目 %s）", manifestFile, name)
			continue
		}
		// 条目 name 必须等于 manifest 里声明的 name（防复制粘贴错名）。
		raw, err := os.ReadFile(filepath.Join(manifestsDir, manifestFile))
		if err != nil {
			t.Errorf("read %s: %v", manifestFile, err)
			continue
		}
		var tool manifest.Tool
		if err := yaml.Unmarshal(raw, &tool); err != nil {
			t.Errorf("parse %s: %v", manifestFile, err)
			continue
		}
		if tool.Name != name {
			t.Errorf("index.yaml 条目名 %q != %s 里声明的 name %q", name, manifestFile, tool.Name)
		}
		if filepath.Base(manifestFile) != tool.Name+".yaml" {
			t.Errorf("manifest 文件名 %s 与声明的 name %q 不一致（约定 <name>.yaml）", manifestFile, tool.Name)
		}
	}

	if idx.Version != 1 {
		t.Errorf("index.yaml version = %d, want 1", idx.Version)
	}
	gen, err := time.Parse("2006-01-02", idx.Generated)
	if err != nil {
		t.Errorf("index.yaml generated = %q, want YYYY-MM-DD", idx.Generated)
	} else if gen.After(time.Now().UTC().Add(24 * time.Hour)) {
		t.Errorf("index.yaml generated = %s 在未来", idx.Generated)
	}
}

// TestManifestSignatureCoverage（P0-02 阶段 2，自激活）：重签完成后，每个工具
// manifest 都必须有自己的 .sig，且签名图覆盖该 manifest 的全部非空 sha256 与
// mirror_sha256。
//
// 数据线未完成时本守卫跳过（并打印缺失清单）。这是「不定义边界就会第一天红」的
// 处理方式：既不阻塞当前树，也不会在重签之后静默放过。
func TestManifestSignatureCoverage(t *testing.T) {
	files := toolManifestFiles(t)

	var unsigned []string
	for _, f := range files {
		if _, err := os.Stat(filepath.Join(manifestsDir, f+".sig")); err != nil {
			unsigned = append(unsigned, f)
		}
	}
	if len(unsigned) > 0 {
		t.Skipf("票 17 数据线未完成：%d/%d 个工具 manifest 尚无 .sig（待重签）—— 缺失示例 %v；重签后本守卫自动生效",
			len(unsigned), len(files), firstN(unsigned, 5))
	}

	pub, err := signer.LoadPublicKey(publicKeyRel)
	if err != nil {
		t.Fatalf("load %s: %v", publicKeyRel, err)
	}

	// 无哈希工具豁免（边界③）：没有任何可用哈希的 manifest（如 npm 型 dsh）没有
	// 可签内容，sign-manifests 会跳过它，因此也不要求有 .sig。
	var hashless []string
	for _, f := range files {
		raw, err := os.ReadFile(filepath.Join(manifestsDir, f))
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		var tool manifest.Tool
		if err := yaml.Unmarshal(raw, &tool); err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		if _, has := manifest.BuildSignatureHashMap(&tool); !has {
			hashless = append(hashless, f)
			if _, err := os.Stat(filepath.Join(manifestsDir, f+".sig")); err == nil {
				t.Errorf("%s 无任何可用哈希却存在 .sig —— 签名端会跳过它，请明确豁免口径 (边界③)", f)
			}
			continue
		}

		sigPath := filepath.Join(manifestsDir, f+".sig")
		ms, err := signer.ReadSignatureFile(sigPath)
		if err != nil {
			t.Errorf("read %s: %v", sigPath, err)
			continue
		}
		if !signer.VerifySignature(pub, ms) {
			t.Errorf("%s 验签失败（tool=%q ts=%s）", sigPath, ms.Tool, ms.Timestamp)
			continue
		}
		if ms.Tool != tool.Name {
			t.Errorf("%s 的签名 tool=%q != manifest name=%q", sigPath, ms.Tool, tool.Name)
		}

		signed, err := signer.ParseSHA256Map(ms.SHA256Map)
		if err != nil {
			t.Errorf("%s 的 sha256_map 解析失败: %v", sigPath, err)
			continue
		}
		// 覆盖完整性：manifest 里每个可用哈希都必须能在签名图的**同一版本**下找到
		// （与下载侧 verifySHAInMapForVersion 的语义一致）。
		for _, v := range tool.VersionList {
			verMap := signed[v.Version]
			for _, a := range v.Archives {
				if !manifest.SignatureCoversArchive(verMap, a.OS+"/"+a.Arch, a.SHA256) {
					t.Errorf("%s@%s: %s/%s 的 sha256 未入签名图 (P0-02 阶段 2)", f, v.Version, a.OS, a.Arch)
				}
				if a.MirrorSHA256 == "" || strings.EqualFold(a.MirrorSHA256, a.SHA256) {
					continue
				}
				// 独立镜像哈希必须入图（票 17 第 3 项：`os/arch#mirror` 键），
				// 否则下载前的覆盖过滤会把镜像候选整批剔除。
				if !manifest.SignatureCoversArchive(verMap, a.OS+"/"+a.Arch, a.MirrorSHA256) {
					t.Errorf("%s@%s: %s/%s 的 mirror_sha256 未入签名图 —— 镜像通路会被覆盖过滤剔除 (票 17 第 3 项)",
						f, v.Version, a.OS, a.Arch)
				}
			}
		}
	}
	if len(hashless) > 0 {
		t.Logf("无哈希工具（签名端跳过，边界③豁免）：%v", hashless)
	}
}

// TestManifests_EveryToolHasCompleteArchive（P2-29）：任何工具都不得是「纯占位」——
// 至少有一个版本带结构完整的归档（url+format，或 npm 型）。守卫与宿主平台无关
// （Windows-only 工具在 Linux CI 上同样必须通过）：这正是 mingw 曾经的形态，
// 实测（2026-09-13）时 52 个工具里只有它整体缺 url/format。
func TestManifests_EveryToolHasCompleteArchive(t *testing.T) {
	tools, err := manifest.LoadToolManifests(manifestsDir)
	if err != nil {
		t.Fatalf("load manifests: %v", err)
	}
	if len(tools) == 0 {
		t.Fatal("no tool manifests found")
	}
	var placeholders []string
	for _, tool := range tools {
		if !tool.HasCompleteArchive() {
			placeholders = append(placeholders, tool.Name+".yaml")
		}
	}
	if len(placeholders) > 0 {
		t.Errorf("以下工具没有任何结构完整的归档（缺 url/format）—— `xdev available` 会展示它们但安装必然失败 (P2-29): %v", placeholders)
	}

	// 平台相关的不可安装只作提示（例如 Windows-only 工具在 Linux 上），不判失败。
	var notHere []string
	for _, tool := range tools {
		if !tool.HasInstallableVersion(runtime.GOOS, runtime.GOARCH) {
			notHere = append(notHere, tool.Name)
		}
	}
	if len(notHere) > 0 {
		t.Logf("本平台（%s/%s）不可安装的工具（正常现象，doctor 也会列出）：%v", runtime.GOOS, runtime.GOARCH, notHere)
	}
}

func firstN(s []string, n int) []string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
