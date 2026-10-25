package storage

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"modernc.org/libc"
	sqlite3 "modernc.org/sqlite/lib"
)

var sqliteTempStartup struct {
	sync.Mutex
	prepared bool
}

// SQLiteTempDirectory owns the spill-directory lease. The process entrypoint
// assigns NativePath to SQLite's temp-directory global before opening databases;
// storage library code does not mutate a foreign package's process globals.
// NativePath is SQLite-allocated memory retained until process exit.
type SQLiteTempDirectory struct {
	*os.File
	NativePath uintptr
}

// PrepareSQLiteTempDirectory is process startup configuration: call exactly once,
// before opening ANY SQLite connection, and retain the returned lease until all
// connections close. SQLite's temp directory is global, not connection-local.
// The entrypoint must assign the returned NativePath to sqlite3_temp_directory.
// Library callers opening multiple workspaces must choose one process spill
// volume explicitly; InitializeWorkspace deliberately does not change globals.
func PrepareSQLiteTempDirectory(databasePath string) (*SQLiteTempDirectory, error) {
	sqliteTempStartup.Lock()
	defer sqliteTempStartup.Unlock()
	if sqliteTempStartup.prepared {
		return nil, errors.New("SQLite temp directory already prepared for this process")
	}
	dir := os.Getenv("SQLITE_TMPDIR")
	override := dir != ""
	if !override {
		dir = filepath.Join(filepath.Dir(databasePath), "tmp")
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create SQLite temp directory: %w", err)
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("SQLite temp directory must be a real directory, not a symlink")
	}
	// An override must be dedicated already; never chmod or sweep a shared /tmp.
	if override && info.Mode().Perm() != 0700 {
		return nil, errors.New("SQLITE_TMPDIR must be a dedicated directory with mode 0700")
	}
	lease, err := acquireScopeProcessLock(filepath.Join(dir, ".sqlite-temp.lock"))
	if err != nil {
		return nil, fmt.Errorf("lease SQLite temp directory: %w", err)
	}
	succeeded := false
	defer func() {
		if !succeeded {
			_ = lease.Close()
		}
	}()
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		// SQLite's unix VFS uses etilqs_* (sqlite reversed). Do not recursively
		// remove directories or unrelated workspace uploads/maintenance files.
		if strings.HasPrefix(entry.Name(), "etilqs_") && !entry.IsDir() {
			if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
				return nil, fmt.Errorf("remove stale SQLite temp file: %w", err)
			}
		}
	}
	probe, err := os.CreateTemp(dir, ".sqlite-probe-")
	if err != nil {
		return nil, fmt.Errorf("write SQLite temp directory: %w", err)
	}
	if err := errors.Join(probe.Close(), os.Remove(probe.Name())); err != nil {
		return nil, err
	}
	// modernc's libc snapshots the environment, and SQLite caches getenv results
	// during VFS initialization. os.Setenv alone is therefore insufficient after
	// either snapshot. The entrypoint sets SQLite's global before any connection
	// can use it. Retain its C string for the process lifetime (including shutdown).
	tls := libc.NewTLS()
	defer tls.Close()
	path := sqlite3.Xsqlite3_malloc(tls, int32(len(dir)+1))
	if path == 0 {
		return nil, errors.New("allocate SQLite temp directory")
	}
	bytes := libc.GoBytes(path, len(dir)+1)
	copy(bytes, dir)
	bytes[len(dir)] = 0
	sqliteTempStartup.prepared = true
	succeeded = true
	log.Printf("sqlite_temp_directory=%q", dir)
	return &SQLiteTempDirectory{File: lease, NativePath: path}, nil
}
