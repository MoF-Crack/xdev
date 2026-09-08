package shim

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/xmdemt/xdev/internal/config"
)

// LookupOnPath 在「剔除 xdev 自身目录」的 PATH 上解析别名对应的真实
// 可执行文件（Spec 038：shim 透传）。
//
// 背景：shims/ 目录排在 PATH 最前。当某个别名有 shim 但对应工具未由
// xdev 管理（孤儿 shim——历史安装残留、目录扫描写入等），Lookup 失败
// 会直接报错，遮蔽系统上已有的同名真实工具（实测：git shim 遮蔽
// Git for Windows，gh 等内部调 git 的工具连带失效）。
//
// 剔除规则（全部大小写不敏感，Windows 语义）：
//   - shim 进程自身所在目录（shim 副本都在那里——可配置 shimDir 的
//     情况也天然覆盖）
//   - $XMDEV_HOME（home 根，xdev.exe 副本所在，防自引用循环）
//   - $XMDEV_HOME/current（xdev 管理目录）
//
// 返回解析到的绝对路径；找不到返回 ("", nil)——调用方保留原始
// Lookup 错误（提示 xdev install）。
func LookupOnPath(alias string) (string, error) {
	excluded := defaultExcludedDirs()
	return lookupOnPath(alias, os.Getenv("PATH"), excluded)
}

// defaultExcludedDirs 计算生产环境的透传排除集。
func defaultExcludedDirs() map[string]bool {
	excluded := map[string]bool{}
	if self, err := os.Executable(); err == nil && self != "" {
		addExcludedDir(excluded, filepath.Dir(self)) // shim 副本目录（= shims/）
	}
	home := config.ResolveHomeDir()
	addExcludedDir(excluded, home)                                // home 根（xdev.exe 副本）
	addExcludedDir(excluded, filepath.Join(home, "current"))      // xdev 管理目录
	addExcludedDir(excluded, filepath.Join(home, "shims"))        // 标准位置（冗余但显式）
	return excluded
}

// addExcludedDir 以绝对路径 + symlink 解析后的形式登记排除目录
//（大小写不敏感，Windows 语义）。
func addExcludedDir(excluded map[string]bool, dir string) {
	if dir == "" {
		return
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return
	}
	if ev, err2 := filepath.EvalSymlinks(abs); err2 == nil {
		abs = ev
	}
	excluded[strings.ToLower(filepath.Clean(abs))] = true
}

// lookupOnPath 是纯函数实现：在 pathValue 中查找 alias，跳过 excluded
// 目录。候选名做 PATHEXT 式展开（Windows：.exe/.cmd/.bat + 裸名；
// Unix：裸名）——不能依赖 exec.LookPath 兜底，因为它会搜索进程真实
// PATH 且只返回第一个命中（无法跳过排除目录）。
func lookupOnPath(alias, pathValue string, excluded map[string]bool) (string, error) {
	alias = strings.TrimSpace(alias)
	if alias == "" || alias != filepath.Base(alias) {
		// 含路径分隔符的输入在 exec.LookPath 语义中是相对/绝对路径
		// 而非 PATH 查找——透传仅处理纯别名。
		return "", nil
	}
	for _, dir := range filepath.SplitList(pathValue) {
		if dir == "" {
			continue
		}
		clean := filepath.Clean(dir)
		if excluded[strings.ToLower(clean)] {
			continue
		}
		for _, cand := range candidatePaths(clean, alias) {
			if fi, err := os.Stat(cand); err == nil && !fi.IsDir() {
				return filepath.Clean(cand), nil
			}
		}
	}
	return "", nil
}

// candidatePaths 展开一个目录下某别名的候选文件路径（Windows 语义：
// 裸名 + .exe/.cmd/.bat；Unix：裸名）。与 hostscan.FindInDir 的候选
// 集合语义一致。
func candidatePaths(dir, alias string) []string {
	if runtime.GOOS == "windows" {
		return []string{
			filepath.Join(dir, alias),
			filepath.Join(dir, alias+".exe"),
			filepath.Join(dir, alias+".cmd"),
			filepath.Join(dir, alias+".bat"),
		}
	}
	return []string{filepath.Join(dir, alias)}
}
