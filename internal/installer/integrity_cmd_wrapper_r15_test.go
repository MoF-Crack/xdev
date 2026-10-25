package installer

import (
	"path/filepath"
	"testing"
)

// Round 15 守卫（真实语料驱动）：`.cmd`/`.bat` 包装脚本必须计入安装完整性。
//
// 实测来源：`xdev install android-ndk@latest` 在 Windows 上**必然失败**。
// Android NDK r29 的 windows 包（android-ndk-r29.zip，833,850,862 字节，
// sha256 与随包 manifest 一致）根部只有：
//
//	android-ndk-r29/ndk-build.cmd
//	android-ndk-r29/ndk-gdb.cmd
//	android-ndk-r29/ndk-stack.cmd
//
// 没有**无扩展名**的 ndk-build（那个在 build/ndk-build）。而 manifest 声明
// `strip: 0`，所以解包后二进制位于 <install>/android-ndk-r29/ndk-build.cmd。
// 旧候选表只覆盖 <install>/<shim>(.exe)、<install>/bin/<shim>(.exe/.cmd)、
// <install>/<sub>/bin/<shim>(...) 与 <install>/<sub>/<shim>(.exe)——
// **根部与「一层子目录根部」的 .cmd 都不在表内**，于是完整性门禁把一棵完整
// 的安装树判成 "missing binary ndk-build, ndk-stack, ndk-gdb" 并回滚。
func TestR15_IntegrityCountsCmdWrapperInNestedRoot(t *testing.T) {
	install := filepath.Join(t.TempDir(), "android-sdk", "android-ndk", "r29")
	// 精确复刻 NDK r29 windows 包的根部形态（strip: 0 → 保留 android-ndk-r29/）。
	for _, name := range []string{"ndk-build.cmd", "ndk-gdb.cmd", "ndk-stack.cmd"} {
		writeFile(t, filepath.Join(install, "android-ndk-r29", name), "@echo off\r\n")
	}
	writeFile(t, filepath.Join(install, "android-ndk-r29", "build", "ndk-build"), "")
	writeFile(t, filepath.Join(install, "android-ndk-r29", "CHANGELOG.md"), "x")

	p := &shimPlugin{name: "android-ndk", shims: []string{"ndk-build", "ndk-stack", "ndk-gdb"}}
	res := VerifyInstallIntegrity(p, install)
	if !res.OK {
		t.Fatalf("完整的 NDK 安装树被判为不完整：reason=%q missing=%v", res.Reason, res.Missing)
	}
}

// 扁平根部布局（strip: 1 / 平铺归档）同样只能靠 .cmd 命中。
func TestR15_IntegrityCountsCmdWrapperAtFlatRoot(t *testing.T) {
	install := filepath.Join(t.TempDir(), "tool", "1.0.0")
	writeFile(t, filepath.Join(install, "tool.cmd"), "@echo off\r\n")

	p := &shimPlugin{name: "tool", shims: []string{"tool"}}
	if res := VerifyInstallIntegrity(p, install); !res.OK {
		t.Fatalf("扁平根部的 tool.cmd 未被计入：reason=%q", res.Reason)
	}
}

// 反向守卫：新增候选不得让「空壳目录」假通过。
func TestR15_IntegrityStillRejectsTreeWithoutAnyShim(t *testing.T) {
	install := filepath.Join(t.TempDir(), "tool", "1.0.0")
	writeFile(t, filepath.Join(install, "README.md"), "not a binary")
	writeFile(t, filepath.Join(install, "sub", "notes.txt"), "not a binary")

	p := &shimPlugin{name: "tool", shims: []string{"tool"}}
	if res := VerifyInstallIntegrity(p, install); res.OK {
		t.Fatal("没有任何 shim 的目录被判为完整 —— 新候选过宽")
	}
}
