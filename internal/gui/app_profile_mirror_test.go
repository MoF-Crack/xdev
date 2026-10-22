// app_profile_mirror_test.go 守卫票 16（P1-17 / P2-15）的 GUI 一侧：
// ImportProfile 导入的 mirrors 逐项经 util.IsSafeHTTPSExternalURL 校验，
// 不安全项不写入 cfg.Mirror，并逐项计入 result.Errors；合法 https 镜像保留。
package gui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImportProfile_RejectsHTTPMirror(t *testing.T) {
	tmp := t.TempDir()
	app := newTestAppWithDirs(t, tmp)
	profilePath := filepath.Join(tmp, "profile.json")

	// 正例用 IP 字面量（https://1.1.1.1/...）：IsSafeHTTPSExternalURL 对 IP
	// 字面量不做 DNS 解析，测试因此不依赖网络。
	const safeMirror = "https://1.1.1.1/mirror"
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

	result := app.ImportProfile(profilePath)
	if !result.Success {
		t.Fatalf("import should succeed overall, got %+v", result)
	}

	// 不安全项必须逐个出现在 result.Errors 中（保持既有 result 结构）。
	joined := strings.Join(result.Errors, "\n")
	for _, name := range []string{"npm", "go", "py"} {
		if !strings.Contains(joined, "mirror "+name) {
			t.Errorf("missing rejection for unsafe mirror %q in result.Errors: %v", name, result.Errors)
		}
	}
	if strings.Contains(joined, "mirror rust") {
		t.Errorf("safe mirror reported as rejected: %v", result.Errors)
	}

	cfg := app.getCfg()
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
}
