package storage

import (
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"modernc.org/libc"
	sqlite3 "modernc.org/sqlite/lib"
)

// SQLite caches VFS environment choices process-wide. Each scenario must start
// before any other test has initialized SQLite, just like a core process.
func TestSQLiteStartupTempDirectory(t *testing.T) {
	if root := os.Getenv("ANX_SQLITE_TEMP_TEST_ROOT"); root != "" {
		testSQLiteTempSpill(t, root)
		return
	}
	root := t.TempDir()
	for _, scenario := range []string{"default", "override", "driver-environment", "invalid-override", "shared-override", "symlink", "leased"} {
		t.Run(scenario, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestSQLiteStartupTempDirectory$", "-test.v")
			for _, entry := range os.Environ() {
				if !strings.HasPrefix(entry, "SQLITE_TMPDIR=") && !strings.HasPrefix(entry, "TMPDIR=") {
					cmd.Env = append(cmd.Env, entry)
				}
			}
			caseRoot := filepath.Join(root, scenario)
			if err := os.MkdirAll(caseRoot, 0700); err != nil {
				t.Fatal(err)
			}
			cmd.Env = append(cmd.Env, "ANX_SQLITE_TEMP_TEST_ROOT="+caseRoot,
				"ANX_SQLITE_TEMP_TEST_SCENARIO="+scenario,
				"TMPDIR="+filepath.Join(caseRoot, "nonexistent-default"))
			if strings.Contains(scenario, "override") || scenario == "driver-environment" {
				override := filepath.Join(caseRoot, "override")
				if scenario == "invalid-override" {
					if err := os.WriteFile(override, nil, 0600); err != nil {
						t.Fatal(err)
					}
				} else if err := os.Mkdir(override, 0700); err != nil {
					t.Fatal(err)
				}
				if scenario == "shared-override" {
					if err := os.Chmod(override, 0755); err != nil {
						t.Fatal(err)
					}
				}
				cmd.Env = append(cmd.Env, "SQLITE_TMPDIR="+override)
			}
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("startup/spill subprocess: %v\n%s", err, out)
			} else {
				t.Logf("%s", out)
			}
		})
	}
}

func testSQLiteTempSpill(t *testing.T, root string) {
	scenario := os.Getenv("ANX_SQLITE_TEMP_TEST_SCENARIO")
	dir := filepath.Join(root, "tmp")
	if override := os.Getenv("SQLITE_TMPDIR"); override != "" {
		dir = override
	}
	if scenario == "invalid-override" || scenario == "shared-override" {
		if _, err := PrepareSQLiteTempDirectory(filepath.Join(root, "state.sqlite")); err == nil {
			t.Fatal("accepted invalid override")
		}
		return
	}
	if scenario == "symlink" {
		if err := os.Symlink(root, dir); err != nil {
			t.Fatal(err)
		}
		if _, err := PrepareSQLiteTempDirectory(filepath.Join(root, "state.sqlite")); err == nil {
			t.Fatal("accepted symlink temp directory")
		}
		return
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(dir, "etilqs_stale")
	preserved := filepath.Join(dir, "upload-in-progress")
	for _, file := range []string{stale, preserved} {
		if err := os.WriteFile(file, []byte("keep non-SQLite work"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	directory := filepath.Join(dir, "etilqs_directory")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if scenario == "leased" {
		lock, err := acquireScopeProcessLock(filepath.Join(dir, ".sqlite-temp.lock"))
		if err != nil {
			t.Fatal(err)
		}
		defer lock.Close()
		if _, err := PrepareSQLiteTempDirectory(filepath.Join(root, "state.sqlite")); err == nil {
			t.Fatal("accepted active spill directory")
		}
		if _, err := os.Stat(stale); err != nil {
			t.Fatalf("cleanup ran without exclusive ownership: %v", err)
		}
		return
	}
	if scenario != "driver-environment" {
		if scenario == "default" {
			if err := os.Chmod(dir, 0755); err != nil {
				t.Fatal(err)
			}
		}
		lease, err := PrepareSQLiteTempDirectory(filepath.Join(root, "state.sqlite"))
		if err != nil {
			t.Fatal(err)
		}
		defer lease.Close()
		sqlite3.Xsqlite3_temp_directory = lease.NativePath
		if _, err := os.Stat(stale); !os.IsNotExist(err) {
			t.Fatalf("stale spill remains: %v", err)
		}
		if _, err := os.Stat(preserved); err != nil {
			t.Fatalf("removed unrelated temp work: %v", err)
		}
		if _, err := os.Stat(directory); err != nil {
			t.Fatalf("recursively removed a directory: %v", err)
		}
		info, err := os.Stat(dir)
		if err != nil || info.Mode().Perm() != 0700 {
			t.Fatalf("temp directory permissions: %v, %v", info, err)
		}
	}
	tls := libc.NewTLS()
	defer tls.Close()
	path, err := libc.CString(filepath.Join(root, "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer libc.Xfree(tls, path)
	// Native output slots must not live on a movable Go stack.
	out := tls.Alloc(32)
	defer tls.Free(32)
	if rc := sqlite3.Xsqlite3_open(tls, path, out); rc != sqlite3.SQLITE_OK {
		t.Fatalf("open: %d", rc)
	}
	db := sqliteTempNativePointer(out)
	defer sqlite3.Xsqlite3_close(tls, db)
	// Ask the real VFS for the filename it would use; this does not create a
	// connection-local PRAGMA or substitute a mock filesystem.
	if rc := sqlite3.Xsqlite3_file_control(tls, db, 0, sqlite3.SQLITE_FCNTL_TEMPFILENAME, out+8); rc != sqlite3.SQLITE_OK {
		t.Fatalf("temp filename: %d", rc)
	}
	filename := sqliteTempNativePointer(out + 8)
	actual := libc.GoString(filename)
	sqlite3.Xsqlite3_free(tls, filename)
	want := dir + string(os.PathSeparator)
	if !strings.HasPrefix(actual, want) {
		t.Fatalf("SQLite temp file %q is outside database-local directory %q", actual, want)
	}
	// 16 MiB exceeds the 8 KiB temp page cache and the default sorter buffer;
	// the table plus index exceed the incident's 64 MiB temporary filesystem.
	// Keep disk-backed temp storage; consuming RAM instead hides the incident.
	query, err := libc.CString(`PRAGMA temp_store=FILE; PRAGMA temp.cache_size=-8;
		CREATE TEMP TABLE spill(id INTEGER, payload BLOB);
		WITH RECURSIVE n(i) AS (VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<16384)
		INSERT INTO spill SELECT i,randomblob(1024) FROM n;
		CREATE INDEX spill_sort ON spill(payload);
		SELECT payload FROM spill ORDER BY payload;`)
	if err != nil {
		t.Fatal(err)
	}
	defer libc.Xfree(tls, query)
	if rc := sqlite3.Xsqlite3_exec(tls, db, query, 0, 0, 0); rc != sqlite3.SQLITE_OK {
		t.Fatalf("disk spill: %d: %s", rc, libc.GoString(sqlite3.Xsqlite3_errmsg(tls, db)))
	}
	// Inspect the actual temp pager's VFS file, rather than assuming the fixture
	// spilled. A memory-only workaround must not make this regression pass.
	temp, err := libc.CString("temp")
	if err != nil {
		t.Fatal(err)
	}
	defer libc.Xfree(tls, temp)
	if rc := sqlite3.Xsqlite3_file_control(tls, db, temp, sqlite3.SQLITE_FCNTL_FILE_POINTER, out+16); rc != sqlite3.SQLITE_OK {
		t.Fatalf("temp pager file: %d", rc)
	}
	file := sqliteTempNativePointer(out + 16)
	if file == 0 {
		t.Fatal("temp pager has no file")
	}
	methods := sqliteTempNativePointer(file)
	fileSize := sqliteTempNativePointer(methods + unsafe.Offsetof(sqlite3.Tsqlite3_io_methods{}.FxFileSize))
	rc := (*(*func(*libc.TLS, uintptr, uintptr) int32)(unsafe.Pointer(&struct{ uintptr }{fileSize})))(tls, file, out+24)
	size := int64(binary.NativeEndian.Uint64(libc.GoBytes(out+24, 8)))
	if rc != sqlite3.SQLITE_OK || size <= 64<<20 {
		t.Fatalf("fixture did not spill over 64 MiB to disk: size=%d rc=%d", size, rc)
	}
	t.Logf("disk-backed temp table and index: %d bytes; VFS spill path: %s", size, actual)
}

func sqliteTempNativePointer(address uintptr) uintptr {
	bytes := libc.GoBytes(address, int(unsafe.Sizeof(uintptr(0))))
	if len(bytes) == 4 {
		return uintptr(binary.NativeEndian.Uint32(bytes))
	}
	return uintptr(binary.NativeEndian.Uint64(bytes))
}
