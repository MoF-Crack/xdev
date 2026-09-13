//go:build windows

// goroot_cleanup_windows.go implements self-healing for the stale GOROOT
// user environment variable left behind by the pre-v2 go.yaml manifest
// (audit-fixes ticket 01 / X-07).
//
// Historical bug: manifests/go.yaml used to declare `GOROOT: '{{install}}'`
// while the Go archives keep their top-level `go/` directory (strip: 0), so
// the real toolchain root is `{{install}}\go`, one level below the value that
// was written to HKCU\Environment. A GOROOT without src/ makes every
// `go build` fail with "package fmt is not in std" — on this layout the bug
// is 100% reproducible.
//
// Two things make the residue sticky:
//
//   - EnrichFromRegistry overrides the process environment of every shim
//     subprocess with HKCU\Environment values, so clearing the variable in
//     a shell is undone on the next shim invocation — the registry VALUE
//     itself must be deleted;
//   - Go >= 1.26 self-derives GOROOT from the go binary location
//     (findGOROOT walks up two levels and validates with the isGOROOT
//     probe), so once the value is gone the toolchain works again.
package env

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
)

// gorootGetFn / gorootDeleteFn / gorootBroadcastFn 是本模块的**测试接缝**：
// 生产默认指向真实的 HKCU\Environment 读写实现，测试可替换为内存假实现。
//
// 为什么必须要有这个接缝（本批第四轮复核的结论）：票 01 的守卫测试原先直接调用
// SetUserEnvVar/DeleteUserEnvVar 去写**开发者真实的 HKCU GOROOT**（只有这样才能在
// 真实注册表上验证 CleanupStaleGOROOT）。后果有两个：
//  1. 并发跑全量测试时，宿主机快照守卫会撞上另一个包"正在写真实注册表"的瞬间，
//     造成**假红**（实测两轮全量里出现两次，明细为 [env] GOROOT 由临时路径变为空）；
//  2. 更危险的是：一旦该测试进程被强杀（CI 超时、管道截断），defer 里的恢复不会执行，
//     用户真实 GOROOT 会**永久留在临时路径**上——正是 P0-17「package fmt is not in std」
//     的病因。
//
// 有了接缝，测试在内存假实现上验证同一套判定逻辑，既确定又不碰宿主。
var (
	gorootGetFn       = GetUserEnvVar
	gorootDeleteFn    = DeleteUserEnvVar
	gorootBroadcastFn = BroadcastEnvironmentChange
)

// isGOROOTDir reports whether dir looks like a real Go toolchain root,
// mirroring the isGOROOT probe used by cmd/go's findGOROOT
// (cmd/go/internal/cfg/cfg.go): the directory must contain pkg/tool.
func isGOROOTDir(dir string) bool {
	if dir == "" {
		return false
	}
	fi, err := os.Stat(filepath.Join(dir, "pkg", "tool"))
	return err == nil && fi.IsDir()
}

// CleanupStaleGOROOT removes the HKCU GOROOT value when it provably points
// one level above a real Go toolchain (the pre-v2 nested `go/` layout):
//
//	delete iff  !isGOROOTDir(V)  &&  isGOROOTDir(V + "\go")
//
// Anything else is left untouched — a valid GOROOT, a manually installed
// secondary toolchain, or a value whose origin we cannot determine are all
// kept (never use a blunt "V != {{install}} ⇒ delete" rule). Idempotent;
// no-op when GOROOT is absent.
func CleanupStaleGOROOT() error {
	v, err := gorootGetFn("GOROOT")
	if err != nil || v == "" {
		// Not set (or unreadable) — nothing to clean.
		return nil
	}
	if isGOROOTDir(v) {
		// Value already points at a real toolchain root — keep it.
		return nil
	}
	if !isGOROOTDir(filepath.Join(v, "go")) {
		// Neither V nor V\go is a GOROOT — unknown provenance, keep it.
		return nil
	}
	if err := gorootDeleteFn("GOROOT"); err != nil {
		return fmt.Errorf("deleting stale GOROOT %q from HKCU\\Environment: %w", v, err)
	}
	log.Printf("[env] removed stale GOROOT %q — the real toolchain is at %s\\go and the go command self-derives GOROOT", v, v)
	// Let new processes (Explorer-spawned terminals) pick up the change
	// immediately instead of inheriting the stale value from their parents.
	gorootBroadcastFn()
	return nil
}
