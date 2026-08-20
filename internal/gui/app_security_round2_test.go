package gui

import (
	"strings"
	"testing"
)

func TestValidateInitBaseDir(t *testing.T) {
	if err := validateInitBaseDir(""); err == nil {
		t.Fatal("empty base dir should be rejected")
	}
	if err := validateInitBaseDir("relative\\xdev"); err == nil {
		t.Fatal("relative base dir should be rejected")
	}
	if err := validateInitBaseDir(`\\server\share\xdev`); err == nil {
		t.Fatal("UNC base dir should be rejected")
	}
	if err := validateInitBaseDir("C:\\XDev"); err != nil {
		t.Fatalf("absolute local path should be accepted: %v", err)
	}
}

func TestInitConfig_RejectsRelativePath(t *testing.T) {
	app := newTestAppWithDirs(t, t.TempDir())
	if err := app.InitConfig("not-absolute"); err == nil {
		t.Fatal("InitConfig should reject a relative path")
	}
}

func TestActivateHook_RejectsUnknownShell(t *testing.T) {
	app := newTestAppWithDirs(t, t.TempDir())
	if err := app.ActivateHook("powershell; calc"); err == nil {
		t.Fatal("ActivateHook should reject unknown/injected shell")
	}
	if err := app.ActivateHook(""); err == nil {
		t.Fatal("ActivateHook should reject empty shell")
	}
}

func TestTestProxy_RejectsUnsafeProxyURL(t *testing.T) {
	app := newTestAppWithDirs(t, t.TempDir())
	result := app.TestProxy("file:///c:/windows/win.ini", "https://example.com")
	if result.Success {
		t.Fatal("TestProxy should not succeed with file:// proxy")
	}
	if !strings.Contains(result.Message, "无效") && !strings.Contains(strings.ToLower(result.Message), "scheme") &&
		!strings.Contains(result.Message, "代理") {
		t.Errorf("unexpected message: %s", result.Message)
	}
}

func TestRunXdevCommand_RejectsUnsafeArgs(t *testing.T) {
	app := newTestAppWithDirs(t, t.TempDir())
	if _, err := app.runXdevCommand("plugin add evil"); err == nil {
		t.Fatal("runXdevCommand should reject unknown subcommand")
	}
	if _, err := app.runXdevCommand("install foo; rm -rf /"); err == nil {
		t.Fatal("runXdevCommand should reject shell metacharacters")
	}
	if _, err := app.runXdevCommand(""); err == nil {
		t.Fatal("runXdevCommand should reject empty command")
	}
}
