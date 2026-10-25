package env

import (
	"os/exec"
	"strings"
	"testing"
)

// Round 15 守卫：PowerShell 分支的取值转义。
//
// 实测缺陷（两个独立审计通道都复现）：psEscape 把 `"` 转义成 `\"`，而
// **PowerShell 的转义字符是反引号**，反斜杠只是普通字符 —— 双引号字符串
// 在 `\"` 处提前结束，剩下的部分被当成**新语句**执行。
//
// 触发链（真实可达）：manifest 的 `env_vars` 值、或 `xdev plugin install`
// 装进来的 Lua 插件 `env_vars()` 返回值 → `xdev env <tool>@<ver> --shell
// powershell`（文档示例就是 `iex "$(xdev env …)"`）→ 用户执行输出 →
// 任意 PowerShell 落地，绕开 Lua 沙箱。
//
// 单测断言的是**转义约定**（确定性、跨平台可跑）；真实 powershell.exe
// 往返验证见 TestR15_PowerShellEnvValueIsInertWhenEvaluated。
func TestR15_PowerShellEnvValueCannotBreakOutOfString(t *testing.T) {
	hostile := `a"; Write-Output PWNED; "b`
	out := HookOutput("powershell", nil, map[string]string{"DEMO_HOME": hostile})

	// 旧实现会生成 `$env:DEMO_HOME = "a\"; Write-Output PWNED; \"b"`：
	// `\"` 不是 PowerShell 的转义，字符串在第一个 `"` 处结束。
	if strings.Contains(out, `\"`) {
		t.Fatalf("PowerShell 输出里出现反斜杠转义的引号（PowerShell 不认）：\n%s", out)
	}
	if !strings.Contains(out, "`\"") {
		t.Fatalf("取值里的双引号必须用反引号转义：\n%s", out)
	}

	// 逐字符钉住另外两个必须转义的字符：子表达式/变量展开会让取值被重写。
	cases := []struct{ in, want string }{
		{"X`Y", "X``Y"},
		{"X$Y", "X`$Y"},
	}
	for _, tc := range cases {
		got := HookOutput("powershell", nil, map[string]string{"DEMO_HOME": tc.in})
		if !strings.Contains(got, tc.want) {
			t.Errorf("取值 %q 未按预期转义（want 含 %q）：\n%s", tc.in, tc.want, got)
		}
	}
}

// 行为级证据：把生成的语句交给真实 powershell 执行，注入的 Write-Output
// 不得作为独立语句执行，且取值必须原样保留。没有 powershell 的机器跳过。
func TestR15_PowerShellEnvValueIsInertWhenEvaluated(t *testing.T) {
	sh, err := exec.LookPath("powershell")
	if err != nil {
		t.Skip("powershell not available")
	}
	const marker = "R15_INJECTION_MARKER"
	hostile := `a"; Write-Output ` + marker + `; "b`
	line := strings.SplitN(HookOutput("powershell", nil, map[string]string{"DEMO_HOME": hostile}), "\n", 2)[0]
	script := line + "\nWrite-Output (\"VALUE=\" + $env:DEMO_HOME)\n"
	cmd := exec.Command(sh, "-NoProfile", "-NonInteractive", "-Command", "-")
	cmd.Stdin = strings.NewReader(script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("powershell failed: %v\n%s", err, out)
	}
	got := string(out)
	for _, ln := range strings.Split(got, "\n") {
		if strings.TrimSpace(ln) == marker {
			t.Fatalf("注入被执行了（marker 作为独立输出出现）：\n%s\n\n生成的语句：\n%s", got, script)
		}
	}
	if !strings.Contains(got, "VALUE="+hostile) {
		t.Fatalf("取值没有被原样保留（转义过度或损坏）：\n%s\n\n生成的语句：\n%s", got, script)
	}
}
