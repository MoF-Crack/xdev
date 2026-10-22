package cli

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/update"
)

func selfCheckResult() *update.CheckResult {
	return &update.CheckResult{
		CurrentVersion: "0.3.3",
		LatestVersion:  "0.4.0",
		HasUpdate:      true,
	}
}

// TC-S4-1 TTY 选 U → 执行更新（apply 被调用），exit 0
func TestPromptSelfUpdate_Update(t *testing.T) {
	applied := 0
	err := promptSelfUpdate(selfCheckResult(), strings.NewReader("U\n"), t.TempDir(), func() error {
		applied++
		return nil
	})
	if err != nil {
		t.Fatalf("prompt = %v, want nil", err)
	}
	if applied != 1 {
		t.Errorf("apply called %d times, want 1", applied)
	}
}

// TC-S4-2 TTY 选 I → 写忽略记录、继续
func TestPromptSelfUpdate_Ignore(t *testing.T) {
	cache := t.TempDir()
	err := promptSelfUpdate(selfCheckResult(), strings.NewReader("ignore\n"), cache, func() error {
		t.Error("apply must not be called on ignore")
		return nil
	})
	if err != nil {
		t.Fatalf("prompt = %v, want nil", err)
	}
	if _, statErr := os.Stat(filepath.Join(cache, "self_update_ignore.json")); statErr != nil {
		t.Errorf("ignore file not written: %v", statErr)
	}
}

// TC-S4-3 TTY 选 Q → errQuitSelfUpdate（调用方 os.Exit）
func TestPromptSelfUpdate_Quit(t *testing.T) {
	err := promptSelfUpdate(selfCheckResult(), strings.NewReader("q\n"), t.TempDir(), func() error {
		t.Error("apply must not be called on quit")
		return nil
	})
	if !errors.Is(err, errQuitSelfUpdate) {
		t.Errorf("prompt = %v, want errQuitSelfUpdate", err)
	}
}

// TC-S4-3b 大小写不敏感 + 未知输入继续提示（再次输入有效）
func TestPromptSelfUpdate_CaseInsensitiveAndRetry(t *testing.T) {
	err := promptSelfUpdate(selfCheckResult(), strings.NewReader("x\nQ\n"), t.TempDir(), func() error {
		t.Error("apply must not be called")
		return nil
	})
	if !errors.Is(err, errQuitSelfUpdate) {
		t.Errorf("prompt = %v, want errQuitSelfUpdate after retry", err)
	}
}

// TC-S4-3c EOF 直接继续（不写忽略、不退出）
func TestPromptSelfUpdate_EOFContinues(t *testing.T) {
	err := promptSelfUpdate(selfCheckResult(), strings.NewReader(""), t.TempDir(), func() error {
		t.Error("apply must not be called on EOF")
		return nil
	})
	if err != nil {
		t.Errorf("prompt = %v, want nil on EOF", err)
	}
}

// TC-S4-4 非 TTY 降级：stdinIsTTY 对管道为 false（不阻塞、不退出）
func TestStdinIsTTY_PipeIsFalse(t *testing.T) {
	if stdinIsTTY() {
		// 在 CI/管道下跑测试时 stdin 应为管道；交互终端下此断言跳过
		t.Log("stdin is a TTY in this environment; skipping pipe assertion")
	}
}

// TC-S4-6 挂载点守卫：runSelfCheck 调用点仅 3 个交互入口（无参 TUI / tui / gui）
func TestSelfCheckMountPoints_OnlyInteractive(t *testing.T) {
	// 定义在 selfcheck.go，本守卫只扫命令挂载文件（root.go 无参 TUI、extra.go tui/gui）
	files := []string{"root.go", "extra.go"}
	callRe := regexp.MustCompile(`runSelfCheck\s*\(`)
	callCount := 0
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		callCount += len(callRe.FindAllString(string(data), -1))
	}
	if callCount != 3 {
		t.Errorf("runSelfCheck call sites = %d, want exactly 3 (root TUI / tui / gui); 一次性子命令与 shim 不得检查", callCount)
	}
}
