//go:build windows

// profile_export_whitelist_test.go 守卫票 14（P0-12 / 审计 E-03）：
// CLI `xdev profile export` 只能把 util.AllowedEnvVarNames 白名单内的用户
// 环境变量写进导出 JSON，其余（与 xdev 无关的敏感变量，如 MY_SECRET /
// GITHUB_TOKEN）必须被跳过，并在摘要里报告跳过数量。
//
// 本文件依赖真实 HKCU\Environment，因此只在 Windows 上编译/运行。
// 测试写入的变量在 t.Cleanup 中精确恢复（参考 internal/env/win_path_test.go
// 的 save/restore 模式），以保证 cli 包的 hostsnapshot 守卫认为主机未被污染。
package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/env"
	"github.com/xmdemt/xdev/internal/util"
)

// userEnvSnapshot 记录一个用户级环境变量的原值，供测试结束后精确恢复。
type userEnvSnapshot struct {
	name    string
	value   string
	existed bool
}

// saveUserEnvVar 读取用户级环境变量的原值（走 trusted 读取，避免白名单限制）。
func saveUserEnvVar(t *testing.T, name string) userEnvSnapshot {
	t.Helper()
	v, err := env.GetUserEnvVarTrusted(name)
	return userEnvSnapshot{name: name, value: v, existed: err == nil}
}

// restore 恢复原值：原本存在则写回原值，原本不存在则删除测试写入的值。
func (s userEnvSnapshot) restore(t *testing.T) {
	t.Helper()
	if s.existed {
		if err := env.SetUserEnvVarTrusted(s.name, s.value); err != nil {
			t.Errorf("restore %s=%q: %v", s.name, s.value, err)
		}
		return
	}
	if err := env.DeleteUserEnvVarTrusted(s.name); err != nil {
		t.Errorf("delete test env var %s: %v", s.name, err)
	}
}

// countNonWhitelistUserEnvVars 统计当前 HKCU 中不在 xdev 白名单内的变量数量。
// 真实主机上本来就可能有若干非白名单变量（如 OneDrive），因此测试用
// "基线 + 1" 断言跳过数，而不是硬编码 1。
func countNonWhitelistUserEnvVars() int {
	n := 0
	for name := range env.GetAllUserEnvVars() {
		if !util.IsAllowedEnvVarName(name) {
			n++
		}
	}
	return n
}

// TestProfileExport_WhitelistOnly 是票 14 的守卫测试：
//   - MY_SECRET（非白名单）不得出现在导出 JSON 中；
//   - JAVA_HOME（白名单）必须出现；
//   - 摘要必须报告被跳过的变量数量（skipped N non-xdev variables）。
func TestProfileExport_WhitelistOnly(t *testing.T) {
	// 先保存原值，测试结束（含失败路径）后恢复，绝不污染用户真实 JAVA_HOME。
	javaSnap := saveUserEnvVar(t, "JAVA_HOME")
	secretSnap := saveUserEnvVar(t, "MY_SECRET")
	t.Cleanup(func() {
		secretSnap.restore(t)
		javaSnap.restore(t)
	})

	// 基线：写入测试变量之前，主机上已有的非白名单变量数量。
	baseline := countNonWhitelistUserEnvVars()

	// MY_SECRET 走 trusted 写路径（它不在白名单里，SetUserEnvVar 会拒绝），
	// 模拟用户自己在 HKCU 里放了一个与 xdev 无关的敏感变量。
	if err := env.SetUserEnvVarTrusted("MY_SECRET", "abc"); err != nil {
		t.Fatalf("SetUserEnvVarTrusted(MY_SECRET): %v", err)
	}
	if err := env.SetUserEnvVar("JAVA_HOME", `C:\Java`); err != nil {
		t.Fatalf("SetUserEnvVar(JAVA_HOME): %v", err)
	}

	// 驱动真实的 cobra 导出命令（隔离到临时 home，输出写入临时文件）。
	home := t.TempDir()
	injectHome(t, home)
	outFile := filepath.Join(t.TempDir(), "profile.json")

	cmd := newExportCmd()
	cmd.SetArgs([]string{outFile})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SilenceUsage = true
	if err := cmd.Execute(); err != nil {
		t.Fatalf("xdev profile export: %v", err)
	}

	raw, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read exported profile: %v", err)
	}

	// 结构断言：导出 JSON 的 envVars 数组。
	var exported struct {
		EnvVars []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
			Level string `json:"level"`
		} `json:"envVars"`
	}
	if err := json.Unmarshal(raw, &exported); err != nil {
		t.Fatalf("exported profile is not valid JSON: %v", err)
	}

	names := map[string]string{}
	for _, ev := range exported.EnvVars {
		names[ev.Name] = ev.Value
		if !util.IsAllowedEnvVarName(ev.Name) {
			t.Errorf("exported profile contains non-whitelisted env var %q — 票 14 regression", ev.Name)
		}
	}
	if _, ok := names["JAVA_HOME"]; !ok {
		t.Errorf("whitelisted JAVA_HOME missing from exported envVars: %v", names)
	}
	if _, forbidden := names["MY_SECRET"]; forbidden {
		t.Errorf("MY_SECRET leaked into exported envVars: %v", names)
	}
	// 明文层面的双保险：整个文件都不应出现这个变量名。
	if bytes.Contains(raw, []byte("MY_SECRET")) {
		t.Errorf("MY_SECRET appears in exported profile file:\n%s", raw)
	}

	// 摘要断言：跳过数 = 基线 + 1（本测试新写入的 MY_SECRET）。
	summary := out.String()
	wantSkipped := baseline + 1
	if !strings.Contains(summary, fmt.Sprintf("skipped %d non-xdev variables", wantSkipped)) {
		t.Errorf("summary must report skipped=%d, got:\n%s", wantSkipped, summary)
	}
	// 在干净主机上（基线为 0）跳过数必须是字面上的 1。
	if baseline == 0 && !strings.Contains(summary, "skipped 1 non-xdev variables") {
		t.Errorf("summary must contain %q, got:\n%s", "skipped 1 non-xdev variables", summary)
	}
	t.Logf("baseline non-whitelist user vars=%d, summary=%q", baseline, strings.TrimSpace(summary))
}

// TestProfileExportSummary_SkippedWording 固定摘要文案（票 14），
// 使 "skipped N non-xdev variables" 的字面形式独立于主机状态被断言。
func TestProfileExportSummary_SkippedWording(t *testing.T) {
	got := profileExportSummary(`C:\tmp\profile.json`, 1)
	if !strings.Contains(got, "skipped 1 non-xdev variables") {
		t.Errorf("profileExportSummary = %q, want it to contain %q", got, "skipped 1 non-xdev variables")
	}
	if !strings.Contains(got, `C:\tmp\profile.json`) {
		t.Errorf("profileExportSummary = %q, want it to contain the output path", got)
	}
}
