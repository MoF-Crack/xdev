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
	"log"
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
	cmd := exec.CommandContext(ctx, path, "self", "version")
	// Windows：GUI 子系统父进程（xdev 无参启动）exec 控制台子进程时会
	// 弹出黑色命令行窗口一闪而过（约 1s）——隐藏子进程控制台。
	util.HideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	return toolmeta.ExtractVersionFromOutput(string(out))
}

// ReplaceStaleCLICopy 探测 homeDir 根下 xdev CLI 副本，若与当前运行二进制
// 不一致则用 currentExe 覆盖副本，替换前后校验 SHA-256。
//
// 镜像一致性语义（冒烟反馈 #5 修复）：home 根副本的职责是「当前运行
// 二进制的镜像」——版本不同即替换。此前的 CompareSemver 判「旧于才替换」
// 在版本号断层上方向反转（旧分支 1.2.2-alpha vs 主线 0.3.3：1.2.2 > 0.3.3
// 被判「副本更新」→ 永不替换）。镜像语义同时覆盖同版本不同构建（本地重编）。
//
// 安全边界（全部跳过并返回 nil，不阻塞启动）：
//   - 副本不存在或为目录（缺失场景由 EnsureSelfInstalled 复制）
//   - 副本路径与 currentExe 相同（自指）
//   - 版本探测失败（不可执行/超时/解析不出版本——防误伤 home 根的无关 exe）
//   - 探测版本 == 当前版本（已是镜像；同版本必同构建，构建元数据不参与）
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
		return nil // 探测失败：跳过（可能是损坏副本或无关 exe，不能盲目覆盖）
	}
	if probed == version.Version {
		return nil // 已是当前版本的镜像：不动
	}

	// 替换：用当前可执行文件覆盖旧副本（票 32 / P1-26 / E-07）。
	// 旧实现直接用 util.CopyFile 覆盖写 copyPath：写失败/中断会留下半截
	// 文件，而半截副本在下次启动探测失败 → 永久跳过，损坏状态无法自愈。
	// 现在先写唯名临时文件并校验 SHA-256，通过后才 rename 覆盖；
	// 任何失败都清理 .tmp 并删除损坏副本，使 EnsureSelfInstalled 的
	// 「缺失即复制」分支下次重拷自愈。
	beforeHash, err := util.FileSHA256(currentExe)
	if err != nil {
		return fmt.Errorf("stale replace: hash current exe: %w", err)
	}
	if err := replaceCopyAtomic(currentExe, copyPath, beforeHash); err != nil {
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

// replaceCopyAtomic publishes src at dst atomically after verifying the bytes
// against wantSHA (SHA-256 of src).
//
// 票 32（P1-26 / E-07）：
//  1. 写 dst 同目录的唯名临时文件（os.CreateTemp，票 31/P1-24 已确立
//     「固定 <path>.tmp 会被并发写者互相截断」的教训）；
//  2. 校验临时文件的 SHA-256 == wantSHA（不匹配视为损坏，不发布）；
//  3. 校验通过才 os.Rename 覆盖 dst（原子发布点）。
//
// 任一环节失败：删除 .tmp，并删除已损坏的 dst（仅普通文件，绝不碰目录），
// 让 EnsureSelfInstalled 的下一次调用重新拷贝，而不是把损坏副本留成永久状态。
func replaceCopyAtomic(src, dst, wantSHA string) error {
	dir := filepath.Dir(dst)
	tmp, err := os.CreateTemp(dir, filepath.Base(dst)+".tmp-*")
	if err != nil {
		removeDamagedCopy(dst)
		return fmt.Errorf("create temp in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	tmp.Close()

	if err := util.CopyFile(src, tmpName); err != nil {
		os.Remove(tmpName)
		removeDamagedCopy(dst)
		return err
	}
	// CreateTemp 建出的是 0600，CopyFile 对已存在文件不改权限——必须显式
	// 还原可执行位，否则 home 根的 CLI 副本将不可执行（源文件权限为准）。
	perm := os.FileMode(0o755)
	if fi, statErr := os.Stat(src); statErr == nil {
		perm = fi.Mode().Perm()
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		os.Remove(tmpName)
		removeDamagedCopy(dst)
		return fmt.Errorf("chmod temp copy: %w", err)
	}
	got, err := util.FileSHA256(tmpName)
	if err != nil {
		os.Remove(tmpName)
		removeDamagedCopy(dst)
		return fmt.Errorf("hash temp copy: %w", err)
	}
	if wantSHA != "" && !strings.EqualFold(got, wantSHA) {
		os.Remove(tmpName)
		removeDamagedCopy(dst)
		return fmt.Errorf("hash mismatch for %s: want %s, got %s", dst, wantSHA, got)
	}
	if err := os.Rename(tmpName, dst); err != nil {
		os.Remove(tmpName)
		removeDamagedCopy(dst)
		return fmt.Errorf("rename %s -> %s: %w", tmpName, dst, err)
	}
	return nil
}

// removeDamagedCopy deletes a damaged CLI copy so the next
// EnsureSelfInstalled call re-copies it.  Directories are never removed.
func removeDamagedCopy(path string) {
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() {
		return
	}
	if err := os.Remove(path); err != nil {
		log.Printf("[update] WARNING: remove damaged copy %s: %v", path, err)
	}
}

// samePath 比较两个已解析路径是否同一文件（Windows 大小写不敏感）。
func samePath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return filepath.Clean(a) == filepath.Clean(b)
}