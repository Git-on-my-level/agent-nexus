package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ReclaimCompactAccessOffline prepares a verified compact file, never overwrites
// the source. Stop the core first and keep it stopped until the operator replaces
// the file: this function's exclusive SQLite lease ends when it returns.
// The serving OS lease excludes live cores; SQLite excludes external writers.
func ReclaimCompactAccessOffline(ctx context.Context, source, destination string) (err error) {
	source, err = filepath.Abs(source)
	if err != nil {
		return err
	}
	destination, err = filepath.Abs(destination)
	if err != nil {
		return err
	}
	if source == destination {
		return errors.New("reclamation requires a new destination")
	}
	if _, err = os.Stat(source); err != nil {
		return err
	}
	lease, err := acquireScopeProcessLock(filepath.Join(filepath.Dir(source), ".scope-serving.lock"))
	if err != nil {
		return err
	}
	defer lease.Close()
	f, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	succeeded := false
	defer func() {
		if !succeeded {
			_ = os.Remove(destination)
		}
	}()
	db, err := sql.Open("sqlite", sqliteDSN(source))
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	// EXCLUSIVE locking mode retains the lock after COMMIT, allowing VACUUM's
	// own transaction without a write gap. Closing this private pool releases it.
	for _, q := range []string{`PRAGMA busy_timeout=0`, `PRAGMA locking_mode=EXCLUSIVE`, `BEGIN EXCLUSIVE`, `COMMIT`} {
		if _, err = conn.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("stop the core before reclaiming: %w", err)
		}
	}
	var stable, pk int
	if err = conn.QueryRowContext(ctx, `SELECT stable FROM resource_access_compact_control WHERE singleton=1`).Scan(&stable); err != nil {
		return err
	}
	if err = conn.QueryRowContext(ctx, `SELECT pk FROM pragma_table_info('resource_access_edges') WHERE name='edge_id' AND type='INTEGER'`).Scan(&pk); err != nil || stable != 1 || pk != 1 {
		return errors.New("raw edge identity is not certified stable")
	}
	if _, err = conn.ExecContext(ctx, `VACUUM INTO ?`, destination); err != nil {
		return err
	}
	if _, err = conn.ExecContext(ctx, `ATTACH DATABASE ? AS compact_output`, destination); err != nil {
		return err
	}
	var integrity string
	if err = conn.QueryRowContext(ctx, `PRAGMA compact_output.integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		return errors.New("reclaimed file failed integrity check")
	}
	rows, err := conn.QueryContext(ctx, `SELECT name FROM main.sqlite_schema WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return err
	}
	var tables []string
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		tables = append(tables, name)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
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
				return err
			}
			if mismatch {
				return errors.New("reclaimed file differs from source")
			}
		}
		// PM uses implicit rowids for pagination/FIFO; reject a SQLite build that
		// renumbers them, even though the explicit raw-edge IDs are now stable.
		if table == "pm_records" {
			var mismatch bool
			if err = conn.QueryRowContext(ctx, `SELECT EXISTS(SELECT rowid,kind,id FROM `+old+` EXCEPT SELECT rowid,kind,id FROM `+newTable+`)`).Scan(&mismatch); err != nil {
				return err
			}
			if mismatch {
				return errors.New("reclamation changed PM cursor identities")
			}
		}
	}
	// Flush the artifact itself before reporting success; publication stays an
	// explicit maintenance-window action, with the original file retained.
	output, err := os.OpenFile(destination, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	err = output.Sync()
	closeErr := output.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	succeeded = true
	return nil
}
