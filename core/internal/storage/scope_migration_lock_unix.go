//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly

package storage

import (
	"fmt"
	"os"
	"syscall"
)

// Keep the descriptor open for the full serving lifetime. Never unlink the
// file: unlink/recreate would permit two independently locked inodes.
func acquireScopeProcessLock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("workspace is already serving: %w", err)
	}
	return f, nil
}
