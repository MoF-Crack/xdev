package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xmdemt/xdev/internal/manifest"
)

// BuildRegistry 是「catalog → 完整 Registry」的**唯一装配入口**。
//
// # 为什么需要这个函数（2026-10-25 架构收口）
//
// 修复前，装配逻辑在四处各写一遍，且语义已经分叉：
//
//	cli/available.go  → RegisterAll（完整：Java 专用 + Generic + Lua/vfox）
//	main_gui.go       → cli.LoadRegistry（完整）
//	cli/hook.go       → 只 NewGeneric 遍历 catalog（**缺 Lua/vfox**）
//	shim/shim.go      → 只 NewGeneric 遍历 catalog（**缺 Lua/vfox**）
//
// 后果是真实功能缺口而非风格问题：`xdev hook`（每次 cd 都跑）与 shim 的
// env 注入（每次执行任何命令都跑）都拿不到 Lua/vfox 插件的 EnvVars ——
// 插件不在 catalog 里，Get 必然 miss，于是插件型工具的项目级版本切换
// 与环境变量注入完全失效。而 P2 刚把 env 注入做成 shim 的核心能力。
//
// 顺带修掉热路径浪费：以前 shim 每次执行都重建一个含 50+ Generic 的
// Registry；现在仍是每次构建（shim 进程短命，无法跨进程缓存），
// 但至少语义统一，且不会因为漏注册而静默失效。
//
// pluginsDir 为空时跳过用户插件扫描（测试与无插件场景）。
func BuildRegistry(catalog *manifest.Catalog, pluginsDir string) *Registry {
	return BuildRegistryExcept(catalog, pluginsDir, nil)
}

// ReadDirFunc 是目录读取的可注入接缝（与 os.ReadDir 同签名）。
//
// 为什么需要它：cli 侧有一个包级测试接缝 pluginsReadDir，用于断言
// 「插件目录只被扫一次」（P2-31 守卫）。装配下沉到本包后，那个接缝
// 必须能传进来，否则守卫失去作用。
//
// nil 表示用 os.ReadDir（生产路径）。
type ReadDirFunc func(dir string) ([]os.DirEntry, error)

// BuildRegistryExcept 同 BuildRegistry，但跳过 exclude 中的工具名。
//
// 用途：cli 需要先注册 Java 专用插件（唯一的自定义 ArchiveFor），
// 再让通用装配跳过它，避免 Generic 先占位。
func BuildRegistryExcept(catalog *manifest.Catalog, pluginsDir string, exclude map[string]bool) *Registry {
	return BuildRegistryWithReadDir(catalog, pluginsDir, exclude, nil)
}

// BuildRegistryWithReadDir 是完整形式，允许注入目录读取函数。
func BuildRegistryWithReadDir(catalog *manifest.Catalog, pluginsDir string, exclude map[string]bool, readDir ReadDirFunc) *Registry {
	r := NewRegistry()
	if catalog == nil {
		return r
	}

	// 1. Generic 插件：遍历完整 catalog。
	//    没有可安装内容的条目（如 manifests/index.yaml 目录索引）跳过 ——
	//    注册后只会产生坏插件。
	for _, t := range catalog.Tools() {
		if exclude[t.Name] {
			continue
		}
		if !isRegisterableTool(t) {
			continue
		}
		r.Register(NewGeneric(t))
	}

	// 2. 用户 Lua/vfox 插件：扫描目录，两种格式各试一次。
	if pluginsDir != "" {
		registerUserPlugins(r, pluginsDir, readDir)
	}

	return r
}

// isRegisterableTool 报告 t 是否为可安装的真实工具：至少要有
// display_name、latest/lts、版本列表或 remote 之一。全空的条目
// （如 manifests/index.yaml 这类目录索引）没有可安装内容，跳过。
//
// 与 cli.isRegisterableTool 同源 —— 下沉到这里是为了让装配只有一个实现。
func isRegisterableTool(t *manifest.Tool) bool {
	if t == nil {
		return false
	}
	return t.DisplayName != "" || t.Latest != "" || t.LTS != "" ||
		len(t.VersionList) > 0 || t.Remote.Type != ""
}

// registerUserPlugins 扫描 pluginsDir 下的 .lua 文件并注册。
//
// 两种格式：xdev 原生优先，失败后尝试 vfox 兼容格式。
// 成功加载的文件只 DoFile 一次（共用同一个 LuaEngine）。
func registerUserPlugins(r *Registry, dir string, readDir ReadDirFunc) {
	if readDir == nil {
		readDir = os.ReadDir
	}
	entries, err := readDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "warning: failed to read plugins dir %s: %v\n", dir, err)
		}
		return
	}

	engine := NewLuaEngine()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".lua") {
			continue
		}
		path := filepath.Join(dir, e.Name())

		// 1) xdev 原生格式（多数插件）。成功即注册，不再尝试 vfox
		//    —— "成功加载的文件只 DoFile 一次"。
		p, xdevErr := engine.LoadPlugin(path)
		if xdevErr == nil {
			if _, exists := r.Get(p.Name()); exists {
				fmt.Fprintf(os.Stderr, "warning: Lua plugin %s conflicts with built-in plugin, skipping\n", p.Name())
				continue
			}
			r.Register(p)
			continue
		}

		// 2) vfox 兼容格式（PLUGIN.info + install()/env_keys()）。
		vfoxPlugin, vfoxErr := engine.LoadVfoxPlugin(path)
		if vfoxErr != nil {
			// 两种格式都读不出来时必须让用户看到原因
			// （两个错误都打印，便于判断是格式写错还是脚本报错）。
			fmt.Fprintf(os.Stderr, "warning: failed to load Lua plugin %s: %v; vfox format also failed: %v\n",
				e.Name(), xdevErr, vfoxErr)
			continue
		}
		if _, exists := r.Get(vfoxPlugin.Name()); exists {
			fmt.Fprintf(os.Stderr, "warning: vfox plugin %s conflicts with existing plugin, skipping\n", vfoxPlugin.Name())
			continue
		}
		r.Register(vfoxPlugin)
		fmt.Fprintf(os.Stderr, "info: loaded vfox plugin %s from %s\n", vfoxPlugin.Name(), e.Name())
	}
}
