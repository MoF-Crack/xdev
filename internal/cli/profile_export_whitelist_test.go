//go:build windows

// profile_export_whitelist_test.go 守卫票 14（P0-12 / 审计 E-03）：
// CLI `xdev profile export` 只能把 util.AllowedEnvVarNames 白名单内的用户
// 环境变量写进导出 JSON，其余（与 xdev 无关的敏感变量，如 MY_SECRET /
// GITHUB_TOKEN）必须被跳过，并在摘要里报告跳过数量。
//
// 第四轮复核改造：本用例原先**真实写入 HKCU\Environment** 的 JAVA_HOME（白名单）与
// MY_SECRET（非白名单）来构造场景，再在 t.Cleanup 里恢复。那既会在并发全量跑时撞上
// 其它包的宿主机快照窗口（假红），也会在进程被强杀时把这两个变量永久留在用户环境里。
// 现在改用 env.UseInMemoryUserEnvForTesting 注入枚举结果：导出命令读到的就是内存映射，
// 宿主零接触，断言口径不变（白名单进、非白名单不进、摘要报跳过数）。
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

// countNonWhitelistUserEnvVars 统计当前**枚举结果**中不在 xdev 白名单内的变量数量。
// 改用内存注入后，它统计的是注入映射（不再是开发者真实环境），因此断言与宿主无关。
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
	// 注入内存版用户环境：JAVA_HOME（白名单）与 MY_SECRET（非白名单）都只存在于内存里，
	// 不写开发者真实的 HKCU\Environment。
	restoreEnv := env.UseInMemoryUserEnvForTesting(map[string]string{
		"JAVA_HOME": `C:\Java`,
		"MY_SECRET": "abc",
	})
	t.Cleanup(restoreEnv)

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

	// 摘要断言：跳过数 == 注入映射里非白名单变量的个数（= MY_SECRET 一个）。
	//
	// 改造说明：原先断言"基线 + 1"（基线=主机上既有的非白名单变量数），依赖开发者真实
	// 环境；现在枚举被内存映射整体替换，跳过数只由注入内容决定 → 与宿主状态无关，
	// 在任何机器上都是确定值。
	summary := out.String()
	wantSkipped := countNonWhitelistUserEnvVars()
	if wantSkipped < 1 {
		t.Fatalf("注入映射里应至少有一个非白名单变量（MY_SECRET），实际 %d", wantSkipped)
	}
	if !strings.Contains(summary, fmt.Sprintf("skipped %d non-xdev variables", wantSkipped)) {
		t.Errorf("summary must report skipped=%d, got:\n%s", wantSkipped, summary)
	}
	if !strings.Contains(summary, "skipped 1 non-xdev variables") {
		t.Errorf("注入映射只有 MY_SECRET 一个非白名单变量，摘要应为字面 %q，got:\n%s",
			"skipped 1 non-xdev variables", summary)
	}
	t.Logf("注入的非白名单变量数=%d, summary=%q", wantSkipped, strings.TrimSpace(summary))
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
