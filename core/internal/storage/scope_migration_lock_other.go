//go:build !windows && !darwin && !linux && !freebsd && !openbsd && !netbsd && !dragonfly

package storage

import (
	"errors"
	"os"
)

func acquireScopeProcessLock(path string) (*os.File, error) {
	return nil, errors.New("workspace serving lease unsupported on this platform")
}
