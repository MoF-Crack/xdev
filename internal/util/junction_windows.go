//go:build windows

package util

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"
)

// createJunction 在 Windows 上创建目录 junction（重解析点）。
//
// 第九轮审计 P1-1（P1，安全）：**绝不能让 cmd.exe 二次解析未转义的路径**。
//
// 旧实现是 `exec.Command("cmd", "/c", "mklink", "/J", link, target)`。
// os/exec 在 Windows 上只对「含空格/制表符/引号」的参数加双引号，而 cmd.exe 会
// **再解析一遍**命令行：`&` 是合法的文件名字符（Windows 允许），于是含 `&` 的
// tool name / version / 安装根目录会把 mklink 的参数切成多条命令 → **任意命令
// 执行**。tool name 与 version 都是 manifest 字段（第三方 manifest、Lua 插件、
// 被嗅探器写入的远端 tag），且注入发生在**激活阶段、先于任何二进制执行**。
//
// 实测（PATH 上放一个写 marker 的 probe.exe）：
//
//	Activate(tool="inj&probe&", ver="1.0.0")   → err=nil, marker=true
//	Activate(tool="verinj", ver="1.0&probe&")  → err=nil, marker=true
//	cmd 同时报 `'.tmp-…' 不是内部或外部命令`（参数确实被切断）。
//
// 同一根因还有**功能性**后果：XMDEV_HOME 含 `&`（例如 `D:\Tools & SDKs`）时
// junction 一律创建失败，在没有 symlink 特权的主机上激活永久失败。
//
// 修复策略（两层，都不依赖 os/exec 的 argv 转义）：
//  1. 自己构造完整命令行并通过 SysProcAttr.CmdLine 传入，**每个路径都加双引号**
//     —— cmd.exe 在双引号内把 `& | < > ^ ( )` 一律当字面量（同时修好上面的
//     功能性问题）；
//  2. 拒绝**无法**在 cmd 命令行里安全表达的字符：双引号（会终止引号）、
//     `%`（cmd 仍会展开 `%VAR%`，即使在引号内）、`!`（延迟展开）、以及控制字符。
//     这些字符出现在工具名/版本/安装根目录里没有合理场景，明确报错优于静默风险。
//
// 注：更彻底的做法是用 golang.org/x/sys 的 FSCTL_SET_REPARSE_POINT 原生建
// junction（go.mod 已依赖 x/sys）。本文件保留 mklink 是因为仓库注释记载「手写
// REPARSE_DATA_BUFFER 曾在不同路径长度下脆弱、被回退过」（Spec 032）；若要迁移，
// 应作为独立改动并配足路径长度矩阵的回归测试。
func createJunction(link, target string) error {
	if err := validateJunctionArg("link", link); err != nil {
		return err
	}
	if err := validateJunctionArg("target", target); err != nil {
		return err
	}

	cmd := exec.Command("cmd.exe")
	HideWindow(cmd)
	// 完整命令行由我们构造：cmd.exe 会二次解析，os/exec 的 argv 转义不足以防注入。
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CmdLine: `cmd.exe /c mklink /J "` + link + `" "` + target + `"`,
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("create junction %s -> %s: %v: %s", link, target, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// validateJunctionArg 拒绝无法在 cmd 命令行里安全表达的路径。
func validateJunctionArg(what, p string) error {
	if p == "" {
		return fmt.Errorf("refusing to create a junction: empty %s path", what)
	}
	for _, r := range p {
		switch r {
		case '"':
			return fmt.Errorf("refusing to create a junction: %s path contains a double quote (%q) — it would terminate the quoted argument", what, p)
		case '%':
			return fmt.Errorf("refusing to create a junction: %s path contains '%%' (%q) — cmd.exe would expand it even inside quotes", what, p)
		case '!':
			return fmt.Errorf("refusing to create a junction: %s path contains '!' (%q) — unsafe under cmd.exe delayed expansion", what, p)
		case '\n', '\r', 0:
			return fmt.Errorf("refusing to create a junction: %s path contains a control character (%q)", what, p)
		}
	}
	return nil
}
