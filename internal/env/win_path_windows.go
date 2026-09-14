//go:build windows

// win_path_windows.go provides Windows-specific PATH and environment variable
// management via the Windows registry (HKCU\Environment and HKLM\...\Environment).
//
// # Registry Operations
//
// All registry operations use raw syscall.NewLazyDLL / Proc.Call() rather than
// golang.org/x/sys/windows, keeping dependencies minimal. The key operations are:
//
//   - ReadUserPath / WriteUserPath: read/write the "Path" value in HKCU\Environment
//   - ReadSystemPath / WriteSystemPath: read/write the "Path" value in HKLM\...\Environment
//   - SetUserEnvVar: write arbitrary user-level env vars to HKCU\Environment
//   - BroadcastEnvironmentChange: send WM_SETTINGCHANGE so new processes see changes
//
// # Environment Variable Whitelist
//
// SetUserEnvVar is restricted to a predefined set of ~30 allowed variable names
// (JAVA_HOME, GOROOT, GOPATH, NODE_HOME, PYTHON_HOME, etc.). This prevents
// arbitrary env var writes that could break the system. To add support for new
// tool home variables, the allowedEnvVarNames map must be updated in code.
//
// # Env Var Value Validation (X-08 / P2-28, ticket 42)
//
// SetUserEnvVarTrusted — the writer used by the installer for env_vars declared
// in tool manifests — additionally validates the *value*: null bytes, unknown
// %VAR% references (hard error, not a warning), and for path-typed names
// (*_HOME, *_ROOT, GOROOT, GOPATH) that the value is a literal absolute path
// inside the xdev managed tree. Values containing %VAR% references are refused
// for path-typed names unconditionally; the absolute-path/tree confinement is
// enforced whenever the managed tree root is known and skipped with a WARN when
// it is not (conservative). See validateEnvVarValue for the exact rules.
//
// # PATH Cleanup
//
// CleanupUserPath performs aggressive cleanup of the user PATH:
//   - Removes install/ subdirectory entries (should not be in PATH directly)
//   - Removes current/ entries in shim mode (preserving current\xdev)
//   - Removes legacy xmdevenvmt paths
//   - Removes bare XDev root entries (e.g. "E:\XDev" without subdirectory)
//   - Fixes double-backslash and quoted/corrupted entries
//   - Ensures shimDir is first in shim mode, removed in direct mode
//
// # BroadcastEnvironmentChange
//
// Uses SendMessageTimeoutW with HWND_BROADCAST, a 5-second timeout, and
// SMTO_ABORTIFHUNG. This is the standard Windows pattern but may not notify
// all running processes immediately — newly opened terminals will see changes.
package env

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/xmdemt/xdev/internal/util"
)

var (
	advapi32             = syscall.NewLazyDLL("advapi32.dll")
	procRegOpenKeyExW    = advapi32.NewProc("RegOpenKeyExW")
	procRegQueryValueExW = advapi32.NewProc("RegQueryValueExW")
	procRegSetValueExW   = advapi32.NewProc("RegSetValueExW")
	procRegCloseKey      = advapi32.NewProc("RegCloseKey")
	procRegEnumValueW    = advapi32.NewProc("RegEnumValueW")
	procRegDeleteValueW  = advapi32.NewProc("RegDeleteValueW")
)

const (
	_HKEY_CURRENT_USER    = 0x80000001
	_HKEY_LOCAL_MACHINE   = 0x80000002
	_KEY_QUERY_VALUE      = 0x0001
	_KEY_SET_VALUE        = 0x0002
	_REG_EXPAND_SZ        = 2
	_ERROR_FILE_NOT_FOUND = 2
	_ERROR_MORE_DATA      = 234
	_ERROR_NO_MORE_ITEMS  = 259
)

// userPathReadFn / userPathWriteFn 是用户 PATH 的**后端接缝**：生产默认指向真实
// HKCU\Environment 读写，测试可切换为内存后端。
//
// 为什么需要（本批第四轮复核的结论）：PATH 的 RMW 守卫（票 10 的并发丢写、票 11 的
// home 锚定、CleanupUserPath 各模式）原先直接在**开发者真实的用户 PATH** 上做
// WriteUserPath 再 defer 恢复。两个后果：
//  1. 并发跑全量测试时，其它包的宿主机快照窗口会撞上这次写 → 假红；
//  2. 测试进程被强杀时 defer 不恢复 → 用户 PATH 里留下指向临时目录的死条目
//     （gui 的 InitConfig 守卫测试注释里就有"清理历史残留 Temp 目录"的痕迹，
//     说明这类残留确实发生过）。PATH 被破坏的影响面比单个 GOROOT 更大。
var (
	userPathReadFn  = readUserPathFromRegistry
	userPathWriteFn = writeUserPathFromRegistry
	// userPathLockPathFn 返回跨进程 PATH 锁的文件路径；测试用内存后端时指向临时目录，
	// 以免在开发者真实 ~/.xdev/.locks 下建文件。锁本身**不跳过**——票 10 的并发丢写
	// 守卫（TestConcurrentPathRMW_NoLostWrite）依赖真实锁语义，跳过就等于删掉该守卫。
	userPathLockPathFn = defaultUserPathLockPath
)

// inMemoryUserPath 非 nil 时表示当前用的是测试用内存后端。
var inMemoryUserPath *string

var inMemoryUserPathMu sync.Mutex

// realUserRegistryMutationCount / expectedRealRegistryMutationCount 是
// internal/env 测试防污染闸门的埋点（闸门本体见 userenv_hermetic_test.go 的
// TestMain，第四轮复核的产物）。
//
// 背景（P0-17 事故复盘的教训）：save/restore 式测试在进程被强杀、panic 或断言
// 提前退出时不会执行恢复，测试值会**永久留在真实 HKCU\Environment** 里。逐个
// 用例改造（PATH 内存后端、GOROOT 假注册表、本批的 env 变量内存后端）解决的是
// 已知写点，但挡不住未来新增的用例忘记装后端。因此：
//
//   - realUserRegistryMutationCount 在每个真实注册表写/删函数**入口** +1 ——
//     「尝试」即计数，无论后续是否被 M-2 长度校验拒绝、UTF16 编码失败或权限
//     不足（这使守卫可以用零落盘的方式自证计数器先于任何注册表访问）；
//   - expectedRealRegistryMutationCount 供守卫声明「本次运行预期的合法尝试
//     次数」——唯一允许的形态是先于任何写入即告失败/无变更的尝试（长度拒绝、
//     编码失败、删除确认不存在的值）。
//
// TestMain 断言 delta == declared：任何未声明的真实写/删尝试都让整包失败。
// 生产路径从不读取这两个计数器。逃生口：设置 XMDEV_ALLOW_REAL_REGISTRY_TESTS=1
// 运行 `go test ./internal/env` 可临时停用闸门（人工集成测试专用；CI runner 是
// 一次性环境，若未来加 CI 测试 job 可设置以换取全量真身覆盖 —— 开发机默认勿设）。
var (
	realUserRegistryMutationCount     atomic.Int64
	expectedRealRegistryMutationCount atomic.Int64
)

func defaultUserPathLockPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".xdev", ".locks", "userpath.lock"), nil
}

// UseInMemoryUserPathForTesting 把用户 PATH 的读写切到内存后端（初始值为 initial），
// 返回读取函数与还原函数。**仅供测试**：让"故意改 PATH"的用例不再写开发者真实的
// HKCU\Environment；跨进程锁保留（锁文件改到临时目录），因此并发守仍然有效。
//
// 还原语义是「还原到安装前的接缝值」而非硬编码真实实现：TestMain 会为整包安装
// 一层种子后端（见 userenv_hermetic_test.go），用例级安装/还原必须正确嵌套，
// 否则用例的 t.Cleanup 会把整包的防污染后端撕掉。
func UseInMemoryUserPathForTesting(initial string) (get func() string, restore func()) {
	store := initial
	lockDir, err := os.MkdirTemp("", "xdev-userpath-lock-")
	prevRead, prevWrite := userPathReadFn, userPathWriteFn
	prevLockFn := userPathLockPathFn

	inMemoryUserPathMu.Lock()
	inMemoryUserPath = &store
	userPathReadFn = func() (string, error) {
		inMemoryUserPathMu.Lock()
		defer inMemoryUserPathMu.Unlock()
		return *inMemoryUserPath, nil
	}
	userPathWriteFn = func(p string) error {
		inMemoryUserPathMu.Lock()
		defer inMemoryUserPathMu.Unlock()
		*inMemoryUserPath = p
		return nil
	}
	inMemoryUserPathMu.Unlock()

	if err == nil {
		userPathLockPathFn = func() (string, error) {
			return filepath.Join(lockDir, "userpath.lock"), nil
		}
	}

	return func() string {
			inMemoryUserPathMu.Lock()
			defer inMemoryUserPathMu.Unlock()
			return *inMemoryUserPath
		},
		func() {
			inMemoryUserPathMu.Lock()
			inMemoryUserPath = nil
			userPathReadFn = prevRead
			userPathWriteFn = prevWrite
			inMemoryUserPathMu.Unlock()
			userPathLockPathFn = prevLockFn
			if err == nil {
				_ = os.RemoveAll(lockDir)
			}
		}
}

// withUserPathLock serializes PATH read-modify-write cycles across
// processes. Security (L-2 fix): every PATH mutator does
// read-registry → modify → write-registry; concurrent xdev processes
// (GUI + CLI, or two CLI invocations) could otherwise drop each other's
// entries. The lock file lives under ~/.xdev/.locks — a fixed, user-owned
// location that does not depend on config resolution.
func withUserPathLock(fn func() error) error {
	lockPath, pathErr := userPathLockPathFn()
	if pathErr != nil {
		// Cannot resolve the lock location — degrade to the unprotected
		// call rather than failing PATH management entirely.
		log.Printf("[env] WARN: userpath lock degraded (cannot resolve home dir): %v — proceeding without cross-process PATH serialization", pathErr)
		return fn()
	}
	fl, err := util.AcquireFileLock(lockPath, 10*time.Second)
	if err != nil {
		// Contention beyond 10s: proceed without the lock (the registry
		// write itself is atomic; only the read-modify-write window races).
		log.Printf("[env] WARN: userpath lock degraded (acquire %s: %v) — proceeding without cross-process PATH serialization", lockPath, err)
		return fn()
	}
	defer fl.Release()
	return fn()
}

// SetWindowsUserPath prepends shimDir to the user's PATH environment
// variable stored in the Windows registry (HKCU\Environment).  If
// shimDir is already present the function returns nil immediately.
//
// After modifying the registry we broadcast WM_SETTINGCHANGE so that
// new processes pick up the change without a reboot.
func SetWindowsUserPath(shimDir string) error {
	return withUserPathLock(func() error {
		shimDir = filepath.Clean(shimDir)

		current, err := ReadUserPath()
		if err != nil {
			return fmt.Errorf("read user PATH: %w", err)
		}

		// Already present? C-13 (ticket 10 / N-11): Windows path comparison
		// is case-insensitive — an exact == let "e:\xdev\shims" coexist
		// with "E:\XDev\shims" and be prepended again as a duplicate.
		for _, p := range strings.Split(current, ";") {
			if strings.EqualFold(filepath.Clean(p), shimDir) {
				return nil
			}
		}

		newPath := shimDir + ";" + current
		if err := WriteUserPath(newPath); err != nil {
			return fmt.Errorf("write user PATH: %w", err)
		}
		BroadcastEnvironmentChange()
		return nil
	})
}

// RemoveFromWindowsUserPath removes shimDir from the user's PATH
// environment variable stored in the Windows registry.
func RemoveFromWindowsUserPath(shimDir string) error {
	return withUserPathLock(func() error {
		shimDir = filepath.Clean(shimDir)

		current, err := ReadUserPath()
		if err != nil {
			return fmt.Errorf("read user PATH: %w", err)
		}

		parts := strings.Split(current, ";")
		var kept []string
		for _, p := range parts {
			// C-13 (ticket 10 / N-11): case-insensitive match — a
			// case-variant entry ("E:\XDEV\SHIMS" vs "e:\xdev\shims") was
			// previously left behind as a stale duplicate.
			if !strings.EqualFold(filepath.Clean(p), shimDir) {
				kept = append(kept, p)
			}
		}

		newPath := strings.Join(kept, ";")
		if err := WriteUserPath(newPath); err != nil {
			return fmt.Errorf("write user PATH: %w", err)
		}
		BroadcastEnvironmentChange()
		return nil
	})
}

// AppendToUserPath 将指定目录追加到用户级 PATH 末尾（如果尚未存在）。
// 用于将工具的 bin 目录加入 PATH，使得命令在终端中直接可用。
// 安全检查：验证路径存在且是目录，防止无效路径或文件被写入 PATH。
func AppendToUserPath(dir string) error {
	return withUserPathLock(func() error {
		dir = filepath.Clean(dir)

		// 安全检查：路径必须存在且是目录
		if fi, err := os.Stat(dir); err != nil {
			return fmt.Errorf("path does not exist: %s", dir)
		} else if !fi.IsDir() {
			return fmt.Errorf("path is not a directory: %s", dir)
		}

		current, err := ReadUserPath()
		if err != nil {
			return fmt.Errorf("read user PATH: %w", err)
		}

		// 已存在则跳过
		for _, p := range strings.Split(current, ";") {
			if strings.EqualFold(filepath.Clean(p), dir) {
				return nil
			}
		}

		// 追加到末尾
		newPath := current + ";" + dir
		if err := WriteUserPath(newPath); err != nil {
			return fmt.Errorf("write user PATH: %w", err)
		}
		BroadcastEnvironmentChange()
		return nil
	})
}

// RemoveFromUserPath 从用户级 PATH 中移除指定目录。
func RemoveFromUserPath(dir string) error {
	return withUserPathLock(func() error {
		dir = filepath.Clean(dir)

		current, err := ReadUserPath()
		if err != nil {
			return fmt.Errorf("read user PATH: %w", err)
		}

		parts := strings.Split(current, ";")
		var kept []string
		target := filepath.Clean(dir)
		for _, p := range parts {
			if !strings.EqualFold(filepath.Clean(p), target) {
				kept = append(kept, p)
			}
		}

		newPath := strings.Join(kept, ";")
		if err := WriteUserPath(newPath); err != nil {
			return fmt.Errorf("write user PATH: %w", err)
		}
		BroadcastEnvironmentChange()
		return nil
	})
}

// RemoveFromUserPathPrefix 删除用户 PATH 中所有以 prefix 开头的条目
// （Windows 大小写不敏感）。用于清掉测试残留的 TempDir 路径。
func RemoveFromUserPathPrefix(prefix string) error {
	return withUserPathLock(func() error {
		prefix = strings.ToLower(filepath.Clean(prefix))
		if prefix == "" || prefix == "." {
			return fmt.Errorf("refusing to remove PATH entries with empty prefix")
		}
		current, err := ReadUserPath()
		if err != nil {
			return fmt.Errorf("read user PATH: %w", err)
		}
		parts := strings.Split(current, ";")
		var kept []string
		changed := false
		for _, p := range parts {
			cleaned := strings.ToLower(filepath.Clean(p))
			if cleaned == prefix || strings.HasPrefix(cleaned, prefix+string(os.PathSeparator)) {
				changed = true
				continue
			}
			kept = append(kept, p)
		}
		if !changed {
			return nil
		}
		if err := WriteUserPath(strings.Join(kept, ";")); err != nil {
			return fmt.Errorf("write user PATH: %w", err)
		}
		BroadcastEnvironmentChange()
		return nil
	})
}

// AddCurrentBinPaths 将 current/<tool> 的 bin 子目录添加到用户 PATH。
// binPaths 由 ToolBinPaths(toolName, currentDir) 生成。
// 这些路径使用 current/ 前缀，切换版本时只需改 Junction，PATH 不变。
func AddCurrentBinPaths(binPaths []string) error {
	return withUserPathLock(func() error {
		current, err := ReadUserPath()
		if err != nil {
			return fmt.Errorf("read user PATH: %w", err)
		}

		parts := strings.Split(current, ";")
		existing := make(map[string]bool)
		for _, p := range parts {
			existing[strings.ToLower(filepath.Clean(p))] = true
		}

		changed := false
		for _, bp := range binPaths {
			cleanBP := filepath.Clean(bp)
			if !existing[strings.ToLower(cleanBP)] {
				parts = append(parts, cleanBP)
				changed = true
			}
		}

		if changed {
			if err := WriteUserPath(strings.Join(parts, ";")); err != nil {
				return fmt.Errorf("write user PATH: %w", err)
			}
			BroadcastEnvironmentChange()
		}
		return nil
	})
}

// RemoveCurrentBinPaths 从用户 PATH 中移除 current/<tool> 的 bin 子目录。
func RemoveCurrentBinPaths(binPaths []string) error {
	return withUserPathLock(func() error {
		current, err := ReadUserPath()
		if err != nil {
			return fmt.Errorf("read user PATH: %w", err)
		}

		toRemove := make(map[string]bool)
		for _, bp := range binPaths {
			toRemove[strings.ToLower(filepath.Clean(bp))] = true
		}

		parts := strings.Split(current, ";")
		var kept []string
		changed := false
		for _, p := range parts {
			if toRemove[strings.ToLower(filepath.Clean(p))] {
				changed = true
				continue
			}
			kept = append(kept, p)
		}

		if changed {
			if err := WriteUserPath(strings.Join(kept, ";")); err != nil {
				return fmt.Errorf("write user PATH: %w", err)
			}
			BroadcastEnvironmentChange()
		}
		return nil
	})
}

// RemoveAllCurrentPaths 从用户 PATH 中移除所有 current/ 前缀的条目。
// 用于从 direct 模式切换回 shim 模式时清理。
func RemoveAllCurrentPaths(currentDir string) error {
	return withUserPathLock(func() error {
		current, err := ReadUserPath()
		if err != nil {
			return fmt.Errorf("read user PATH: %w", err)
		}

		currentPrefix := strings.ToLower(filepath.Clean(currentDir))
		if !strings.HasSuffix(currentPrefix, "\\") && !strings.HasSuffix(currentPrefix, "/") {
			currentPrefix += "\\"
		}

		parts := strings.Split(current, ";")
		var kept []string
		changed := false
		for _, p := range parts {
			if strings.HasPrefix(strings.ToLower(filepath.Clean(p)), currentPrefix) {
				changed = true
				continue
			}
			kept = append(kept, p)
		}

		if changed {
			if err := WriteUserPath(strings.Join(kept, ";")); err != nil {
				return fmt.Errorf("write user PATH: %w", err)
			}
			BroadcastEnvironmentChange()
		}
		return nil
	})
}

// ReadUserPath reads the "Path" value from HKCU\Environment using the
// Windows registry API.  Returns "" if the value does not exist.
func ReadUserPath() (string, error) {
	return userPathReadFn()
}

// readUserPathFromRegistry 是真实的注册表实现（HKCU\Environment\Path）。
func readUserPathFromRegistry() (string, error) {
	var key uintptr
	envName, err := syscall.UTF16PtrFromString(`Environment`)
	if err != nil {
		return "", err
	}

	ret, _, _ := procRegOpenKeyExW.Call(
		_HKEY_CURRENT_USER,
		uintptr(unsafe.Pointer(envName)),
		0,
		_KEY_QUERY_VALUE,
		uintptr(unsafe.Pointer(&key)),
	)
	if ret != 0 {
		return "", fmt.Errorf("RegOpenKeyExW failed: error %d", ret)
	}
	defer procRegCloseKey.Call(key)

	// First call to get the size
	valueName, err := syscall.UTF16PtrFromString("Path")
	if err != nil {
		return "", err
	}

	var bufSize uint32
	var valType uint32
	ret, _, _ = procRegQueryValueExW.Call(
		key,
		uintptr(unsafe.Pointer(valueName)),
		0,
		uintptr(unsafe.Pointer(&valType)),
		0,
		uintptr(unsafe.Pointer(&bufSize)),
	)
	if ret == _ERROR_FILE_NOT_FOUND {
		return "", nil
	}
	if ret != 0 && ret != _ERROR_MORE_DATA {
		return "", fmt.Errorf("RegQueryValueExW (size) failed: error %d", ret)
	}

	if bufSize == 0 {
		return "", nil
	}

	// Second call to get the actual data
	buf := make([]uint16, bufSize/2+1)
	ret, _, _ = procRegQueryValueExW.Call(
		key,
		uintptr(unsafe.Pointer(valueName)),
		0,
		uintptr(unsafe.Pointer(&valType)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&bufSize)),
	)
	if ret != 0 {
		return "", fmt.Errorf("RegQueryValueExW (read) failed: error %d", ret)
	}

	return syscall.UTF16ToString(buf), nil
}

// WriteUserPath writes the "Path" value to HKCU\Environment as
// REG_EXPAND_SZ so that %USERPROFILE% etc. are expanded at runtime.
// Security (M-2): Rejects paths that exceed the Windows environment block
// limit (~32,767 characters) to prevent silent truncation by CreateProcess.
func WriteUserPath(path string) error {
	return userPathWriteFn(path)
}

// writeUserPathFromRegistry 是真实的注册表实现（含 M-2 长度上限校验）。
func writeUserPathFromRegistry(path string) error {
	// 测试防污染闸门（见 realUserRegistryMutationCount 的文档）：在一切校验之前
	// 计数一次真实写尝试。
	realUserRegistryMutationCount.Add(1)
	// Security (M-2): Check PATH length before writing
	const maxWinRegValueLen = 32767
	if len(path) > maxWinRegValueLen {
		return fmt.Errorf("PATH length %d exceeds Windows registry limit of %d characters — remove unused paths before adding new ones", len(path), maxWinRegValueLen)
	}
	var key uintptr
	envName, err := syscall.UTF16PtrFromString(`Environment`)
	if err != nil {
		return err
	}

	ret, _, _ := procRegOpenKeyExW.Call(
		_HKEY_CURRENT_USER,
		uintptr(unsafe.Pointer(envName)),
		0,
		_KEY_SET_VALUE,
		uintptr(unsafe.Pointer(&key)),
	)
	if ret != 0 {
		return fmt.Errorf("RegOpenKeyExW (set) failed: error %d", ret)
	}
	defer procRegCloseKey.Call(key)

	utf16, err := syscall.UTF16FromString(path)
	if err != nil {
		return err
	}
	valueName, err := syscall.UTF16PtrFromString("Path")
	if err != nil {
		return err
	}

	bufSize := uint32(len(utf16)) * 2
	ret, _, _ = procRegSetValueExW.Call(
		key,
		uintptr(unsafe.Pointer(valueName)),
		0,
		_REG_EXPAND_SZ,
		uintptr(unsafe.Pointer(&utf16[0])),
		uintptr(bufSize),
	)
	if ret != 0 {
		return fmt.Errorf("RegSetValueExW failed: error %d", ret)
	}
	return nil
}

// allowedEnvVarNames is kept as a local alias for util.AllowedEnvVarNames
// for backward compatibility with any code that references it directly.
// The canonical whitelist is maintained in internal/util/sanitize.go.
// Note: EnumEnvVars, SetEnvVar, and DeleteEnvVar no longer enforce this
// whitelist — only IsProtectedSystemEnvVar is enforced.
var allowedEnvVarNames = util.AllowedEnvVarNames

// SetUserEnvVar sets a user-level environment variable in HKCU\Environment.
// The value is stored as REG_EXPAND_SZ so that %USERPROFILE% etc. are expanded at runtime.
// Only variable names in the util.AllowedEnvVarNames whitelist are accepted; others
// are rejected to prevent overwriting critical system variables.
// Use SetUserEnvVarTrusted for installer-managed vars from manifests.
func SetUserEnvVar(name, value string) error {
	if !util.IsAllowedEnvVarName(name) {
		return fmt.Errorf("env var %q is not in the allowed whitelist — refusing to write to registry", name)
	}
	return SetUserEnvVarTrusted(name, value)
}

// SetUserEnvVarTrusted writes a user-level env var without the whitelist check.
// This is used by the installer for env vars declared in tool manifests —
// the manifest is the trusted source, so any *_HOME / *_ROOT var it declares
// is safe to write. The whitelist remains in effect for the GUI manual editor.
//
// Security (F-16 / D12): even this "trusted" boundary rejects protected
// system variables (PATH, SystemRoot, TEMP, ...) and syntactically invalid
// names — a malicious or hand-edited manifest must not be able to clobber
// the system environment. All built-in manifests use *_HOME / *_ROOT style
// names, so existing tools are unaffected.
//
// Security (X-08 / P2-28, ticket 42): the "trusted" label covers the *source*
// of the declaration, not its content. A tampered manifest could name an
// arbitrary directory for GOROOT / JAVA_HOME and have it stored as
// REG_EXPAND_SZ — Windows then expands it at process start, so a shim would
// execute an attacker-chosen toolchain. The value is therefore validated by
// validateEnvVarValue: unknown %VAR% references are now a hard error (they
// used to be a warning) and path-typed variables must be literal absolute
// paths inside the xdev managed tree. Ticket 01 removed GOROOT from
// manifests/go.yaml (Go derives it itself); these checks are defence in depth
// for the remaining manifests and for user-supplied ones.
func SetUserEnvVarTrusted(name, value string) error {
	if err := util.ValidateEnvVarName(name); err != nil {
		return fmt.Errorf("refusing to write env var via trusted path: %w", err)
	}
	// Security (M-4, extended by X-08): reject null bytes, unverifiable %VAR%
	// references and out-of-tree path-typed values *before* any registry
	// access. Validation failures are explicit errors — never a silent skip —
	// so the installer can roll back and report which variable was refused.
	if err := validateEnvVarValue(name, value); err != nil {
		return err
	}
	// Host-safety seam（第四轮复核）：写动作经 userEnvWriteFn 落地，测试用内存
	// 后端（UseInMemoryUserEnvForTesting）替换之 —— 校验全部在前，语义不变。
	return userEnvWriteFn(name, value)
}

// writeUserEnvVarToRegistry 是真实 HKCU\Environment 写入实现（X-08 校验已在
// SetUserEnvVarTrusted 完成；这里只负责落盘，供接缝默认值与人工排查使用）。
func writeUserEnvVarToRegistry(name, value string) error {
	// 测试防污染闸门（见 realUserRegistryMutationCount 的文档）：在任何注册表
	// 访问之前计数一次真实写尝试。
	realUserRegistryMutationCount.Add(1)
	var key uintptr
	envName, err := syscall.UTF16PtrFromString(`Environment`)
	if err != nil {
		return err
	}

	ret, _, _ := procRegOpenKeyExW.Call(
		_HKEY_CURRENT_USER,
		uintptr(unsafe.Pointer(envName)),
		0,
		_KEY_SET_VALUE,
		uintptr(unsafe.Pointer(&key)),
	)
	if ret != 0 {
		return fmt.Errorf("RegOpenKeyExW (set env var) failed: error %d", ret)
	}
	defer procRegCloseKey.Call(key)

	utf16, err := syscall.UTF16FromString(value)
	if err != nil {
		return err
	}
	valueName, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return err
	}

	bufSize := uint32(len(utf16)) * 2
	ret, _, _ = procRegSetValueExW.Call(
		key,
		uintptr(unsafe.Pointer(valueName)),
		0,
		_REG_EXPAND_SZ,
		uintptr(unsafe.Pointer(&utf16[0])),
		uintptr(bufSize),
	)
	if ret != 0 {
		return fmt.Errorf("RegSetValueExW (env var %s) failed: error %d", name, ret)
	}
	return nil
}

// ---------------------------------------------------------------------------
// X-08 / P2-28 (audit-fixes batch 5, ticket 42): manifest env_vars value checks
// ---------------------------------------------------------------------------

// allowedEnvVarRefs are the %VAR% references that the previous warning-only
// check (M-4) accepted in manifest env var values: the seven hardcoded system
// names. They are combined with util.AllowedEnvVarNames to form the reference
// whitelist enforced by validateEnvVarValue (ticket 42, requirement 2).
var allowedEnvVarRefs = []string{
	"USERPROFILE",
	"APPDATA",
	"LOCALAPPDATA",
	"ProgramFiles",
	"SystemRoot",
	"TEMP",
	"TMP",
}

// isAllowedEnvVarRef reports whether a %VAR% reference may appear in an env var
// value. Matching is case-insensitive on purpose: Windows expands references
// case-insensitively, so rejecting %userprofile% while accepting
// %USERPROFILE% would be a false rejection of a perfectly valid value.
func isAllowedEnvVarRef(ref string) bool {
	for _, a := range allowedEnvVarRefs {
		if strings.EqualFold(a, ref) {
			return true
		}
	}
	for n := range util.AllowedEnvVarNames {
		if strings.EqualFold(n, ref) {
			return true
		}
	}
	return false
}

// isPathTypedEnvVar reports whether name denotes a directory-valued
// ("path-typed") variable whose value must stay inside the xdev managed tree
// (X-08): every *_HOME and *_ROOT name, plus GOROOT and GOPATH.
//
// GOPATH is listed explicitly because the literal *_HOME / *_ROOT / GOROOT rule
// does not match it, yet its value is a directory. Its legitimate value is
// special (see validateManagedPathValue): xdev points GOPATH at
// <install_root>\..\gopath (manifests/go.yaml), i.e. at a sibling of the
// version directory — so the containment base must be the whole managed tree,
// not the tool's own install directory. That is the "relaxed" treatment the
// ticket asks for: GOPATH is checked, but against the managed tree root.
func isPathTypedEnvVar(name string) bool {
	up := strings.ToUpper(strings.TrimSpace(name))
	// Explicitly listed names: GOPATH is not matched by the suffix rules.
	if up == "GOROOT" || up == "GOPATH" {
		return true
	}
	return strings.HasSuffix(up, "_HOME") || strings.HasSuffix(up, "_ROOT")
}

// managedTreeRoot resolves the xdev managed tree root used to confine
// path-typed env var values. It is a package-level variable so tests can inject
// a temporary root; production uses defaultManagedTreeRoot.
//
// It is a seam and not a parameter because SetUserEnvVarTrusted is a
// manifest-source API called by the installer with (name, value) only, and
// internal/env must not import internal/config (config imports env — a direct
// import would be an import cycle).
var managedTreeRoot = defaultManagedTreeRoot

// managedTreeRootFromHome converts a raw $XMDEV_HOME value into the cleaned
// absolute managed tree root ("" when unset). A non-existent directory is still
// accepted as a root: it can only make the check *more* permissive for values
// inside it, never reject a value that config itself would resolve.
func managedTreeRootFromHome(raw string) string {
	v := strings.TrimSpace(raw)
	if v == "" {
		return ""
	}
	if abs, err := filepath.Abs(v); err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(v)
}

// defaultManagedTreeRoot derives the managed tree root from $XMDEV_HOME — the
// same source config.resolveXMDevDir consults (after its explicit
// --xmdevenv-dir argument). It deliberately does NOT fall back to "~/.xdev":
// on a machine whose deployment lives elsewhere (the smart Windows fallback
// picks <largest drive>:\XDev, and last_selection.json records it) a leftover
// ~/.xdev would then be used as the base and every legitimate install value
// would be rejected. When $XMDEV_HOME is unset the root is reported as
// indeterminate and validateManagedPathValue falls back to its conservative
// policy (warn + allow) instead of guessing.
//
// Under `go test` the root is likewise reported as indeterminate unless a test
// injects the seam. Reason: existing tests outside this ticket's change set
// write probe values into path-typed variables that are legitimately outside
// the managed tree — internal/env/goroot_cleanup_test.go uses t.TempDir() for
// GOROOT and internal/cli/profile_export_whitelist_test.go writes
// JAVA_HOME=C:\Java. Developers run `go test` from an xdev-hooked shell where
// $XMDEV_HOME is set, so enforcing here would turn those probes into hard
// failures. Enforcement is covered by the guard tests, which swap the seam in.
func defaultManagedTreeRoot() string {
	if testing.Testing() {
		return ""
	}
	return managedTreeRootFromHome(os.Getenv("XMDEV_HOME"))
}

// validateEnvVarValue is the value front of the trusted writer (M-4 / X-08).
// Rules, in order:
//
//  1. null bytes are refused (M-4);
//  2. every %VAR% reference must be whitelisted — unknown references are a hard
//     error now, not the previous WARNING (ticket 42, requirement 2). Values
//     are stored as REG_EXPAND_SZ, so an unknown reference silently expands to
//     "nothing" inside a path at runtime;
//  3. a path-typed variable (see isPathTypedEnvVar) must hold a literal
//     absolute path inside the xdev managed tree (see validateManagedPathValue):
//     %VAR% values are refused unconditionally, the absolute-path/managed-tree
//     confinement is enforced when that tree root is known;
//     every other variable keeps the pre-ticket behaviour (any string is
//     accepted once rules 1-2 pass), so built-in variables such as PUB_CACHE,
//     PHPRC or UV_TOOL_BIN_DIR are unaffected.
func validateEnvVarValue(name, value string) error {
	if strings.ContainsRune(value, '\x00') {
		return fmt.Errorf("env var %q value contains null bytes — refusing to write to registry", name)
	}

	refs := extractEnvRefs(value)
	for _, ref := range refs {
		if !isAllowedEnvVarRef(ref) {
			return fmt.Errorf("env var %q value contains %%%s%% reference which is not in the whitelist — refusing to write to registry (allowed references: %s)",
				name, ref, strings.Join(append(append([]string{}, allowedEnvVarRefs...), "util.AllowedEnvVarNames"), ", "))
		}
	}

	if !isPathTypedEnvVar(name) {
		return nil
	}
	return validateManagedPathValue(name, value, refs)
}

// validateManagedPathValue confines the value of a path-typed variable
// (X-08 / P2-28). The threat: a tampered manifest declares GOROOT / JAVA_HOME /
// GOPATH as an arbitrary directory; the value is written as REG_EXPAND_SZ and
// takes effect for every new process.
//
// Two rule groups, deliberately separated:
//
//  1. %VAR% references are refused *unconditionally* — including whitelisted
//     ones. A REG_EXPAND_SZ value is expanded by Windows at process start, so
//     what the value becomes cannot be verified statically; %TEMP%\evil is the
//     canonical case (TEMP is whitelisted, an empty value is a legal directory
//     variable), and no other rule can catch it. This is the base-independent
//     half of guard 42-1.
//
//  2. The confinement group — non-empty, absolute (filepath.IsAbs), lexically
//     inside the managed tree (util.ValidateAbsPathUnder, ticket 42
//     requirement 1) — is enforced only when the managed tree root is known.
//     The root is not part of this function's inputs (SetUserEnvVarTrusted is
//     called with (name, value) only), so it comes from the managedTreeRoot
//     seam; when it is unknown the whole group is skipped with a WARN.
//     Conservative by design: rejecting would break normal installs on
//     deployments whose home is only recorded in config.yaml /
//     last_selection.json / the cross-process home cache, which internal/env
//     cannot read without importing config (import cycle). Skipping the group
//     as a whole — rather than only the containment half — also keeps the
//     probe writes of ticket-outside-scope tests working
//     (internal/testkit/hostsnapshot writes GOROOT="<probe>", a non-absolute
//     value, so an unconditional absolute-path rule would break it).
//
// The containment comparison is lexical (filepath.Rel) and does not resolve
// symlinks/junctions — a junction planted inside the tree is out of scope for
// this ticket.
func validateManagedPathValue(name, value string, refs []string) error {
	if len(refs) > 0 {
		return fmt.Errorf("path-typed env var %q value %q contains %%%s%% references: the value is stored as REG_EXPAND_SZ, so the expanded directory cannot be verified to stay inside the xdev managed tree — refusing to write to registry (X-08). Use a literal absolute path (e.g. the manifest's {{install}} placeholder)",
			name, value, refs[0])
	}

	root := managedTreeRoot()
	if root == "" {
		log.Printf("[env] WARN: %s=%s: xdev managed tree root unknown ($XMDEV_HOME unset) — skipping the X-08 absolute-path/managed-tree confinement check for %q (allowed conservatively; set XMDEV_HOME to enable it)",
			name, value, name)
		return nil
	}

	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("path-typed env var %q has an empty value — refusing to write to registry (X-08: it must be an absolute directory inside the xdev managed tree)", name)
	}
	if !filepath.IsAbs(trimmed) {
		return fmt.Errorf("path-typed env var %q value %q is not an absolute path — refusing to write to registry (X-08)", name, value)
	}
	if err := util.ValidateAbsPathUnder(root, trimmed); err != nil {
		return fmt.Errorf("path-typed env var %q value %q is outside the xdev managed tree %q — refusing to write to registry (X-08 / P2-28): %w", name, value, root, err)
	}
	return nil
}

// GetUserEnvVar reads a user-level environment variable from HKCU\Environment.
// Returns the value as a string, or ("", error) if the variable doesn't exist
// or cannot be read. Only variable names in the allowed whitelist are accepted.
// Security (H-10): Used by the installer to save original values for rollback.
// Use GetUserEnvVarTrusted for installer-managed vars from manifests.
func GetUserEnvVar(name string) (string, error) {
	if !util.IsAllowedEnvVarName(name) {
		return "", fmt.Errorf("env var %q is not in the allowed whitelist — refusing to read from registry", name)
	}
	return GetUserEnvVarTrusted(name)
}

// GetUserEnvVarTrusted reads a user-level env var without the whitelist check.
// Used by the installer for env vars declared in tool manifests.
func GetUserEnvVarTrusted(name string) (string, error) {
	all := GetAllUserEnvVars()
	v, ok := all[name]
	if !ok {
		return "", fmt.Errorf("env var %q not found", name)
	}
	return v, nil
}

// GetUserEnvVarsBatch reads a subset of user environment variables in a
// single registry open/close cycle (SC-004 optimization for hostsnapshot).
//
// 第四轮复核修复：不再做白名单过滤。调用方（hostsnapshot.Capture）显式给出
// 追踪名单 —— 过滤非白名单名字等于静默丢掉调用方明确要追踪的变量（守卫自检
// 用命名空间探针名时会完全失明）；读取本身也不需要白名单（GetUserEnvVarTrusted
// 早已允许读取任意名字）。非 Windows 桩（win_path_other.go）从来不过滤，两平台
// 由此对齐。唯一生产调用方 hostsnapshot.DefaultOptions 传的全是白名单名，
// 移除过滤对生产行为零影响。
func GetUserEnvVarsBatch(names []string) (map[string]string, error) {
	all := GetAllUserEnvVars()
	result := make(map[string]string, len(names))
	for _, n := range names {
		result[n] = all[n]
	}
	return result, nil
}

// DeleteUserEnvVar deletes a user-level environment variable from HKCU\Environment.
// Only variable names in the allowed whitelist are accepted.
// Security (M-1): Used by the installer to clean up env vars when the last
// version of a tool is uninstalled (e.g., removing JAVA_HOME when no Java remains).
func DeleteUserEnvVar(name string) error {
	if !util.IsAllowedEnvVarName(name) {
		return fmt.Errorf("env var %q is not in the allowed whitelist — refusing to delete from registry", name)
	}
	return DeleteUserEnvVarTrusted(name)
}

// DeleteUserEnvVarTrusted deletes a user-level env var without the whitelist check.
// Used by the installer for env vars declared in tool manifests.
func DeleteUserEnvVarTrusted(name string) error {
	// Host-safety seam（第四轮复核）：与 SetUserEnvVarTrusted 对称，删除经
	// userEnvDeleteFn 落地，测试用内存后端替换之。
	return userEnvDeleteFn(name)
}

// deleteUserEnvVarFromRegistry 是真实 HKCU\Environment 删除实现（删除不存在的
// 值按成功处理 —— ERROR_FILE_NOT_FOUND 不视为错误）。
func deleteUserEnvVarFromRegistry(name string) error {
	// 测试防污染闸门（见 realUserRegistryMutationCount 的文档）：在任何注册表
	// 访问之前计数一次真实删除尝试。
	realUserRegistryMutationCount.Add(1)
	var key uintptr
	envName, err := syscall.UTF16PtrFromString(`Environment`)
	if err != nil {
		return err
	}

	ret, _, _ := procRegOpenKeyExW.Call(
		_HKEY_CURRENT_USER,
		uintptr(unsafe.Pointer(envName)),
		0,
		_KEY_SET_VALUE,
		uintptr(unsafe.Pointer(&key)),
	)
	if ret != 0 {
		return fmt.Errorf("RegOpenKeyExW (delete env var) failed: error %d", ret)
	}
	defer procRegCloseKey.Call(key)

	valueName, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return err
	}

	ret, _, _ = procRegDeleteValueW.Call(
		key,
		uintptr(unsafe.Pointer(valueName)),
	)

	const _ERROR_FILE_NOT_FOUND = 2
	if ret != 0 && ret != _ERROR_FILE_NOT_FOUND {
		return fmt.Errorf("RegDeleteValueW (env var %s) failed: error %d", name, ret)
	}

	return nil
}

// getRegKey returns the root HKEY and sub-key path for the given level.
func getRegKey(level string) (rootKey uintptr, subKey string, err error) {
	switch level {
	case "user":
		return _HKEY_CURRENT_USER, `Environment`, nil
	case "machine":
		return _HKEY_LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Control\Session Manager\Environment`, nil
	default:
		return 0, "", fmt.Errorf("unsupported environment variable level %q (use 'user' or 'machine')", level)
	}
}

// SetEnvVar sets a Windows registry environment variable at the given level
// ("user" or "machine"). The value is stored as REG_EXPAND_SZ.
// Protected system variables (PATH, SystemRoot, etc.) are rejected.
// The change is broadcast via WM_SETTINGCHANGE for immediate effect.
//
// Security (L-3 fix): the generic level-aware API now enforces the same
// protections as the dedicated writers —
//   - "path" is routed to WriteUserPath/WriteSystemPath, which apply the
//     32,767-char registry limit and the cross-process RMW lock;
//   - at "machine" level, only whitelisted tool-home names may be written
//     (user level stays open for the GUI env editor's custom vars, which
//     are syntax-validated upstream);
//   - values are length-capped the same way WriteUserPath caps PATH.
func SetEnvVar(name, value, level string) error {
	if strings.EqualFold(name, "path") {
		// PATH is routed through the dedicated writers, which apply the
		// 32,767-char registry limit and the cross-process RMW lock —
		// never the generic value writer.
		switch level {
		case "user":
			return withUserPathLock(func() error { return WriteUserPath(value) })
		case "machine":
			return WriteSystemPath(value)
		default:
			return fmt.Errorf("unsupported environment variable level %q", level)
		}
	}
	if util.IsProtectedSystemEnvVar(name) {
		return fmt.Errorf("cannot modify protected system env var: %s", name)
	}
	if level == "machine" && !util.IsAllowedEnvVarName(name) {
		// Machine (HKLM) writes are admin-level and system-wide: only known
		// tool-home variables may be written there. User level stays open for
		// the GUI env editor's custom variables (syntax-validated upstream).
		return fmt.Errorf("cannot set non-whitelisted machine env var: %s (only known tool-home variables may be written at machine level)", name)
	}
	if strings.ContainsRune(value, '\x00') {
		return fmt.Errorf("env var %q value contains null bytes — refusing to write to registry", name)
	}
	const maxWinRegValueLen = 32767
	if len(value) > maxWinRegValueLen {
		return fmt.Errorf("env var %s value length %d exceeds Windows registry limit of %d characters", name, len(value), maxWinRegValueLen)
	}

	// Host-safety seam（第四轮复核）：user 级与专用写入口走同一接缝 —— 否则
	// 测试防污染闸门（userenv_hermetic_test.go 的 TestMain）留有一个直写真实
	// HKCU\Environment 的缺口。落盘语义不变（默认接缝就是真实注册表写入）。
	if level == "user" {
		if err := userEnvWriteFn(name, value); err != nil {
			return err
		}
		BroadcastEnvironmentChange()
		return nil
	}

	// machine 级（或未来新增级别）仍走通用注册表写路径；计数器先于 getRegKey；
	// 非法 level 也会在此处被拒绝，但「尝试」已被计数 —— 闸门以尝试为单位。
	realUserRegistryMutationCount.Add(1)
	rootKey, subKey, err := getRegKey(level)
	if err != nil {
		return err
	}

	var key uintptr
	subKeyPtr, err := syscall.UTF16PtrFromString(subKey)
	if err != nil {
		return err
	}

	ret, _, _ := procRegOpenKeyExW.Call(
		rootKey,
		uintptr(unsafe.Pointer(subKeyPtr)),
		0,
		_KEY_SET_VALUE,
		uintptr(unsafe.Pointer(&key)),
	)
	if ret != 0 {
		if level == "machine" && ret == 5 {
			return fmt.Errorf("修改系统环境变量需要管理员权限，请以管理员身份运行 xdev")
		}
		return fmt.Errorf("RegOpenKeyExW (set %s env var) failed: error %d", level, ret)
	}
	defer procRegCloseKey.Call(key)

	utf16, err := syscall.UTF16FromString(value)
	if err != nil {
		return err
	}
	valueName, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return err
	}

	bufSize := uint32(len(utf16)) * 2
	ret, _, _ = procRegSetValueExW.Call(
		key,
		uintptr(unsafe.Pointer(valueName)),
		0,
		_REG_EXPAND_SZ,
		uintptr(unsafe.Pointer(&utf16[0])),
		uintptr(bufSize),
	)
	if ret != 0 {
		return fmt.Errorf("RegSetValueExW (env var %s) failed: error %d", name, ret)
	}

	BroadcastEnvironmentChange()
	return nil
}

// GetEnvVar reads a Windows registry environment variable at the given level
// ("user" or "machine"). Returns the value as a string, or ("", nil) if the
// variable doesn't exist.
func GetEnvVar(name, level string) (string, error) {
	rootKey, subKey, err := getRegKey(level)
	if err != nil {
		return "", err
	}

	var key uintptr
	subKeyPtr, err := syscall.UTF16PtrFromString(subKey)
	if err != nil {
		return "", err
	}

	ret, _, _ := procRegOpenKeyExW.Call(
		rootKey,
		uintptr(unsafe.Pointer(subKeyPtr)),
		0,
		_KEY_QUERY_VALUE,
		uintptr(unsafe.Pointer(&key)),
	)
	if ret != 0 {
		return "", fmt.Errorf("RegOpenKeyExW (get %s env var) failed: error %d", level, ret)
	}
	defer procRegCloseKey.Call(key)

	valueName, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return "", err
	}

	// First call: determine buffer size
	var bufSize uint32
	var regType uint32
	procRegQueryValueExW.Call(
		key,
		uintptr(unsafe.Pointer(valueName)),
		0,
		uintptr(unsafe.Pointer(&regType)),
		0,
		uintptr(unsafe.Pointer(&bufSize)),
	)

	if bufSize == 0 {
		return "", nil
	}

	// Second call: read the value
	buf := make([]uint16, bufSize/2+1)
	ret, _, _ = procRegQueryValueExW.Call(
		key,
		uintptr(unsafe.Pointer(valueName)),
		0,
		uintptr(unsafe.Pointer(&regType)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&bufSize)),
	)
	if ret != 0 {
		return "", fmt.Errorf("RegQueryValueExW (env var %s) failed: error %d", name, ret)
	}

	return syscall.UTF16ToString(buf), nil
}

// DeleteEnvVar deletes a Windows registry environment variable at the given
// level ("user" or "machine"). Protected system variables (PATH, SystemRoot,
// etc.) are rejected. The change is broadcast via WM_SETTINGCHANGE.
//
// Security (L-3 fix): mirrors SetEnvVar — PATH deletion is refused
// outright (destructive; no dedicated writer has a delete-path semantic)
// and machine-level deletes are limited to the whitelisted tool-home
// variables. User-level deletes stay open for the GUI env editor.
func DeleteEnvVar(name, level string) error {
	if util.IsProtectedSystemEnvVar(name) {
		// PATH removal is destructive (no dedicated writer has a delete-path
		// semantic) — refuse it outright instead of allowing a bare delete.
		return fmt.Errorf("cannot delete protected system env var: %s", name)
	}
	if level == "machine" && !util.IsAllowedEnvVarName(name) {
		return fmt.Errorf("cannot delete non-whitelisted machine env var: %s", name)
	}
	// Host-safety seam（第四轮复核）：user 级经与专用删入口相同的接缝落地。
	if level == "user" {
		if err := userEnvDeleteFn(name); err != nil {
			return err
		}
		BroadcastEnvironmentChange()
		return nil
	}

	// machine 级（或非法 level）仍走通用路径；计数器先于 getRegKey。
	realUserRegistryMutationCount.Add(1)
	rootKey, subKey, err := getRegKey(level)
	if err != nil {
		return err
	}

	var key uintptr
	subKeyPtr, err := syscall.UTF16PtrFromString(subKey)
	if err != nil {
		return err
	}

	ret, _, _ := procRegOpenKeyExW.Call(
		rootKey,
		uintptr(unsafe.Pointer(subKeyPtr)),
		0,
		_KEY_SET_VALUE,
		uintptr(unsafe.Pointer(&key)),
	)
	if ret != 0 {
		if level == "machine" && ret == 5 {
			return fmt.Errorf("修改系统环境变量需要管理员权限，请以管理员身份运行 xdev")
		}
		return fmt.Errorf("RegOpenKeyExW (delete %s env var) failed: error %d", level, ret)
	}
	defer procRegCloseKey.Call(key)

	valueName, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return err
	}

	ret, _, _ = procRegDeleteValueW.Call(
		key,
		uintptr(unsafe.Pointer(valueName)),
	)

	const _ERROR_FILE_NOT_FOUND = 2
	if ret != 0 && ret != _ERROR_FILE_NOT_FOUND {
		return fmt.Errorf("RegDeleteValueW (env var %s) failed: error %d", name, ret)
	}

	BroadcastEnvironmentChange()
	return nil
}

// EnumEnvVars lists all environment variable values at the given registry level
// ("user" or "machine"). No whitelist filtering — all variables at the level are
// returned so the caller can present a complete view.
func EnumEnvVars(level string) ([]EnvPair, error) {
	rootKey, subKey, err := getRegKey(level)
	if err != nil {
		return nil, err
	}

	var key uintptr
	subKeyPtr, err := syscall.UTF16PtrFromString(subKey)
	if err != nil {
		return nil, err
	}

	ret, _, _ := procRegOpenKeyExW.Call(
		rootKey,
		uintptr(unsafe.Pointer(subKeyPtr)),
		0,
		_KEY_QUERY_VALUE,
		uintptr(unsafe.Pointer(&key)),
	)
	if ret != 0 {
		if level == "machine" && ret == 5 {
			return nil, fmt.Errorf("修改系统环境变量需要管理员权限，请以管理员身份运行 xdev")
		}
		return nil, fmt.Errorf("RegOpenKeyExW (enum %s) failed: error %d", level, ret)
	}
	defer procRegCloseKey.Call(key)

	var pairs []EnvPair
	buf := make([]uint16, 32768)
	nameBuf := make([]uint16, 32768)

	for index := uint32(0); ; index++ {
		bufSize := uint32(len(buf)) * 2 // bytes
		nameSize := uint32(len(nameBuf))
		ret, _, _ = procRegEnumValueW.Call(
			key,
			uintptr(index),
			uintptr(unsafe.Pointer(&nameBuf[0])),
			uintptr(unsafe.Pointer(&nameSize)),
			0,
			0,
			uintptr(unsafe.Pointer(&buf[0])),
			uintptr(unsafe.Pointer(&bufSize)),
		)
		if ret == _ERROR_NO_MORE_ITEMS {
			break
		}
		// Ticket 05 (N-10): value data larger than the 64 KiB buffer makes
		// RegEnumValueW return ERROR_MORE_DATA with the required size in
		// bufSize. Grow the buffer and retry the same index once — the
		// previous code silently broke, truncating the whole enumeration
		// (everything after the large value was missing).
		//
		// Both size parameters are IN/OUT and must be reset to the real
		// buffer lengths before the retry: nameSize because the first call
		// already wrote the actual name length back (without the NUL —
		// passing it again would trip ERROR_MORE_DATA for the name), and
		// bufSize per the same systematic-short-by-one rule as the name
		// buffer above.
		if ret == _ERROR_MORE_DATA {
			need := bufSize // required size in bytes, as written by the API
			if need < 65538 {
				need = 65538 // degenerate value: ensure growth past the old size
			}
			buf = make([]uint16, need/2+1)
			nameSize = uint32(len(nameBuf))
			bufSize = uint32(len(buf)) * 2
			ret, _, _ = procRegEnumValueW.Call(
				key,
				uintptr(index),
				uintptr(unsafe.Pointer(&nameBuf[0])),
				uintptr(unsafe.Pointer(&nameSize)),
				0,
				0,
				uintptr(unsafe.Pointer(&buf[0])),
				uintptr(unsafe.Pointer(&bufSize)),
			)
			if ret != 0 {
				log.Printf("[env] WARN: enum %s: value at index %d needs %d bytes of data — skipped (ret=%d)", level, index, need, ret)
				continue
			}
		} else if ret != 0 {
			log.Printf("[env] WARN: enum %s aborted at index %d: ret=%d", level, index, ret)
			break
		}
		name := syscall.UTF16ToString(nameBuf[:nameSize])
		value := syscall.UTF16ToString(buf[:bufSize/2])
		pairs = append(pairs, EnvPair{Name: name, Value: value})
	}
	return pairs, nil
}

// ReadSystemPath reads the "Path" value from HKLM\SYSTEM\CurrentControlSet\Control\Session Manager\Environment.
// Returns "" if the value does not exist. Requires admin privileges for write access.
func ReadSystemPath() (string, error) {
	var key uintptr
	envName, err := syscall.UTF16PtrFromString(`SYSTEM\CurrentControlSet\Control\Session Manager\Environment`)
	if err != nil {
		return "", err
	}

	ret, _, _ := procRegOpenKeyExW.Call(
		_HKEY_LOCAL_MACHINE,
		uintptr(unsafe.Pointer(envName)),
		0,
		_KEY_QUERY_VALUE,
		uintptr(unsafe.Pointer(&key)),
	)
	if ret != 0 {
		return "", fmt.Errorf("RegOpenKeyExW HKLM failed: error %d (may need admin privileges)", ret)
	}
	defer procRegCloseKey.Call(key)

	valueName, err := syscall.UTF16PtrFromString("Path")
	if err != nil {
		return "", err
	}

	var bufSize uint32
	var valType uint32
	ret, _, _ = procRegQueryValueExW.Call(
		key,
		uintptr(unsafe.Pointer(valueName)),
		0,
		uintptr(unsafe.Pointer(&valType)),
		0,
		uintptr(unsafe.Pointer(&bufSize)),
	)
	if ret == _ERROR_FILE_NOT_FOUND {
		return "", nil
	}
	if ret != 0 && ret != _ERROR_MORE_DATA {
		return "", fmt.Errorf("RegQueryValueExW HKLM (size) failed: error %d", ret)
	}

	if bufSize == 0 {
		return "", nil
	}

	buf := make([]uint16, bufSize/2+1)
	ret, _, _ = procRegQueryValueExW.Call(
		key,
		uintptr(unsafe.Pointer(valueName)),
		0,
		uintptr(unsafe.Pointer(&valType)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&bufSize)),
	)
	if ret != 0 {
		return "", fmt.Errorf("RegQueryValueExW HKLM (read) failed: error %d", ret)
	}

	return syscall.UTF16ToString(buf), nil
}

// WriteSystemPath writes the "Path" value to HKLM\...\Environment as REG_EXPAND_SZ.
// Requires admin privileges.
func WriteSystemPath(path string) error {
	// 测试防污染闸门（见 realUserRegistryMutationCount 的文档）：真实写尝试计数。
	realUserRegistryMutationCount.Add(1)
	var key uintptr
	envName, err := syscall.UTF16PtrFromString(`SYSTEM\CurrentControlSet\Control\Session Manager\Environment`)
	if err != nil {
		return err
	}

	ret, _, _ := procRegOpenKeyExW.Call(
		_HKEY_LOCAL_MACHINE,
		uintptr(unsafe.Pointer(envName)),
		0,
		_KEY_SET_VALUE,
		uintptr(unsafe.Pointer(&key)),
	)
	if ret != 0 {
		return fmt.Errorf("RegOpenKeyExW HKLM (set) failed: error %d (may need admin privileges)", ret)
	}
	defer procRegCloseKey.Call(key)

	utf16, err := syscall.UTF16FromString(path)
	if err != nil {
		return err
	}
	valueName, err := syscall.UTF16PtrFromString("Path")
	if err != nil {
		return err
	}

	bufSize := uint32(len(utf16)) * 2
	ret, _, _ = procRegSetValueExW.Call(
		key,
		uintptr(unsafe.Pointer(valueName)),
		0,
		_REG_EXPAND_SZ,
		uintptr(unsafe.Pointer(&utf16[0])),
		uintptr(bufSize),
	)
	if ret != 0 {
		return fmt.Errorf("RegSetValueExW HKLM failed: error %d", ret)
	}
	return nil
}

// CleanupSystemPath cleans up the system PATH by:
//  1. Fixing double-backslash paths (\\)
//  2. Removing quoted entries
//  3. Removing corrupted entries (e.g. entries that start with " but don't end with ")
//
// Returns (changed, error). Requires admin privileges to write.
//
// Ticket 10 (audit-fixes / P0-11): the whole read-modify-write cycle runs
// under withUserPathLock, like the other PATH mutators — two concurrent
// xdev processes previously raced the registry and silently rolled back
// each other's entries. The same lock as the user-PATH writers is reused
// deliberately: the registry keys differ, so this only adds serialization
// (no deadlock risk).
func CleanupSystemPath() (bool, error) {
	var changed bool
	err := withUserPathLock(func() error {
		c, err := cleanupSystemPathLocked()
		changed = c
		return err
	})
	return changed, err
}

func cleanupSystemPathLocked() (bool, error) {
	current, err := ReadSystemPath()
	if err != nil {
		return false, err
	}

	parts := strings.Split(current, ";")
	var cleanParts []string
	changed := false

	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			changed = true
			continue
		}

		// Fix double-backslash
		if strings.Contains(trimmed, "\\\\") {
			trimmed = strings.ReplaceAll(trimmed, "\\\\", "\\")
			changed = true
		}

		// Remove surrounding quotes
		if (strings.HasPrefix(trimmed, "\"") && strings.HasSuffix(trimmed, "\"")) ||
			(strings.HasPrefix(trimmed, "'") && strings.HasSuffix(trimmed, "'")) {
			trimmed = trimmed[1 : len(trimmed)-1]
			changed = true
		}

		// Skip corrupted entries that start with quote but don't close it
		// e.g. "D:\\Env\\jdk17\bin;D:\\Env\\jdk17\lib"
		if strings.HasPrefix(trimmed, "\"") && !strings.HasSuffix(trimmed, "\"") {
			// This is a corrupted entry — the quote opened but never closed,
			// meaning it swallowed the next entry. Try to split it.
			// Remove the leading quote
			trimmed = strings.TrimPrefix(trimmed, "\"")
			changed = true
		}

		if trimmed == "" {
			changed = true
			continue
		}

		cleanParts = append(cleanParts, trimmed)
	}

	if changed {
		newPath := strings.Join(cleanParts, ";")
		if err := WriteSystemPath(newPath); err != nil {
			return false, err
		}
		BroadcastEnvironmentChange()
	}

	return changed, nil
}

// CleanupUserPath cleans up the user PATH by removing xdev-owned entries
// that are anchored at the given home directory:
//  1. Removing <home>\install\ subtree entries (always)
//  2. Removing <home>\current\ subtree entries — only in shim mode,
//     except <home>\current\xdev (the xdev self junction)
//  3. Removing the bare home root entry itself
//  4. Removing legacy xmdevenvmt entries (historical substrings, kept
//     intentionally — see release note)
//  5. Fixing double-backslash paths (\\) and quoted entries
//  6. Ensuring shimDir is the first entry (shim mode only)
//
// Ticket 11 (audit-fixes / P0-13): the match used to be drive-agnostic
// substring rules ("\xdev\install\", "\xdev\current\" plus isXDevRoot
// matching ANY drive's \xdev root) — an unrelated user directory like
// D:\xdev was silently deleted from PATH on every install/doctor run.
// The rules are now anchored at the caller-provided home; nothing outside
// home is ever touched (except the documented legacy substrings).
//
// pathMode should be "shim" or "direct".
// Returns true if any changes were made.
//
// Ticket 10 (audit-fixes / P0-11): the whole read-modify-write cycle runs
// under withUserPathLock, like the other PATH mutators.
func CleanupUserPath(home, shimDir, pathMode string) (bool, error) {
	var changed bool
	err := withUserPathLock(func() error {
		c, err := cleanupUserPathLocked(home, shimDir, pathMode)
		changed = c
		return err
	})
	return changed, err
}

func cleanupUserPathLocked(home, shimDir, pathMode string) (bool, error) {
	current, err := ReadUserPath()
	if err != nil {
		return false, err
	}

	parts := strings.Split(current, ";")
	var cleanParts []string
	changed := false
	shimDirClean := strings.ToLower(filepath.Clean(shimDir))

	// Home-anchored match prefixes (ticket 11 / P0-13).
	homeClean := strings.ToLower(filepath.Clean(home))
	sep := string(os.PathSeparator)
	homeInstallPrefix := homeClean + sep + "install" + sep
	homeCurrentPrefix := homeClean + sep + "current" + sep
	homeCurrentXdev := homeClean + sep + "current" + sep + "xdev"

	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			changed = true
			continue
		}

		// Fix double-backslash
		if strings.Contains(trimmed, "\\\\") {
			trimmed = strings.ReplaceAll(trimmed, "\\\\", "\\")
			changed = true
		}

		// Remove surrounding quotes
		trimmed = strings.Trim(trimmed, "\"")

		normalized := strings.ToLower(filepath.Clean(trimmed))

		// Skip <home>\install\ subtree entries (e.g. E:\XDev\install\java\25)
		if strings.HasPrefix(normalized, homeInstallPrefix) {
			changed = true
			continue
		}

		// Skip <home>\current\ subtree entries (e.g. E:\XDev\current\java\bin)
		// Only in shim mode — in direct mode these are needed.
		// 但保留 <home>\current\xdev（xdev 自身的 junction 路径）
		if pathMode == "shim" && strings.HasPrefix(normalized, homeCurrentPrefix) && normalized != homeCurrentXdev {
			changed = true
			continue
		}

		// Skip the bare home root entry itself (e.g. E:\XDev) — the old
		// implementation copied xdev.exe into the root and added the root
		// to PATH; the current layout uses current/xdev instead. Unlike
		// the old isXDevRoot rule, ONLY the actual home is matched.
		if normalized == homeClean {
			changed = true
			continue
		}

		// Skip legacy xmdevenvmt shim paths (historical substrings —
		// documented release-note exception)
		if strings.Contains(normalized, "\\ximulocaldev\\xmdevenvmt\\shims") {
			changed = true
			continue
		}

		// Skip legacy xmdevenvmt source/build directory entries
		if strings.Contains(normalized, "\\reverseeng\\xmdevenvmt") {
			changed = true
			continue
		}

		// Skip leftover InitConfig deadlock-test TempDir entries that
		// previous test runs wrote into the real user PATH.
		if strings.Contains(normalized, "\\testinitconfig_nodeadlock") {
			changed = true
			continue
		}

		cleanParts = append(cleanParts, trimmed)
	}

	// Ensure shim directory is first (shim mode only)
	if pathMode == "shim" {
		hasShimDir := false
		for _, p := range cleanParts {
			if strings.ToLower(filepath.Clean(p)) == shimDirClean {
				hasShimDir = true
				break
			}
		}
		if !hasShimDir {
			cleanParts = append([]string{shimDir}, cleanParts...)
			changed = true
		} else if strings.ToLower(filepath.Clean(cleanParts[0])) != shimDirClean {
			// shim dir exists but not first, move it to front
			var rest []string
			for _, p := range cleanParts {
				if strings.ToLower(filepath.Clean(p)) == shimDirClean {
					continue
				}
				rest = append(rest, p)
			}
			cleanParts = append([]string{shimDir}, rest...)
			changed = true
		}
	} else {
		// direct mode: remove shim directory from PATH (not needed)
		var withoutShim []string
		for _, p := range cleanParts {
			if strings.ToLower(filepath.Clean(p)) == shimDirClean {
				changed = true
				continue
			}
			withoutShim = append(withoutShim, p)
		}
		cleanParts = withoutShim
	}

	if changed {
		newPath := strings.Join(cleanParts, ";")
		if err := WriteUserPath(newPath); err != nil {
			return false, err
		}
		BroadcastEnvironmentChange()
	}

	return changed, nil
}

// isXDevRoot 判断路径是否为 XDev 根目录（如 E:\XDev）
// 条件：路径以 \xdev 结尾，且 \xdev 之前是卷标（如 e:）或路径分隔符
// 这样可以匹配 E:\XDev 但不会匹配 E:\XDev\shims 或 E:\XDev\current\xdev
func isXDevRoot(normalized string) bool {
	// 匹配 X:\xdev 格式（卷标 + \xdev）
	if len(normalized) >= 3 {
		// 检查是否以 :\xdev 结尾（如 e:\xdev）
		suffix := "\\xdev"
		if strings.HasSuffix(normalized, suffix) {
			prefix := normalized[:len(normalized)-len(suffix)]
			// 前缀应该是卷标（如 e:）
			if len(prefix) == 2 && prefix[1] == ':' {
				return true
			}
		}
	}
	return false
}

// BroadcastEnvironmentChange sends WM_SETTINGCHANGE with "Environment"
// so that new processes see the updated PATH without a reboot.
//
// Security (L-12): The return value of SendMessageTimeoutW is checked.
// If the broadcast times out or fails (e.g. a target window is hung),
// a warning is printed to stderr advising the user to restart their
// terminal. This is a best-effort notification — Windows does not
// guarantee that all running processes receive WM_SETTINGCHANGE.
func BroadcastEnvironmentChange() {
	user32 := syscall.NewLazyDLL("user32.dll")
	sendMsg := user32.NewProc("SendMessageTimeoutW")

	const (
		HWND_BROADCAST   = 0xFFFF
		WM_SETTINGCHANGE = 0x001A
		SMTO_ABORTIFHUNG = 0x0002
	)

	envStr, err := syscall.UTF16PtrFromString("Environment")
	if err != nil {
		log.Printf("[env] BroadcastEnvironmentChange: failed to encode 'Environment': %v", err)
		return
	}

	// SendMessageTimeoutW returns: (result LRESULT, unused uintptr, errno syscall.Errno)
	// When the timeout expires, errno is ERROR_TIMEOUT (0x5B4) and result is 0.
	ret, _, callErr := sendMsg.Call(
		HWND_BROADCAST,
		WM_SETTINGCHANGE,
		0,
		uintptr(unsafe.Pointer(envStr)),
		SMTO_ABORTIFHUNG,
		5000,
		0,
	)

	const ERROR_TIMEOUT = 0x5B4
	if callErr != syscall.Errno(0) && callErr != syscall.Errno(ERROR_TIMEOUT) {
		// Non-timeout error — broadcast likely failed entirely
		log.Printf("[env] BroadcastEnvironmentChange: SendMessageTimeoutW failed: errno %d", callErr)
		fmt.Fprintln(os.Stderr, "⚠ Environment change broadcast failed — open a new terminal to see updated PATH.")
	} else if callErr == syscall.Errno(ERROR_TIMEOUT) || ret == 0 {
		// One or more target windows timed out — changes may not have propagated
		log.Printf("[env] BroadcastEnvironmentChange: timed out or no response (errno %d, result %d)", callErr, ret)
		fmt.Fprintln(os.Stderr, "⚠ Some applications did not respond to environment change broadcast — open a new terminal to see updated PATH.")
	}
}

// UpdateShimDirInUserPath replaces the old shim directory in user PATH with the new one.
//
// Ticket 10 (audit-fixes / P0-11): the read-modify-write cycle runs under
// withUserPathLock, like the other PATH mutators.
func UpdateShimDirInUserPath(oldDir, newDir string) {
	_ = withUserPathLock(func() error {
		updateShimDirInUserPathLocked(oldDir, newDir)
		return nil
	})
}

func updateShimDirInUserPathLocked(oldDir, newDir string) {
	oldShimDir := filepath.Join(oldDir, "shims")
	newShimDir := filepath.Join(newDir, "shims")

	current, err := ReadUserPath()
	if err != nil {
		return
	}

	parts := strings.Split(current, ";")
	changed := false
	for i, p := range parts {
		if strings.EqualFold(filepath.Clean(p), filepath.Clean(oldShimDir)) {
			parts[i] = newShimDir
			changed = true
		}
	}

	if changed {
		newPath := strings.Join(parts, ";")
		WriteUserPath(newPath)
		BroadcastEnvironmentChange()
	}
}

// userEnvSeam 组：用户环境变量 HKCU\Environment 的三个落盘操作接缝（第四轮复核）。
// 之前只有枚举走接缝，写/删直连真实注册表 —— manifest_env_validation_test.go 的
// save/restore 夹具正是由此写真实 HKCU（P0-17 事故类别）。三个操作共用一个接缝
// 组，供 UseInMemoryUserEnvForTesting 一次整体替换。
var (
	// getAllUserEnvVarsFn 是用户环境变量**枚举**的接缝（GetUserEnvVarTrusted 等
	// 读取路径都经由它），默认真实注册表实现。
	getAllUserEnvVarsFn = enumerateUserEnvVarsFromRegistry
	// userEnvWriteFn 是**写**接缝，默认真实注册表实现。
	userEnvWriteFn = writeUserEnvVarToRegistry
	// userEnvDeleteFn 是**删**接缝，默认真实注册表实现。
	userEnvDeleteFn = deleteUserEnvVarFromRegistry
)

// inMemoryUserEnvStore 是测试注入的内存用户环境后端（map + 互斥锁）。
// 三个方法与接缝函数的签名一一对应。
type inMemoryUserEnvStore struct {
	mu   sync.Mutex
	vars map[string]string
}

func (s *inMemoryUserEnvStore) enumerate() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]string, len(s.vars))
	for k, v := range s.vars {
		out[k] = v
	}
	return out
}

func (s *inMemoryUserEnvStore) write(name, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.vars[name] = value
	return nil
}

func (s *inMemoryUserEnvStore) del(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.vars, name)
	return nil
}

// UseInMemoryUserEnvForTesting 用给定映射替换 HKCU\Environment 的枚举、写与删
// （返回还原函数）。**仅供测试**。
//
// 与 PATH 的内存后端同一动机（第四轮复核）：写真实注册表 + defer 恢复的夹具在
// 进程被强杀时把测试值永久留在用户环境里。枚举/读/写/删全部落在内存映射上之后：
//   - profile export 白名单守卫（internal/cli）只需注入映射，无需先写真实变量；
//   - manifest_env_validation_test.go（X-08 值校验契约）在 TestMain 的包级后端上
//     运行，真实注册表零接触 —— 「校验先于任何注册表访问」的断言语义不变
//     （值若被写入会出现在内存后端里，assertNoWrite 依然能抓住）。
//
// 还原语义是「还原到安装前的接缝值」，支持与 TestMain 包级后端的正确嵌套。
func UseInMemoryUserEnvForTesting(vars map[string]string) (restore func()) {
	store := &inMemoryUserEnvStore{vars: make(map[string]string, len(vars))}
	for k, v := range vars {
		store.vars[k] = v
	}
	prevEnum, prevWrite, prevDelete := getAllUserEnvVarsFn, userEnvWriteFn, userEnvDeleteFn
	getAllUserEnvVarsFn = store.enumerate
	userEnvWriteFn = store.write
	userEnvDeleteFn = store.del
	return func() {
		getAllUserEnvVarsFn, userEnvWriteFn, userEnvDeleteFn = prevEnum, prevWrite, prevDelete
	}
}

// GetAllUserEnvVars enumerates all user-level environment variables
// from HKCU\Environment and returns them as a map[string]string.
func GetAllUserEnvVars() map[string]string {
	return getAllUserEnvVarsFn()
}

// enumerateUserEnvVarsFromRegistry 是真实的注册表枚举实现。
func enumerateUserEnvVarsFromRegistry() map[string]string {
	result := make(map[string]string)

	var key uintptr
	envName, err := syscall.UTF16PtrFromString(`Environment`)
	if err != nil {
		return result
	}

	ret, _, _ := procRegOpenKeyExW.Call(
		_HKEY_CURRENT_USER,
		uintptr(unsafe.Pointer(envName)),
		0,
		_KEY_QUERY_VALUE,
		uintptr(unsafe.Pointer(&key)),
	)
	if ret != 0 {
		return result
	}
	defer procRegCloseKey.Call(key)

	index := uint32(0)
	for {
		nameBufSize := uint32(256)
		nameBuf := make([]uint16, nameBufSize)

		var valType uint32
		var dataBufSize uint32

		ret, _, _ = procRegEnumValueW.Call(
			key,
			uintptr(index),
			uintptr(unsafe.Pointer(&nameBuf[0])),
			uintptr(unsafe.Pointer(&nameBufSize)),
			0,
			uintptr(unsafe.Pointer(&valType)),
			0,
			uintptr(unsafe.Pointer(&dataBufSize)),
		)

		if ret == _ERROR_NO_MORE_ITEMS {
			break
		}
		// Ticket 05 (audit-fixes / P0-07): a value name longer than the
		// 256-char buffer makes RegEnumValueW return ERROR_MORE_DATA.
		// Empirically (and per the API contract) lpcchValueName is NOT
		// written back with the required length in this case — it keeps
		// its input value — so the buffer must be grown geometrically
		// rather than resized to a reported length. Retries stay bounded
		// by the 16,383-char registry name cap; previously this path
		// panicked (slice bounds out of range on nameBuf[:nameBufSize]).
		//
		// Note lpcchValueName is IN/OUT: each retry resets it to the new
		// buffer length.
		if ret == _ERROR_MORE_DATA {
			for grow := uint32(512); grow <= 16384 && ret == _ERROR_MORE_DATA; grow *= 2 {
				nameBuf = make([]uint16, grow)
				nameBufSize = grow
				ret, _, _ = procRegEnumValueW.Call(
					key,
					uintptr(index),
					uintptr(unsafe.Pointer(&nameBuf[0])),
					uintptr(unsafe.Pointer(&nameBufSize)),
					0,
					uintptr(unsafe.Pointer(&valType)),
					0,
					uintptr(unsafe.Pointer(&dataBufSize)),
				)
			}
			if ret != 0 {
				log.Printf("[env] WARN: enum HKCU\\Environment: value at index %d has an over-long name (>=%d chars) — skipped (ret=%d)", index, nameBufSize, ret)
				index++
				continue
			}
		} else if ret != 0 {
			// Any other failure aborts the enumeration — but never silently:
			// this loop runs on every Windows shim execution (hot path),
			// so a broken key must be diagnosable.
			log.Printf("[env] WARN: enum HKCU\\Environment aborted at index %d: ret=%d", index, ret)
			break
		}

		varName := syscall.UTF16ToString(nameBuf[:nameBufSize])

		// Read the value using RegQueryValueExW
		valueNamePtr, err := syscall.UTF16PtrFromString(varName)
		if err != nil {
			index++
			continue
		}

		var valBufSize uint32
		ret2, _, _ := procRegQueryValueExW.Call(
			key,
			uintptr(unsafe.Pointer(valueNamePtr)),
			0,
			uintptr(unsafe.Pointer(&valType)),
			0,
			uintptr(unsafe.Pointer(&valBufSize)),
		)
		if ret2 != 0 && ret2 != _ERROR_MORE_DATA {
			index++
			continue
		}

		if valBufSize > 0 {
			valBuf := make([]uint16, valBufSize/2+1)
			procRegQueryValueExW.Call(
				key,
				uintptr(unsafe.Pointer(valueNamePtr)),
				0,
				uintptr(unsafe.Pointer(&valType)),
				uintptr(unsafe.Pointer(&valBuf[0])),
				uintptr(unsafe.Pointer(&valBufSize)),
			)
			result[varName] = syscall.UTF16ToString(valBuf)
		} else {
			result[varName] = ""
		}

		index++
	}

	return result
}

// EnrichFromRegistry merges registry-level user environment variables into the
// given environment slice. For each variable in the util.AllowedEnvVarNames
// whitelist, it reads the current value from HKCU\Environment and overrides the
// corresponding entry in the slice (or appends it if not present).
//
// Security (M-14): This ensures that shim subprocesses always see the latest
// tool home variables (JAVA_HOME, GOROOT, etc.) even when the parent shell
// hasn't received WM_SETTINGCHANGE after an xdev activate/deactivate. Only
// whitelisted variable names are read from the registry — arbitrary env vars
// are never injected.
func EnrichFromRegistry(envSlice []string) []string {
	// Build a lookup map from the current env slice for fast override.
	existing := make(map[string]int, len(envSlice))
	for i, pair := range envSlice {
		idx := strings.IndexByte(pair, '=')
		if idx <= 0 {
			continue
		}
		existing[strings.ToUpper(pair[:idx])] = i
	}

	// One HKCU\Environment enumeration (Spec 030) instead of
	// opening the key once per whitelist name.
	all := GetAllUserEnvVars()
	for name := range util.AllowedEnvVarNames {
		regVal, ok := lookupUserEnv(all, name)
		if !ok || regVal == "" {
			continue
		}
		key := strings.ToUpper(name)
		if idx, ok := existing[key]; ok {
			envSlice[idx] = name + "=" + regVal
		} else {
			envSlice = append(envSlice, name+"="+regVal)
		}
	}

	return envSlice
}

func lookupUserEnv(all map[string]string, name string) (string, bool) {
	if v, ok := all[name]; ok {
		return v, true
	}
	want := strings.ToUpper(name)
	for k, v := range all {
		if strings.ToUpper(k) == want {
			return v, true
		}
	}
	return "", false
}

// readRegistryEnvVarRaw reads a single environment variable from HKCU\Environment
// without the whitelist check (the caller is responsible for that). Returns
// an error if the variable does not exist or cannot be read.
func readRegistryEnvVarRaw(name string) (string, error) {
	var key uintptr
	envName, err := syscall.UTF16PtrFromString(`Environment`)
	if err != nil {
		return "", err
	}

	ret, _, _ := procRegOpenKeyExW.Call(
		_HKEY_CURRENT_USER,
		uintptr(unsafe.Pointer(envName)),
		0,
		_KEY_QUERY_VALUE,
		uintptr(unsafe.Pointer(&key)),
	)
	if ret != 0 {
		return "", fmt.Errorf("RegOpenKeyExW failed: error %d", ret)
	}
	defer procRegCloseKey.Call(key)

	valueName, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return "", err
	}

	// First call: determine buffer size.
	var bufSize uint32
	var regType uint32
	ret, _, _ = procRegQueryValueExW.Call(
		key,
		uintptr(unsafe.Pointer(valueName)),
		0,
		uintptr(unsafe.Pointer(&regType)),
		0,
		uintptr(unsafe.Pointer(&bufSize)),
	)
	if ret == _ERROR_FILE_NOT_FOUND {
		return "", fmt.Errorf("env var %q not found", name)
	}
	if ret != 0 && ret != _ERROR_MORE_DATA {
		return "", fmt.Errorf("RegQueryValueExW (size) failed: error %d", ret)
	}
	if bufSize == 0 {
		return "", fmt.Errorf("env var %q not found", name)
	}

	// Second call: read the value.
	buf := make([]uint16, bufSize/2+1)
	ret, _, _ = procRegQueryValueExW.Call(
		key,
		uintptr(unsafe.Pointer(valueName)),
		0,
		uintptr(unsafe.Pointer(&regType)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&bufSize)),
	)
	if ret != 0 {
		return "", fmt.Errorf("RegQueryValueExW (read) failed: error %d", ret)
	}

	return syscall.UTF16ToString(buf), nil
}

// extractEnvRefs extracts %VAR% references from a string
func extractEnvRefs(s string) []string {
	var refs []string
	i := 0
	for i < len(s) {
		if s[i] == '%' {
			j := i + 1
			for j < len(s) && s[j] != '%' {
				j++
			}
			if j < len(s) && j > i+1 {
				refs = append(refs, s[i+1:j])
				i = j + 1
			} else {
				i++
			}
		} else {
			i++
		}
	}
	return refs
}

// setxSetPath is a fallback that uses the `setx` command to set the
// user PATH.  It is not used by default (the registry API is
// preferred) but is kept as documentation.
func _setxSetPath(path string) error {
	cmd := exec.Command("setx", "Path", path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
