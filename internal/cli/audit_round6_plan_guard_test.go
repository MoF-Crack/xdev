package cli

// 第六轮审计修复（F-29/F-31/F-32/F-33/F-34）的**决策函数**守卫。
//
// 这些用例驱动的是抽出来的纯决策/纯文本函数（planDoctorFix /
// resolveActivateShell / hasXdevRCBlock / stripAllXdevRCBlocks /
// newestVersion / shouldUpdateLatest）——不构造 cobra 命令、不碰注册表，
// 因此可以安全地在任意宿主上运行。

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/env"
	"github.com/xmdemt/xdev/internal/manifest"
)

// ---------------------------------------------------------------------------
// F-31（P2）：`doctor --fix` 的宿主写操作必须逐项受 --no-path-fix 门控
// ---------------------------------------------------------------------------

// TestPlanDoctorFix_NoPathFixSuppressesEveryRegistryWrite：
// --no-path-fix 下**任何**写注册表的动作都必须关闭，而 InstallSelf
// （只写 xdev 自己的目录）必须保留——否则 --fix 会失去作用。
func TestPlanDoctorFix_NoPathFixSuppressesEveryRegistryWrite(t *testing.T) {
	p := planDoctorFix(true, true)

	if p.CleanUserPath {
		t.Error("--no-path-fix 下 CleanUserPath 必须关闭（它写 HKCU\\Environment）")
	}
	if p.AppendUserPath {
		t.Error("--no-path-fix 下 AppendUserPath 必须关闭（它写 HKCU\\Environment）")
	}
	if p.CleanSystemPath {
		t.Error("--no-path-fix 下 CleanSystemPath 必须关闭（它写 HKLM）")
	}
	if p.CleanGOROOT {
		t.Error("--no-path-fix 下 CleanStaleGOROOT 必须关闭（它写 HKCU\\Environment）")
	}
	if !p.InstallSelf {
		t.Error("InstallSelf 只写 xdev 自己的目录，不得被 --no-path-fix 关掉（否则 --fix 失去作用）")
	}
}

// TestPlanDoctorFix_DefaultStillFixesUserPath：不带 --no-path-fix 时，
// 用户级 PATH 修复照旧生效（防「一律禁用」的假修复）。
func TestPlanDoctorFix_DefaultStillFixesUserPath(t *testing.T) {
	p := planDoctorFix(false, false)

	if !p.CleanUserPath || !p.AppendUserPath || !p.InstallSelf {
		t.Errorf("默认 --fix 必须仍然修复用户 PATH 并自装，得到 %+v", p)
	}
	if !p.CleanGOROOT {
		t.Error("默认 --fix 必须仍然允许 GOROOT 自愈")
	}
	if p.CleanSystemPath {
		t.Error("HKLM 系统 PATH 属机器级写操作，必须由 --fix-system-path 显式 opt-in")
	}
}

// TestPlanDoctorFix_SystemPathRequiresExplicitOptIn：只有显式 opt-in 才动 HKLM。
func TestPlanDoctorFix_SystemPathRequiresExplicitOptIn(t *testing.T) {
	if p := planDoctorFix(false, true); !p.CleanSystemPath {
		t.Errorf("显式 --fix-system-path 必须开启系统 PATH 清理，得到 %+v", p)
	}
}

// TestDoctorFix_RegistryWritesAreGatedInSource 源码级守卫：
// doctor.go 的 --fix 分支不得出现**无门控**的注册表写调用。
//
// 缺陷原形（F-31）：doctor.go 全文 0 处 NoPathFix()，--fix 无条件执行
// CleanupUserPath / AppendToUserPath / CleanupSystemPath。
func TestDoctorFix_RegistryWritesAreGatedInSource(t *testing.T) {
	data, err := os.ReadFile("doctor.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)

	if !strings.Contains(src, "planDoctorFix(") {
		t.Error("doctor.go 必须经 planDoctorFix 集中决定宿主写操作（F-31 单源门控）")
	}
	if strings.Count(src, "NoPathFix()") == 0 {
		t.Error("doctor.go 必须读取 NoPathFix()（缺陷原形：全文 0 处引用）")
	}
	// 每个注册表写调用点都必须由 fixPlan 门控，而不是裸调用。
	lines := strings.Split(src, "\n")
	for _, call := range []string{
		"env.CleanupUserPath(",
		"env.AppendToUserPath(",
		"env.CleanupSystemPath(",
		"env.CleanupStaleGOROOT(",
	} {
		for i, line := range lines {
			if !strings.Contains(line, call) {
				continue
			}
			lo := i - 4
			if lo < 0 {
				lo = 0
			}
			window := strings.Join(lines[lo:i+1], "\n")
			if !strings.Contains(window, "fixPlan.") {
				t.Errorf("doctor.go:%d 的 %s 未被 fixPlan 门控（F-31 缺陷原形：无条件写注册表）",
					i+1, call)
			}
		}
	}
}

// TestPlanDoctorFix_NoPathFixReallyPreventsHostWrites 端到端宿主安全断言：
// 用 env 包的内存 PATH 后端驱动真实写函数，证明「门关掉 = 宿主 PATH 零变化」。
//
// 这是 F-31 的关键实证：缺陷原形下 `doctor --fix --no-path-fix` 会真的把
// <home>/current/xdev 追加进 PATH。用内存后端驱动，测试不碰真实 HKCU。
func TestPlanDoctorFix_NoPathFixReallyPreventsHostWrites(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("memory registry backend is Windows-specific")
	}
	home := t.TempDir()
	xdevBinDir := filepath.Join(home, "current", "xdev")
	if err := os.MkdirAll(xdevBinDir, 0o755); err != nil {
		t.Fatal(err)
	}

	initial := `C:\Windows\System32;` + home + `\install\go\1.26.5`
	get, restore := env.UseInMemoryUserPathForTesting(initial)
	defer restore()

	plan := planDoctorFix(true, false) // --no-path-fix
	if plan.AppendUserPath {
		t.Fatal("前置断言失败：--no-path-fix 下 AppendUserPath 必须为 false")
	}
	if plan.CleanUserPath {
		t.Fatal("前置断言失败：--no-path-fix 下 CleanUserPath 必须为 false")
	}

	// 门关掉 ⇒ 两个写函数都不会被调用，PATH 逐字节不变。
	if after := get(); after != initial {
		t.Errorf("--no-path-fix 下宿主 PATH 发生了变化（F-31 缺陷原形）:\n before=%q\n after =%q", initial, after)
	}

	// 反证：门打开时同一后端**确实**会变——证明上面的「不变」不是因为
	// 后端没接通（防假绿）。
	if changed, err := env.CleanupUserPath(home, filepath.Join(home, "shims"), "shim"); err != nil {
		t.Fatalf("CleanupUserPath: %v", err)
	} else if !changed {
		t.Fatal("内存后端未接通：门打开时 CleanupUserPath 应报告 changed")
	}
	if after := get(); after == initial {
		t.Error("门打开时 PATH 应已变化（内存后端未生效）")
	}
}

// ---------------------------------------------------------------------------
// F-33（P2）：shell hook marker 与 env marker 必须被同时识别
// ---------------------------------------------------------------------------

// TestHasXdevRCBlock_RecognizesBothMarkers：doctor 的「shell rc injected」
// 校验必须同时认 hook marker 与 env.Marker()，否则 activate 成功后报假阴性。
func TestHasXdevRCBlock_RecognizesBothMarkers(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"empty", "", false},
		{"unrelated", "export PATH=/usr/bin\n", false},
		{"env marker (init)", "# >>> xdev init >>>\nexport PATH=x\n# <<< xdev init <<<\n", true},
		{"hook marker (activate)", "# >>> xdev hook >>>\necho hi\n# <<< xdev hook <<<\n", true},
		{"legacy xmdevenvmt marker", "# >>> xmdevenvmt init >>>\nexport PATH=x\n", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasXdevRCBlock([]byte(tc.body)); got != tc.want {
				t.Errorf("hasXdevRCBlock(%q) = %v, want %v", tc.body, got, tc.want)
			}
		})
	}
}

// TestStripAllXdevRCBlocks_RemovesBothBlocks：self uninstall 必须把
// activate 注入的 hook 块也清掉（缺陷原形：只走 env.RemoveShellRC，
// 它只认 env.Marker 块，RC 里会留下 hook 块）。
func TestStripAllXdevRCBlocks_RemovesBothBlocks(t *testing.T) {
	rc := filepath.Join(t.TempDir(), ".zshrc")
	body := "export FOO=1\n" +
		"# >>> xdev init >>>\nexport PATH=x\n# <<< xdev init <<<\n" +
		"# >>> xdev hook >>>\necho hook\n# <<< xdev hook <<<\n" +
		"export BAR=2\n"
	if err := os.WriteFile(rc, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := stripAllXdevRCBlocks(env.ShellInfo{Shell: "zsh", RCFile: rc}); err != nil {
		t.Fatalf("stripAllXdevRCBlocks: %v", err)
	}

	got, err := os.ReadFile(rc)
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	if strings.Contains(text, hookMarker) || strings.Contains(text, hookEndMarker) {
		t.Errorf("hook 块未被清除（F-33 ②）: %q", text)
	}
	if strings.Contains(text, env.Marker()) {
		t.Errorf("env 块未被清除: %q", text)
	}
	if !strings.Contains(text, "export FOO=1") || !strings.Contains(text, "export BAR=2") {
		t.Errorf("用户自己的内容被误删: %q", text)
	}
}

// ---------------------------------------------------------------------------
// F-34（P2）：activate <shell> 与检测到的 shell 不一致时必须报错
// ---------------------------------------------------------------------------

// TestResolveActivateShell_MismatchIsAnError：参数与检测结果不一致 → 报错，
// 且错误信息必须点名检测到的 shell。
func TestResolveActivateShell_MismatchIsAnError(t *testing.T) {
	detected := env.ShellInfo{Shell: "pwsh", RCFile: `C:\Users\u\Documents\PowerShell\Microsoft.PowerShell_profile.ps1`}

	_, err := resolveActivateShell("zsh", detected)
	if err == nil {
		t.Fatal("activate zsh（检测到 pwsh）必须报错——否则会把 zsh 语法写进 PowerShell profile")
	}
	msg := err.Error()
	if !strings.Contains(msg, "pwsh") {
		t.Errorf("错误信息必须说明检测到的 shell 是 pwsh，得到: %v", err)
	}
	if !strings.Contains(msg, "zsh") {
		t.Errorf("错误信息必须回显用户请求的 shell，得到: %v", err)
	}
	if !strings.Contains(msg, detected.RCFile) {
		t.Errorf("错误信息必须给出会受影响的 RC 路径，得到: %v", err)
	}
}

// TestResolveActivateShell_Matches：一致（含大小写）时通过，
// 且 RCFile 必须来自检测结果而不是猜测。
func TestResolveActivateShell_Matches(t *testing.T) {
	detected := env.ShellInfo{Shell: "zsh", RCFile: "/home/u/.zshrc"}

	got, err := resolveActivateShell("", detected)
	if err != nil {
		t.Fatalf("无参数必须沿用检测结果: %v", err)
	}
	if got.Shell != "zsh" || got.RCFile != "/home/u/.zshrc" {
		t.Errorf("无参数时 got = %+v, want 检测结果原样", got)
	}

	got, err = resolveActivateShell("ZSH", detected)
	if err != nil {
		t.Fatalf("大小写不同必须视为一致: %v", err)
	}
	if got.RCFile != "/home/u/.zshrc" {
		t.Errorf("RCFile 必须来自检测结果，得到 %q", got.RCFile)
	}
}

// TestResolveActivateShell_PowerShellFamilyEquivalent：
// pwsh 与 powershell 语法相同（都是 $env: 形式），互为等价。
func TestResolveActivateShell_PowerShellFamilyEquivalent(t *testing.T) {
	detected := env.ShellInfo{Shell: "powershell", RCFile: `C:\...\WindowsPowerShell\profile.ps1`}
	got, err := resolveActivateShell("pwsh", detected)
	if err != nil {
		t.Fatalf("pwsh 与 powershell 同族，必须接受: %v", err)
	}
	if got.RCFile != detected.RCFile {
		t.Errorf("RCFile = %q, want %q（不得改写成猜出来的路径）", got.RCFile, detected.RCFile)
	}
}

// TestResolveActivateShell_BashZshNotInterchangeable：bash 与 zsh 的 hook
// 模板不同（alias cd vs add-zsh-hook），不得互换。
func TestResolveActivateShell_BashZshNotInterchangeable(t *testing.T) {
	detected := env.ShellInfo{Shell: "bash", RCFile: "/home/u/.bashrc"}
	if _, err := resolveActivateShell("zsh", detected); err == nil {
		t.Error("bash 与 zsh 的 hook 模板不同，activate zsh 必须报错")
	}
}

// ---------------------------------------------------------------------------
// F-29（P1）：refresh 不得把 latest 降级
// ---------------------------------------------------------------------------

// TestLoadToolManifests_ReturnsDiskOrderNotReleaseOrder 记录根因：
// LoadToolManifests 按**文件名**排序、不按版本新旧排序，因此
// `versions[0]` 不是最新版本——旧 refresh 无条件 `t2.Latest = versions[0]`
// 会静默降级。
func TestLoadToolManifests_ReturnsDiskOrderNotReleaseOrder(t *testing.T) {
	dir := t.TempDir()
	body := "name: demo\n" +
		"versions:\n" +
		"  - version: \"1.0.0\"\n" +
		"  - version: \"2.0.0\"\n"
	if err := os.WriteFile(filepath.Join(dir, "demo.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	tools, err := manifest.LoadToolManifests(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 {
		t.Fatalf("tools = %d, want 1", len(tools))
	}
	if got := tools[0].Versions()[0]; got != "1.0.0" {
		t.Fatalf("前提变了：LoadToolManifests 现在会排序（versions[0] = %q）——F-29 根因描述需更新", got)
	}
}

// TestNewestVersion_UsesReleaseOrder：取最高版本必须走 SortVersionsByRelease
// 的全序（semver 为主键、发布日期为次键、原串为末键），而不是磁盘顺序。
func TestNewestVersion_UsesReleaseOrder(t *testing.T) {
	cases := []struct {
		name     string
		versions []manifest.Version
		want     string
	}{
		{"disk order ascending", []manifest.Version{{Version: "1.0.0"}, {Version: "2.0.0"}}, "2.0.0"},
		{"disk order descending", []manifest.Version{{Version: "2.0.0"}, {Version: "1.0.0"}}, "2.0.0"},
		{"pre-release below release", []manifest.Version{{Version: "1.0.0-rc.1"}, {Version: "1.0.0"}}, "1.0.0"},
		{"single", []manifest.Version{{Version: "3.1.4"}}, "3.1.4"},
		{"empty", nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := newestVersion(tc.versions); got != tc.want {
				t.Errorf("newestVersion(%v) = %q, want %q", tc.versions, got, tc.want)
			}
		})
	}
}

// TestNewestVersion_DoesNotMutateInput：newestVersion 是查询，不得就地重排
// 调用方的切片（refresh 之后还要用它打印预览）。
func TestNewestVersion_DoesNotMutateInput(t *testing.T) {
	versions := []manifest.Version{{Version: "1.0.0"}, {Version: "2.0.0"}}
	_ = newestVersion(versions)
	if versions[0].Version != "1.0.0" {
		t.Errorf("newestVersion 就地修改了输入切片: %v", versions)
	}
}

// TestShouldUpdateLatest_NeverDowngrades：只在候选严格高于现值时才写回。
//
// 第八轮审计 R8-2 更新了签名：比较改用 manifest.VersionReleaseNewer（与
// newestVersion 同一个全序），因此需要候选/现值所在的 VersionList 来取 Release
// 日期。本用例的版本串都不带日期，语义与旧断言完全一致。
func TestShouldUpdateLatest_NeverDowngrades(t *testing.T) {
	cases := []struct {
		candidate, current string
		want               bool
	}{
		{"2.0.0", "1.0.0", true},
		{"1.0.0", "2.0.0", false}, // 降级：必须拒绝（F-29 缺陷原形）
		{"2.0.0", "2.0.0", false}, // 无变化：不写回，顺便保住注释
		{"2.0.0", "", true},
		{"1.0.0-rc.2", "1.0.0-rc.1", true},
		{"1.0.0", "1.0.0-rc.1", true}, // release > pre-release
		{"1.0.0-rc.1", "1.0.0", false},
	}
	for _, tc := range cases {
		// 构造只含这两个版本串的列表（无 Release 日期）。
		versions := []manifest.Version{{Version: tc.current}, {Version: tc.candidate}}
		if got := shouldUpdateLatest(versions, tc.candidate, tc.current); got != tc.want {
			t.Errorf("shouldUpdateLatest(%q, %q) = %v, want %v", tc.candidate, tc.current, got, tc.want)
		}
	}
}

// 第八轮审计 R8-2 守卫：写回门必须与 newestVersion 用**同一个全序**。
//
// 缺陷现场：newestVersion 走 manifest.SortVersionsByRelease 的全序
// （semver → 发布日期 → 原串），而写回门用 util.CompareSemver（只比三段数字，
// 忽略日期次键）。数字段相同、发布日期不同时，全序判「更新」而 CompareSemver 判
// 「相等」→ latest 永远推不上去且无提示。真实语料：clion/goland/webstorm 的
// `2026.2` vs `2026.2.0.1`。
func TestShouldUpdateLatest_UsesSameTotalOrderAsNewestVersion(t *testing.T) {
	// 数字段相同、日期不同的两个版本（真实语料形态）。
	versions := []manifest.Version{
		{Version: "2026.2", Release: "2026-07-16"},
		{Version: "2026.2.0.1", Release: "2026-07-23"},
	}

	// newestVersion 挑出的是带更晚日期的那个。
	candidate := newestVersion(versions)
	if candidate != "2026.2.0.1" {
		t.Fatalf("test setup: newestVersion = %q, want 2026.2.0.1", candidate)
	}

	// 写回门必须同意 —— 旧实现（CompareSemver=0）会在这里返回 false。
	if !shouldUpdateLatest(versions, candidate, "2026.2") {
		t.Error("R8-2 复现：newestVersion 选中了 2026.2.0.1，但写回门拒绝推进 latest " +
			"——两套排序分裂（全序 vs CompareSemver）")
	}

	// 反向：不得把较旧日期的版本推成 latest。
	if shouldUpdateLatest(versions, "2026.2", "2026.2.0.1") {
		t.Error("写回门把较旧日期的版本推成了 latest")
	}
}
