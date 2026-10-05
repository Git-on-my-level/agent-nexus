//go:build !windows

package filelock

import (
	"os"
	"syscall"
)

// OpenNoFollow opens a lock or log file without following a final symlink.
func OpenNoFollow(path string, flag int, perm os.FileMode) (*os.File, error) {
	return os.OpenFile(path, flag|syscall.O_NOFOLLOW, perm)
}

// Lock takes an exclusive process-wide advisory lock until Unlock or Close.
func Lock(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_EX)
}

func Unlock(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
}

// TryLock uses the same kernel lock as Lock, without waiting. Lock files must
// remain at a stable inode: never unlink one to recover a dead owner.
func TryLock(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}
