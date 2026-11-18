package installer

import (
	"archive/zip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/xmdemt/xdev/internal/downloader"
	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/util"
)

type formInstallPlugin struct {
	*fakePlugin
	manifest *manifest.Tool
}

func (p *formInstallPlugin) Manifest() *manifest.Tool { return p.manifest }

func TestInstallFormControlsDownloadedArchiveAndInstalledBytes(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)
	ins.SetNoEnvWrite(true)
	base := &fakePlugin{name: "formfixture", shims: []string{"formfixture"}, archive: &manifest.Archive{URL: "https://example.test/default.zip", Format: "zip"}}
	selected := manifest.Archive{OS: runtime.GOOS, Arch: manifest.CanonicalArch(runtime.GOOS, runtime.GOARCH), URL: "https://example.test/gui.zip", Format: "zip", Form: "gui", Strip: 1}
	p := &formInstallPlugin{fakePlugin: base, manifest: &manifest.Tool{Name: base.name, VersionList: []manifest.Version{{Version: "1.0.0", Archives: []manifest.Archive{*base.archive, selected}}}}}
	calls := 0
	res, err := ins.InstallWithProgress(context.Background(), p, "1.0.0", nil, InstallOptions{NoActivate: true, Form: "gui", DownloadFunc: func(ctx context.Context, opts downloader.Options) error {
		calls++
		if opts.URL != selected.URL {
			t.Fatalf("downloaded %s, want %s", opts.URL, selected.URL)
		}
		makeFakeZip(t, opts.Dest, base.name)
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || res.URL != selected.URL {
		t.Fatalf("selected metadata lost: %+v calls=%d", res, calls)
	}
	path, err := p.InstallDir(cfg.Core.InstallDir, "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyInstallIntegrity(p, path).OK {
		t.Fatal("selected archive not extracted")
	}
	_, err = ins.InstallWithProgress(context.Background(), p, "1.0.0", nil, InstallOptions{Form: "missing"})
	if err == nil {
		t.Fatal("missing form silently fell back")
	}
}

func TestInstallFormIdentity(t *testing.T) {
	for name, hash := range map[string]string{"empty": "", "placeholder": "0000000000000000000000000000000000000000000000000000000000000000"} {
		t.Run(name, func(t *testing.T) {
			ins, cfg, _ := newTestInstaller(t)
			ins.SetNoEnvWrite(true)
			cli := manifest.Archive{OS: runtime.GOOS, Arch: manifest.CanonicalArch(runtime.GOOS, runtime.GOARCH), URL: "https://example.test/cli.zip", Format: "zip", Form: "cli", SHA256: hash}
			gui := cli
			gui.Form = "gui"
			gui.URL = "https://example.test/gui.zip"
			base := &fakePlugin{name: "shape", shims: []string{"shape"}, archive: &cli}
			p := &formInstallPlugin{fakePlugin: base, manifest: &manifest.Tool{Name: base.name, VersionList: []manifest.Version{{Version: "1.0.0", Archives: []manifest.Archive{cli, gui}}}}}
			var destinations []string
			download := func(_ context.Context, opts downloader.Options) error {
				destinations = append(destinations, opts.Dest)
				if err := os.MkdirAll(filepath.Dir(opts.Dest), 0755); err != nil {
					return err
				}
				f, err := os.Create(opts.Dest)
				if err != nil {
					return err
				}
				z := zip.NewWriter(f)
				binary := "shape"
				if runtime.GOOS == "windows" {
					binary += ".exe"
				}
				header := &zip.FileHeader{Name: binary, Method: zip.Deflate}
				header.SetMode(0755)
				w, err := z.CreateHeader(header)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := w.Write([]byte(opts.URL)); err != nil {
					t.Fatal(err)
				}
				if err := z.Close(); err != nil {
					t.Fatal(err)
				}
				if err := f.Close(); err != nil {
					return err
				}
				pin, err := util.FileSHA256(opts.Dest)
				if err != nil {
					return err
				}
				return os.WriteFile(opts.Dest+".sha256", []byte(pin), 0644)
			}
			install := func(form string, force bool) error {
				_, err := ins.InstallWithProgress(context.Background(), p, "1.0.0", nil, InstallOptions{NoActivate: true, Form: form, Force: force, DownloadFunc: download})
				return err
			}
			if err := install("cli", false); err != nil {
				t.Fatal(err)
			}
			path, _ := p.InstallDir(cfg.Core.InstallDir, "1.0.0")
			binary := "shape"
			if runtime.GOOS == "windows" {
				binary += ".exe"
			}
			assertBytes := func(want string) {
				t.Helper()
				b, err := os.ReadFile(filepath.Join(path, binary))
				if err != nil || string(b) != want {
					t.Fatalf("installed bytes %q, want %q: %v", b, want, err)
				}
			}
			if err := install("gui", false); !errors.Is(err, ErrIncompleteInstall) {
				t.Fatalf("silent form switch: %v", err)
			}
			assertBytes(cli.URL)
			if err := install("gui", true); err != nil {
				t.Fatal(err)
			}
			assertBytes(gui.URL)
			if len(destinations) != 2 || destinations[0] == destinations[1] {
				t.Fatalf("form cache collision: %v", destinations)
			}
			if err := install("gui", false); err != nil {
				t.Fatal(err)
			}
			if err := install("cli", true); err != nil {
				t.Fatal(err)
			}
			assertBytes(cli.URL)
			if len(destinations) != 2 {
				t.Fatal("warm form cache was not reused")
			}
			p.manifest.VersionList[0].Archives[0].URL = "https://example.test/cli-repacked.zip"
			if err := install("cli", false); !errors.Is(err, ErrIncompleteInstall) {
				t.Fatalf("changed selected URL accepted: %v", err)
			}
			if err := install("cli", true); err != nil {
				t.Fatal(err)
			}
			assertBytes("https://example.test/cli-repacked.zip")
			if len(destinations) != 3 || destinations[2] == destinations[0] {
				t.Fatal("changed URL reused pinned old payload")
			}
			// Old installations without selection metadata cannot satisfy an
			// explicit form claim, but legacy no-form archives remain supported.
			if err := os.Remove(filepath.Join(path, installSelectionFile)); err != nil {
				t.Fatal(err)
			}
			if err := install("cli", false); !errors.Is(err, ErrIncompleteInstall) {
				t.Fatalf("unknown installed form accepted: %v", err)
			}
			legacy := cli
			legacy.Form = ""
			if err := verifyInstalledSelection(path, &legacy, ""); err != nil {
				t.Fatalf("legacy form-agnostic broken: %v", err)
			}
		})
	}
}

func TestInstallBeforeCommitPreservesExistingTreeAfterDownload(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)
	p := &fakePlugin{name: "cancelcommit", shims: []string{"cancelcommit"}, archive: &manifest.Archive{URL: "https://example.test/commit.zip", Format: "zip", Strip: 1}}
	path, _ := p.InstallDir(cfg.Core.InstallDir, "1.0.0")
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(path, "old-tree")
	if err := os.WriteFile(marker, []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	downloaded, commits := false, 0
	_, err := ins.InstallWithProgress(context.Background(), p, "1.0.0", nil, InstallOptions{Force: true, NoActivate: true, DownloadFunc: func(ctx context.Context, opts downloader.Options) error {
		downloaded = true
		makeFakeZip(t, opts.Dest, p.name)
		return nil
	}, BeforeCommit: func() error {
		commits++
		if !downloaded {
			t.Fatal("commit before transfer")
		}
		return context.Canceled
	}})
	if !errors.Is(err, context.Canceled) || commits != 1 {
		t.Fatalf("commit guard outcome %v calls=%d", err, commits)
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "keep" {
		t.Fatal("old tree removed despite cancellation", err)
	}
}

func TestInstallBeforeCommitGuardsAlreadyInstalledActivation(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)
	p := &fakePlugin{name: "already", shims: []string{"already"}, archive: &manifest.Archive{URL: "https://example.test/already.zip", Format: "zip"}}
	path, _ := p.InstallDir(cfg.Core.InstallDir, "1.0.0")
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
	binary := "already"
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	if err := os.WriteFile(filepath.Join(path, binary), []byte("existing"), 0755); err != nil {
		t.Fatal(err)
	}
	calls := 0
	_, err := ins.InstallWithProgress(context.Background(), p, "1.0.0", nil, InstallOptions{BeforeCommit: func() error { calls++; return context.Canceled }})
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("activation guard %v calls=%d", err, calls)
	}
	if _, err := os.Lstat(filepath.Join(cfg.Core.XMDevDir, "current", p.name)); !os.IsNotExist(err) {
		t.Fatal("activation occurred", err)
	}
}
