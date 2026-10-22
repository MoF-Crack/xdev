// profile_import_mirror_test.go 守卫票 16（P1-17 / P2-15）的 CLI 一侧：
// `xdev profile import` 的 mirrors 逐项用 util.IsSafeHTTPSExternalURL 校验，
// 不安全项不得写入 cfg.Mirror；合法 https 镜像仍然生效。
//
// 与 GUI 侧 internal/gui/app_profile_mirror_test.go 使用同一函数、同一语义
// （表驱动思路一致，两处独立断言各自的代码路径）。
package cli

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImportProfile_RejectsHTTPMirror(t *testing.T) {
	home := t.TempDir()
	injectHome(t, home)

	// 正例用 IP 字面量（https://1.1.1.1/...）：IsSafeHTTPSExternalURL 对 IP
	// 字面量不做 DNS 解析，测试因此不依赖网络。
	const safeMirror = "https://1.1.1.1/mirror"
	profilePath := filepath.Join(t.TempDir(), "profile.json")
	payload := map[string]any{
		"version": "test",
		"proxy":   map[string]any{"http": "", "https": "", "noProxy": "", "enabled": false},
		"mirrors": map[string]string{
			"npm":  "http://127.0.0.1/x",     // 明文 http → 拒绝
			"go":   "https://127.0.0.1/y",    // localhost → 拒绝
			"rust": safeMirror,               // 合法 https → 保留
			"py":   "https://10.0.0.5/plain", // 内网地址 → 拒绝
		},
		"tools":   []any{},
		"envVars": []any{},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(profilePath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	// 捕获 stdout：导入命令用 fmt.Printf 报告被拒绝的镜像。
	oldStdout := os.Stdout
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = pw
	cmd := newImportCmd()
	cmd.SetArgs([]string{profilePath})
	cmd.SilenceUsage = true
	execErr := cmd.Execute()
	pw.Close()
	os.Stdout = oldStdout
	outBytes, _ := io.ReadAll(pr)
	pr.Close()
	output := string(outBytes)

	if execErr != nil {
		t.Fatalf("xdev profile import: %v\noutput:\n%s", execErr, output)
	}

	cfg := Cfg()
	if _, ok := cfg.Mirror["npm"]; ok {
		t.Errorf("unsafe http mirror was written to cfg.Mirror: %v", cfg.Mirror)
	}
	if _, ok := cfg.Mirror["go"]; ok {
		t.Errorf("localhost https mirror was written to cfg.Mirror: %v", cfg.Mirror)
	}
	if _, ok := cfg.Mirror["py"]; ok {
		t.Errorf("private-IP https mirror was written to cfg.Mirror: %v", cfg.Mirror)
	}
	if got := cfg.Mirror["rust"]; got != safeMirror {
		t.Errorf("safe https mirror not applied: cfg.Mirror[rust] = %q, want %q (mirrors=%v)", got, safeMirror, cfg.Mirror)
	}
	// 非空错误报告：每个被拒绝的镜像都要有一条消息。
	for _, name := range []string{"npm", "go", "py"} {
		if !strings.Contains(output, "mirror "+name) {
			t.Errorf("missing rejection message for mirror %q in output:\n%s", name, output)
		}
	}
	if strings.Contains(output, "mirror rust") {
		t.Errorf("safe mirror reported as rejected:\n%s", output)
	}
	t.Logf("import output:\n%s", output)
}
