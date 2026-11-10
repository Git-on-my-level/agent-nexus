//go:build !windows

package app

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func openPMConnectDirectory(state string, uid int) (*os.File, error) {
	fd, err := unix.Open(filepath.Clean(state), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	dir := os.NewFile(uintptr(fd), state)
	var stat unix.Stat_t
	if err = unix.Fstat(fd, &stat); err != nil || int(stat.Uid) != uid {
		dir.Close()
		return nil, fmt.Errorf("PM diagnostic directory must be owned by the current user")
	}
	return dir, nil
}

func writePMConnectFile(state string, raw []byte) error {
	dir, err := openPMConnectDirectory(state, os.Getuid())
	if err != nil {
		return err
	}
	defer dir.Close()
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return err
	}
	name := ".pm-connect-" + hex.EncodeToString(nonce[:])
	fd, err := unix.Openat(int(dir.Fd()), name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return err
	}
	defer unix.Unlinkat(int(dir.Fd()), name, 0)
	file := os.NewFile(uintptr(fd), name)
	if err = file.Chmod(0600); err != nil {
		file.Close()
		return err
	}
	_, err = file.Write(raw)
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	// Keep both operations anchored to the checked directory, even if renamed.
	return unix.Renameat(int(dir.Fd()), name, int(dir.Fd()), "last-connect.json")
}
