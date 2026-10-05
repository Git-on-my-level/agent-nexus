//go:build windows

package filelock

import (
	"errors"
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

const lockfileExclusiveLock = 0x00000002

var (
	kernel32        = syscall.NewLazyDLL("kernel32.dll")
	lockFileEx      = kernel32.NewProc("LockFileEx")
	unlockFileEx    = kernel32.NewProc("UnlockFileEx")
	errReparsePoint = errors.New("refusing to open a reparse point")
)

// OpenNoFollow rejects existing reparse points (including symlinks) using
// Lstat before opening. Windows has no os.OpenFile equivalent of O_NOFOLLOW;
// a concurrent replacement between Lstat and OpenFile remains possible.
func OpenNoFollow(path string, flag int, perm os.FileMode) (*os.File, error) {
	info, err := os.Lstat(path)
	if err == nil {
		attrs, ok := info.Sys().(*syscall.Win32FileAttributeData)
		if !ok || attrs.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 || info.Mode()&os.ModeSymlink != 0 {
			return nil, &os.PathError{Op: "open", Path: path, Err: errReparsePoint}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return os.OpenFile(path, flag, perm)
}

// Lock blocks on an exclusive lock for the entire file, shared across processes.
func Lock(file *os.File) error {
	var overlapped syscall.Overlapped
	r, _, err := lockFileEx.Call(file.Fd(), lockfileExclusiveLock, 0, 0xffffffff, 0xffffffff, uintptr(unsafe.Pointer(&overlapped)))
	runtime.KeepAlive(file)
	if r == 0 {
		return syscallError(err)
	}
	return nil
}

func Unlock(file *os.File) error {
	var overlapped syscall.Overlapped
	r, _, err := unlockFileEx.Call(file.Fd(), 0, 0xffffffff, 0xffffffff, uintptr(unsafe.Pointer(&overlapped)))
	runtime.KeepAlive(file)
	if r == 0 {
		return syscallError(err)
	}
	return nil
}

func syscallError(err error) error {
	if err == nil || errors.Is(err, syscall.Errno(0)) {
		return syscall.EINVAL
	}
	return err
}

func TryLock(file *os.File) error {
	var overlapped syscall.Overlapped
	r, _, err := lockFileEx.Call(file.Fd(), lockfileExclusiveLock|0x00000001, 0, 0xffffffff, 0xffffffff, uintptr(unsafe.Pointer(&overlapped)))
	runtime.KeepAlive(file)
	if r == 0 {
		return syscallError(err)
	}
	return nil
}
