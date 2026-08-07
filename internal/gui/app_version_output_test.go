// app_version_output_test.go tests extractVersionFromOutput — the
// multi-line version parser used by getVersion during migration/refresh
// (gradle "unsafe-version-blocked" fix).
package gui

import "testing"

func TestExtractVersionFromOutput(t *testing.T) {
	cases := []struct {
		name, output, want string
	}{
		{
			// gradle: 首行是分隔线，第二行才是版本
			"gradle separator first line",
			"------------------------------------------------------------\nGradle 9.4.1\nBuild time:   2025-05-30 12:00:00 UTC\n",
			"9.4.1",
		},
		{
			// go: 单行多段（extractVersion 保留 "go1.26.5" 原始 token）
			"go version single line",
			"go version go1.26.5 windows/amd64",
			"go1.26.5",
		},
		{
			// 单 token 版本
			"bare version token",
			"v3.12",
			"3.12",
		},
		{
			// java: 多行输出，版本在第 3 行
			"java multiline",
			"openjdk 25.0.1 2025-04-15\nOpenJDK Runtime Environment (build 25.0.1+12)\nOpenJDK 64-Bit Server VM (build 25.0.1+12, mixed mode, sharing)",
			"25.0.1",
		},
		{
			// 没有任何行可提取：回退首行原文
			"only separator fallback",
			"------------------------------------------------------------\n",
			"------------------------------------------------------------",
		},
		{
			"empty output",
			"",
			"",
		},
	}
	for _, c := range cases {
		if got := extractVersionFromOutput(c.output); got != c.want {
			t.Errorf("%s: extractVersionFromOutput(%q) = %q, want %q", c.name, c.output, got, c.want)
		}
	}

	// 超长首行回退时截断（前 60 字符 + "..."）——期望值程序化计算，避免手数出错。
	longLine := "this-is-a-very-long-tool-output-line-without-any-version-numbers-in-it-at-all-0123456789"
	wantTruncated := longLine[:60] + "..."
	if got := extractVersionFromOutput(longLine); got != wantTruncated {
		t.Errorf("long line truncation: got %q, want %q", got, wantTruncated)
	}
}
