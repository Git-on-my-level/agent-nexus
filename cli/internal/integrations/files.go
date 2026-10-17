package integrations

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"agent-nexus-cli/internal/filelock"
)

const MaxFileBytes = 20 << 20

var ErrSettingsChanged = errors.New("harness settings changed concurrently; retry setup")

// Create only missing directories, then set their exact private mode even under
// a restrictive umask. Existing directory permissions belong to the user.
func privateMkdirAll(path string) error {
	if st, err := os.Stat(path); err == nil {
		if !st.IsDir() {
			return fmt.Errorf("not a directory: %s", path)
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := privateMkdirAll(filepath.Dir(path)); err != nil {
		return err
	}
	if err := os.Mkdir(path, 0700); err != nil {
		if errors.Is(err, os.ErrExist) {
			return privateMkdirAll(path)
		}
		return err
	}
	return os.Chmod(path, 0700)
}

func Read(path string) ([]byte, error) {
	st, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("refusing non-regular file %s", path)
	}
	f, err := filelock.OpenNoFollow(path, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, MaxFileBytes+1))
	if err == nil && len(b) > MaxFileBytes {
		err = fmt.Errorf("file exceeds %d bytes", MaxFileBytes)
	}
	return b, err
}
func ReadSettings(path string) ([]byte, error) {
	b, err := Read(path)
	if errors.Is(err, os.ErrNotExist) {
		return []byte("{}"), nil
	}
	return b, err
}
func LoadConfig(path string) (Config, error) {
	var c Config
	b, err := Read(path)
	if err != nil {
		return c, err
	}
	err = json.Unmarshal(b, &c)
	return c, err
}
func JSON(v any) []byte { b, _ := json.MarshalIndent(v, "", "  "); return append(b, '\n') }

// AtomicWrite rejects symlinks and keeps the existing mode. Files and parent
// directories created by ANX are owner-only. The rename is on the same volume.
func AtomicWrite(path string, raw []byte) error {
	return atomicWrite(path, raw, nil)
}

func atomicWrite(path string, raw []byte, beforeRename func() error) error {
	if len(raw) > MaxFileBytes {
		return fmt.Errorf("file exceeds %d bytes", MaxFileBytes)
	}
	mode := os.FileMode(0600)
	if st, err := os.Lstat(path); err == nil {
		if !st.Mode().IsRegular() {
			return fmt.Errorf("refusing non-regular file %s", path)
		}
		mode = st.Mode().Perm()
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := privateMkdirAll(filepath.Dir(path)); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".anx-write-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(raw)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if beforeRename != nil {
		if err := beforeRename(); err != nil {
			return err
		}
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// UpdateSettings runs under the caller's ANX lock, but external harness writers
// do not share it. Compare both identity and contents immediately before each
// rename, rebuilding the merge at most three times. This narrows the unavoidable
// filesystem check/rename race without overwriting observed concurrent edits.
func UpdateSettings(path string, merge func([]byte) ([]byte, bool, error)) ([]byte, bool, error) {
	for attempt := 0; attempt < 3; attempt++ {
		info, err := os.Lstat(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, false, err
		}
		raw, err := ReadSettings(path)
		if err != nil {
			return nil, false, err
		}
		next, changed, err := merge(raw)
		if err != nil {
			return nil, false, err
		}
		if !changed {
			return next, false, nil
		}
		if err = Backup(path, raw); err != nil {
			return nil, false, err
		}
		err = atomicWrite(path, next, func() error {
			checkIdentity := func() error {
				current, statErr := os.Lstat(path)
				if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
					return statErr
				}
				if (info == nil) != (current == nil) {
					return ErrSettingsChanged
				}
				if info != nil && (!os.SameFile(info, current) || info.Size() != current.Size() || !info.ModTime().Equal(current.ModTime()) || info.Mode() != current.Mode()) {
					return ErrSettingsChanged
				}
				return nil
			}
			if err := checkIdentity(); err != nil {
				return err
			}
			latest, readErr := ReadSettings(path)
			if readErr != nil {
				return readErr
			}
			if !bytes.Equal(raw, latest) {
				return ErrSettingsChanged
			}
			return checkIdentity()
		})
		if errors.Is(err, ErrSettingsChanged) {
			continue
		}
		return next, err == nil, err
	}
	return nil, false, ErrSettingsChanged
}
func Backup(path string, raw []byte) error {
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".anx-backup-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	// Publish a complete backup only, without ever replacing the first snapshot.
	if err = os.Link(f.Name(), path+".anx-backup"); errors.Is(err, os.ErrExist) {
		return nil
	}
	return err
}

// Lock never waits beyond ctx; hook code uses a much shorter persistence budget.
func Lock(ctx context.Context, path string) (func(), error) {
	if _, bounded := ctx.Deadline(); !bounded {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
	}
	if err := privateMkdirAll(filepath.Dir(path)); err != nil {
		return nil, err
	}
	f, err := filelock.OpenNoFollow(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	for {
		if err = filelock.TryLock(f); err == nil {
			return func() { _ = filelock.Unlock(f); _ = f.Close() }, nil
		} else if !filelock.Contended(err) {
			_ = f.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// Private cache/queue files never inherit a permissive planted file mode.
func PrivateWrite(path string, raw []byte) error {
	if st, e := os.Lstat(path); e == nil && (!st.Mode().IsRegular() || st.Mode().Perm() != 0600) {
		return fmt.Errorf("private file must be regular mode 0600")
	} else if e != nil && !errors.Is(e, os.ErrNotExist) {
		return e
	}
	return AtomicWrite(path, raw)
}
func PrivateRead(path string) ([]byte, error) {
	st, e := os.Lstat(path)
	if e != nil {
		return nil, e
	}
	if !st.Mode().IsRegular() || st.Mode().Perm() != 0600 {
		return nil, fmt.Errorf("private file must be regular mode 0600")
	}
	return Read(path)
}
