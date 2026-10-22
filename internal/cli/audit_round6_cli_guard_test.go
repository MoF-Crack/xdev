package cli

// 第六轮审计修复（F-30/F-32/F-35/F-36）守卫测试。
//
// 宿主安全：本文件所有用例都不触碰真实注册表与真实 home——
//   - 纯函数用例只驱动内存数据；
//   - 需要文件系统的用例一律写 t.TempDir()。

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/signer"
)

// ---------------------------------------------------------------------------
// F-32（P2）：installer.New 调用点必须全部接 --no-path-fix 门
// ---------------------------------------------------------------------------

// TestInstallerNewCallSitesAreGatedByNoPathFix 结构性守卫：
// internal/cli 下每个 `ins := installer.New(` 赋值点都必须在紧随的几行内
// 调用 SetNoEnvWrite。
//
// 为什么是源码级守卫（而不是逐个补）：这类「六处漏两处」的缺陷每次
// 都是同一个形态——新增调用点忘记接门。逐个补只能修当下这一处。
func TestInstallerNewCallSitesAreGatedByNoPathFix(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}

	assignRe := regexp.MustCompile(`^\s*(\w+)\s*:?=\s*installer\.New\(`)
	seen := 0

	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			m := assignRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			seen++
			v := m[1]

			// 窗口 8 行，注释行不占窗口（缺陷原形是**完全没有**门，
			// 不是门晚了几行）。
			end := i + 8
			if end > len(lines) {
				end = len(lines)
			}
			gated := false
			for _, next := range lines[i+1 : end] {
				if strings.HasPrefix(strings.TrimSpace(next), "//") {
					continue
				}
				if strings.Contains(next, v+".SetNoEnvWrite(") {
					gated = true
					break
				}
			}
			if !gated {
				t.Errorf("%s:%d: `%s := installer.New(...)` 之后没有 %s.SetNoEnvWrite(NoPathFix())"+
					"——Uninstall/Activate 会真实写宿主注册表（JAVA_HOME/GOPATH/PATH）",
					name, i+1, v, v)
			}
		}
	}

	if seen < 5 {
		t.Fatalf("只找到 %d 个 installer.New 调用点——守卫扫描失效（预期 ≥5）", seen)
	}
}

// TestUninstallCmd_GatesNoEnvWrite：`xdev uninstall` 的调用点必须显式带门。
func TestUninstallCmd_GatesNoEnvWrite(t *testing.T) {
	data, err := os.ReadFile("install.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)

	idx := strings.Index(src, "func newUninstallCmd()")
	if idx < 0 {
		t.Fatal("newUninstallCmd not found")
	}
	rest := src[idx+len("func newUninstallCmd()"):]
	end := strings.Index(rest, "\nfunc ")
	if end < 0 {
		end = len(rest)
	}
	body := rest[:end]

	if !strings.Contains(body, "installer.New(") {
		t.Fatal("newUninstallCmd 不再调用 installer.New——守卫需要更新")
	}
	if !strings.Contains(body, "SetNoEnvWrite(NoPathFix())") {
		t.Error("newUninstallCmd 必须调用 ins.SetNoEnvWrite(NoPathFix())：" +
			"删掉最后一个版本时会走 removeUserEnvVars，真实删除宿主的 JAVA_HOME/GOPATH")
	}
}

// ---------------------------------------------------------------------------
// F-35（P2）：config set 的 URL / 路径校验
// ---------------------------------------------------------------------------

// TestConfigSet_Mirror_RejectsUnsafeURLs：mirror 分支必须复用
// util.IsSafeHTTPSExternalURL（与 GUI SetMirrorConfig、profile import 同源）。
func TestConfigSet_Mirror_RejectsUnsafeURLs(t *testing.T) {
	rejected := []string{
		"http://mirrors.example.com/python", // 明文 http
		"https://127.0.0.1/mirror",          // localhost
		"https://10.0.0.5/mirror",           // 内网地址
		"ftp://mirrors.example.com/x",       // 非法 scheme
		"not-a-url",
	}
	for _, v := range rejected {
		cfg := &config.Config{}
		if err := configSet(cfg, "mirror.python", v); err == nil {
			t.Errorf("configSet(mirror.python, %q) = nil, want rejection", v)
		}
		if _, ok := cfg.Mirror["python"]; ok {
			t.Errorf("被拒的 mirror %q 不得写入 cfg.Mirror", v)
		}
	}

	// 正例用 IP 字面量（IsSafeHTTPSExternalURL 对 IP 字面量不做 DNS 解析），
	// 测试因此不依赖网络。
	const safe = "https://1.1.1.1/python"
	cfg := &config.Config{}
	if err := configSet(cfg, "mirror.python", safe); err != nil {
		t.Fatalf("合法 https 镜像必须接受: %v", err)
	}
	if cfg.Mirror["python"] != safe {
		t.Errorf("cfg.Mirror[python] = %q, want %q", cfg.Mirror["python"], safe)
	}
}

// TestConfigSet_CoreDirs_RejectRelativeAndUNC：
// 与 core.xmdevenv_dir 同源的 validateLocalAbsPath 必须铺开到其余目录字段。
func TestConfigSet_CoreDirs_RejectRelativeAndUNC(t *testing.T) {
	fields := []string{"download_dir", "install_dir", "shim_dir", "cache_dir", "env_dir", "manifest_dir"}
	bad := []string{"relative/dir", `C:relative`, `\\server\share\x`, "//server/share"}
	for _, field := range fields {
		for _, v := range bad {
			cfg := &config.Config{}
			if err := configSet(cfg, "core."+field, v); err == nil {
				t.Errorf("configSet(core.%s, %q) = nil, want rejection", field, v)
			}
		}
		cfg := &config.Config{}
		if err := configSet(cfg, "core."+field, `D:\XDev\sub`); err != nil {
			t.Errorf("configSet(core.%s, 绝对路径) = %v, want nil", field, err)
		}
	}
}

// ---------------------------------------------------------------------------
// F-36（P3）：多字节文本截断必须产出合法 UTF-8
// ---------------------------------------------------------------------------

// TestTruncate_KeepsValidUTF8：按字节截断会把一个汉字劈成半个，产出非法 UTF-8。
func TestTruncate_KeepsValidUTF8(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"中文描述文本", 4, "中文描…"},
		{"中文描述文本", 3, "中文…"},
		{"中文描述文本", 100, "中文描述文本"},
		{"ascii text", 5, "asci…"},
		{"ascii text", 1, "…"}, // n=1 时省略号恰好占 1 个 rune，且保留「被截断」语义
		{"a", 1, "a"},          // 本来就不超长 → 原样返回
		{"ascii text", 0, ""},
		{"🚀🚀🚀🚀", 3, "🚀🚀…"},
		{"  padded  ", 100, "padded"},
	}
	for _, tc := range cases {
		got := truncate(tc.in, tc.n)
		if !utf8.ValidString(got) {
			t.Errorf("truncate(%q, %d) = %q — 不是合法 UTF-8", tc.in, tc.n, got)
		}
		if got != tc.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tc.in, tc.n, got, tc.want)
		}
	}
}

// TestPrintRow_KeepsValidUTF8：表格渲染的窄列截断同样不得劈开多字节字符。
func TestPrintRow_KeepsValidUTF8(t *testing.T) {
	readStdout := captureStdout(t)
	printRow([]string{"中文描述文本很长", "ok"}, []int{5, 4})
	out := readStdout()

	if !utf8.ValidString(out) {
		t.Errorf("printRow 输出不是合法 UTF-8: %q", out)
	}
	if !strings.Contains(out, "中文描述…") {
		t.Errorf("窄列截断结果不符（期望按字符截断）: %q", out)
	}
}

// ---------------------------------------------------------------------------
// F-30（P1）：sign-manifests 全部写失败仍 exit 0
// ---------------------------------------------------------------------------

// signManifestFixture 写一个带真实哈希的 manifest（否则会被 "no SHA-256
// hashes" 分支跳过，测不到写失败路径）。
func signManifestFixture(t *testing.T, dir, tool string) {
	t.Helper()
	body := "name: " + tool + "\n" +
		"display_name: " + tool + "\n" +
		"versions:\n" +
		"  - version: \"1.0.0\"\n" +
		"    archives:\n" +
		"      - os: linux\n" +
		"        arch: x86_64\n" +
		"        format: zip\n" +
		"        url: https://example.com/" + tool + ".zip\n" +
		"        sha256: \"" + strings.Repeat("a", 64) + "\"\n"
	if err := os.WriteFile(filepath.Join(dir, tool+".yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestSignManifests_FailsWhenASignatureCannotBeWritten：逐文件写失败必须
// 让命令非零退出（与 fill-sha256 的 R5 同型），--allow-partial 显式豁免。
func TestSignManifests_FailsWhenASignatureCannotBeWritten(t *testing.T) {
	manifests := t.TempDir()
	outDir := t.TempDir()

	signManifestFixture(t, manifests, "good")
	signManifestFixture(t, manifests, "broken")

	// 让 broken.yaml.sig 的原子写失败。
	//
	// 第九轮 A-10 更新：原实现把 `<sig 路径>.tmp` 做成目录来制造失败，这**耦合了
	// 旧 AtomicWriteFile 的固定临时文件名**。A-10 迁移到 os.CreateTemp 后临时名是
	// 唯名的（`<base>.tmp-*`），那个目录不再被触碰，故障注入随之失效——属「测试
	// 依赖实现细节」而非断言过时。改为**与实现无关**的注入：把 .sig 目标本身做成
	// 目录，任何「写临时文件再 rename 覆盖目标」的实现都会在 rename 阶段失败，
	// 而 good.yaml.sig 仍可正常写出。
	// 断言本身（必须返回错误、错误含 "failed"、good 仍写成功、--allow-partial 放行）
	// 一字未改。
	if err := os.MkdirAll(filepath.Join(outDir, "broken.yaml.sig"), 0o755); err != nil {
		t.Fatal(err)
	}

	keyDir := t.TempDir()
	privPath := filepath.Join(keyDir, "priv.key")
	if err := signer.GenerateKeyPair(filepath.Join(keyDir, "pub.key"), privPath); err != nil {
		t.Fatal(err)
	}

	run := func(extra ...string) error {
		cmd := newSignManifestsCmd()
		args := append([]string{
			"--manifests-dir", manifests,
			"--output-dir", outDir,
			"--priv-key", privPath,
		}, extra...)
		cmd.SetArgs(args)
		cmd.SetOut(&strings.Builder{})
		cmd.SetErr(&strings.Builder{})
		cmd.SilenceUsage = true
		return cmd.Execute()
	}

	// 默认：有写失败 → 必须返回错误。
	err := run()
	if err == nil {
		t.Fatal("sign-manifests 有 .sig 写失败却返回 nil——CI 会把「全部签名写失败」当成成功")
	}
	if !strings.Contains(err.Error(), "failed") {
		t.Errorf("错误信息应聚合失败数，得到: %v", err)
	}

	// 好条目仍然签出来了（失败不阻断其余条目）。
	if _, statErr := os.Stat(filepath.Join(outDir, "good.yaml.sig")); statErr != nil {
		t.Errorf("good.yaml.sig 应已写出: %v", statErr)
	}

	// --allow-partial：显式接受缺口时返回 nil（与 fill-sha256 对称）。
	if err := run("--allow-partial"); err != nil {
		t.Fatalf("--allow-partial 必须不使命令失败: %v", err)
	}
}

// TestSignManifests_AllGoodReturnsNil：全成功时不得因为新计数器而误报失败。
func TestSignManifests_AllGoodReturnsNil(t *testing.T) {
	manifests := t.TempDir()
	outDir := t.TempDir()

	signManifestFixture(t, manifests, "ok")

	keyDir := t.TempDir()
	privPath := filepath.Join(keyDir, "priv.key")
	if err := signer.GenerateKeyPair(filepath.Join(keyDir, "pub.key"), privPath); err != nil {
		t.Fatal(err)
	}

	cmd := newSignManifestsCmd()
	cmd.SetArgs([]string{"--manifests-dir", manifests, "--output-dir", outDir, "--priv-key", privPath})
	cmd.SetOut(&strings.Builder{})
	cmd.SetErr(&strings.Builder{})
	cmd.SilenceUsage = true
	if err := cmd.Execute(); err != nil {
		t.Fatalf("全部成功时必须返回 nil: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "ok.yaml.sig")); err != nil {
		t.Errorf("ok.yaml.sig 未写出: %v", err)
	}
}

// TestSignManifestsCmd_HasAllowPartialFlag：与 fill-sha256 对称的豁免 flag。
func TestSignManifestsCmd_HasAllowPartialFlag(t *testing.T) {
	cmd := newSignManifestsCmd()
	if f := cmd.Flags().Lookup("allow-partial"); f == nil {
		t.Error("sign-manifests 必须提供 --allow-partial（与 fill-sha256 对称）")
	}
}

// TestSignManifests_RejectsUnsafeToolNameStillFails：路径安全校验失败同样
// 计入 failed（它也是「这个工具没签出来」）。
func TestSignManifests_RejectsUnsafeToolNameStillFails(t *testing.T) {
	manifests := t.TempDir()
	outDir := t.TempDir()

	// 文件名 "..evil.yaml" 解析出的 tool 名为 "..evil" → validateManifestPathName 拒绝。
	signManifestFixture(t, manifests, "..evil")

	keyDir := t.TempDir()
	privPath := filepath.Join(keyDir, "priv.key")
	if err := signer.GenerateKeyPair(filepath.Join(keyDir, "pub.key"), privPath); err != nil {
		t.Fatal(err)
	}

	cmd := newSignManifestsCmd()
	cmd.SetArgs([]string{"--manifests-dir", manifests, "--output-dir", outDir, "--priv-key", privPath})
	cmd.SetOut(&strings.Builder{})
	cmd.SetErr(&strings.Builder{})
	cmd.SilenceUsage = true
	if err := cmd.Execute(); err == nil {
		t.Fatal("被路径安全校验拒绝的工具名必须让命令非零退出（否则 CI 会漏掉整批未签出的 manifest）")
	}
}
