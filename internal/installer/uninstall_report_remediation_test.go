package installer

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/plugin"
)

type uninstallReportPlugin struct {
	*fakePlugin
	deactivate func(string) error
	envCalls   int
}

func (p *uninstallReportPlugin) Deactivate(path string) error {
	if p.deactivate != nil {
		return p.deactivate(path)
	}
	return nil
}
func (p *uninstallReportPlugin) EnvVars(string) map[string]string { p.envCalls++; return nil }

func TestReportD04NonActivePreservesEntries(t *testing.T) {
	for _, mode := range []string{"shim", "auto", "direct"} {
		t.Run(mode, func(t *testing.T) {
			ins, cfg, _ := newTestInstaller(t)
			ins.SetNoEnvWrite(true)
			cfg.Path.Mode = mode
			a := makeFakeInstalledVersion(t, cfg.Core.InstallDir, "fakego", "1.0.0")
			b := makeFakeInstalledVersion(t, cfg.Core.InstallDir, "fakego", "2.0.0")
			p := &uninstallReportPlugin{fakePlugin: &fakePlugin{name: "fakego", shims: []string{"fakego", "fakego1.2"}}}
			if err := ins.Activate(p, b, "2.0.0"); err != nil {
				t.Fatal(err)
			}
			residue := filepath.Join(cfg.Core.XMDevDir, "current", "unrelated.stale-12345")
			if err := os.Mkdir(residue, 0755); err != nil {
				t.Fatal(err)
			}
			oldTime := time.Now().Add(-2 * time.Hour)
			if err := os.Chtimes(residue, oldTime, oldTime); err != nil {
				t.Fatal(err)
			}
			p.envCalls = 0
			p.deactivate = func(string) error { t.Fatal("inactive uninstall called Deactivate"); return nil }
			paths := []string{filepath.Join(cfg.Core.XMDevDir, "current", "fakego"), residue}
			if mode != "direct" {
				for _, name := range p.Shims() {
					paths = append(paths, filepath.Join(cfg.Core.ShimDir, name))
					if runtime.GOOS == "windows" {
						paths = append(paths, filepath.Join(cfg.Core.ShimDir, name+".exe"))
					}
				}
			}
			before := map[string]os.FileInfo{}
			for _, path := range paths {
				fi, err := os.Lstat(path)
				if err != nil {
					t.Fatal(err)
				}
				before[path] = fi
			}
			for _, ver := range []string{"1.0.0", "9.0.0"} {
				if err := ins.Uninstall(p, ver); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := os.Stat(a); !os.IsNotExist(err) {
				t.Fatalf("removed tree: %v", err)
			}
			if got, ok := ins.CurrentVersion("fakego"); !ok || got != "2.0.0" {
				t.Fatalf("current=%s,%v", got, ok)
			}
			for path, old := range before {
				now, err := os.Lstat(path)
				if err != nil || !os.SameFile(old, now) {
					t.Errorf("active entry changed: %s (%v)", path, err)
				}
			}
			if p.envCalls != 0 {
				t.Fatalf("inactive uninstall touched env/Activate: calls=%d", p.envCalls)
			}
		})
	}
}

func TestReportD04DeactivateFailurePreservesCauseAndTree(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)
	ins.SetNoEnvWrite(true)
	path := makeFakeInstalledVersion(t, cfg.Core.InstallDir, "fakego", "1.0.0")
	cause := errors.New("deactivate sentinel")
	p := &uninstallReportPlugin{fakePlugin: &fakePlugin{name: "fakego", shims: []string{"fakego"}}, deactivate: func(string) error { return cause }}
	if err := ins.Activate(p, path, "1.0.0"); err != nil {
		t.Fatal(err)
	}
	err := ins.Uninstall(p, "1.0.0")
	if !errors.Is(err, cause) || !strings.Contains(err.Error(), "deactivate") {
		t.Fatalf("cause lost: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("deactivate failure deleted tree: %v", err)
	}
}

func TestReportD04ShimCleanupFailureVisible(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)
	ins.SetNoEnvWrite(true)
	path := makeFakeInstalledVersion(t, cfg.Core.InstallDir, "fakego", "1.0.0")
	p := &fakePlugin{name: "fakego", shims: []string{"fakego"}}
	if err := ins.Activate(p, path, "1.0.0"); err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(cfg.Core.ShimDir, "fakego")
	if err := os.Remove(blocked); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(blocked, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blocked, "keep"), []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	err := ins.Uninstall(p, "1.0.0")
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) || !strings.Contains(err.Error(), "shim") {
		t.Fatalf("cleanup cause hidden: %v", err)
	}
}

func TestReportD04ReactivationFailurePreservesCause(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)
	ins.SetNoEnvWrite(true)
	a := makeFakeInstalledVersion(t, cfg.Core.InstallDir, "fakego", "1.0.0")
	makeFakeInstalledVersion(t, cfg.Core.InstallDir, "fakego", "2.0.0")
	p := &fakePlugin{name: "fakego", shims: []string{"fakego"}}
	if err := ins.Activate(p, a, "1.0.0"); err != nil {
		t.Fatal(err)
	}
	cause := errors.New("re-activation link sentinel")
	previous := linkCurrent
	linkCurrent = func(string, string) error { return cause }
	t.Cleanup(func() { linkCurrent = previous })
	err := ins.Uninstall(p, "1.0.0")
	if !errors.Is(err, cause) || !strings.Contains(err.Error(), "re-activate") {
		t.Fatalf("reactivation failure hidden: %v", err)
	}
}

func TestReportD04CurrentCleanupFailureVisible(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)
	ins.SetNoEnvWrite(true)
	a := makeFakeInstalledVersion(t, cfg.Core.InstallDir, "fakego", "1.0.0")
	current := filepath.Join(cfg.Core.XMDevDir, "current", "fakego")
	p := &uninstallReportPlugin{fakePlugin: &fakePlugin{name: "fakego", shims: []string{"fakego"}}}
	if err := ins.Activate(p, a, "1.0.0"); err != nil {
		t.Fatal(err)
	}
	p.deactivate = func(string) error {
		if err := os.Remove(current); err != nil {
			return err
		}
		if err := os.Mkdir(current, 0755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(current, "keep"), []byte("keep"), 0644)
	}
	err := ins.Uninstall(p, "1.0.0")
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) || !strings.Contains(err.Error(), "current link") {
		t.Fatalf("current cleanup failure hidden: %v", err)
	}
}

func TestReportD18UninstallRepointsWithinInstallRoot(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)
	ins.SetNoEnvWrite(true)
	p := plugin.NewGeneric(&manifest.Tool{Name: "android-ndk", InstallRoot: "android-sdk", Latest: "r30", Shims: []string{"ndk-build"}})
	for _, v := range []string{"r30", "r29", "27.2.12479018"} {
		path, err := p.InstallDir(cfg.Core.InstallDir, v)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.MkdirAll(path, 0755); err != nil {
			t.Fatal(err)
		}
	}
	active, err := p.InstallDir(cfg.Core.InstallDir, "r30")
	if err != nil {
		t.Fatal(err)
	}
	if err = ins.Activate(p, active, "r30"); err != nil {
		t.Fatal(err)
	}
	if err = ins.Uninstall(p, "r30"); err != nil {
		t.Fatal(err)
	}
	if got, ok := ins.CurrentVersion(p.Name()); !ok || got != "r29" {
		t.Fatalf("NDK fallback=%q,%v", got, ok)
	}
}

func TestReportD18FallbackUsesInstallRootAndToolScheme(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)
	p := plugin.NewGeneric(&manifest.Tool{Name: "android-ndk", InstallRoot: "android-sdk", Latest: "r29"})
	for _, v := range []string{"r26", "27.2.12479018", "r29", "unknown7"} {
		path, err := p.InstallDir(cfg.Core.InstallDir, v)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.MkdirAll(path, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if got := ins.findOtherVersion(p); got != "r29" {
		t.Fatalf("fallback=%q want r29", got)
	}
	q := &fakePlugin{name: "unknown"}
	if err := os.MkdirAll(filepath.Join(cfg.Core.InstallDir, "unknown", "unknown7"), 0755); err != nil {
		t.Fatal(err)
	}
	if got := ins.findOtherVersion(q); got != "" {
		t.Fatalf("unknown became ordered version: %s", got)
	}
}
