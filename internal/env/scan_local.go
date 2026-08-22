package env

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/xmdemt/xdev/internal/hostscan"
	"github.com/xmdemt/xdev/internal/toolmeta"
	"github.com/xmdemt/xdev/internal/util"
)

// LocalTool 表示扫描到的本地已安装工具
type LocalTool struct {
	Name     string            `json:"name"`     // 工具名：java, node, python 等
	Path     string            `json:"path"`     // 工具安装路径：如 D:\Env\JDK17
	Version  string            `json:"version"`  // 版本号
	BinPaths []string          `json:"binPaths"` // bin 目录列表
	EnvVars  map[string]string `json:"envVars"`  // 关联的环境变量
}

// toolScanRule 是扫描规则的桥接结构，由 toolmeta 表构造（Spec 033 R1 收口：
// 工具→二进制名映射的单一权威来源在 internal/toolmeta）。
type toolScanRule struct {
	name       string   // 工具名
	binNames   []string // 可执行文件名（不含扩展名）
	envVarKeys []string // 关联的环境变量名
}

// ScanLocalTools 扫描系统中已安装的开发工具
// xdevDir 用于排除 xdev 自身管理的目录
func ScanLocalTools(xdevDir string) []LocalTool {
	metas := toolmeta.ScanTargets()
	results := make([]LocalTool, len(metas))
	seen := make(map[string]bool)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i, m := range metas {
		rule := toolScanRule{name: m.Name, binNames: m.BinNames, envVarKeys: m.EnvVarKeys}
		wg.Add(1)
		go func(idx int, rule toolScanRule) {
			defer wg.Done()
			tool := scanTool(rule, xdevDir)
			if tool != nil {
				mu.Lock()
				if !seen[tool.Name] {
					seen[tool.Name] = true
					results[idx] = *tool
				}
				mu.Unlock()
			}
		}(i, rule)
	}
	wg.Wait()

	// 收集非空结果
	var filtered []LocalTool
	for _, t := range results {
		if t.Name != "" {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

// scanTool 根据规则扫描单个工具
func scanTool(rule toolScanRule, xdevDir string) *LocalTool {
	// 1. 通过环境变量检测
	if tool := scanByEnvVars(rule, xdevDir); tool != nil {
		return tool
	}

	// 2. 通过 PATH 条目检测
	if tool := scanByPath(rule, xdevDir); tool != nil {
		return tool
	}

	// 3. 通过常见安装目录检测
	if tool := scanByCommonDirs(rule, xdevDir); tool != nil {
		return tool
	}

	return nil
}

// scanByEnvVars 通过环境变量检测工具
func scanByEnvVars(rule toolScanRule, xdevDir string) *LocalTool {
	for _, envKey := range rule.envVarKeys {
		val := os.Getenv(envKey)
		if val == "" {
			continue
		}

		// 排除 xdev 管理的目录
		if hostscan.InsideDir(val, xdevDir) {
			continue
		}

		// 环境变量指向的路径可能直接就是安装目录
		installPath := resolveInstallPath(val, rule)
		if installPath == "" {
			continue
		}

		// 验证安装路径下是否有对应的可执行文件
		if !verifyToolBinaries(installPath, rule) {
			continue
		}

		version := getToolVersion(rule, installPath)
		binPaths := ToolBinPaths(rule.name, installPath)
		envVars := collectEnvVars(rule, installPath)

		return &LocalTool{
			Name:     rule.name,
			Path:     installPath,
			Version:  version,
			BinPaths: binPaths,
			EnvVars:  envVars,
		}
	}
	return nil
}

// scanByPath 通过 PATH 环境变量检测工具
func scanByPath(rule toolScanRule, xdevDir string) *LocalTool {
	shimDir := ""
	if xdevDir != "" {
		shimDir = filepath.Join(xdevDir, "shims")
	}
	for _, binName := range rule.binNames {
		path, ok := hostscan.LookPath(binName, shimDir)
		if !ok {
			continue
		}

		// 排除 xdev shim 目录和 install 目录
		if hostscan.InsideDir(path, xdevDir) {
			continue
		}

		// 从可执行文件路径推导安装目录
		installPath := resolveInstallPathFromBin(path, rule)
		if installPath == "" {
			continue
		}

		version := getToolVersion(rule, installPath)
		binPaths := ToolBinPaths(rule.name, installPath)
		envVars := collectEnvVars(rule, installPath)

		return &LocalTool{
			Name:     rule.name,
			Path:     installPath,
			Version:  version,
			BinPaths: binPaths,
			EnvVars:  envVars,
		}
	}
	return nil
}

// scanByCommonDirs 通过常见安装目录检测工具
func scanByCommonDirs(rule toolScanRule, xdevDir string) *LocalTool {
	commonPaths := toolmeta.CommonInstallPaths(rule.name)
	for _, p := range commonPaths {
		if hostscan.InsideDir(p, xdevDir) {
			continue
		}

		fi, err := os.Stat(p)
		if err != nil || !fi.IsDir() {
			continue
		}

		// 对于某些工具，commonPath 指向的是父目录（如 Program Files\Java）
		// 需要遍历子目录找到实际的安装路径
		installPath := findInstallInDir(p, rule)
		if installPath == "" {
			continue
		}

		version := getToolVersion(rule, installPath)
		binPaths := ToolBinPaths(rule.name, installPath)
		envVars := collectEnvVars(rule, installPath)

		return &LocalTool{
			Name:     rule.name,
			Path:     installPath,
			Version:  version,
			BinPaths: binPaths,
			EnvVars:  envVars,
		}
	}
	return nil
}

// hasGitRuntime 判断目录是否包含 Git for Windows / PortableGit 的运行时
// （mingw64/ 目录，或 usr/ + bin/ 目录组合）。仅含 launcher（cmd/git.exe
// 等启动器）的目录无法独立运行 git，不应被迁移。
func hasGitRuntime(dir string) bool {
	if fi, err := os.Stat(filepath.Join(dir, "mingw64")); err == nil && fi.IsDir() {
		return true
	}
	if fi1, err1 := os.Stat(filepath.Join(dir, "usr")); err1 == nil && fi1.IsDir() {
		if fi2, err2 := os.Stat(filepath.Join(dir, "bin")); err2 == nil && fi2.IsDir() {
			return true
		}
	}
	return false
}

// liftCmdToInstallRoot 处理 Git for Windows / PortableGit 的安装布局：
// PATH 中通常只包含 <root>/cmd（launcher 目录，cmd/git.exe 只是定位真实
// 二进制的启动器），而真正的安装根目录是 cmd 的父目录，其中包含
// mingw64/（运行时）以及 usr/、bin/ 等目录。若 dir 名为 cmd 且父目录
// 具备 git 运行时标记（mingw64/ 目录，或 usr/ + bin/ 目录组合），则
// 返回父目录；否则原样返回。
func liftCmdToInstallRoot(dir string) string {
	if strings.ToLower(filepath.Base(dir)) != "cmd" {
		return dir
	}
	parent := filepath.Dir(dir)
	if hasGitRuntime(parent) {
		return parent
	}
	return dir
}

// resolveInstallPath 从环境变量值推导工具安装路径
func resolveInstallPath(envVal string, rule toolScanRule) string {
	fi, err := os.Stat(envVal)
	if err != nil {
		return ""
	}

	if fi.IsDir() {
		// 环境变量直接指向安装目录
		return envVal
	}

	// 环境变量指向的是可执行文件，向上推导安装目录
	dir := filepath.Dir(envVal)
	if filepath.Base(dir) == "bin" {
		dir = filepath.Dir(dir)
	}
	// Git for Windows：环境变量可能指向 <root>/cmd/git.exe（launcher）
	return liftCmdToInstallRoot(dir)
}

// resolveInstallPathFromBin 从可执行文件路径推导安装目录
func resolveInstallPathFromBin(binPath string, rule toolScanRule) string {
	dir := filepath.Dir(binPath)

	// 如果在 bin 子目录下，安装目录是父目录
	if filepath.Base(dir) == "bin" {
		return filepath.Dir(dir)
	}

	// 如果在 Scripts 子目录下（Python），安装目录是父目录
	if filepath.Base(dir) == "Scripts" {
		return filepath.Dir(dir)
	}

	// Git for Windows / PortableGit：PATH 条目通常是 <root>/cmd，
	// cmd/git.exe 只是 launcher，安装根目录是 cmd 的父目录。
	// 必须先于 looksLikeInstallDir 处理，避免把 launcher 目录误当安装根。
	if lifted := liftCmdToInstallRoot(dir); lifted != dir {
		return lifted
	}

	// 某些工具（如 node）可执行文件直接在安装根目录
	// 检查该目录是否看起来像安装目录
	if looksLikeInstallDir(dir, rule) {
		return dir
	}

	return dir
}

// looksLikeInstallDir 检查目录是否看起来像工具的安装目录
func looksLikeInstallDir(dir string, rule toolScanRule) bool {
	// 检查是否有 bin 子目录
	binDir := filepath.Join(dir, "bin")
	if fi, err := os.Stat(binDir); err == nil && fi.IsDir() {
		return true
	}

	// 检查目录名是否包含工具名（如 jdk-17, nodejs 等）
	base := strings.ToLower(filepath.Base(dir))
	name := strings.ToLower(rule.name)
	if strings.Contains(base, name) {
		return true
	}

	// 特殊情况：node 安装目录通常叫 nodejs
	if name == "node" && strings.Contains(base, "nodejs") {
		return true
	}

	return false
}

// findInstallInDir 在父目录中查找实际的安装路径
// 某些 commonPath 指向的是父目录（如 Program Files\Java），需要遍历子目录
func findInstallInDir(parentDir string, rule toolScanRule) string {
	// 先检查父目录本身是否就是安装目录
	if verifyToolBinaries(parentDir, rule) {
		return parentDir
	}

	// 遍历子目录
	entries, err := os.ReadDir(parentDir)
	if err != nil {
		return ""
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		subDir := filepath.Join(parentDir, e.Name())
		if verifyToolBinaries(subDir, rule) {
			return subDir
		}
	}
	return ""
}

// verifyToolBinaries 验证安装路径下是否有对应的可执行文件
func verifyToolBinaries(installPath string, rule toolScanRule) bool {
	for _, binName := range toolmeta.BinNames(rule.name) {
		candidates := []string{
			filepath.Join(installPath, "bin", binName),
			filepath.Join(installPath, binName),
		}
		if runtime.GOOS == "windows" {
			candidates = append(candidates,
				filepath.Join(installPath, "bin", binName+".exe"),
				filepath.Join(installPath, "bin", binName+".cmd"),
				filepath.Join(installPath, binName+".exe"),
				filepath.Join(installPath, binName+".cmd"),
			)
		}
		for _, c := range candidates {
			if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
				return true
			}
		}
	}
	return false
}

// getToolVersion 尝试获取工具版本号
func getToolVersion(rule toolScanRule, installPath string) string {
	// 找到可执行文件路径
	var binPath string
	for _, binName := range rule.binNames {
		candidates := []string{
			filepath.Join(installPath, "bin", binName),
			filepath.Join(installPath, binName),
		}
		if runtime.GOOS == "windows" {
			candidates = append(candidates,
				filepath.Join(installPath, "bin", binName+".exe"),
				filepath.Join(installPath, binName+".cmd"),
				filepath.Join(installPath, binName+".exe"),
				filepath.Join(installPath, binName+".cmd"),
			)
		}
		for _, c := range candidates {
			if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
				binPath = c
				break
			}
		}
		if binPath != "" {
			break
		}
	}

	if binPath == "" {
		return ""
	}

	// 执行版本命令（版本参数查 toolmeta 表，Spec 033 R2 收口）
	flags := toolmeta.VersionArg(rule.binNames[0])

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binPath, flags...)
	// Windows 上隐藏控制台窗口
	util.HideWindow(cmd)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}

	// 解析版本号
	line := strings.TrimSpace(string(output))
	if idx := strings.Index(line, "\n"); idx > 0 {
		line = line[:idx]
	}
	return toolmeta.ExtractVersionFromOutput(line)
}

// collectEnvVars 收集工具关联的环境变量（键查 toolmeta 表，Spec 033 R1 收口）
func collectEnvVars(rule toolScanRule, installPath string) map[string]string {
	vars := make(map[string]string)
	for _, envKey := range toolmeta.EnvVarKeys(rule.name) {
		vars[envKey] = installPath
	}
	return vars
}

// MigrateLocalTool 将本地工具迁移到 xdev 管理
// 使用 Windows junction 指向原始安装路径，不复制文件
func MigrateLocalTool(tool LocalTool, xdevDir string) error {
	// Security (F-10): 工具名必须安全（防止目录穿越），源路径必须是绝对路径。
	if err := util.ValidateShimName(tool.Name); err != nil {
		return fmt.Errorf("invalid tool name %q: %w", tool.Name, err)
	}
	if tool.Path == "" || !filepath.IsAbs(tool.Path) {
		return fmt.Errorf("source path must be absolute: %q", tool.Path)
	}

	// Git for Windows / PortableGit 的 cmd/（以及单独拷出的 launcher 目录）
	// 只包含启动器：cmd/git.exe 会以自身目录的父目录作为 git 根目录去查找
	// mingw64/bin/git.exe，缺少运行时（mingw64/ 等）时任何命令都会报
	// "error launching git"。拒绝迁移这类残缺目录，避免迁移后 git 不可用。
	if tool.Name == "git" && !hasGitRuntime(tool.Path) {
		return fmt.Errorf("source path %q looks like a git launcher-only directory (missing mingw64/ runtime); migrate the git install root instead", tool.Path)
	}

	installDir := filepath.Join(xdevDir, "install")
	version := tool.Version
	if version == "" {
		version = "local"
	}

	// 清理版本号，使其适合作为目录名
	version = sanitizeVersion(version)

	// 目标路径：xdevDir/install/toolName/version
	targetPath := filepath.Join(installDir, tool.Name, version)
	// F-10: 目标必须位于 install 目录内。
	if err := util.ValidateAbsPathUnder(installDir, targetPath); err != nil {
		return fmt.Errorf("target path escapes install dir: %w", err)
	}

	// 检查目标是否已存在
	if _, err := os.Stat(targetPath); err == nil {
		return fmt.Errorf("target path already exists: %s", targetPath)
	}

	// 检查源路径是否存在
	srcInfo, err := os.Stat(tool.Path)
	if err != nil {
		return fmt.Errorf("source path does not exist: %s", tool.Path)
	}
	if !srcInfo.IsDir() {
		return fmt.Errorf("source path is not a directory: %s", tool.Path)
	}

	// 确保目标父目录存在
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return fmt.Errorf("failed to create target directory: %v", err)
	}

	// 创建 junction：targetPath -> tool.Path
	if err := util.CreateJunction(targetPath, tool.Path); err != nil {
		return fmt.Errorf("failed to create junction: %v", err)
	}

	// 更新 current junction 指向新创建的链接
	currentLink := filepath.Join(xdevDir, "current", tool.Name)
	currentDir := filepath.Join(xdevDir, "current")
	if err := os.MkdirAll(currentDir, 0o755); err != nil {
		return fmt.Errorf("failed to create current directory: %v", err)
	}

	// 移除旧的 current 链接
	os.Remove(currentLink)

	// 创建新的 current junction
	if err := util.CreateJunction(currentLink, targetPath); err != nil {
		return fmt.Errorf("failed to create current junction: %v", err)
	}

	return nil
}

// sanitizeVersion 清理版本号，使其适合作为目录名
func sanitizeVersion(ver string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, ver)
}
