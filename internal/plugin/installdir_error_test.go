package plugin

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/xmdemt/xdev/internal/manifest"
)

// 20261021 审计 P1-5（阶段 2）：InstallDir 以前对非法 tool/version 是
// fail-open（替换成 unsafe-version-blocked 继续）。现在必须返回
// ErrUnsafeInstallPath，调用方才能在任何文件写入之前中止。
func TestGenericInstallDir_RejectsUnsafeComponents(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*manifest.Tool)
		version string
	}{
		{
			name:    "tool name traversal",
			mutate:  func(m *manifest.Tool) { m.Name = "../evil" },
			version: "1.0.0",
		},
		{
			name:    "tool name separator",
			mutate:  func(m *manifest.Tool) { m.Name = `a\b` },
			version: "1.0.0",
		},
		{
			name:    "version traversal",
			mutate:  func(*manifest.Tool) {},
			version: "../evil",
		},
		{
			name:    "version empty",
			mutate:  func(*manifest.Tool) {},
			version: "",
		},
		{
			name:    "install_root escape",
			mutate:  func(m *manifest.Tool) { m.InstallRoot = "../../evil" },
			version: "1.0.0",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := sampleTool()
			tc.mutate(m)
			p := NewGeneric(m)

			dir, err := p.InstallDir(t.TempDir(), tc.version)
			if err == nil {
				t.Fatalf("InstallDir = %q, want an error (fail-open regression)", dir)
			}
			if !errors.Is(err, ErrUnsafeInstallPath) {
				t.Errorf("error = %v, want errors.Is(ErrUnsafeInstallPath)", err)
			}
		})
	}
}

func TestGenericInstallDir_AcceptsSafeComponents(t *testing.T) {
	root := t.TempDir()
	m := sampleTool()
	p := NewGeneric(m)

	dir, err := p.InstallDir(root, "go1.22.10")
	if err != nil {
		t.Fatalf("InstallDir: %v", err)
	}
	want := filepath.Join(root, m.Name, "go1.22.10")
	if dir != want {
		t.Errorf("InstallDir = %q, want %q", dir, want)
	}
}
