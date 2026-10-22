package config

import (
	"os"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// 票 40（P2-33 / N-18）：UpdateConfigPaths 的失败日志文案必须指向真正的文件
// ---------------------------------------------------------------------------
//
// 该日志原先复制粘贴成了 last_selection.json，而 UpdateConfigPaths 写的是
// <dir>/config.yaml —— 出错时用户会去排查一个从未被写过的文件。
//
// 说明：这条路径只在 AtomicWriteFile 失败时触发，无法在测试里稳定构造
// （Windows 的 rename 语义 + Go 的共享删除句柄让「只读/被占用」都写不
// 失败），因此按本仓库既有的「源码守卫测试」惯例（见 internal/cli 的
// selfcheck/startup 入口守卫）直接对源码断言文案。

// TestUpdateConfigPaths_LogNamesConfigYaml 失败日志必须写 config.yaml，
// 且不得再出现 last_selection.json（该文案与 config.yaml 毫无关系）。
func TestUpdateConfigPaths_LogNamesConfigYaml(t *testing.T) {
	src, err := os.ReadFile("config.go")
	if err != nil {
		t.Fatalf("读取 config.go 源码失败: %v", err)
	}
	text := string(src)

	if !strings.Contains(text, `failed to save config.yaml`) {
		t.Error("UpdateConfigPaths 的失败日志没有指向 config.yaml（票 40 / P2-33）")
	}
	if strings.Contains(text, `failed to save last_selection.json`) {
		t.Error("仍存在把 config.yaml 写失败报成 last_selection.json 的错误文案")
	}

	// 交叉核验：文案描述的文件名必须与函数实际写入的路径一致。
	idx := strings.Index(text, "func UpdateConfigPaths(")
	if idx < 0 {
		t.Fatal("未找到 UpdateConfigPaths（重命名/移动后请同步本守卫）")
	}
	body := text[idx:]
	if end := strings.Index(body, "\n}\n"); end > 0 {
		body = body[:end]
	}
	if !strings.Contains(body, `filepath.Join(dir, "config.yaml")`) {
		t.Error("UpdateConfigPaths 实际写入的路径不再是 <dir>/config.yaml，日志文案守卫需同步")
	}
}
