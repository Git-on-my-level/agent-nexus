//go:build windows

package storage

import (
	"golang.org/x/sys/windows"
)

func filesystemFreeBytes(path string) (int64, error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var freeAvailable, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(pathPtr, &freeAvailable, &total, &totalFree); err != nil {
		return 0, err
	}
	return int64(freeAvailable), nil
}
