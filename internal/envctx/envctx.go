// Package envctx 把「工具版本」解析成一份可注入子进程的环境上下文。
//
// # 为什么需要这个包
//
// xdev 传统上把 Tool.EnvVars 写进 Windows 用户注册表（HKCU）：
//
//	installer.setUserEnvVars → env.SetUserEnvVarTrusted
//
// 这个做法有三个无法回避的问题：
//
//  1. **非 Windows 完全不生效**：setUserEnvVars 开头就 `if runtime.GOOS != "windows" { return nil }`，
//     macOS/Linux 上 JAVA_HOME / GOROOT 从未被设置，Maven/Gradle 系工具链直接不可用。
//  2. **全局单份**：注册表只有一个 JAVA_HOME。多版本并存时后激活者覆盖前者；
//     ANDROID_HOME 被 5 个 manifest 声明，卸载任一组件的最后一个版本会无条件删除它。
//  3. **污染进程外状态**：写注册表意味着「装完就有」，但卸载/切换失败会留下
//     指向已删除目录的变量（历史上确有此类缺陷）。
//
// 本包提供另一条路径：**只在进程内计算**，由 shim 注入给子进程。
// 它不碰注册表、不依赖平台、天然支持多版本并存（不同项目解析出不同的 Context）。
//
// # 与注册表路径的关系
//
// 两者并存，不是替代关系：
//   - 进程注入覆盖「经由 shim 启动的一切」（终端、构建脚本、shim 拉起的子进程）；
//   - 注册表写入保留给「不经过 shim」的场景（IDE 直接跑绝对路径的 java.exe、
//     系统服务等）。IDE 那类场景拿不到进程注入，这是本方案的固有边界。
//
// # 使用方式
//
//	ctx := envctx.Resolve(plugin, installPath)   // 计算
//	cmd.Env = ctx.Apply(os.Environ())            // 注入
//
// Resolve 是纯函数（不读磁盘、不写状态），Apply 只操作传入的环境切片。
package envctx

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/xmdemt/xdev/internal/plugin"
)

// Context 是一次解析得到的进程环境上下文。
type Context struct {
	// Vars 是要设置的变量（已插值，绝对路径）。
	// 键为变量名，值为最终字符串。
	Vars map[string]string

	// PathAppend 是要**前插**到 PATH 的目录（已插值、去重、绝对路径）。
	// 顺序即优先级：靠前的先被搜索。
	PathAppend []string
}

// Empty 报告该上下文是否没有任何内容可注入。
// 调用方可用它跳过不必要的环境切片复制。
func (c *Context) Empty() bool {
	return c == nil || (len(c.Vars) == 0 && len(c.PathAppend) == 0)
}

// Resolve 计算某个已安装版本的环境上下文。
//
// p 可为 nil（未知插件）——此时返回空上下文而不是报错：shim 是热路径，
// 拿不到插件信息不应阻断用户命令执行。
//
// installPath 必须是该版本的真实安装目录（install/<tool>/<version>）。
func Resolve(p plugin.Plugin, installPath string) *Context {
	ctx := &Context{Vars: map[string]string{}}
	if p == nil || installPath == "" {
		return ctx
	}

	// EnvVars / PathAppend 内部已做模板插值与反斜杠归一（见 plugin.Generic），
	// 这里只做一次「是否可用」的过滤。
	for k, v := range p.EnvVars(installPath) {
		if !isUsableVar(k, v) {
			continue
		}
		// 路径型变量做 Clean：manifest 里大量写成 `{{install}}\..\gopath`，
		// 插值后会得到 `.../1.27.3/../gopath` 这种带 `..` 的形式。
		// 虽然操作系统能解析，但它会被子进程原样写进输出
		// （如 `go env GOPATH`）、日志、缓存键，既难看也容易在字符串
		// 比较时出意外（同一目录两种写法）。
		// 只对看起来像路径的值做处理，避免误改非路径值（如 "true"、"1"）。
		if looksLikePath(v) {
			v = filepath.Clean(v)
		}
		ctx.Vars[k] = v
	}

	for _, dir := range p.PathAppend(installPath) {
		dir = strings.TrimSpace(dir)
		// 相对路径在 PATH 里会随 CWD 变化，语义不确定，一律丢弃。
		if dir == "" || !filepath.IsAbs(dir) {
			continue
		}
		ctx.PathAppend = append(ctx.PathAppend, dir)
	}

	return ctx
}

// isUsableVar 过滤掉不该注入的变量。
//
// PATH 被显式排除：它必须走 PathAppend 通道（前插 + 去重），
// 直接整串覆盖会毁掉用户已有 PATH。这与 installer.setUserEnvVars
// 里 `if name == "PATH" { continue }` 是同一条约束。
func isUsableVar(name, value string) bool {
	if name == "" || strings.TrimSpace(value) == "" {
		return false
	}
	if strings.EqualFold(name, "PATH") {
		return false
	}
	return true
}

// looksLikePath 判断一个值是否应按路径规范化。
//
// 启发式：含路径分隔符，或本身就是绝对路径。
// 不追求完备 —— 误判的代价只是对一个非路径值调了 filepath.Clean
// （对 "true"/"1"/"mozilla" 这类无分隔符的值是恒等变换）。
func looksLikePath(v string) bool {
	if filepath.IsAbs(v) {
		return true
	}
	return strings.ContainsAny(v, `\/`)
}

// Apply 把上下文注入到给定的环境切片，返回新切片（不修改入参）。
//
// 语义：
//   - Vars 中的变量**覆盖**同名项（工具的设定优先于继承值）；
//   - PathAppend 中的目录按顺序**前插**到 PATH，已有的同名项不重复添加
//     （幂等：同一目录在 PATH 里已存在时跳过，但不重置已累积的前缀）；
//   - 其余变量原样保留。
func (c *Context) Apply(base []string) []string {
	if c.Empty() {
		return base
	}

	pathKey := "PATH"
	if runtime.GOOS == "windows" {
		// Windows 环境变量名大小写不敏感，实际存储通常写作 "Path"。
		// 这里沿用调用方已有的写法，找不到时回退 PATH。
		pathKey = findPathKey(base)
	}

	// 1. 先算新 PATH。
	oldPath := lookupEnv(base, pathKey)
	newPath := c.buildPath(oldPath)

	// 2. 组装结果：跳过被覆盖的键与旧 PATH，其余原样。
	out := make([]string, 0, len(base)+len(c.Vars)+1)
	for _, kv := range base {
		name, _, ok := strings.Cut(kv, "=")
		if !ok {
			out = append(out, kv)
			continue
		}
		if strings.EqualFold(name, pathKey) {
			continue // PATH 单独处理
		}
		if _, override := c.Vars[name]; override {
			continue // 由下面的 Vars 覆盖
		}
		out = append(out, kv)
	}

	// 3. 写入 Vars。
	for k, v := range c.Vars {
		out = append(out, k+"="+v)
	}

	// 4. 写入合并后的 PATH（放在最后，保证只有一条）。
	if newPath != "" {
		out = append(out, pathKey+"="+newPath)
	}

	return out
}

// buildPath 把 PathAppend 前插到 oldPath，并做去重。
//
// 去重规则与 shim.PathPrepend 保持一致：遇到已在结果中的项跳过，
// 但不因为「发现重复」而重置已累积的前缀——否则重复调用会让顺序不稳定。
func (c *Context) buildPath(oldPath string) string {
	if len(c.PathAppend) == 0 {
		return oldPath
	}
	sep := string(os.PathListSeparator)

	// 现有项集合（用于去重，大小写按平台语义处理）。
	existing := make(map[string]bool)
	if oldPath != "" {
		for _, p := range strings.Split(oldPath, sep) {
			existing[pathKeyOf(p)] = true
		}
	}

	var prefix []string
	for _, dir := range c.PathAppend {
		key := pathKeyOf(dir)
		if existing[key] {
			continue // 已在 PATH 中，不重复添加
		}
		existing[key] = true
		prefix = append(prefix, dir)
	}
	if len(prefix) == 0 {
		return oldPath
	}

	joined := strings.Join(prefix, sep)
	if oldPath == "" {
		return joined
	}
	return joined + sep + oldPath
}

// pathKeyOf 返回用于比较的规范化键。
// Windows 路径大小写不敏感；Unix 保留精确大小写。
func pathKeyOf(p string) string {
	p = strings.TrimSpace(p)
	p = strings.TrimSuffix(p, string(os.PathSeparator)) // 去掉尾部分隔符
	if runtime.GOOS == "windows" {
		return strings.ToLower(p)
	}
	return p
}

// lookupEnv 在 "K=V" 切片里查一个键的值（Windows 大小写不敏感）。
func lookupEnv(env []string, name string) string {
	for _, kv := range env {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if k == name || (runtime.GOOS == "windows" && strings.EqualFold(k, name)) {
			return v
		}
	}
	return ""
}

// findPathKey 找到调用方环境里 PATH 的实际写法（Windows 常写作 "Path"）。
func findPathKey(env []string) string {
	for _, kv := range env {
		k, _, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if strings.EqualFold(k, "PATH") {
			return k
		}
	}
	return "PATH"
}
