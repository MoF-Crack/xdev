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
	addExcludedDir(excluded, home)                           // home 根（xdev.exe 副本）
	addExcludedDir(excluded, filepath.Join(home, "current")) // xdev 管理目录
	addExcludedDir(excluded, filepath.Join(home, "shims"))   // 标准位置（冗余但显式）
	return excluded
}

// addExcludedDir 以绝对路径 + symlink 解析后的形式登记排除目录
// （大小写不敏感，Windows 语义）。
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
//
// C-09（P1-33）：PATH 条目一侧也必须与 addExcludedDir 同款归一化后再比对。
// 只比字面路径时，任何「指向 shim 目录的别名」条目都会漏过排除集——多一层
// junction/符号链接、相对裸名（"shims"）、或者排除集本身是别名形式，都能
// 让透传解析回 shim 目录里的副本，shim 再 exec 自己 → 无限递归。
func lookupOnPath(alias, pathValue string, excluded map[string]bool) (string, error) {
	alias = strings.TrimSpace(alias)
	if alias == "" || alias != filepath.Base(alias) {
		// 含路径分隔符的输入在 exec.LookPath 语义中是相对/绝对路径
		// 而非 PATH 查找——透传仅处理纯别名。
		return "", nil
	}
	// Windows junction 兜底所需的目录标识：每次 lookup 只 stat 一遍排除
	// 目录，避免 PATH 条目 × 排除目录的 N×M 次系统调用。
	excludedIDs := excludedDirIdentities(excluded)
	for _, dir := range filepath.SplitList(pathValue) {
		if dir == "" {
			continue
		}
		clean := filepath.Clean(dir)
		if isExcludedPathEntry(clean, excluded, excludedIDs) {
			continue
		}
		for _, cand := range candidatePaths(clean, alias) {
			// P2-09：os.Stat（而非 Lstat）——LookPath 语义跟随符号链接，
			// 布局如 /usr/local/bin/node -> ../lib/node 的安装必须仍然命中。
			fi, err := os.Stat(cand)
			if err != nil || fi.IsDir() || !executableCandidate(fi) {
				continue
			}
			return filepath.Clean(cand), nil
		}
	}
	return "", nil
}

// isExcludedPathEntry 判断一个 PATH 条目是否落在排除目录上（大小写
// 不敏感，Windows 语义）。三层判据缺一不可：
//
//  1. 字面比对（filepath.Clean 后）——「末位兜底」：条目本身就是排除目录
//     的字面写法（含尾分隔符、"." / ".." 已被 Clean 消解）时最省的路径；
//  2. Abs + EvalSymlinks 归一化后比对——主防线，覆盖符号链接、相对路径
//     （裸名 "shims" 相对 cwd 解析）、短名等别名形式；
//  3. os.SameFile 文件标识比对（仅 Windows，见下）——junction 兜底。
//
// 第 3 层不能省：实测 Go 1.26.5 的 filepath.EvalSymlinks 不解析目录
// junction（原样返回 junction 路径），因此「PATH 条目是 junction、排除集
// 是真实目录」以及反向组合都只有文件标识比对能识别。它是辅助判据——
// shim 目录的**副本**（copy 型分发）不是同一文件，仍由第 1/2 层按路径
// 覆盖。
func isExcludedPathEntry(entry string, excluded map[string]bool, excludedIDs []os.FileInfo) bool {
	if len(excluded) == 0 {
		return false
	}
	if excluded[strings.ToLower(filepath.Clean(entry))] {
		return true
	}
	if abs, err := filepath.Abs(entry); err == nil {
		if ev, err2 := filepath.EvalSymlinks(abs); err2 == nil {
			abs = ev
		}
		if excluded[strings.ToLower(filepath.Clean(abs))] {
			return true
		}
	}
	if len(excludedIDs) > 0 {
		if fi, err := os.Stat(entry); err == nil && fi.IsDir() {
			for _, id := range excludedIDs {
				if os.SameFile(fi, id) {
					return true
				}
			}
		}
	}
	return false
}

// excludedDirIdentities 预先 stat 排除目录，得到 os.SameFile 兜底所需的
// 文件标识。非 Windows 平台返回 nil：那里 EvalSymlinks 已能解析符号链接，
// 而归一化会把排除集键小写化（Unix 大小写敏感），再 stat 键反而可能
// 指向不存在的路径。Windows 的目录 junction 是唯一需要该兜底的形态。
func excludedDirIdentities(excluded map[string]bool) []os.FileInfo {
	if runtime.GOOS != "windows" {
		return nil
	}
	ids := make([]os.FileInfo, 0, len(excluded))
	for key := range excluded {
		if fi, err := os.Stat(key); err == nil && fi.IsDir() {
			ids = append(ids, fi)
		}
	}
	return ids
}

// candidatePaths 展开一个目录下某别名的候选文件路径。
//
// Windows（P2-09）：按 PATHEXT 展开（先例 internal/hostscan/hostscan.go 的
// windowsPathExts/expandNames），裸名作为**末位**兜底保留。顺序很重要：
// cmd/CreateProcess 只执行带 PATHEXT 扩展名的文件，裸名排最后才能让
// "git" 优先命中同目录下的 git.exe，而不是恰好存在的无扩展名同名文件
// （历史布局里存在这种副本）。alias 自带 PATHEXT 扩展名时不再追加
// （同 hostscan.hasWindowsExt 语义）——否则会产出 git.exe.exe 这类候选。
// PATHEXT 每次调用现读（测试可用 t.Setenv 覆盖）。
//
// Unix：只有裸名一个候选；可执行性由 lookupOnPath 的执行位校验兜底，
// 不在此处按扩展名过滤（Unix 的可执行文件通常没有扩展名）。
func candidatePaths(dir, alias string) []string {
	if runtime.GOOS != "windows" {
		return []string{filepath.Join(dir, alias)}
	}
	exts := windowsPathExts()
	cands := make([]string, 0, len(exts)+1)
	if !hasWindowsPathExt(alias, exts) {
		for _, ext := range exts {
			// 统一小写（NTFS 大小写不敏感，小写与 hostscan 的候选风格一致）。
			cands = append(cands, filepath.Join(dir, alias+strings.ToLower(ext)))
		}
	}
	// 裸名末位兜底（P2-09 明确保留）。
	return append(cands, filepath.Join(dir, alias))
}

// windowsPathExts 返回 PATHEXT 展开用的扩展名列表（含前导点）。
// 与 internal/hostscan 的同名函数语义一致（该包为未导出实现，不跨包共享，
// 改动面小于导出重构）；PATHEXT 缺失/为空时退回 Windows 默认值。
func windowsPathExts() []string {
	raw := os.Getenv("PATHEXT")
	if raw == "" {
		return []string{".COM", ".EXE", ".BAT", ".CMD"}
	}
	parts := strings.Split(raw, ";")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if !strings.HasPrefix(p, ".") {
			p = "." + p
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return []string{".COM", ".EXE", ".BAT", ".CMD"}
	}
	return out
}

// hasWindowsPathExt 报告名字是否已带某个 PATHEXT 扩展名（大小写不敏感）。
func hasWindowsPathExt(name string, exts []string) bool {
	lower := strings.ToLower(name)
	for _, ext := range exts {
		if strings.HasSuffix(lower, strings.ToLower(ext)) {
			return true
		}
	}
	return false
}

// executableCandidate 报告候选文件是否"可执行"（P2-09）。
//
// Unix：必须有任一执行位。修复前只判 !IsDir()——PATH 里一个不可执行的同名
// 数据文件（配置、脚本源码、占位文件）会被当作"真实工具"透传出去，调用方
// exec 时失败，错误信息与真正原因（文件不可执行）完全脱节。
//
// Windows：可执行性由 PATHEXT 扩展名承载，文件模式没有执行位概念
// （os.Chmod 只影响只读属性）→ 一律通过；此处不重复扩展名判定。
func executableCandidate(fi os.FileInfo) bool {
	if runtime.GOOS == "windows" {
		return true
	}
	return hasExecBit(fi.Mode())
}

// hasExecBit 是平台无关的执行位判定（Unix 语义）。抽成纯函数是为了让
// 执行位语义在 Windows 开发机上也能被单元测试直接覆盖——本机无法构造
// "有执行位/无执行位"的文件（Windows 没有该位），只能测判定本身；
// 端到端用例见 shim_pathcandidate_b5_test.go（Unix 上运行）。
func hasExecBit(mode os.FileMode) bool { return mode.Perm()&0o111 != 0 }
