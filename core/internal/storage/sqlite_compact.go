package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
)

// DefaultCompactReserveBytes is the free-space headroom required on the
// destination filesystem in addition to the source's live-page bytes. It keeps
// VACUUM INTO from filling the volume that also holds the source and its WAL.
const DefaultCompactReserveBytes int64 = 2 * 1024 * 1024 * 1024

// CompactOptions controls offline SQLite compaction. The zero value is the
// generic mode: no compact-access certification precondition, the default
// reserve, and the serving lease.
type CompactOptions struct {
	// RequireStableAccess keeps the compact-access reclaim precondition: the
	// source must have certified stable raw edge identities.
	RequireStableAccess bool
	// ReserveBytes is free space that must remain after the live pages are
	// copied. Zero selects DefaultCompactReserveBytes.
	ReserveBytes int64
}

// CompactResult reports the verified output and the space decision.
type CompactResult struct {
	SourceBytes      int64
	DestinationBytes int64
	SavedBytes       int64
	LivePageBytes    int64
	WALBytes         int64
	FreeBytes        int64
	ReserveBytes     int64
	PageSize         int64
	AutoVacuum       int
}

// CompactSQLiteOffline writes a verified, compacted copy of a stopped SQLite
// database. It never overwrites the source or an existing destination. The
// output uses auto_vacuum=INCREMENTAL so later deletes can be reclaimed online
// with bounded PRAGMA incremental_vacuum calls.
//
// The caller must stop the core first and keep it stopped until the operator
// publishes the copy. The serving OS lease excludes live cores; SQLite's
// exclusive lock excludes external writers.
func CompactSQLiteOffline(ctx context.Context, source, destination string, options CompactOptions) (result CompactResult, err error) {
	source, err = filepath.Abs(source)
	if err != nil {
		return result, err
	}
	destination, err = filepath.Abs(destination)
	if err != nil {
		return result, err
	}
	if source == destination {
		return result, errors.New("compaction requires a new destination")
	}
	sourceInfo, err := os.Stat(source)
	if err != nil {
		return result, err
	}
	if _, err = os.Stat(destination); err == nil {
		return result, errors.New("compaction destination already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return result, err
	}
	reserve := options.ReserveBytes
	if reserve < 0 {
		return result, errors.New("reserve bytes must not be negative")
	}
	if reserve == 0 {
		reserve = DefaultCompactReserveBytes
	}
	result.ReserveBytes = reserve

	lease, err := acquireScopeProcessLock(filepath.Join(filepath.Dir(source), ".scope-serving.lock"))
	if err != nil {
		return result, err
	}
	defer lease.Close()

	db, err := sql.Open("sqlite", sqliteDSN(source))
	if err != nil {
		return result, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	conn, err := db.Conn(ctx)
	if err != nil {
		return result, err
	}
	defer conn.Close()
	// EXCLUSIVE locking mode retains the lock after COMMIT, allowing VACUUM's
	// own transaction without a write gap. Closing this private pool releases it.
	for _, q := range []string{`PRAGMA busy_timeout=0`, `PRAGMA locking_mode=EXCLUSIVE`, `BEGIN EXCLUSIVE`, `COMMIT`} {
		if _, err = conn.ExecContext(ctx, q); err != nil {
			return result, fmt.Errorf("stop the core before compacting: %w", err)
		}
	}
	if options.RequireStableAccess {
		var stable, pk int
		if err = conn.QueryRowContext(ctx, `SELECT stable FROM resource_access_compact_control WHERE singleton=1`).Scan(&stable); err != nil {
			return result, err
		}
		if err = conn.QueryRowContext(ctx, `SELECT pk FROM pragma_table_info('resource_access_edges') WHERE name='edge_id' AND type='INTEGER'`).Scan(&pk); err != nil || stable != 1 || pk != 1 {
			return result, errors.New("raw edge identity is not certified stable")
		}
	}

	var pageSize, pageCount, freelistCount int64
	if err = conn.QueryRowContext(ctx, `PRAGMA page_size`).Scan(&pageSize); err != nil {
		return result, err
	}
	if err = conn.QueryRowContext(ctx, `PRAGMA page_count`).Scan(&pageCount); err != nil {
		return result, err
	}
	if err = conn.QueryRowContext(ctx, `PRAGMA freelist_count`).Scan(&freelistCount); err != nil {
		return result, err
	}
	result.PageSize = pageSize
	result.LivePageBytes = (pageCount - freelistCount) * pageSize
	if result.LivePageBytes < 0 {
		result.LivePageBytes = 0
	}
	// The source WAL shares the destination filesystem and can grow while the
	// copy runs, so it counts against the headroom the copy must leave free.
	result.WALBytes, err = sqliteFileBytes(source + "-wal")
	if err != nil {
		return result, fmt.Errorf("check source WAL size: %w", err)
	}
	result.FreeBytes, err = filesystemFreeBytes(filepath.Dir(destination))
	if err != nil {
		return result, fmt.Errorf("check destination free space: %w", err)
	}
	if result.LivePageBytes > math.MaxInt64-reserve {
		return result, errors.New("compaction free-space estimate overflows: reserve bytes too large")
	}
	required := result.LivePageBytes + reserve
	if result.WALBytes > math.MaxInt64-required {
		return result, errors.New("compaction free-space estimate overflows: source WAL too large")
	}
	required += result.WALBytes
	if result.FreeBytes < required {
		return result, fmt.Errorf(
			"insufficient free space on destination filesystem: need %d bytes (live pages %d + WAL %d + reserve %d), have %d",
			required, result.LivePageBytes, result.WALBytes, reserve, result.FreeBytes,
		)
	}

	f, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return result, err
	}
	succeeded := false
	attached := false
	defer func() {
		if succeeded {
			return
		}
		// Detach and close the verification connection before deleting the
		// partial artifact; an open handle can keep the file from being
		// removed on some platforms. Use a fresh context so cleanup still
		// runs after the caller's context is canceled.
		if attached {
			_, _ = conn.ExecContext(context.Background(), `DETACH DATABASE compact_output`)
		}
		_ = conn.Close()
		// Closing the private pool releases the actual SQLite handle; returning
		// the connection to an idle pool would keep the output file open.
		_ = db.Close()
		_ = os.Remove(destination)
	}()
	if err = f.Close(); err != nil {
		return result, err
	}

	// Setting the desired mode on the source connection makes VACUUM INTO build
	// the new file with incremental auto-vacuum. The source file itself keeps
	// its existing mode until a VACUUM runs on it.
	if _, err = conn.ExecContext(ctx, `PRAGMA auto_vacuum=INCREMENTAL`); err != nil {
		return result, err
	}
	if _, err = conn.ExecContext(ctx, `VACUUM INTO ?`, destination); err != nil {
		return result, err
	}
	if _, err = conn.ExecContext(ctx, `ATTACH DATABASE ? AS compact_output`, destination); err != nil {
		return result, err
	}
	attached = true
	var integrity string
	if err = conn.QueryRowContext(ctx, `PRAGMA compact_output.integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		return result, errors.New("compacted file failed integrity check")
	}
	var outputAutoVacuum int
	if err = conn.QueryRowContext(ctx, `PRAGMA compact_output.auto_vacuum`).Scan(&outputAutoVacuum); err != nil {
		return result, err
	}
	if outputAutoVacuum != 2 {
		return result, fmt.Errorf("compacted file auto_vacuum=%d, want 2 (incremental)", outputAutoVacuum)
	}
	result.AutoVacuum = outputAutoVacuum
	var outputPageSize int64
	if err = conn.QueryRowContext(ctx, `PRAGMA compact_output.page_size`).Scan(&outputPageSize); err != nil {
		return result, err
	}
	if outputPageSize != pageSize {
		return result, fmt.Errorf("compacted file page_size=%d, want %d", outputPageSize, pageSize)
	}

	rows, err := conn.QueryContext(ctx, `SELECT name FROM main.sqlite_schema WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return result, err
	}
	var tables []string
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			rows.Close()
			return result, err
		}
		tables = append(tables, name)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	quote := func(name string) string { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }
	for _, table := range tables {
		old, newTable := "main."+quote(table), "compact_output."+quote(table)
		for _, q := range []string{
			`SELECT (SELECT count(*) FROM ` + old + `)<>(SELECT count(*) FROM ` + newTable + `)`,
			`SELECT EXISTS(SELECT * FROM ` + old + ` EXCEPT SELECT * FROM ` + newTable + `)`,
			`SELECT EXISTS(SELECT * FROM ` + newTable + ` EXCEPT SELECT * FROM ` + old + `)`,
		} {
			var mismatch bool
			if err = conn.QueryRowContext(ctx, q).Scan(&mismatch); err != nil {
				return result, err
			}
			if mismatch {
				return result, errors.New("compacted file differs from source")
			}
		}
		// PM uses implicit rowids for pagination/FIFO; reject a SQLite build that
		// renumbers them, even though explicit raw-edge IDs are stable.
		if table == "pm_records" {
			var mismatch bool
			if err = conn.QueryRowContext(ctx, `SELECT EXISTS(SELECT rowid,kind,id FROM `+old+` EXCEPT SELECT rowid,kind,id FROM `+newTable+`)`).Scan(&mismatch); err != nil {
				return result, err
			}
			if mismatch {
				return result, errors.New("compaction changed PM cursor identities")
			}
		}
	}
	// Flush the artifact itself before reporting success; publication stays an
	// explicit maintenance-window action, with the original file retained.
	output, err := os.OpenFile(destination, os.O_RDWR, 0)
	if err != nil {
		return result, err
	}
	err = output.Sync()
	closeErr := output.Close()
	if err != nil {
		return result, err
	}
	if closeErr != nil {
		return result, closeErr
	}
	outputInfo, err := os.Stat(destination)
	if err != nil {
		return result, err
	}
	result.SourceBytes = sourceInfo.Size()
	result.DestinationBytes = outputInfo.Size()
	result.SavedBytes = result.SourceBytes - result.DestinationBytes
	succeeded = true
	return result, nil
}

func sqliteFileBytes(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	return info.Size(), nil
}
