//go:build darwin || linux

package filetransport

import (
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

func openDirectory(path string) (*os.File, error) {
	fd, e := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if e != nil {
		return nil, e
	}
	for _, part := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		if part == "" || part == "." || part == ".." {
			unix.Close(fd)
			return nil, ErrUnsafe
		}
		next, err := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		unix.Close(fd)
		if err != nil {
			return nil, err
		}
		fd = next
	}
	f := os.NewFile(uintptr(fd), path)
	if e = directoryOwned(f); e != nil {
		f.Close()
		return nil, e
	}
	return f, nil
}
func dupDirectory(f *os.File) (*os.File, error) { return openDirectoryAt(f, ".") }
func openDirectoryAt(d *os.File, n string) (*os.File, error) {
	fd, e := unix.Openat(int(d.Fd()), n, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if e != nil {
		return nil, e
	}
	f := os.NewFile(uintptr(fd), n)
	if e = directoryOwned(f); e != nil {
		f.Close()
		return nil, e
	}
	return f, nil
}
func openFileAt(d *os.File, n string, flag int, mode os.FileMode) (*os.File, error) {
	fd, e := unix.Openat(int(d.Fd()), n, flag|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, uint32(mode))
	if e != nil {
		return nil, e
	}
	return os.NewFile(uintptr(fd), n), nil
}
func directoryOwned(f *os.File) error {
	var s unix.Stat_t
	if e := unix.Fstat(int(f.Fd()), &s); e != nil {
		return e
	}
	if int(s.Uid) != os.Getuid() || s.Mode&unix.S_IFMT != unix.S_IFDIR || s.Mode&0077 != 0 {
		return ErrUnsafe
	}
	return nil
}
func regularOwned(f *os.File) error {
	var s unix.Stat_t
	if e := unix.Fstat(int(f.Fd()), &s); e != nil {
		return e
	}
	if int(s.Uid) != os.Getuid() || s.Mode&unix.S_IFMT != unix.S_IFREG || s.Nlink != 1 {
		return ErrUnsafe
	}
	return nil
}
func renameAt(a *os.File, n string, b *os.File, m string) error {
	return unix.Renameat(int(a.Fd()), n, int(b.Fd()), m)
}
func removeAt(d *os.File, n string) error    { return unix.Unlinkat(int(d.Fd()), n, 0) }
func removeDirAt(d *os.File, n string) error { return unix.Unlinkat(int(d.Fd()), n, unix.AT_REMOVEDIR) }
func mkdirAt(d *os.File, n string, mode os.FileMode) error {
	return unix.Mkdirat(int(d.Fd()), n, uint32(mode))
}
func symlinkAt(target string, d *os.File, n string) error {
	return unix.Symlinkat(target, int(d.Fd()), n)
}

func readlinkAt(d *os.File, n string) (string, error) {
	b := make([]byte, 512)
	size, e := unix.Readlinkat(int(d.Fd()), n, b)
	if e != nil {
		return "", e
	}
	if size == len(b) {
		return "", ErrUnsafe
	}
	return string(b[:size]), nil
}

func linkAt(a *os.File, n string, b *os.File, m string) error {
	return unix.Linkat(int(a.Fd()), n, int(b.Fd()), m, 0)
}
