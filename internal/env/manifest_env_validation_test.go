//go:build windows

// manifest_env_validation_test.go — 守卫测试 42-1（审计 X-08 / P2-28，Batch 5 票 42）。
//
// SetUserEnvVarTrusted 是安装器写入 manifest env_vars 的入口。本文件固定它的
// 值校验契约：
//
//	拒绝：
//	  - 路径型变量（*_HOME / *_ROOT / GOROOT / GOPATH）的值含 %VAR% 引用
//	    （%TEMP%\evil —— REG_EXPAND_SZ 展开后逃出管理树，且无法静态验证）；
//	    ← 该条与「管理树根是否可知」无关，始终生效；
//	  - 未知 %VAR% 引用（旧行为仅 WARNING，现为硬错误）；
//	  - 管理树根**已知**时：路径型变量的非绝对路径 / 相对路径 / 空值，以及
//	    位于管理树之外的绝对路径。
//
//	放行：
//	  - 白名单 %VAR% 引用（非路径型变量）：7 项硬编码系统引用 + AllowedEnvVarNames；
//	  - 管理树内的绝对路径，含 GOPATH 的 <install_root>\..\gopath 形态
//	    （manifest 的 {{install}}\..\gopath 插值结果）；
//	  - 管理树根**未知**时的保守放行（WARN 日志，不拒绝；见
//	    TestSetUserEnvVarTrusted_ConservativeWhenRootUnknown）。
//
// 这些用例原先写真实 HKCU\Environment（save/restore + t.Cleanup 夹具）。第四轮
// 复核（P0-17 事故类别根除）后：userenv_hermetic_test.go 的 TestMain 为整包安装
// 内存用户环境后端（种子 = 启动时的真实 HKCU 快照），SetUserEnvVarTrusted /
// DeleteUserEnvVarTrusted 经 userEnvWriteFn / userEnvDeleteFn 接缝全部落在内存
// 映射上 —— 强杀/panic 也不可能把测试值留在用户环境里。本文件的 save/restore
// 夹具保留作为用例内的第二道保险（断言语义不变：assertNoWrite 验证「校验先于
// 任何落盘尝试」—— 值若被写入会出现在内存后端里，一样能抓住）。
package env

import (
	"path/filepath"
	"strings"
	"testing"
)

// ---- 测试夹具 ----

// managedRootSeam 在测试期间替换「xdev 管理树根」探针，结束后还原。
// root == "" 表示「根不可确定」（生产默认在 go test 下即为此值）。
func managedRootSeam(t *testing.T, root string) {
	t.Helper()
	prev := managedTreeRoot
	managedTreeRoot = func() string { return root }
	t.Cleanup(func() { managedTreeRoot = prev })
}

// envVarSnapshot 记录一个用户级环境变量的原值，供测试结束后精确恢复。
type envVarSnapshot struct {
	name    string
	value   string
	existed bool
}

// saveEnvVar 读取原值（走 trusted 读取，避免白名单限制）并注册恢复。
func saveEnvVar(t *testing.T, name string) envVarSnapshot {
	t.Helper()
	v, err := GetUserEnvVarTrusted(name)
	snap := envVarSnapshot{name: name, value: v, existed: err == nil}
	t.Cleanup(func() { snap.restore(t) })
	return snap
}

// restore 恢复原值：原本存在则写回原值，原本不存在则删除测试写入的值。
//
// 恢复期间把管理树根强制为「不可确定」：原值完全可能合法地位于管理树之外
// （例如主机自带 JDK 的 JAVA_HOME），绝不能让 X-08 的新校验挡住恢复操作。
func (s envVarSnapshot) restore(t *testing.T) {
	t.Helper()
	prev := managedTreeRoot
	managedTreeRoot = func() string { return "" }
	defer func() { managedTreeRoot = prev }()

	if s.existed {
		if err := SetUserEnvVarTrusted(s.name, s.value); err != nil {
			t.Errorf("restore %s=%q: %v", s.name, s.value, err)
		}
		return
	}
	if err := DeleteUserEnvVarTrusted(s.name); err != nil {
		t.Errorf("delete test env var %s: %v", s.name, err)
	}
}

// assertNoWrite 断言被拒绝的调用没有写注册表：值与快照完全一致。
func assertNoWrite(t *testing.T, snap envVarSnapshot) {
	t.Helper()
	got, err := GetUserEnvVarTrusted(snap.name)
	if !snap.existed {
		if err == nil {
			t.Errorf("%s 被写入注册表（值 %q），尽管写入被拒绝 —— 校验必须发生在任何注册表访问之前", snap.name, got)
		}
		return
	}
	if err != nil || got != snap.value {
		t.Errorf("%s 的注册表值被改动（got %q, err %v, want %q），尽管写入被拒绝", snap.name, got, err, snap.value)
	}
}

// ---- 42-1a：路径型变量的 %VAR% 值必须被拒 ----

// TestSetUserEnvVarTrusted_RejectsRefInPathTypedVar 固定守卫 42-1 的第一条：
// %TEMP%\evil 形式的路径型变量值必须被拒绝，错误信息含变量名。
//
// 关键性质：该拒绝**不依赖**管理树根是否可知（%VAR% 无法静态验证），因此
// 三种根状态（已知 / 未知）下都必须拒绝 —— 这正是 X-08 想堵的 REG_EXPAND_SZ
// 运行时展开逃逸：%TEMP% 本身在白名单内，唯有路径规则能拦下它。
func TestSetUserEnvVarTrusted_RejectsRefInPathTypedVar(t *testing.T) {
	cases := []struct {
		desc  string
		name  string
		value string
	}{
		{"%TEMP% 在白名单内但仍逃出管理树", "GOROOT", `%TEMP%\evil`},
		{"白名单引用同样无法静态验证", "JAVA_HOME", `%USERPROFILE%\evil`},
		{"APPDATA 变体", "XDEVTEST_HOME", `%APPDATA%\evil`},
		{"ProgramFiles 变体", "XDEVTEST_ROOT", `%ProgramFiles%\evil`},
		{"GOPATH 亦为路径型", "GOPATH", `%LOCALAPPDATA%\evil`},
		{"SystemRoot 变体", "ANDROID_SDK_ROOT", `%SystemRoot%\evil`},
		{"小写引用（Windows 展开不区分大小写）", "XDEVTEST_HOME", `%temp%\evil`},
	}

	roots := []struct {
		desc string
		root string
	}{
		{"管理树根已知", t.TempDir()},
		{"管理树根未知（保守策略仍拒绝）", ""},
	}

	for _, rt := range roots {
		for _, tc := range cases {
			t.Run(rt.desc+"/"+tc.name+"="+tc.value, func(t *testing.T) {
				managedRootSeam(t, rt.root)
				snap := saveEnvVar(t, tc.name)

				err := SetUserEnvVarTrusted(tc.name, tc.value)
				if err == nil {
					t.Fatalf("SetUserEnvVarTrusted(%q, %q) = nil, 必须被拒绝（X-08）", tc.name, tc.value)
				}
				if !strings.Contains(err.Error(), tc.name) {
					t.Errorf("错误信息 %q 必须包含变量名 %q", err.Error(), tc.name)
				}
				assertNoWrite(t, snap)
			})
		}
	}
}

// ---- 42-1b：白名单引用必须放行（非路径型变量） ----

// TestSetUserEnvVarTrusted_AllowsWhitelistedRefs 固定守卫 42-1 的第二条：
// 7 项硬编码白名单引用 + util.AllowedEnvVarNames 内的引用在**非路径型**变量
// 上必须正常通过（写入成功）。
//
// 说明（与保守策略一致）：路径型变量上的白名单引用仍被拒（见上一个测试）——
// %VAR% 的展开结果无法静态证明落在管理树内。因此「白名单引用放行」这一条
// 以非路径型变量为断言对象；真实内置 manifest 里 PUB_CACHE / JULIA_DEPOT_PATH
// / UV_* / PHPRC 等都属于此类。
func TestSetUserEnvVarTrusted_AllowsWhitelistedRefs(t *testing.T) {
	managedRootSeam(t, "")
	const name = "XDEVTESTENVREFPASS"
	saveEnvVar(t, name) // 注册恢复（该变量原本不应存在）

	refs := append([]string{}, allowedEnvVarRefs...)
	refs = append(refs,
		"userprofile", // 大小写不敏感
		"Node_Home",   // util.AllowedEnvVarNames 成员，同样大小写不敏感
		"UV_TOOL_DIR", // Spec 022 官方变量名
	)
	for _, ref := range refs {
		value := "%" + ref + `%\x`
		t.Run(ref, func(t *testing.T) {
			if err := SetUserEnvVarTrusted(name, value); err != nil {
				t.Fatalf("白名单引用 %%%s%% 必须放行，got error: %v", ref, err)
			}
			got, err := GetUserEnvVarTrusted(name)
			if err != nil {
				t.Fatalf("读回 %s: %v", name, err)
			}
			if got != value {
				t.Errorf("%s = %q, want %q", name, got, value)
			}
		})
	}
}

// TestSetUserEnvVarTrusted_AllowsNonPathTypedLiteralPaths 记录票 42 的**范围**：
// 非路径型变量（PUB_CACHE / UV_TOOL_BIN_DIR / PHPRC …）只受「未知引用」规则约束，
// 仍可承载任意字面量路径 —— 这是票面 requirement 1 的明确边界，不是遗漏。
func TestSetUserEnvVarTrusted_AllowsNonPathTypedLiteralPaths(t *testing.T) {
	managedRootSeam(t, t.TempDir()) // 已知根也不影响非路径型变量
	const name = "XDEVTESTNONPATH"
	saveEnvVar(t, name)

	for _, value := range []string{`C:\anywhere\at\all`, `\\server\share\x`, "relative-is-fine-here"} {
		if err := SetUserEnvVarTrusted(name, value); err != nil {
			t.Errorf("非路径型变量 %s=%q 必须放行，got error: %v", name, value, err)
		}
	}
}

// ---- requirement 2：未知 %VAR% 引用从 WARNING 升级为硬错误 ----

// TestSetUserEnvVarTrusted_RejectsUnknownRef 固定 requirement 2：白名单之外
// 的 %VAR% 引用必须拒绝写入（旧行为只打印 WARNING），错误含引用名与变量名，
// 且对路径型/非路径型变量都生效（引用检查在路径检查之前）。
func TestSetUserEnvVarTrusted_RejectsUnknownRef(t *testing.T) {
	managedRootSeam(t, t.TempDir())

	cases := []struct {
		name  string
		value string
	}{
		{"XDEVTESTENVREFBAD", `%TOTALLY_UNKNOWN_VAR%\x`},       // 非路径型
		{"XDEVTEST_HOME", `%TOTALLY_UNKNOWN_VAR%\x`},           // 路径型
		{"JAVA_HOME", `%USERPROFILE%\%TOTALLY_UNKNOWN_VAR%\x`}, // 混合：白名单 + 未知
	}
	for _, tc := range cases {
		t.Run(tc.name+"/"+tc.value, func(t *testing.T) {
			snap := saveEnvVar(t, tc.name)
			err := SetUserEnvVarTrusted(tc.name, tc.value)
			if err == nil {
				t.Fatalf("未知引用必须被拒绝（票 42 requirement 2），value=%q", tc.value)
			}
			if !strings.Contains(err.Error(), "TOTALLY_UNKNOWN_VAR") {
				t.Errorf("错误信息 %q 必须包含未知引用名", err.Error())
			}
			if !strings.Contains(err.Error(), tc.name) {
				t.Errorf("错误信息 %q 必须包含变量名 %q", err.Error(), tc.name)
			}
			assertNoWrite(t, snap)
		})
	}
}

// ---- requirement 1：路径型变量的绝对路径 + 管理树约束 ----

// TestSetUserEnvVarTrusted_PathTypedConfinedToManagedTree 是 X-08 的核心守卫：
// 管理树根已知时，路径型变量只接受管理树内的绝对路径；树外的绝对路径、
// 相对路径、空值一律拒绝，错误必须点名变量与原因。
func TestSetUserEnvVarTrusted_PathTypedConfinedToManagedTree(t *testing.T) {
	root := t.TempDir()
	managedRootSeam(t, root)
	outside := filepath.Join(filepath.Dir(root), "xdev-test-outside")

	pass := []struct {
		name  string
		value string
	}{
		{"XDEVTEST_HOME", filepath.Join(root, "install", "mytool", "1.0")},
		{"XDEVTEST_ROOT", filepath.Join(root, "install", "mytool")},
		{"GOROOT", filepath.Join(root, "install", "go", "1.26.5", "go")},
		{"GOPATH", filepath.Join(root, "install", "go", "gopath")},
		{"JAVA_HOME", filepath.Join(root, "install", "java", "25")},
		{"ANDROID_SDK_ROOT", filepath.Join(root, "install", "android-platform", "36.0.0", "..")},
	}
	for _, tc := range pass {
		t.Run("pass/"+tc.name, func(t *testing.T) {
			saveEnvVar(t, tc.name)
			if err := SetUserEnvVarTrusted(tc.name, tc.value); err != nil {
				t.Fatalf("管理树内的 %s=%q 必须放行，got error: %v", tc.name, tc.value, err)
			}
			got, err := GetUserEnvVarTrusted(tc.name)
			if err != nil {
				t.Fatalf("读回 %s: %v", tc.name, err)
			}
			if got != tc.value {
				t.Errorf("%s = %q, want %q", tc.name, got, tc.value)
			}
		})
	}

	reject := []struct {
		desc    string
		name    string
		value   string
		wantSub string
	}{
		{"树外绝对路径（任意路径劫持）", "GOROOT", `C:\evil`, "outside the xdev managed tree"},
		{"管理树的兄弟目录", "JAVA_HOME", outside, "outside the xdev managed tree"},
		{"相对路径", "XDEVTEST_HOME", `install\go`, "not an absolute path"},
		{"空值", "GOPATH", "", "empty value"},
		{"只有空白", "XDEVTEST_ROOT", "   ", "empty value"},
	}
	for _, tc := range reject {
		t.Run("reject/"+tc.desc, func(t *testing.T) {
			snap := saveEnvVar(t, tc.name)
			err := SetUserEnvVarTrusted(tc.name, tc.value)
			if err == nil {
				t.Fatalf("%s=%q 必须被拒绝（X-08）", tc.name, tc.value)
			}
			if !strings.Contains(err.Error(), tc.name) {
				t.Errorf("错误信息 %q 必须包含变量名 %q", err.Error(), tc.name)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("错误信息 %q 必须含原因 %q", err.Error(), tc.wantSub)
			}
			assertNoWrite(t, snap)
		})
	}
}

// TestSetUserEnvVarTrusted_GOPATHRelaxedToManagedTree 固定守卫 42-1 的 GOPATH 条：
// manifest go.yaml 的 {{install}}\..\gopath 插值后为
// <install_root>\<tool>\<version>\..\gopath，落在**版本目录之外、工具目录之内**
// （即 xdev 的托管工作区），因此 base 取「管理树根」而非工具安装目录 —— 这就是
// 本票对 GOPATH 的放宽：显式列入检查，但按管理树根判定。
func TestSetUserEnvVarTrusted_GOPATHRelaxedToManagedTree(t *testing.T) {
	root := t.TempDir()
	managedRootSeam(t, root)

	// 与安装器一致的字面量形态（Generic.EnvVars 只做字符串替换，不 Clean）。
	literal := root + `\install\go\1.26.5\..\gopath`
	saveEnvVar(t, "GOPATH")
	if err := SetUserEnvVarTrusted("GOPATH", literal); err != nil {
		t.Fatalf("GOPATH=%q（install_root\\..\\gopath 形态）必须放行，got error: %v", literal, err)
	}
	got, err := GetUserEnvVarTrusted("GOPATH")
	if err != nil {
		t.Fatalf("读回 GOPATH: %v", err)
	}
	if got != literal {
		t.Errorf("GOPATH = %q, want %q", got, literal)
	}

	// 放宽的边界：管理树之外的同名形态仍必须拒绝，且不覆盖刚写入的合法值。
	outside := filepath.Join(filepath.Dir(root), "gopath")
	if err := SetUserEnvVarTrusted("GOPATH", outside); err == nil {
		t.Errorf("GOPATH=%q（管理树之外）必须被拒绝", outside)
	}
	if got, err := GetUserEnvVarTrusted("GOPATH"); err != nil || got != literal {
		t.Errorf("被拒绝的写入不得改动注册表：GOPATH = %q (err %v), want %q", got, err, literal)
	}
}

// TestSetUserEnvVarTrusted_ConservativeWhenRootUnknown 固定保守策略：管理树根
// 无法确定时（$XMDEV_HOME 未设置），「绝对路径 + 必须在管理树内」这一组约束
// 整体跳过 → WARN + 放行（而非拒绝），以免破坏自部署（config.yaml /
// last_selection.json / home cache 记录 home，internal/env 读不到）的正常安装，
// 也避免误伤票外既有测试的探针写入（internal/testkit/hostsnapshot 会写
// GOROOT="<非绝对探针>"，无条件强制 IsAbs 会把它打成硬失败）。
//
// 唯一与根无关、始终生效的是 %VAR% 规则（见
// TestSetUserEnvVarTrusted_RejectsRefInPathTypedVar：根未知时 %TEMP%\evil 仍被拒）。
func TestSetUserEnvVarTrusted_ConservativeWhenRootUnknown(t *testing.T) {
	managedRootSeam(t, "")

	// 放行：树外的合法绝对路径（主机 JDK）
	saveEnvVar(t, "JAVA_HOME")
	if err := SetUserEnvVarTrusted("JAVA_HOME", `C:\Program Files\Java\jdk-17`); err != nil {
		t.Fatalf("管理树根未知时必须保守放行，got error: %v", err)
	}
	if got, err := GetUserEnvVarTrusted("JAVA_HOME"); err != nil || got != `C:\Program Files\Java\jdk-17` {
		t.Errorf("JAVA_HOME = %q (err %v), want 原样写入", got, err)
	}

	// 同样放行（保守分支）：相对路径、空值 —— 形态约束属于「管理树约束」组。
	allowed := []struct {
		name  string
		value string
	}{
		{"GOPATH", `gopath`},
		{"XDEVTEST_HOME", ""},
		{"XDEVTEST_ROOT", `install\go`},
	}
	for _, tc := range allowed {
		t.Run("allow/"+tc.name, func(t *testing.T) {
			saveEnvVar(t, tc.name)
			if err := SetUserEnvVarTrusted(tc.name, tc.value); err != nil {
				t.Fatalf("根未知时 %s=%q 必须保守放行（confine 组整体跳过），got error: %v", tc.name, tc.value, err)
			}
			got, err := GetUserEnvVarTrusted(tc.name)
			if err != nil {
				t.Fatalf("读回 %s: %v", tc.name, err)
			}
			if got != tc.value {
				t.Errorf("%s = %q, want %q", tc.name, got, tc.value)
			}
		})
	}

	// 仍拒绝：%VAR% 规则与根无关（这是守卫 42-1 的无条件部分）。
	reject := []struct {
		name    string
		value   string
		wantSub string
	}{
		{"GOROOT", `%TEMP%\evil`, "%TEMP%"},
		{"GOPATH", `%USERPROFILE%\evil`, "%USERPROFILE%"},
	}
	for _, tc := range reject {
		t.Run("reject/"+tc.name, func(t *testing.T) {
			s := saveEnvVar(t, tc.name)
			err := SetUserEnvVarTrusted(tc.name, tc.value)
			if err == nil {
				t.Fatalf("%s=%q 在根未知时仍必须被拒绝（%%VAR%% 规则与根无关）", tc.name, tc.value)
			}
			if !strings.Contains(err.Error(), tc.name) {
				t.Errorf("错误信息 %q 必须包含变量名 %q", err.Error(), tc.name)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("错误信息 %q 必须含原因 %q", err.Error(), tc.wantSub)
			}
			assertNoWrite(t, s)
		})
	}
}

// ---- 纯函数：路径型判定与管理树根推导 ----

// TestIsPathTypedEnvVar 固定路径型的判定集合（票 42 requirement 1）：
// *_HOME / *_ROOT / GOROOT / GOPATH，全部大小写不敏感；
// 未被列入的名字（含 PYTHONHOME 这类不含下划线的变体）不属于本票范围。
func TestIsPathTypedEnvVar(t *testing.T) {
	pathTyped := []string{
		"JAVA_HOME", "ANDROID_SDK_ROOT", "DOTNET_ROOT", "GOROOT", "GOPATH",
		"java_home", "goroot", "gopath", "XDEVTEST_HOME", "XDEVTEST_ROOT",
		// 真实内置 manifest 的路径型变量名
		"NODE_HOME", "DART_HOME", "FLUTTER_HOME", "GHC_HOME", "CARGO_HOME", "RUSTUP_HOME",
	}
	for _, n := range pathTyped {
		if !isPathTypedEnvVar(n) {
			t.Errorf("isPathTypedEnvVar(%q) = false, want true", n)
		}
	}
	notPathTyped := []string{
		"PUB_CACHE", "PHPRC", "JULIA_DEPOT_PATH", "UV_TOOL_BIN_DIR", "UV_CACHE_DIR",
		"npm_config_prefix", "DENO_INSTALL", "BUN_INSTALL", "PYTHONHOME", "PATH", "",
	}
	for _, n := range notPathTyped {
		if isPathTypedEnvVar(n) {
			t.Errorf("isPathTypedEnvVar(%q) = true, want false（本票范围外）", n)
		}
	}
}

// TestManagedTreeRootFromHome 覆盖默认推导中唯一可测的纯函数部分：
// 空值 → 不可确定（""），其余归一为绝对、清理后的路径。
func TestManagedTreeRootFromHome(t *testing.T) {
	if got := managedTreeRootFromHome(""); got != "" {
		t.Errorf("managedTreeRootFromHome(\"\") = %q, want \"\"", got)
	}
	if got := managedTreeRootFromHome("   "); got != "" {
		t.Errorf("managedTreeRootFromHome(空白) = %q, want \"\"", got)
	}
	dir := t.TempDir()
	if got := managedTreeRootFromHome(dir); got != filepath.Clean(dir) {
		t.Errorf("managedTreeRootFromHome(%q) = %q, want %q", dir, got, filepath.Clean(dir))
	}
	if got := managedTreeRootFromHome("  " + dir + "  "); got != filepath.Clean(dir) {
		t.Errorf("未裁剪空白: got %q, want %q", got, filepath.Clean(dir))
	}
	if got := managedTreeRootFromHome("relative-xdev-home"); !filepath.IsAbs(got) {
		t.Errorf("相对 $XMDEV_HOME 必须归一为绝对路径，got %q", got)
	}
}

// TestDefaultManagedTreeRoot_IndeterminateUnderGoTest 固定「测试下默认不可确定」
// 这一决策：见 defaultManagedTreeRoot 的注释 —— 票外既有测试（goroot_cleanup_test.go
// 的 t.TempDir() GOROOT、cli 的 JAVA_HOME=C:\Java）会写管理树之外的探针值，
// 而在 xdev hook 过的 shell 里跑 go test 时 $XMDEV_HOME 是设置的，若在此强制
// 校验会把探针变成硬失败。强制校验由 managedRootSeam 注入的测试覆盖。
func TestDefaultManagedTreeRoot_IndeterminateUnderGoTest(t *testing.T) {
	t.Setenv("XMDEV_HOME", t.TempDir())
	if got := defaultManagedTreeRoot(); got != "" {
		t.Errorf("defaultManagedTreeRoot() 在 go test 下 = %q, want \"\"（不可确定 → 保守放行）", got)
	}
}
