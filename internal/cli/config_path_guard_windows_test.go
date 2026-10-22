//go:build windows

// Windows-only config-home guard tests.
//
// B0（20261021 审计）：这两个用例依赖 driveFixture / Windows 盘符语义，
// 原先放在无 build tag 的 config_path_guard_test.go 里，导致
// `GOOS=linux go vet ./...` 编译失败（undefined: driveFixture）。
// Windows 专属断言必须与平台绑定，否则 Linux/macOS CI 连测试都编不过。

package cli

import (
	"bytes"
	"runtime"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/config"
)

// TC-33-1a `config set core.xmdevenv_dir` 拒绝相对路径 / UNC，接受本地绝对路径。
func TestConfigSet_XMDevDir_RejectsRelativeAndUNC(t *testing.T) {
	rejected := []string{
		"relative/xdev",
		`relative\xdev`,
		"./xdev",
		"../xdev",
		"xdev",
		`C:xdev`,         // Windows 驱动器相对形式
		`\xdev`,          // 根相对（无驱动器）——Windows 下非绝对
		`\\server\share`, // UNC
		`\\server\share\xdev`,
		"//server/share", // UNC（POSIX 形态）
		"",
	}
	for _, v := range rejected {
		cfg := &config.Config{}
		if err := configSet(cfg, "core.xmdevenv_dir", v); err == nil {
			t.Errorf("configSet(core.xmdevenv_dir, %q) = nil, want rejection", v)
		}
		if cfg.Core.XMDevDir != "" {
			t.Errorf("configSet(core.xmdevenv_dir, %q) 写入了被拒值 %q", v, cfg.Core.XMDevDir)
		}
	}

	accepted := []string{
		`D:\XDev`,
		`D:/XDev`,
		`C:\Users\tester\.xdev`,
	}
	if runtime.GOOS != "windows" {
		// 保留原分支：该文件已限定 Windows，此分支仅作为未来复用到其它
		// 宿主时的安全网（不会在本平台执行）。
		accepted = append(accepted, "/opt/xdev")
	}
	for _, v := range accepted {
		cfg := &config.Config{}
		if err := configSet(cfg, "core.xmdevenv_dir", v); err != nil {
			t.Errorf("configSet(core.xmdevenv_dir, %q) = %v, want nil（绝对路径零影响）", v, err)
			continue
		}
		if cfg.Core.XMDevDir != v {
			t.Errorf("XMDevDir = %q, want %q", cfg.Core.XMDevDir, v)
		}
	}
}

// TC-33-1b init 向导自定义路径分支（promptDriveWith "p"）同样拒绝相对/UNC，
// 并在被拒后重新提示，最终接受绝对路径。
func TestPromptDriveCustomPath_RejectsRelativeAndUNC(t *testing.T) {
	out := &bytes.Buffer{}
	in := strings.NewReader(
		"p\nrelative\\xdev\n" + // 相对 → 重新提示
			"p\n\\\\server\\share\\xdev\n" + // UNC → 重新提示
			"p\nD:\\XDev\n") // 绝对 → 接受
	choice, err := promptDriveWith(in, out, driveFixture())
	if err != nil {
		t.Fatalf("promptDriveWith: %v", err)
	}
	if choice.Quit {
		t.Fatal("unexpected Quit")
	}
	if choice.Path != `D:\XDev` {
		t.Errorf("Path = %q, want %q（绝对路径必须原样接受）", choice.Path, `D:\XDev`)
	}
	if !strings.Contains(out.String(), "must be absolute") {
		t.Errorf("缺少相对路径拒绝提示，output: %q", out.String())
	}
	if !strings.Contains(out.String(), "UNC path") {
		t.Errorf("缺少 UNC 拒绝提示，output: %q", out.String())
	}
}
