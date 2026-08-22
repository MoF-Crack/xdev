// Spec 034: 启动时替换 XMDevDir 根下旧版本 xdev CLI 副本（internal/update/stale.go）。
//
// 只处理 XMDevDir 根的 CLI 副本（xdev / xdev.exe）；xdev-gui 副本维持
// GUI 侧 EnsureSelfInstalled 的「缺失才复制」逻辑（GUI 二进制无轻量版本
// 探测接口，且 CLI/GUI 联动发布）。install/ 多版本目录由 installer 语义
// 刻意保留，此处一概不动。
package update

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/xmdemt/xdev/internal/toolmeta"
	"github.com/xmdemt/xdev/internal/util"
	"github.com/xmdemt/xdev/internal/version"
)

// probeVersion 探测副本二进制版本；不可执行/超时/解析失败返回 ""（调用方跳过）。
// 独立函数便于测试注入。
type probeVersionFunc func(path string) string

var defaultProbeVersion probeVersionFunc = func(path string) string {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "self", "version").CombinedOutput()
	if err != nil {
		return ""
	}
	return toolmeta.ExtractVersionFromOutput(string(out))
}

// ReplaceStaleCLICopy 探测 homeDir 根下 xdev CLI 副本版本，若旧于当前版本
// （version.Version）则用 currentExe 覆盖副本，替换前后校验 SHA-256。
//
// 安全边界（全部跳过并返回 nil，不阻塞启动）：
//   - 副本不存在或为目录（缺失场景由 EnsureSelfInstalled 复制）
//   - 副本路径与 currentExe 相同（自指）
//   - 版本探测失败（不可执行/超时/解析不出版本）
//   - 副本版本不旧于当前版本
func ReplaceStaleCLICopy(currentExe, homeDir string) error {
	return ReplaceStaleCLICopyWithProbe(currentExe, homeDir, defaultProbeVersion)
}

// ReplaceStaleCLICopyWithProbe 与 ReplaceStaleCLICopy 相同，探测函数可注入（测试）。
func ReplaceStaleCLICopyWithProbe(currentExe, homeDir string, probe probeVersionFunc) error {
	cliName := "xdev"
	if runtime.GOOS == "windows" {
		cliName = "xdev.exe"
	}
	copyPath := filepath.Join(homeDir, cliName)

	fi, err := os.Stat(copyPath)
	if err != nil || fi.IsDir() {
		return nil // 无副本/目录：缺失复制场景，交给 EnsureSelfInstalled
	}

	// 自指保护：副本与当前可执行文件相同（EvalSymlinks 解析真实路径）。
	cur, err := filepath.EvalSymlinks(currentExe)
	if err != nil {
		return nil
	}
	cp, err := filepath.EvalSymlinks(copyPath)
	if err != nil {
		return nil
	}
	if samePath(cur, cp) {
		return nil
	}

	probed := probe(copyPath)
	if probed == "" {
		return nil // 探测失败：跳过（可能是损坏副本，不能盲目覆盖）
	}
	if util.CompareSemver(probed, version.Version) >= 0 {
		return nil // 副本不旧于当前：不动
	}

	// 替换：用当前可执行文件覆盖旧副本，前后 SHA-256 校验一致。
	beforeHash, err := util.FileSHA256(currentExe)
	if err != nil {
		return fmt.Errorf("stale replace: hash current exe: %w", err)
	}
	if err := util.CopyFile(currentExe, copyPath); err != nil {
		return fmt.Errorf("stale replace: copy %s -> %s: %w", currentExe, copyPath, err)
	}
	afterHash, err := util.FileSHA256(copyPath)
	if err != nil {
		return fmt.Errorf("stale replace: hash copy: %w", err)
	}
	if beforeHash != afterHash {
		return fmt.Errorf("stale replace: hash mismatch after copy (%s)", copyPath)
	}
	return nil
}

// samePath 比较两个已解析路径是否同一文件（Windows 大小写不敏感）。
func samePath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return filepath.Clean(a) == filepath.Clean(b)
}