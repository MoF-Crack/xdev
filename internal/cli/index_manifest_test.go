// index_manifest_test.go — 票 18 / P2-15：`xdev manifest index` 命令的行为守卫。
//
// 全部用例在 t.TempDir() 里造 manifest 树（不读仓库 manifests/，也绝不触碰真实
// xdev home）：命令只做文件 IO，不依赖配置或环境。
package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeToolManifest 在 dir 下写一个最小工具 manifest。
func writeToolManifest(t *testing.T, dir, name, display, category string) {
	t.Helper()
	body := "name: " + name + "\n" +
		"display_name: " + display + "\n" +
		"category: " + category + "\n" +
		"versions:\n" +
		"  - version: \"1.0.0\"\n"
	if err := os.WriteFile(filepath.Join(dir, name+".yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestManifestIndexCmd_WritesAndChecks(t *testing.T) {
	dir := t.TempDir()
	writeToolManifest(t, dir, "go", "Go", "language")
	writeToolManifest(t, dir, "vscode", "Visual Studio Code", "development_tools")
	// 非工具 YAML：不得进入索引。
	if err := os.WriteFile(filepath.Join(dir, "notes.yaml"), []byte("# just a comment\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	run := func(args ...string) (string, error) {
		cmd := newManifestIndexCmd()
		cmd.SetArgs(args)
		cmd.SetOut(&strings.Builder{})
		var out strings.Builder
		cmd.SetOut(&out)
		err := cmd.Execute()
		return out.String(), err
	}

	if _, err := run("--manifests-dir", dir, "--date", "2026-09-13"); err != nil {
		t.Fatalf("generate: %v", err)
	}
	idxPath := filepath.Join(dir, "index.yaml")
	data, err := os.ReadFile(idxPath)
	if err != nil {
		t.Fatalf("index.yaml not written: %v", err)
	}
	out := string(data)
	for _, want := range []string{"- name: go", "- name: vscode", "manifest: vscode.yaml", `generated: "2026-09-13"`} {
		if !strings.Contains(out, want) {
			t.Errorf("generated index missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "notes.yaml") {
		t.Errorf("non-tool YAML leaked into the index:\n%s", out)
	}

	// --check 对刚生成的索引必须通过。
	if _, err := run("--manifests-dir", dir, "--check"); err != nil {
		t.Fatalf("--check on a fresh index must pass: %v", err)
	}

	// 新增工具后 --check 必须失败（漂移检测），生成后再次通过。
	writeToolManifest(t, dir, "rust", "Rust", "language")
	_, err = run("--manifests-dir", dir, "--check")
	if err == nil {
		t.Fatal("--check must fail when a tool is missing from the index (P2-15 drift)")
	}
	if !strings.Contains(err.Error(), "out of date") {
		t.Errorf("error = %v, want an 'out of date' hint", err)
	}
	if _, err := run("--manifests-dir", dir, "--date", "2026-09-13"); err != nil {
		t.Fatalf("regenerate: %v", err)
	}
	if _, err := run("--manifests-dir", dir, "--check"); err != nil {
		t.Fatalf("--check after regeneration must pass: %v", err)
	}

	// --date 校验
	if _, err := run("--manifests-dir", dir, "--date", "13/09/2026"); err == nil {
		t.Error("--date must reject non-ISO dates")
	}
}

// TestManifestIndexCmd_CheckFailsOnMissingFile：--check 在索引不存在时报错而不是
// 静默通过（避免 CI 里「文件没了但守卫绿」）。
func TestManifestIndexCmd_CheckFailsOnMissingFile(t *testing.T) {
	dir := t.TempDir()
	writeToolManifest(t, dir, "go", "Go", "language")
	cmd := newManifestIndexCmd()
	cmd.SetArgs([]string{"--manifests-dir", dir, "--check"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("--check must fail when index.yaml does not exist")
	}
}
