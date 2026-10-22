package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// 票 40（P2-18 / E-14）：doctor 的 shims 检查必须来自磁盘事实
// ---------------------------------------------------------------------------
//
// 旧实现写死 shimOK=true：shim 目录被删掉/权限不足时 doctor 依然报 ✔，
// 安装树残迹因此更不可见。新实现由 os.ReadDir 推导，但「零 shim」必须
// 保持 info 语义——doctor 的退出码被首跑体验与 CI 断言依赖，新装用户
// （shim 目录为空）不能因此变成失败。
//
// 这些用例只驱动纯函数 shimDirCheck：绝不构造/执行 cobra 命令，避免
// 进程级 OnInitialize 把配置解析回真实 home。

// TestShimDirCheck_EmptyDirIsInfoNotFailure 新装用户：目录存在但为空 →
// 通过（info），消息说明「尚无 shim」。
func TestShimDirCheck_EmptyDirIsInfoNotFailure(t *testing.T) {
	dir := t.TempDir()

	ok, msg := shimDirCheck(dir)
	if !ok {
		t.Errorf("shimDirCheck(空目录) ok = false, want true（零 shim 不是故障，退出码不得变化）: %s", msg)
	}
	if !strings.Contains(msg, "no shims installed yet") {
		t.Errorf("消息缺少 info 语义: %q", msg)
	}
}

// TestShimDirCheck_MissingDirIsFailure 目录不可读 → 失败并给出原因。
func TestShimDirCheck_MissingDirIsFailure(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does-not-exist")

	ok, msg := shimDirCheck(dir)
	if ok {
		t.Errorf("shimDirCheck(不存在的目录) ok = true, want false（旧实现恒真的直接回归点）: %s", msg)
	}
	if !strings.Contains(msg, dir) || !strings.Contains(msg, "cannot read shim dir") {
		t.Errorf("失败消息未带路径与原因: %q", msg)
	}
}

// TestShimDirCheck_NonDirPathIsFailure 路径存在但不是目录（同名文件）→ 失败。
func TestShimDirCheck_NonDirPathIsFailure(t *testing.T) {
	file := filepath.Join(t.TempDir(), "shims-is-a-file")
	if err := os.WriteFile(file, []byte("not a dir"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	// Windows 上 ReadDir 对普通文件返回错误；POSIX 上返回 ENOTDIR。
	if ok, msg := shimDirCheck(file); ok {
		t.Errorf("shimDirCheck(普通文件) ok = true, want false: %s", msg)
	}
}

// TestShimDirCheck_CountsInstalledShims 目录非空 → 通过并报告数量。
func TestShimDirCheck_CountsInstalledShims(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"go.exe", "node.exe", "python.exe"} {
		shim := filepath.Join(dir, name)
		if runtime.GOOS != "windows" {
			shim = filepath.Join(dir, strings.TrimSuffix(name, ".exe"))
		}
		if err := os.WriteFile(shim, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
	}

	ok, msg := shimDirCheck(dir)
	if !ok {
		t.Errorf("shimDirCheck(3 个 shim) ok = false, want true: %s", msg)
	}
	if !strings.Contains(msg, "3 shim(s) installed") {
		t.Errorf("消息未报告数量: %q", msg)
	}
}
