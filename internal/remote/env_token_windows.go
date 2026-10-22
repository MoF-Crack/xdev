//go:build windows

package remote

import (
	"strings"
	"syscall"
	"unsafe"
)

var (
	advapi32             = syscall.NewLazyDLL("advapi32.dll")
	procRegOpenKeyExW    = advapi32.NewProc("RegOpenKeyExW")
	procRegQueryValueExW = advapi32.NewProc("RegQueryValueExW")
	procRegCloseKey      = advapi32.NewProc("RegCloseKey")
)

const (
	_HKEY_CURRENT_USER    = 0x80000001
	_KEY_QUERY_VALUE      = 0x0001
	_ERROR_FILE_NOT_FOUND = 2
	_ERROR_MORE_DATA      = 234
)

// windowsUserEnv reads a Windows user-level environment variable from
// HKCU\Environment.  This is a fallback when os.Getenv returns empty
// because the GUI process was launched from Windows Explorer and does
// not inherit newly-added User environment variables.
func windowsUserEnv(key string) string {
	var hKey uintptr
	subKey, err := syscall.UTF16PtrFromString(`Environment`)
	if err != nil {
		return ""
	}

	ret, _, _ := procRegOpenKeyExW.Call(
		_HKEY_CURRENT_USER,
		uintptr(unsafe.Pointer(subKey)),
		0,
		_KEY_QUERY_VALUE,
		uintptr(unsafe.Pointer(&hKey)),
	)
	if ret != 0 {
		return ""
	}
	defer procRegCloseKey.Call(hKey)

	valueName, err := syscall.UTF16PtrFromString(key)
	if err != nil {
		return ""
	}

	// First call: determine buffer size.
	var bufSize uint32
	var regType uint32
	ret, _, _ = procRegQueryValueExW.Call(
		hKey,
		uintptr(unsafe.Pointer(valueName)),
		0,
		uintptr(unsafe.Pointer(&regType)),
		0,
		uintptr(unsafe.Pointer(&bufSize)),
	)
	if ret == _ERROR_FILE_NOT_FOUND {
		return ""
	}
	if ret != 0 && ret != _ERROR_MORE_DATA {
		return ""
	}
	if bufSize == 0 {
		return ""
	}

	// Second call: read the value.
	buf := make([]uint16, bufSize/2+1)
	ret, _, _ = procRegQueryValueExW.Call(
		hKey,
		uintptr(unsafe.Pointer(valueName)),
		0,
		uintptr(unsafe.Pointer(&regType)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&bufSize)),
	)
	if ret != 0 {
		return ""
	}

	return strings.TrimSpace(syscall.UTF16ToString(buf))
}
