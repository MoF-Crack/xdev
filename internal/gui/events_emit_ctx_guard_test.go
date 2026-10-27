package gui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 回归守卫（2026-10-26）：所有 wailsRuntime.EventsEmit 调用必须在
// `if a.ctx == nil { return }`（或等价）保护之内。
//
// # 为什么这是硬约束
//
// Wails 的 EventsEmit 对 nil context 会 **log.Fatalf 直接杀进程**。
// 项目里已有注释记录过这个坑（app_profile.go）：测试环境没有 Wails
// runtime，跑到 emit 分支时进程被静默杀死，连 FAIL 行都不打印 ——
// 表现为「测试莫名 exit 1」。
//
// # 实测发现的缺口
//
// 2026-10-26 排查时发现 4 处缺保护，全在安装路径上：
//   - app_install_npm.go 的 installProgress / installError /
//     xdev:activation-warning（3 处）
//   - app_install.go 的 TOFU 回调（1 处）
//
// 已全部改为走带保护的辅助函数（emitInstallProgress / emitInstallError /
// emitActivationWarning）。本守卫防止以后有人新写一处裸 EventsEmit。
func TestAllEventsEmitAreCtxGuarded(t *testing.T) {
	files, err := filepath.Glob("app*.go")
	if err != nil {
		t.Fatal(err)
	}

	var checked int
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		lines := strings.Split(string(data), "\n")

		for i, line := range lines {
			if !strings.Contains(line, "wailsRuntime.EventsEmit(") {
				continue
			}
			// 跳过注释行。
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			checked++

			// 向上找最近的保护判断（最多 8 行）；遇到函数边界则停止。
			//
			// 两种等价写法都接受：
			//   if a.ctx == nil { return }        （否定式，提前返回）
			//   if a.ctx != nil { EventsEmit(...) } （肯定式，包住调用）
			guarded := false
			for back := 1; back <= 8 && i-back >= 0; back++ {
				prev := lines[i-back]
				if strings.Contains(prev, "a.ctx == nil") || strings.Contains(prev, "a.ctx != nil") {
					guarded = true
					break
				}
				if strings.HasPrefix(prev, "func ") {
					break
				}
			}
			if !guarded {
				t.Errorf("%s:%d 的 wailsRuntime.EventsEmit 缺少 a.ctx nil 保护 —— "+
					"Wails 对 nil context 会 log.Fatalf 杀进程（测试里表现为静默 exit 1）\n  %s",
					f, i+1, strings.TrimSpace(line))
			}
		}
	}

	if checked == 0 {
		t.Skip("没有找到 EventsEmit 调用（文件布局可能变了）")
	}
	t.Logf("检查了 %d 处 EventsEmit 调用", checked)
}
