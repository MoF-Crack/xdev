package gui

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestInstallProgressSerializesTaskID(t *testing.T) {
	b, err := json.Marshal(InstallProgress{TaskID: "node@22", Tool: "node", Version: "22"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"taskId":"node@22"`) {
		t.Fatalf("missing taskId: %s", b)
	}
}

// TestInstallEventPayloadsInSourceContainTaskID 守卫「每个安装事件都带 taskID」。
//
// 2026-10-25（架构收口）：事件上报已从 app_install.go 集中到
// app_install_delegate.go 的两个辅助函数，因此判据相应调整：
//   - 结构体只在辅助函数里构造一次（必须带 TaskID）
//   - 其余源码不得绕过辅助函数直接 emit（那会丢 taskID 与 a.ctx nil 保护）
//
// 守卫意图不变，但不再因为重构而误报。
func TestInstallEventPayloadsInSourceContainTaskID(t *testing.T) {
	files := []string{"app_install.go", "app_install_delegate.go"}
	var combined strings.Builder
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		combined.Write(b)
	}
	s := combined.String()

	// 1. 两个辅助函数必须把 taskID 写进结构体。
	if !strings.Contains(s, "TaskID:  taskID") {
		t.Error("InstallProgress 构造必须携带 TaskID: taskID")
	}
	if !strings.Contains(s, `"taskId":  taskID`) {
		t.Error("installError 载荷必须携带 \"taskId\": taskID")
	}

	// 2. 除两个辅助函数体外，其余源码不得直接 emit 这两个事件。
	outside := stripHelperBodies(s)
	for _, forbidden := range []string{
		`EventsEmit(a.ctx, "installProgress"`,
		`EventsEmit(a.ctx, "installError"`,
	} {
		if strings.Contains(outside, forbidden) {
			t.Errorf("必须经 emitInstallProgress/emitInstallError 上报，发现直接调用: %s", forbidden)
		}
	}
}

// stripHelperBodies 移除两个事件辅助函数的函数体，只留其余源码。
//
// 实现：找到函数起始位置，向后找下一个顶层 `func ` 声明（或文件尾），
// 把 [start, end) 这段整段删掉。两个函数都是简单的一小段，不需要
// 真正的 Go 语法解析。
func stripHelperBodies(src string) string {
	for _, sig := range []string{
		"func (a *App) emitInstallProgress(",
		"func (a *App) emitInstallError(",
	} {
		idx := strings.Index(src, sig)
		if idx < 0 {
			continue
		}
		rest := src[idx+len(sig):]
		end := strings.Index(rest, "\nfunc ")
		if end < 0 {
			end = len(rest)
		}
		src = src[:idx] + src[idx+len(sig)+end:]
	}
	return src
}
