// npm_install_test.go covers the npm install kind (Spec 039): toolchain
// resolution, the pre-install npm dependency check, download skipping, and
// the npm CLI invocation contract.
package installer

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/manifest"
)

// npmTestPlugin wraps fakePlugin with a real manifest so npmPackageSpec /
// nodeMinVersion / install flow see Remote + Depends. ResolveVersion and
// ArchiveFor are overridden to return the npm fixture version.
type npmTestPlugin struct {
	*fakePlugin
	m      *manifest.Tool
	shim   string
	ver    string
	archiv *manifest.Archive
}

func (f *npmTestPlugin) Manifest() *manifest.Tool { return f.m }
func (f *npmTestPlugin) DefaultVersion() string   { return f.ver }
func (f *npmTestPlugin) ResolveVersion(spec string) (string, error) {
	return f.ver, nil
}
func (f *npmTestPlugin) ArchiveFor(version string) (*manifest.Archive, error) {
	a := *f.archiv
	return &a, nil
}

// newNPMTestPlugin builds a dsh-shaped plugin: npm remote, prerelease
// version, npm-kind archive entry with a bogus URL (any download attempt
// would fail offline — proving the download skip).
func newNPMTestPlugin(name, ver string, withMinNode bool) *npmTestPlugin {
	m := &manifest.Tool{
		Name: name,
		Remote: manifest.RemoteSource{
			Type:    "npm",
			Package: "@deepseek-ai/dsh",
		},
	}
	if withMinNode {
		m.Depends = []manifest.Dependency{
			{Tool: "node", MinVersion: "22.0.0", InstallPath: "npm"},
		}
	}
	return &npmTestPlugin{
		fakePlugin: &fakePlugin{name: name, shims: []string{name}},
		m:          m,
		ver:        ver,
		archiv: &manifest.Archive{
			OS: "any", Arch: "any", Format: "tar.gz",
			URL:         "https://registry.example.invalid/@deepseek-ai/dsh/-/dsh-" + ver + ".tgz",
			InstallKind: manifest.InstallKindNPM,
		},
	}
}

// fakeNodeLayout creates a xdev-managed Node.js directory under
// <xmdeDir>/current/node with the standard layout (node.exe / bin/node,
// node_modules/npm/bin/npm-cli.js, npm.cmd / bin/npm).
func fakeNodeLayout(t *testing.T, xmdeDir string) string {
	t.Helper()
	nodeDir := filepath.Join(xmdeDir, "current", "node")
	npmCLIDir := filepath.Join(nodeDir, "node_modules", "npm", "bin")
	if err := os.MkdirAll(npmCLIDir, 0o755); err != nil {
		t.Fatalf("mkdir fake node: %v", err)
	}
	if runtime.GOOS == "windows" {
		for _, f := range []string{"node.exe", "npm.cmd"} {
			if err := os.WriteFile(filepath.Join(nodeDir, f), []byte("fake"), 0o755); err != nil {
				t.Fatalf("write %s: %v", f, err)
			}
		}
	} else {
		binDir := filepath.Join(nodeDir, "bin")
		libNpmCLI := filepath.Join(nodeDir, "lib", "node_modules", "npm", "bin")
		for _, d := range []string{binDir, libNpmCLI} {
			if err := os.MkdirAll(d, 0o755); err != nil {
				t.Fatalf("mkdir %s: %v", d, err)
			}
		}
		for _, f := range []string{filepath.Join(binDir, "node"), filepath.Join(binDir, "npm")} {
			if err := os.WriteFile(f, []byte("fake"), 0o755); err != nil {
				t.Fatalf("write %s: %v", f, err)
			}
		}
		if err := os.WriteFile(filepath.Join(libNpmCLI, "npm-cli.js"), []byte("fake"), 0o644); err != nil {
			t.Fatalf("write npm-cli.js: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(npmCLIDir, "npm-cli.js"), []byte("fake"), 0o644); err != nil {
		t.Fatalf("write npm-cli.js: %v", err)
	}
	return nodeDir
}

// npmLayoutRunner fakes the npm CLI: it records the invocation like
// mockExecRunner and then materializes the standard npm global-prefix
// layout (Windows: <prefix>/dsh.cmd + dsh; Unix: <prefix>/bin/dsh) so the
// post-install integrity gate passes.
type npmLayoutRunner struct {
	mockExecRunner
}

func (m *npmLayoutRunner) Run(ctx context.Context, name string, args []string, env []string, dir string) error {
	m.mockExecRunner.Run(ctx, name, args, env, dir)
	// args: [npm-cli.js, install, --global, --prefix, <dir>, pkg@ver, ...]
	for i, a := range args {
		if a == "--prefix" && i+1 < len(args) {
			dir = args[i+1]
		}
	}
	binDir := dir
	if runtime.GOOS != "windows" {
		binDir = filepath.Join(dir, "bin")
		if err := os.MkdirAll(binDir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(binDir, "dsh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return err
	}
	for _, f := range []string{"dsh.cmd", "dsh"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("fake-shim"), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// TC-NPM1 xdev 管理的 Node 优先（current/node），npm-cli.js 直调 node。
func TestNPMResolveNodeToolchain_ManagedPreferred(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)
	nodeDir := fakeNodeLayout(t, cfg.Core.XMDevDir)

	tc, err := ins.resolveNodeToolchain()
	if err != nil {
		t.Fatalf("resolveNodeToolchain: %v", err)
	}
	if !tc.managed {
		t.Error("expected managed node (current/node)")
	}
	if !strings.HasPrefix(tc.nodeBin, nodeDir) {
		t.Errorf("nodeBin = %q, want under %s", tc.nodeBin, nodeDir)
	}
	if tc.npmCLI == "" {
		t.Error("expected npm-cli.js to be located next to the managed node")
	}
}

// TC-NPM2 无任何 Node（清空 PATH 且无 current/node）→ 可执行错误信息。
func TestNPMResolveNodeToolchain_NoNodeActionableError(t *testing.T) {
	ins, _, _ := newTestInstaller(t)
	t.Setenv("PATH", "")

	_, err := ins.resolveNodeToolchain()
	if err == nil {
		t.Fatal("expected error when no node/npm exists")
	}
	if !strings.Contains(err.Error(), "xdev install node") {
		t.Errorf("error should point at `xdev install node`, got: %v", err)
	}
}

// TC-NPM3 npm-kind 安装跳过归档下载并直调 npm（node <npm-cli.js> install
// --global --prefix ... <pkg>@<ver>）。归档 URL 是无效域——若未跳过下载，
// 安装必然失败；成功即证明跳过。
func TestNPMInstall_SkipsDownloadAndRunsNPM(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)
	fakeNodeLayout(t, cfg.Core.XMDevDir)
	mr := &npmLayoutRunner{}
	ins.SetExecRunner(mr)

	p := newNPMTestPlugin("dsh", "0.1.2-rc.1", false)
	res, err := ins.InstallWithProgress(context.Background(), p, "0.1.2-rc.1", nil, InstallOptions{NoActivate: true})
	if err != nil {
		t.Fatalf("InstallWithProgress (npm kind): %v", err)
	}
	if !mr.called {
		t.Fatal("execRunner (npm) was not invoked")
	}
	// node <npm-cli.js> install --global --prefix <installPath> pkg@ver
	wantSpec := "@deepseek-ai/dsh@0.1.2-rc.1"
	joined := strings.Join(mr.args, " ")
	for _, want := range []string{"install", "--global", "--prefix", wantSpec, "--no-fund", "--no-audit"} {
		if !strings.Contains(joined, want) {
			t.Errorf("npm args missing %q: %v", want, mr.args)
		}
	}
	if !strings.Contains(joined, "npm-cli.js") {
		t.Errorf("expected npm-cli.js invocation via node, got %q %v", mr.name, mr.args)
	}
	// --prefix points at the install dir and the layout integrity passed.
	if !strings.HasPrefix(mr.dir, filepath.Join(cfg.Core.InstallDir, "dsh")) {
		t.Errorf("runner dir = %q, want under install/dsh", mr.dir)
	}
	if res.InstallPath == "" || !strings.HasPrefix(res.InstallPath, cfg.Core.InstallDir) {
		t.Errorf("InstallPath = %q, want under %s", res.InstallPath, cfg.Core.InstallDir)
	}
	// No archive was downloaded for the bogus URL: cache holds no dsh tarball.
	if _, err := os.Stat(filepath.Join(cfg.Core.CacheDir, "dsh-0.1.2-rc.1.tar.gz")); err == nil {
		t.Error("npm-kind install must not download the archive")
	}
}

// TC-NPM4 安装前 npm 依赖预检：node 版本低于 depends 声明 → 硬失败，
// 错误信息含要求的版本与 xdev 指引；npm 不被调用。
func TestNPMInstall_NodeVersionTooLow(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)
	fakeNodeLayout(t, cfg.Core.XMDevDir)
	mr := &npmLayoutRunner{}
	ins.SetExecRunner(mr)

	origProbe := npmNodeVersionProbe
	npmNodeVersionProbe = func(ctx context.Context, nodeBin string) (string, error) {
		return "v18.19.0", nil
	}
	defer func() { npmNodeVersionProbe = origProbe }()

	p := newNPMTestPlugin("dsh", "0.1.2-rc.1", true)
	_, err := ins.InstallWithProgress(context.Background(), p, "0.1.2-rc.1", nil, InstallOptions{})
	if err == nil {
		t.Fatal("expected version-too-low failure")
	}
	if !strings.Contains(err.Error(), "22.0.0") || !strings.Contains(err.Error(), "18.19.0") {
		t.Errorf("error should state required and found versions, got: %v", err)
	}
	if !strings.Contains(err.Error(), "xdev install node") {
		t.Errorf("error should point at `xdev install node`, got: %v", err)
	}
	if mr.called {
		t.Error("npm must not run when the node version pre-check fails")
	}
}

// TC-NPM5 npm 工具链缺失 → 安装前硬失败（runner 未被调用）。
func TestNPMInstall_MissingNodeFailsBeforeRunner(t *testing.T) {
	ins, _, _ := newTestInstaller(t)
	t.Setenv("PATH", "")
	mr := &npmLayoutRunner{}
	ins.SetExecRunner(mr)

	p := newNPMTestPlugin("dsh", "0.1.2-rc.1", false)
	_, err := ins.InstallWithProgress(context.Background(), p, "0.1.2-rc.1", nil, InstallOptions{})
	if err == nil {
		t.Fatal("expected missing-node failure")
	}
	if !strings.Contains(err.Error(), "Node.js") {
		t.Errorf("error should mention Node.js, got: %v", err)
	}
	if mr.called {
		t.Error("npm must not run when node is missing")
	}
}

// TC-NPM6 包名注入防御：Remote.Package 携带非法字符 → 拒绝安装。
func TestNPMInstall_InvalidPackageSpec(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)
	fakeNodeLayout(t, cfg.Core.XMDevDir)
	mr := &npmLayoutRunner{}
	ins.SetExecRunner(mr)

	p := newNPMTestPlugin("dsh", "0.1.2-rc.1", false)
	p.m.Remote.Package = "evil pkg; calc"
	if _, err := ins.InstallWithProgress(context.Background(), p, "0.1.2-rc.1", nil, InstallOptions{}); err == nil {
		t.Fatal("expected invalid-package-name failure")
	} else if !strings.Contains(err.Error(), "invalid npm package name") {
		t.Errorf("error should mention invalid package name, got: %v", err)
	}
	if mr.called {
		t.Error("npm must not run for an invalid package spec")
	}
}

// TC-NPM7 InstallNPMPackage（GUI 自包含入口）：npm 安装 + 完整性门 + 激活
// （current junction + shims/dsh）。
func TestInstallNPMPackage_ActivatesAndShims(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)
	fakeNodeLayout(t, cfg.Core.XMDevDir)
	mr := &npmLayoutRunner{}
	ins.SetExecRunner(mr)

	p := newNPMTestPlugin("dsh", "0.1.2-rc.1", false)
	installPath, dirErr := p.InstallDir(cfg.Core.InstallDir, "0.1.2-rc.1")
	if dirErr != nil {
		t.Fatal(dirErr)
	}
	res, err := ins.InstallNPMPackage(context.Background(), p, p.archiv, "0.1.2-rc.1", installPath)
	if err != nil {
		t.Fatalf("InstallNPMPackage: %v", err)
	}
	if res == nil || !res.Activated {
		t.Fatalf("expected Activated=true, got %+v (activationError=%q)", res, res.ActivationError)
	}
	// current/dsh junction exists and points at the install dir.
	link := filepath.Join(cfg.Core.XMDevDir, "current", "dsh")
	if fi, err := os.Stat(link); err != nil || !fi.IsDir() {
		t.Errorf("current/dsh missing after activation: %v", err)
	}
	// shim created for the declared name.
	shimName := "dsh.exe"
	if runtime.GOOS != "windows" {
		shimName = "dsh"
	}
	if _, err := os.Stat(filepath.Join(cfg.Core.ShimDir, shimName)); err != nil {
		t.Errorf("shim %s missing after activation: %v", shimName, err)
	}
}

// TC-NPM8 npmRunner 分派：npm-cli.js 优先；缺 CLI 时 Windows .cmd 走
// cmd /c 包装（但含 cmd.exe 元字符的路径必须拒绝），Unix 直接执行。
func TestNPMRunnerDispatch(t *testing.T) {
	tc := nodeToolchain{nodeBin: "/x/node", npmCLI: "/x/node_modules/npm/bin/npm-cli.js"}
	name, prefix, err := tc.npmRunner()
	if err != nil || name != "/x/node" || len(prefix) != 1 || prefix[0] != tc.npmCLI {
		t.Errorf("npmCLI dispatch: name=%q prefix=%v err=%v", name, prefix, err)
	}

	tc2 := nodeToolchain{nodeBin: "/x/node", npmBin: "/x/npm.cmd"}
	name2, prefix2, err2 := tc2.npmRunner()
	if err2 != nil {
		t.Fatalf("npmBin dispatch: %v", err2)
	}
	if runtime.GOOS == "windows" {
		if name2 != "cmd" || len(prefix2) != 2 || prefix2[0] != "/c" || prefix2[1] != "/x/npm.cmd" {
			t.Errorf("windows .cmd wrap: name=%q prefix=%v", name2, prefix2)
		}
	} else {
		// Unix npm launchers are real executables — run directly.
		if name2 != "/x/npm.cmd" || len(prefix2) != 0 {
			t.Errorf("unix direct: name=%q prefix=%v", name2, prefix2)
		}
	}

	tc3 := nodeToolchain{nodeBin: "/x/node"}
	if _, _, err3 := tc3.npmRunner(); err3 == nil {
		t.Error("expected error when no npm is present")
	}
}

// 审查 S-03 回归：`.cmd` 路径含 cmd.exe 元字符时必须拒绝，不得交给 cmd /c。
//
// 实测背景：`exec.Command("cmd", "/c", p)` 中 p 含 `&` 时，`&` 后的内容会被
// 当作独立命令执行（已在 Windows 上用 Go 程序复现，marker 文件被改写）。
func TestNPMRunnerRejectsShellMetaInCmdPath(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("cmd /c 包装仅存在于 Windows")
	}

	unsafePaths := []string{
		`C:\dir&calc\npm.cmd`,     // 命令分隔
		`C:\dir|whoami\npm.cmd`,   // 管道
		`C:\dir^esc\npm.cmd`,      // 转义符
		`C:\100%portable\npm.cmd`, // 变量展开
		`C:\dir"quoted\npm.cmd`,   // 引号破坏
		"C:\\dir\r\nnpm.cmd",      // 换行注入
		`C:\a<b>c\npm.cmd`,        // 重定向
	}
	for _, p := range unsafePaths {
		tc := nodeToolchain{nodeBin: `C:\node\node.exe`, npmBin: p}
		name, prefix, err := tc.npmRunner()
		if err == nil {
			t.Errorf("npmRunner(%q) 未拒绝：name=%q prefix=%v", p, name, prefix)
		}
	}

	// 正常路径仍应放行
	tc := nodeToolchain{nodeBin: `C:\node\node.exe`, npmBin: `C:\Program Files\nodejs\npm.cmd`}
	name, prefix, err := tc.npmRunner()
	if err != nil {
		t.Fatalf("正常路径被误拒: %v", err)
	}
	if name != "cmd" || len(prefix) != 2 {
		t.Errorf("正常路径分派异常: name=%q prefix=%v", name, prefix)
	}

	// 有 npm-cli.js 时永远走 node，不受元字符影响
	tc2 := nodeToolchain{nodeBin: `C:\node\node.exe`, npmCLI: `C:\dir&calc\npm-cli.js`}
	name2, _, err2 := tc2.npmRunner()
	if err2 != nil || name2 != `C:\node\node.exe` {
		t.Errorf("npmCLI 优先路径不应受影响: name=%q err=%v", name2, err2)
	}
}

// cmdShimUnsafeChar 单元测试。
func TestCmdShimUnsafeChar(t *testing.T) {
	for _, bad := range []string{"a&b", "a|b", "a<b", "a>b", "a^b", "a%b", "a\"b", "a\nb", "a\rb"} {
		if _, ok := cmdShimUnsafeChar(bad); !ok {
			t.Errorf("cmdShimUnsafeChar(%q) = false, want true", bad)
		}
	}
	for _, ok := range []string{`C:\Program Files\nodejs\npm.cmd`, `/usr/bin/npm`, `C:\node\npm.cmd`} {
		if ch, bad := cmdShimUnsafeChar(ok); bad {
			t.Errorf("cmdShimUnsafeChar(%q) = true (%q), want false", ok, ch)
		}
	}
}

// TC-NPM9 包名/版本字符集校验。
func TestNPMNameAndVersionValidation(t *testing.T) {
	for _, ok := range []string{"dsh", "@deepseek-ai/dsh", "typescript", "@a/b"} {
		if !validNpmPackageName(ok) {
			t.Errorf("validNpmPackageName(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"", "evil pkg", "a;b", "a|b", "..", "a/../b", "@", "/a", "a/", "a@", "a@b/c", "a b c"} {
		if validNpmPackageName(bad) {
			t.Errorf("validNpmPackageName(%q) = true, want false", bad)
		}
	}
	for _, ok := range []string{"0.1.2-rc.1", "1.0.0", "2.3.4-alpha.10"} {
		if !npmVersionCharset.MatchString(ok) {
			t.Errorf("npmVersionCharset rejected %q", ok)
		}
	}
	for _, bad := range []string{"1.0.0 && calc", "x;calc", "1 2"} {
		if npmVersionCharset.MatchString(bad) {
			t.Errorf("npmVersionCharset accepted %q", bad)
		}
	}
}
