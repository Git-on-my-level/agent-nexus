//go:build !windows && !darwin && !linux && !freebsd && !openbsd && !netbsd && !dragonfly

package storage

import "errors"

func filesystemFreeBytes(string) (int64, error) {
	return 0, errors.New("destination free-space check is unsupported on this platform")
}
