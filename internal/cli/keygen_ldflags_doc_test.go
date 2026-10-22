package cli

// F-49（P2）：仓库内给出的公钥注入命令必须是**完整导入路径**，否则 Go 的
// -ldflags -X 会静默忽略（短路径 "internal/signer.rootPublicKeyB64" 不匹配
// 任何包级符号，构建照样成功但公钥为空 → 签名校验静默禁用）。
//
// 本文件只覆盖 internal/cli 与 cmd/xdev-keygen 的所有权范围；
// internal/signer/verify.go 的注释文字由另一处守卫覆盖（仅注释，无逻辑）。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const wantLDFlagPath = "github.com/xmdemt/xdev/internal/signer.rootPublicKeyB64"

// buildCommandLines 返回 src 中所有「构建命令」行的行号与内容。
func buildCommandLines(src string) []string {
	var out []string
	for _, line := range strings.Split(src, "\n") {
		if strings.Contains(line, "go build") {
			out = append(out, strings.TrimSpace(line))
		}
	}
	return out
}

// shortPathInBuildCommand 报告 src 中是否存在「实际构建命令用了短路径」的行。
//
// 只匹配同时含 `go build` 与短路径的行——注释里**说明**这个反例
// （`"-X internal/signer.rootPublicKeyB64=..." is silently ignored`）
// 是有意保留的文档，不构成缺陷。
func shortPathInBuildCommand(src string) (int, string) {
	for i, line := range strings.Split(src, "\n") {
		if strings.Contains(line, "go build") && strings.Contains(line, "-X internal/signer.rootPublicKeyB64") {
			return i + 1, strings.TrimSpace(line)
		}
	}
	return 0, ""
}

// staleBuildTargetInBuildCommand 报告构建命令行里是否还指向不存在的
// ./cmd/xmdevenvmt/（本仓库的可执行入口是仓库根的 `.`）。
//
// 注意：只扫**构建命令**行。verify.go 另有一处非构建命令的陈旧路径引用
// （"Called during init from cmd/xmdevenvmt/main.go."），它不在 F-49 的
// 修复范围内（该文件只允许改「注释里的构建命令文字」），已在报告中列出。
func staleBuildTargetInBuildCommand(src string) (int, string) {
	for i, line := range strings.Split(src, "\n") {
		if strings.Contains(line, "go build") && strings.Contains(line, "cmd/xmdevenvmt") {
			return i + 1, strings.TrimSpace(line)
		}
	}
	return 0, ""
}

// TestKeygenOutputUsesFullImportPath：用户可见的输出（stdout/stderr）不得给出
// 短路径写法，也不得指向不存在的 ./cmd/xmdevenvmt/。
func TestKeygenOutputUsesFullImportPath(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "cmd", "xdev-keygen", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)

	if !strings.Contains(src, wantLDFlagPath) {
		t.Errorf("cmd/xdev-keygen/main.go 必须给出完整导入路径 %s", wantLDFlagPath)
	}
	if line, text := shortPathInBuildCommand(src); line > 0 {
		t.Errorf("cmd/xdev-keygen/main.go:%d 的构建命令仍使用短路径（Go 静默忽略）: %s", line, text)
	}
	if line, text := staleBuildTargetInBuildCommand(src); line > 0 {
		t.Errorf("cmd/xdev-keygen/main.go:%d 的构建命令仍指向不存在的 ./cmd/xmdevenvmt/（应为 .）: %s", line, text)
	}
	if n := strings.Count(src, "cmd/xmdevenvmt"); n > 0 {
		t.Errorf("cmd/xdev-keygen/main.go 仍含 %d 处 cmd/xmdevenvmt 路径引用", n)
	}
	// 三处（文件头注释 + 两处用户可见输出）都必须带完整路径。
	if n := strings.Count(src, wantLDFlagPath); n < 3 {
		t.Errorf("cmd/xdev-keygen/main.go 中完整导入路径出现 %d 次，want >= 3（文件头注释 + 两处输出）", n)
	}
}

// TestSignerVerifyDocCommentUsesFullImportPath：verify.go 的**构建命令**注释。
func TestSignerVerifyDocCommentUsesFullImportPath(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "signer", "verify.go"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)

	cmds := buildCommandLines(src)
	if len(cmds) == 0 {
		t.Fatal("verify.go 里找不到任何 go build 注释——守卫失效")
	}
	if line, text := shortPathInBuildCommand(src); line > 0 {
		t.Errorf("internal/signer/verify.go:%d 的构建命令仍使用短路径写法（F-49）: %s", line, text)
	}
	if line, text := staleBuildTargetInBuildCommand(src); line > 0 {
		t.Errorf("internal/signer/verify.go:%d 的构建命令仍指向不存在的 ./cmd/xmdevenvmt/: %s", line, text)
	}
	if !strings.Contains(src, wantLDFlagPath) {
		t.Errorf("internal/signer/verify.go 的注释必须给出完整导入路径 %s", wantLDFlagPath)
	}
	// 每条构建命令都必须以完整路径注入、且输出目标为仓库根 `.`。
	for _, c := range cmds {
		if !strings.Contains(c, wantLDFlagPath) {
			t.Errorf("构建命令缺少完整导入路径: %s", c)
		}
	}
}
