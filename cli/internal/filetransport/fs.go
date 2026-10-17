package filetransport

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// All paths below the opened .anx directory are resolved relative to stable
// directory descriptors, refusing symlinks in EVERY component. Go 1.23 supports
// the openat implementation; no version bump or path-based race fallback.
type FS struct {
	root         *os.File
	writeFailure func(string) error
}

func Open(directory string) (*FS, error) {
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		return nil, ErrUnsafe
	}
	f, e := openDirectory(directory)
	if e != nil {
		return nil, e
	}
	return &FS{root: f}, nil
}
func (f *FS) Close() error { return f.root.Close() }
func (f *FS) parent(path string) (*os.File, string, error) {
	if path == "" || filepath.IsAbs(path) || filepath.Clean(path) != path || strings.Contains(path, "\\") {
		return nil, "", ErrUnsafe
	}
	parts := strings.Split(path, "/")
	for _, p := range parts {
		if p == "" || p == "." || p == ".." {
			return nil, "", ErrUnsafe
		}
	}
	dir, e := dupDirectory(f.root)
	if e != nil {
		return nil, "", e
	}
	for _, part := range parts[:len(parts)-1] {
		next, e := openDirectoryAt(dir, part)
		dir.Close()
		if e != nil {
			return nil, "", e
		}
		dir = next
	}
	return dir, parts[len(parts)-1], nil
}
func (f *FS) read(path string, limit int) ([]byte, error) {
	d, name, e := f.parent(path)
	if e != nil {
		return nil, e
	}
	defer d.Close()
	r, e := openFileAt(d, name, os.O_RDONLY, 0)
	if e != nil {
		return nil, e
	}
	defer r.Close()
	if e = regularOwned(r); e != nil {
		return nil, e
	}
	before, e := r.Stat()
	if e != nil {
		return nil, e
	}
	b, e := io.ReadAll(io.LimitReader(r, int64(limit+1)))
	after, err := r.Stat()
	if err != nil {
		return nil, err
	}
	if before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return nil, fmt.Errorf("entry_changed_during_read")
	}
	if e == nil && len(b) > limit {
		return nil, fmt.Errorf("file_too_large")
	}
	return b, e
}
func (f *FS) write(path string, b []byte, mode os.FileMode) error {
	if f.writeFailure != nil {
		if e := f.writeFailure(path); e != nil {
			return e
		}
	}
	d, name, e := f.parent(path)
	if e != nil {
		return e
	}
	defer d.Close()
	temp := ".tmp-" + UUID()
	w, e := openFileAt(d, temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer removeAt(d, temp)
	if _, e = w.Write(b); e == nil {
		e = w.Chmod(mode)
	}
	if e == nil {
		e = w.Sync()
	}
	ce := w.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	if e = renameAt(d, temp, d, name); e != nil {
		return e
	}
	return d.Sync()
}
func (f *FS) mkdir(path string) error {
	d, name, e := f.parent(path)
	if e != nil {
		return e
	}
	defer d.Close()
	if e = mkdirAt(d, name, 0700); e != nil && !os.IsExist(e) {
		return e
	}
	check, e := openDirectoryAt(d, name)
	if e != nil {
		return e
	}
	defer check.Close()
	return directoryOwned(check)
}
func (f *FS) names(path string, limit int) ([]string, error) {
	var d *os.File
	var e error
	if path == "" {
		d, e = dupDirectory(f.root)
	} else {
		p, n, err := f.parent(path)
		if err != nil {
			return nil, err
		}
		defer p.Close()
		d, e = openDirectoryAt(p, n)
	}
	if e != nil {
		return nil, e
	}
	defer d.Close()
	names, e := d.Readdirnames(limit + 1)
	if e == io.EOF {
		e = nil
	}
	if len(names) > limit {
		return nil, ErrCapacity
	}
	return names, e
}
func (f *FS) rename(from, to string) error {
	a, n, e := f.parent(from)
	if e != nil {
		return e
	}
	defer a.Close()
	b, m, e := f.parent(to)
	if e != nil {
		return e
	}
	defer b.Close()
	if e = renameAt(a, n, b, m); e != nil {
		return e
	}
	if e = a.Sync(); e != nil {
		return e
	}
	return b.Sync()
}
func (f *FS) remove(path string) error {
	d, n, e := f.parent(path)
	if e != nil {
		return e
	}
	defer d.Close()
	if e = removeAt(d, n); e != nil && !os.IsNotExist(e) {
		return e
	}
	return d.Sync()
}
func (f *FS) symlink(target, path string) error {
	d, n, e := f.parent(path)
	if e != nil {
		return e
	}
	defer d.Close()
	return symlinkAt(target, d, n)
}
func (f *FS) chmodDir(path string, mode os.FileMode) error {
	d, n, e := f.parent(path)
	if e != nil {
		return e
	}
	defer d.Close()
	c, e := openDirectoryAt(d, n)
	if e != nil {
		return e
	}
	defer c.Close()
	if e = c.Chmod(mode); e != nil {
		return e
	}
	return c.Sync()
}
func (f *FS) removeTree(path string) error {
	names, e := f.names(path, MaxRecords*2)
	if e != nil {
		return e
	}
	if e = f.chmodDir(path, 0700); e != nil {
		return e
	}
	for _, n := range names {
		child := path + "/" + n
		if e = f.remove(child); e != nil {
			if e = f.removeTree(child); e != nil {
				return e
			}
		}
	}
	d, n, e := f.parent(path)
	if e != nil {
		return e
	}
	defer d.Close()
	return removeDirAt(d, n)
}

func (f *FS) readlink(path string) (string, error) {
	d, n, e := f.parent(path)
	if e != nil {
		return "", e
	}
	defer d.Close()
	return readlinkAt(d, n)
}

// Link publication never replaces an existing directory claim, even when two
// independently configured enrollments enable the same directory concurrently.
func (f *FS) installMarker(id string, raw []byte) error {
	stage := ".identity-" + id
	if e := f.write(stage, raw, 0600); e != nil {
		return e
	}
	defer f.remove(stage)
	d, n, e := f.parent(stage)
	if e != nil {
		return e
	}
	defer d.Close()
	if e = linkAt(d, n, d, "IDENTITY.json"); e != nil {
		if os.IsExist(e) {
			return checkMarker(f, id)
		}
		return e
	}
	if e = removeAt(d, n); e != nil {
		return e
	}
	return d.Sync()
}
