package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultCompactReserveBytes is the free-space headroom required on the
// destination filesystem in addition to the source's live-page bytes. It keeps
// VACUUM INTO from filling the volume that also holds the source and its WAL.
const DefaultCompactReserveBytes int64 = 2 * 1024 * 1024 * 1024

// CompactVerifyMode selects how much equivalence checking runs after VACUUM
// INTO. The zero value is full verification.
type CompactVerifyMode string

const (
	// CompactVerifyFull runs integrity_check, per-table counts, both EXCEPT
	// directions, and the PM cursor-identity check.
	CompactVerifyFull CompactVerifyMode = "full"
	// CompactVerifyQuick runs quick_check, output settings, schema comparison,
	// per-table counts, and a bidirectional PM cursor-identity check. It skips
	// the ordinary-table EXCEPT passes.
	CompactVerifyQuick CompactVerifyMode = "quick"
)

// CompactPhase names a timed compaction phase.
type CompactPhase string

const (
	CompactPhasePreflight      CompactPhase = "preflight"
	CompactPhaseVacuumInto     CompactPhase = "vacuum_into"
	CompactPhaseCheck          CompactPhase = "check"
	CompactPhaseSchemaSettings CompactPhase = "schema_settings"
	CompactPhaseCounts         CompactPhase = "counts"
	CompactPhasePMIdentity     CompactPhase = "pm_identity"
	CompactPhaseExcept         CompactPhase = "except"
	CompactPhaseFSync          CompactPhase = "fsync"
	CompactPhaseTotal          CompactPhase = "total"
)

// CompactPhaseObserver receives each completed phase and its elapsed time. On
// failure the failed phase is reported before the error is returned, followed
// by the total phase.
type CompactPhaseObserver func(phase CompactPhase, elapsed time.Duration)

// CompactOptions controls offline SQLite compaction. The zero value is the
// generic mode: no compact-access certification precondition, the default
// reserve, full verification, and the serving lease.
type CompactOptions struct {
	// RequireStableAccess keeps the compact-access reclaim precondition: the
	// source must have certified stable raw edge identities.
	RequireStableAccess bool
	// ReserveBytes is free space that must remain after the live pages are
	// copied. Zero selects DefaultCompactReserveBytes.
	ReserveBytes int64
	// Verify selects full or quick verification. Empty selects full.
	Verify CompactVerifyMode
	// OnlineDryRun copies a live database read-only without the serving lease
	// or an exclusive SQLite lock. The output is a disposable snapshot: it is
	// never compared with the changing source and must not be published. It
	// requires quick verification.
	OnlineDryRun bool
	// PhaseObserver, when set, receives each completed phase and its elapsed
	// time. Library callers that leave it nil keep the previous behaviour.
	PhaseObserver CompactPhaseObserver

	// testAfterVacuumInto is a test-only hook invoked after VACUUM INTO and
	// before verification. Production callers must not set it.
	testAfterVacuumInto func(destination string) error
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
	// DryRun reports that the output is a disposable online snapshot and must
	// not be published.
	DryRun bool
	// Verified reports that the output passed the selected offline verification.
	Verified bool
}

// CompactSQLiteOffline writes a verified, compacted copy of a stopped SQLite
// database. It never overwrites the source or an existing destination. The
// output uses auto_vacuum=INCREMENTAL so later deletes can be reclaimed online
// with bounded PRAGMA incremental_vacuum calls.
//
// The caller must stop the core first and keep it stopped until the operator
// publishes the copy. The serving OS lease excludes live cores; SQLite's
// exclusive lock excludes external writers.
//
// With OnlineDryRun the source is opened read-only without the serving lease or
// exclusive lock, so a running core may keep serving. The output is a stale
// snapshot that must never be published; the caller owns it and must remove it
// after recording the timings.
func CompactSQLiteOffline(ctx context.Context, source, destination string, options CompactOptions) (result CompactResult, err error) {
	verify, err := options.Verify.normalized()
	if err != nil {
		return result, err
	}
	if options.OnlineDryRun && verify != CompactVerifyQuick {
		return result, errors.New("online dry run requires quick verification")
	}
	phases := newCompactPhases(options.PhaseObserver)
	defer func() {
		if err != nil {
			phases.fail()
			phases.total()
		}
	}()

	phases.begin(CompactPhasePreflight)
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

	// The online dry run deliberately skips the serving lease: a live core must
	// keep running while the copy is measured.
	var lease *os.File
	if !options.OnlineDryRun {
		lease, err = acquireScopeProcessLock(filepath.Join(filepath.Dir(source), ".scope-serving.lock"))
		if err != nil {
			return result, err
		}
		defer lease.Close()
	}

	dsn := sqliteDSN(source)
	if options.OnlineDryRun {
		dsn = sqliteReadOnlyDSN(source)
	}
	db, err := sql.Open("sqlite", dsn)
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
	if !options.OnlineDryRun {
		// EXCLUSIVE locking mode retains the lock after COMMIT, allowing VACUUM's
		// own transaction without a write gap. Closing this private pool releases it.
		for _, q := range []string{`PRAGMA busy_timeout=0`, `PRAGMA locking_mode=EXCLUSIVE`, `BEGIN EXCLUSIVE`, `COMMIT`} {
			if _, err = conn.ExecContext(ctx, q); err != nil {
				return result, fmt.Errorf("stop the core before compacting: %w", err)
			}
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
	phases.end()

	phases.begin(CompactPhaseVacuumInto)
	// Setting the desired mode on the source connection makes VACUUM INTO build
	// the new file with incremental auto-vacuum. The source file itself keeps
	// its existing mode until a VACUUM runs on it. On a read-only connection
	// SQLite applies this as a connection-level desired mode for the output and
	// does not write the source; if a build rejects it, the disposable dry-run
	// output is converted on a separate connection below.
	if options.OnlineDryRun {
		_, _ = conn.ExecContext(ctx, `PRAGMA auto_vacuum=INCREMENTAL`)
	} else if _, err = conn.ExecContext(ctx, `PRAGMA auto_vacuum=INCREMENTAL`); err != nil {
		return result, err
	}
	if _, err = conn.ExecContext(ctx, `VACUUM INTO ?`, destination); err != nil {
		return result, err
	}
	phases.end()
	if options.testAfterVacuumInto != nil {
		if err = options.testAfterVacuumInto(destination); err != nil {
			return result, err
		}
	}

	phases.begin(CompactPhaseCheck)
	if options.OnlineDryRun {
		if err = compactCheckOutputQuick(ctx, destination); err != nil {
			return result, err
		}
	} else {
		if _, err = conn.ExecContext(ctx, `ATTACH DATABASE ? AS compact_output`, destination); err != nil {
			return result, err
		}
		attached = true
		pragma, label := `PRAGMA compact_output.integrity_check`, "integrity check"
		if verify == CompactVerifyQuick {
			pragma, label = `PRAGMA compact_output.quick_check`, "quick check"
		}
		if err = compactIntegrityCheck(ctx, conn, pragma, label); err != nil {
			return result, err
		}
	}
	phases.end()

	phases.begin(CompactPhaseSchemaSettings)
	if options.OnlineDryRun {
		var outputAutoVacuum int
		outputAutoVacuum, err = compactCheckOutputSettings(ctx, destination, pageSize)
		if err != nil {
			return result, err
		}
		if outputAutoVacuum != 2 {
			// Fallback for SQLite builds that ignore the read-only auto_vacuum
			// pragma: convert the disposable output on its own connection. This
			// adds a second VACUUM to the measured dry-run time.
			if err = compactConvertOutputAutoVacuum(ctx, destination); err != nil {
				return result, err
			}
			if err = compactCheckOutputQuick(ctx, destination); err != nil {
				return result, err
			}
			outputAutoVacuum, err = compactCheckOutputSettings(ctx, destination, pageSize)
			if err != nil {
				return result, err
			}
		}
		if outputAutoVacuum != 2 {
			return result, fmt.Errorf("compacted file auto_vacuum=%d, want 2 (incremental)", outputAutoVacuum)
		}
		result.AutoVacuum = outputAutoVacuum
	} else {
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
		if verify == CompactVerifyQuick {
			if err = compactCompareSchema(ctx, conn); err != nil {
				return result, err
			}
		}
	}
	phases.end()

	if !options.OnlineDryRun {
		phases.begin(CompactPhaseCounts)
		var countTables []string
		countTables, err = compactTableNames(ctx, conn, "main", true)
		if err != nil {
			return result, err
		}
		if err = compactCompareCounts(ctx, conn, countTables); err != nil {
			return result, err
		}
		phases.end()

		if verify == CompactVerifyFull {
			phases.begin(CompactPhaseExcept)
			var exceptTables []string
			exceptTables, err = compactTableNames(ctx, conn, "main", false)
			if err != nil {
				return result, err
			}
			if err = compactCompareExcept(ctx, conn, exceptTables); err != nil {
				return result, err
			}
			phases.end()
		}

		phases.begin(CompactPhasePMIdentity)
		if compactTableExists(countTables, "pm_records") {
			if err = compactComparePMIdentity(ctx, conn, verify == CompactVerifyQuick); err != nil {
				return result, err
			}
		}
		phases.end()
	}

	phases.begin(CompactPhaseFSync)
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
	phases.end()

	outputInfo, err := os.Stat(destination)
	if err != nil {
		return result, err
	}
	result.SourceBytes = sourceInfo.Size()
	result.DestinationBytes = outputInfo.Size()
	result.SavedBytes = result.SourceBytes - result.DestinationBytes
	result.DryRun = options.OnlineDryRun
	result.Verified = !options.OnlineDryRun
	phases.total()
	succeeded = true
	return result, nil
}

func (m CompactVerifyMode) normalized() (CompactVerifyMode, error) {
	switch m {
	case "", CompactVerifyFull:
		return CompactVerifyFull, nil
	case CompactVerifyQuick:
		return CompactVerifyQuick, nil
	default:
		return "", fmt.Errorf("unknown verification mode %q", m)
	}
}

type compactPhases struct {
	observer CompactPhaseObserver
	start    time.Time
	current  CompactPhase
	began    time.Time
}

func newCompactPhases(observer CompactPhaseObserver) *compactPhases {
	return &compactPhases{observer: observer, start: time.Now()}
}

func (p *compactPhases) begin(phase CompactPhase) {
	p.current = phase
	p.began = time.Now()
}

func (p *compactPhases) end() {
	if p.observer != nil && p.current != "" {
		p.observer(p.current, time.Since(p.began))
	}
	p.current = ""
}

func (p *compactPhases) fail() {
	if p.observer != nil && p.current != "" {
		p.observer(p.current, time.Since(p.began))
	}
	p.current = ""
}

func (p *compactPhases) total() {
	if p.observer != nil {
		p.observer(CompactPhaseTotal, time.Since(p.start))
	}
}

func sqliteReadOnlyDSN(databasePath string) string {
	dsn := &url.URL{
		Scheme: "file",
		Path:   databasePath,
	}
	query := dsn.Query()
	// mode=ro keeps the source and its WAL read-only. Do not add query_only:
	// SQLite rejects VACUUM INTO on a query_only connection.
	query.Add("mode", "ro")
	query.Add("_pragma", "busy_timeout(20000)")
	return dsn.String()
}

func openCompactReadOnly(ctx context.Context, path string) (*sql.DB, *sql.Conn, error) {
	db, err := sql.Open("sqlite", sqliteReadOnlyDSN(path))
	if err != nil {
		return nil, nil, err
	}
	db.SetMaxOpenConns(1)
	conn, err := db.Conn(ctx)
	if err != nil {
		_ = db.Close()
		return nil, nil, err
	}
	return db, conn, nil
}

func compactCheckOutputQuick(ctx context.Context, destination string) error {
	db, conn, err := openCompactReadOnly(ctx, destination)
	if err != nil {
		return err
	}
	defer db.Close()
	defer conn.Close()
	return compactIntegrityCheck(ctx, conn, `PRAGMA quick_check`, "quick check")
}

func compactCheckOutputSettings(ctx context.Context, destination string, pageSize int64) (int, error) {
	db, conn, err := openCompactReadOnly(ctx, destination)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	defer conn.Close()
	return compactOutputSettings(ctx, conn, pageSize)
}

func compactOutputSettings(ctx context.Context, conn *sql.Conn, pageSize int64) (int, error) {
	var autoVacuum int
	if err := conn.QueryRowContext(ctx, `PRAGMA auto_vacuum`).Scan(&autoVacuum); err != nil {
		return 0, err
	}
	var outputPageSize int64
	if err := conn.QueryRowContext(ctx, `PRAGMA page_size`).Scan(&outputPageSize); err != nil {
		return 0, err
	}
	if outputPageSize != pageSize {
		return 0, fmt.Errorf("compacted file page_size=%d, want %d", outputPageSize, pageSize)
	}
	return autoVacuum, nil
}

func compactConvertOutputAutoVacuum(ctx context.Context, destination string) error {
	db, err := sql.Open("sqlite", "file:"+destination)
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
	if _, err = conn.ExecContext(ctx, `PRAGMA auto_vacuum=INCREMENTAL`); err != nil {
		return err
	}
	_, err = conn.ExecContext(ctx, `VACUUM`)
	return err
}

func compactIntegrityCheck(ctx context.Context, conn *sql.Conn, pragma, label string) error {
	rows, err := conn.QueryContext(ctx, pragma)
	if err != nil {
		return err
	}
	defer rows.Close()
	var results []string
	for rows.Next() {
		var result string
		if err = rows.Scan(&result); err != nil {
			return err
		}
		results = append(results, result)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if len(results) != 1 || results[0] != "ok" {
		return fmt.Errorf("compacted file failed %s", label)
	}
	return nil
}

type compactSchemaRow struct {
	typ     sql.NullString
	name    sql.NullString
	tblName sql.NullString
	sql     sql.NullString
}

func compactSchemaRows(ctx context.Context, conn *sql.Conn, schema string) ([]compactSchemaRow, error) {
	rows, err := conn.QueryContext(ctx, `SELECT type, name, tbl_name, sql FROM `+schema+`.sqlite_schema ORDER BY type, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []compactSchemaRow
	for rows.Next() {
		var row compactSchemaRow
		if err = rows.Scan(&row.typ, &row.name, &row.tblName, &row.sql); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func compactCompareSchema(ctx context.Context, conn *sql.Conn) error {
	mainRows, err := compactSchemaRows(ctx, conn, "main")
	if err != nil {
		return err
	}
	outputRows, err := compactSchemaRows(ctx, conn, "compact_output")
	if err != nil {
		return err
	}
	if len(mainRows) != len(outputRows) {
		return errors.New("compacted file schema differs from source")
	}
	for i := range mainRows {
		if mainRows[i] != outputRows[i] {
			return errors.New("compacted file schema differs from source")
		}
	}
	return nil
}

func compactTableNames(ctx context.Context, conn *sql.Conn, schema string, includeInternal bool) ([]string, error) {
	query := `SELECT name FROM ` + schema + `.sqlite_schema WHERE type='table'`
	if !includeInternal {
		query += ` AND name NOT LIKE 'sqlite_%'`
	}
	query += ` ORDER BY name`
	rows, err := conn.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			return nil, err
		}
		tables = append(tables, name)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return tables, nil
}

func compactTableExists(tables []string, name string) bool {
	for _, table := range tables {
		if table == name {
			return true
		}
	}
	return false
}

func quoteSQLiteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func compactCompareCounts(ctx context.Context, conn *sql.Conn, tables []string) error {
	for _, table := range tables {
		quoted := quoteSQLiteIdent(table)
		query := `SELECT (SELECT count(*) FROM main.` + quoted + `)<>(SELECT count(*) FROM compact_output.` + quoted + `)`
		var mismatch bool
		if err := conn.QueryRowContext(ctx, query).Scan(&mismatch); err != nil {
			return err
		}
		if mismatch {
			return errors.New("compacted file differs from source")
		}
	}
	return nil
}

func compactCompareExcept(ctx context.Context, conn *sql.Conn, tables []string) error {
	for _, table := range tables {
		old, newTable := "main."+quoteSQLiteIdent(table), "compact_output."+quoteSQLiteIdent(table)
		for _, query := range []string{
			`SELECT EXISTS(SELECT * FROM ` + old + ` EXCEPT SELECT * FROM ` + newTable + `)`,
			`SELECT EXISTS(SELECT * FROM ` + newTable + ` EXCEPT SELECT * FROM ` + old + `)`,
		} {
			var mismatch bool
			if err := conn.QueryRowContext(ctx, query).Scan(&mismatch); err != nil {
				return err
			}
			if mismatch {
				return errors.New("compacted file differs from source")
			}
		}
	}
	return nil
}

func compactComparePMIdentity(ctx context.Context, conn *sql.Conn, bidirectional bool) error {
	queries := []string{
		`SELECT EXISTS(SELECT rowid,kind,id FROM main.pm_records EXCEPT SELECT rowid,kind,id FROM compact_output.pm_records)`,
	}
	if bidirectional {
		queries = append(queries, `SELECT EXISTS(SELECT rowid,kind,id FROM compact_output.pm_records EXCEPT SELECT rowid,kind,id FROM main.pm_records)`)
	}
	for _, query := range queries {
		var mismatch bool
		if err := conn.QueryRowContext(ctx, query).Scan(&mismatch); err != nil {
			return err
		}
		if mismatch {
			return errors.New("compaction changed PM cursor identities")
		}
	}
	return nil
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
