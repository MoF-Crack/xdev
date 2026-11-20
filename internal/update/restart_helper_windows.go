//go:build windows

package update

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

type restartParent struct{ handle windows.Handle }

func restartCreated(handle windows.Handle) (int64, error) {
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(handle, &created, &exited, &kernel, &user); err != nil {
		return 0, err
	}
	return int64(created.HighDateTime)<<32 | int64(created.LowDateTime), nil
}

func restartCurrentProcessCreated() (int64, error) { return restartCreated(windows.CurrentProcess()) }

func openRestartParent(plan restartPlan) (restartParent, error) {
	var parent restartParent
	if plan.ParentPID != os.Getppid() {
		return parent, errors.New("restart plan does not identify the helper's parent")
	}
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(plan.ParentPID))
	if err != nil {
		return parent, fmt.Errorf("open parent process: %w", err)
	}
	accepted := false
	defer func() {
		if !accepted {
			_ = windows.CloseHandle(handle)
		}
	}()
	created, err := restartCreated(handle)
	if err != nil || created != plan.ParentCreated {
		return parent, errors.New("restart parent creation time does not match")
	}
	path := make([]uint16, 32768)
	n := uint32(len(path))
	if err := windows.QueryFullProcessImageName(handle, 0, &path[0], &n); err != nil {
		return parent, fmt.Errorf("read restart parent executable: %w", err)
	}
	image, err := canonicalRestartPath(windows.UTF16ToString(path[:n]))
	if err != nil || !sameRestartPath(image, plan.Target) {
		return parent, errors.New("restart target does not match the parent executable")
	}
	status, err := windows.WaitForSingleObject(handle, 0)
	if err != nil || status != uint32(windows.WAIT_TIMEOUT) {
		return parent, errors.New("restart parent is no longer running")
	}
	accepted = true
	return restartParent{handle: handle}, nil
}

func (p restartParent) close() { _ = windows.CloseHandle(p.handle) }

func (p restartParent) wait(timeout time.Duration) error {
	status, err := windows.WaitForSingleObject(p.handle, uint32(timeout/time.Millisecond))
	if err != nil {
		return fmt.Errorf("wait for parent process: %w", err)
	}
	switch status {
	case windows.WAIT_OBJECT_0:
		return nil
	case uint32(windows.WAIT_TIMEOUT):
		return fmt.Errorf("parent process did not exit within %s; update was not installed", timeout)
	default:
		return fmt.Errorf("unexpected result waiting for parent process: %d", status)
	}
}

func configureRestartProcess(cmd *exec.Cmd) {
	// CREATE_NO_WINDOW suppresses a console while leaving the GUI's first
	// ShowWindow call unmodified (STARTF_USESHOWWINDOW/SW_HIDE would not).
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
}

func openRestartInput(path string) (*os.File, error) {
	return openRestartFile(path, windows.GENERIC_READ)
}

func openRestartMovable(path string) (*os.File, error) {
	return openRestartFile(path, windows.GENERIC_READ|windows.DELETE)
}

func openRestartFile(path string, access uint32) (*os.File, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(name, access, windows.FILE_SHARE_READ, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open restart input", Path: path, Err: err}
	}
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		_ = windows.CloseHandle(handle)
		return nil, err
	}
	if info.FileAttributes&(windows.FILE_ATTRIBUTE_REPARSE_POINT|windows.FILE_ATTRIBUTE_DIRECTORY) != 0 {
		_ = windows.CloseHandle(handle)
		return nil, fmt.Errorf("restart input is not a regular file: %s", path)
	}
	return os.NewFile(uintptr(handle), path), nil
}

// Rename the exact object opened with DELETE access. No intervening path lookup
// of the source, and FILE_SHARE_READ excludes other write/delete handles.
func renameRestartInput(input *os.File, destination string, replace bool) error {
	parent := filepath.Dir(destination)
	if !canonicalRestartIdentity(parent) {
		return errors.New("restart destination directory is no longer canonical")
	}
	path, err := windows.UTF16PtrFromString(parent)
	if err != nil {
		return err
	}
	dir, err := windows.CreateFile(path, windows.FILE_READ_ATTRIBUTES|windows.FILE_TRAVERSE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(dir)
	var attributes windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(dir, &attributes); err != nil {
		return err
	}
	if attributes.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || !canonicalRestartIdentity(parent) {
		return errors.New("restart destination directory is a reparse point")
	}
	name, err := windows.UTF16FromString(filepath.Base(destination))
	if err != nil {
		return err
	}
	// FILE_RENAME_INFORMATION; RootDirectory makes FileName a relative leaf.
	var info struct {
		ReplaceIfExists uint32
		RootDirectory   windows.Handle
		FileNameLength  uint32
		FileName        [260]uint16
	}
	if len(name) > len(info.FileName) {
		return errors.New("restart destination name is too long")
	}
	if replace {
		info.ReplaceIfExists = 1
	}
	info.RootDirectory = dir
	info.FileNameLength = uint32((len(name) - 1) * 2)
	copy(info.FileName[:], name)
	var status windows.IO_STATUS_BLOCK
	err = windows.NtSetInformationFile(windows.Handle(input.Fd()), &status, (*byte)(unsafe.Pointer(&info)),
		uint32(unsafe.Offsetof(info.FileName))+info.FileNameLength, windows.FileRenameInformation)
	runtime.KeepAlive(input)
	return err
}

// Serialize prepared-restart helpers for a canonical installation path. The
// handle exists only while it is used; an abandoned owner releases the mutex.
func lockRestartTarget(target string) (func(), error) {
	id := sha256.Sum256([]byte(strings.ToLower(target)))
	name, err := windows.UTF16PtrFromString(fmt.Sprintf(`Global\XDev-PreparedRestart-%x`, id))
	if err != nil {
		return nil, err
	}
	m, err := windows.CreateMutexEx(nil, name, 0, windows.SYNCHRONIZE|windows.MUTEX_MODIFY_STATE)
	if err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return nil, err
	}
	runtime.LockOSThread()
	status, err := windows.WaitForSingleObject(m, 5000)
	if err != nil || (status != windows.WAIT_OBJECT_0 && status != windows.WAIT_ABANDONED) {
		runtime.UnlockOSThread()
		_ = windows.CloseHandle(m)
		if err != nil {
			return nil, err
		}
		return nil, errors.New("another restart update is using this executable")
	}
	return func() {
		_ = windows.ReleaseMutex(m)
		_ = windows.CloseHandle(m)
		runtime.UnlockOSThread()
	}, nil
}
