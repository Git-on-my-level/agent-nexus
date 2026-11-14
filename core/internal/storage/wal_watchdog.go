package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"agent-nexus-core/internal/sqliteutil"
)

const (
	defaultWALWatchdogInterval          = 5 * time.Minute
	defaultWALWarnBytes           int64 = 512 * 1024 * 1024
	defaultWALPinnedRuns                = 3
	defaultWALTruncateBusyTimeout       = 250 * time.Millisecond
	defaultWALTruncateFloorBytes  int64 = 4 * 1024 * 1024
	defaultJournalSizeLimit       int64 = 64 * 1024 * 1024
	defaultMaxOpenConns                 = 64
	defaultMaxIdleConns                 = 8
	defaultConnMaxLifetime              = 30 * time.Minute
	defaultConnMaxIdleTime              = 5 * time.Minute
)

type sqliteMaintenanceConfig struct {
	enabled             bool
	interval            time.Duration
	warnBytes           int64
	pinnedRuns          int
	truncateBusyTimeout time.Duration
	journalSizeLimit    int64
	maxOpenConns        int
	maxIdleConns        int
	connMaxLifetime     time.Duration
	connMaxIdleTime     time.Duration
	incrementalVacuum   incrementalVacuumConfig
}

func loadSQLiteMaintenanceConfig() (sqliteMaintenanceConfig, error) {
	config := sqliteMaintenanceConfig{
		enabled:             true,
		interval:            defaultWALWatchdogInterval,
		warnBytes:           defaultWALWarnBytes,
		pinnedRuns:          defaultWALPinnedRuns,
		truncateBusyTimeout: defaultWALTruncateBusyTimeout,
		journalSizeLimit:    defaultJournalSizeLimit,
		maxOpenConns:        defaultMaxOpenConns,
		maxIdleConns:        defaultMaxIdleConns,
		connMaxLifetime:     defaultConnMaxLifetime,
		connMaxIdleTime:     defaultConnMaxIdleTime,
		incrementalVacuum: incrementalVacuumConfig{
			enabled:            true,
			minFreelistBytes:   defaultIncrementalVacuumMinFreelistBytes,
			minFreelistPercent: defaultIncrementalVacuumMinFreelistPercent,
			pages:              defaultIncrementalVacuumPages,
			busyTimeout:        defaultIncrementalVacuumBusyTimeout,
		},
	}
	if value, ok := os.LookupEnv("ANX_SQLITE_WAL_WATCHDOG"); ok {
		parsed, err := strconv.ParseBool(strings.TrimSpace(value))
		if err != nil {
			return config, fmt.Errorf("invalid ANX_SQLITE_WAL_WATCHDOG: must be a boolean")
		}
		config.enabled = parsed
	}
	// The journal size limit is part of the connection DSN, not watchdog
	// tuning, so it is parsed and validated even when the watchdog is disabled.
	if value, ok := os.LookupEnv("ANX_SQLITE_JOURNAL_SIZE_LIMIT_BYTES"); ok {
		parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err != nil || parsed <= 0 {
			return config, fmt.Errorf("invalid ANX_SQLITE_JOURNAL_SIZE_LIMIT_BYTES: must be a positive integer")
		}
		config.journalSizeLimit = parsed
	}
	// Pool limits apply to the workspace *sql.DB even when the watchdog is
	// disabled. They bound connection growth; they do not close an in-use
	// leaked or stuck read snapshot.
	if value, ok := os.LookupEnv("ANX_SQLITE_MAX_OPEN_CONNS"); ok {
		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || parsed < 1 {
			return config, fmt.Errorf("invalid ANX_SQLITE_MAX_OPEN_CONNS: must be an integer >= 1")
		}
		config.maxOpenConns = parsed
	}
	if value, ok := os.LookupEnv("ANX_SQLITE_MAX_IDLE_CONNS"); ok {
		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || parsed < 0 {
			return config, fmt.Errorf("invalid ANX_SQLITE_MAX_IDLE_CONNS: must be an integer >= 0")
		}
		config.maxIdleConns = parsed
	}
	if value, ok := os.LookupEnv("ANX_SQLITE_CONN_MAX_LIFETIME"); ok {
		parsed, err := time.ParseDuration(strings.TrimSpace(value))
		if err != nil || parsed <= 0 {
			return config, fmt.Errorf("invalid ANX_SQLITE_CONN_MAX_LIFETIME: must be a positive duration")
		}
		config.connMaxLifetime = parsed
	}
	if value, ok := os.LookupEnv("ANX_SQLITE_CONN_MAX_IDLE_TIME"); ok {
		parsed, err := time.ParseDuration(strings.TrimSpace(value))
		if err != nil || parsed <= 0 {
			return config, fmt.Errorf("invalid ANX_SQLITE_CONN_MAX_IDLE_TIME: must be a positive duration")
		}
		config.connMaxIdleTime = parsed
	}
	// Tuning values only matter while the watchdog runs. A disabled watchdog
	// must not block workspace initialization on an invalid tuning value.
	if !config.enabled {
		return config, nil
	}
	if value, ok := os.LookupEnv("ANX_SQLITE_WAL_WATCHDOG_INTERVAL"); ok {
		parsed, err := time.ParseDuration(strings.TrimSpace(value))
		if err != nil || parsed <= 0 {
			return config, fmt.Errorf("invalid ANX_SQLITE_WAL_WATCHDOG_INTERVAL: must be a positive duration")
		}
		config.interval = parsed
	}
	if value, ok := os.LookupEnv("ANX_SQLITE_WAL_WARN_BYTES"); ok {
		parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err != nil || parsed <= 0 {
			return config, fmt.Errorf("invalid ANX_SQLITE_WAL_WARN_BYTES: must be a positive integer")
		}
		config.warnBytes = parsed
	}
	if value, ok := os.LookupEnv("ANX_SQLITE_WAL_PINNED_RUNS"); ok {
		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || parsed < 1 {
			return config, fmt.Errorf("invalid ANX_SQLITE_WAL_PINNED_RUNS: must be at least 1")
		}
		config.pinnedRuns = parsed
	}
	if value, ok := os.LookupEnv("ANX_SQLITE_WAL_TRUNCATE_BUSY_TIMEOUT"); ok {
		parsed, err := time.ParseDuration(strings.TrimSpace(value))
		if err != nil || parsed <= 0 {
			return config, fmt.Errorf("invalid ANX_SQLITE_WAL_TRUNCATE_BUSY_TIMEOUT: must be a positive duration")
		}
		config.truncateBusyTimeout = parsed
	}
	if value, ok := os.LookupEnv("ANX_SQLITE_INCREMENTAL_VACUUM"); ok {
		parsed, err := strconv.ParseBool(strings.TrimSpace(value))
		if err != nil {
			return config, fmt.Errorf("invalid ANX_SQLITE_INCREMENTAL_VACUUM: must be a boolean")
		}
		config.incrementalVacuum.enabled = parsed
	}
	if !config.incrementalVacuum.enabled {
		return config, nil
	}
	if value, ok := os.LookupEnv("ANX_SQLITE_INCREMENTAL_VACUUM_MIN_FREELIST_BYTES"); ok {
		parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err != nil || parsed <= 0 {
			return config, fmt.Errorf("invalid ANX_SQLITE_INCREMENTAL_VACUUM_MIN_FREELIST_BYTES: must be a positive integer")
		}
		config.incrementalVacuum.minFreelistBytes = parsed
	}
	if value, ok := os.LookupEnv("ANX_SQLITE_INCREMENTAL_VACUUM_MIN_FREELIST_PERCENT"); ok {
		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || parsed < 0 || parsed > 100 {
			return config, fmt.Errorf("invalid ANX_SQLITE_INCREMENTAL_VACUUM_MIN_FREELIST_PERCENT: must be an integer from 0 to 100")
		}
		config.incrementalVacuum.minFreelistPercent = parsed
	}
	if value, ok := os.LookupEnv("ANX_SQLITE_INCREMENTAL_VACUUM_PAGES"); ok {
		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || parsed <= 0 || parsed > maxIncrementalVacuumPages {
			return config, fmt.Errorf("invalid ANX_SQLITE_INCREMENTAL_VACUUM_PAGES: must be an integer from 1 to %d", maxIncrementalVacuumPages)
		}
		config.incrementalVacuum.pages = parsed
	}
	if value, ok := os.LookupEnv("ANX_SQLITE_INCREMENTAL_VACUUM_BUSY_TIMEOUT"); ok {
		parsed, err := time.ParseDuration(strings.TrimSpace(value))
		if err != nil || parsed <= 0 {
			return config, fmt.Errorf("invalid ANX_SQLITE_INCREMENTAL_VACUUM_BUSY_TIMEOUT: must be a positive duration")
		}
		config.incrementalVacuum.busyTimeout = parsed
	}
	return config, nil
}

func applyWorkspacePoolLimits(db *sql.DB, config sqliteMaintenanceConfig) {
	db.SetMaxOpenConns(config.maxOpenConns)
	db.SetMaxIdleConns(config.maxIdleConns)
	db.SetConnMaxLifetime(config.connMaxLifetime)
	db.SetConnMaxIdleTime(config.connMaxIdleTime)
}

type walCheckpointResult struct {
	busy               int64
	logFrames          int64
	checkpointedFrames int64
}

type walCheckpointExecutor interface {
	Passive(ctx context.Context) (walCheckpointResult, error)
	TruncateCheckpoint(ctx context.Context) (walCheckpointResult, error)
	Close(ctx context.Context) error
}

type walWatchdog struct {
	db           *sql.DB
	walPath      string
	config       sqliteMaintenanceConfig
	statWAL      func() (int64, error)
	openExecutor func(ctx context.Context) (walCheckpointExecutor, error)
	openVacuum   func(ctx context.Context) (incrementalVacuumExecutor, error)
	logf         func(format string, args ...any)
	cancel       context.CancelFunc
	done         chan struct{}
	consecutive  int
	warned       bool
}

func newWALWatchdog(db *sql.DB, walPath string, config sqliteMaintenanceConfig) *walWatchdog {
	watchdog := &walWatchdog{
		db:      db,
		walPath: walPath,
		config:  config,
		logf:    log.Printf,
	}
	watchdog.statWAL = watchdog.statWALFile
	watchdog.openExecutor = func(ctx context.Context) (walCheckpointExecutor, error) {
		conn, err := db.Conn(ctx)
		if err != nil {
			return nil, err
		}
		return &sqliteWALCheckpointExecutor{
			conn:                conn,
			truncateBusyTimeout: config.truncateBusyTimeout,
		}, nil
	}
	watchdog.openVacuum = func(ctx context.Context) (incrementalVacuumExecutor, error) {
		conn, err := db.Conn(ctx)
		if err != nil {
			return nil, err
		}
		return &sqliteIncrementalVacuumExecutor{conn: conn}, nil
	}
	return watchdog
}

func (w *Workspace) startWALWatchdog(config sqliteMaintenanceConfig, databasePath string) {
	if !config.enabled {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	watchdog := newWALWatchdog(w.db, databasePath+"-wal", config)
	watchdog.cancel = cancel
	watchdog.done = make(chan struct{})
	w.walWatchdogCancel = cancel
	w.walWatchdogDone = watchdog.done
	go watchdog.run(ctx)
}

func (w *walWatchdog) run(ctx context.Context) {
	defer close(w.done)
	ticker := time.NewTicker(w.config.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.tick(ctx)
		}
	}
}

func (w *walWatchdog) tick(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	defer w.reclaimIncremental(ctx)
	executor, err := w.openExecutor(ctx)
	if err != nil {
		w.recordFailure(ctx)
		return
	}
	defer func() {
		_ = executor.Close(ctx)
	}()
	walBytes, err := w.statWAL()
	if err != nil {
		w.recordFailure(ctx)
		return
	}
	passive, err := executor.Passive(ctx)
	if err != nil {
		w.recordFailure(ctx)
		return
	}
	warn, truncate := w.evaluate(walBytes, passive)
	if warn {
		w.logf(
			"anx-core: WAL watchdog WARN stage=wal_pinned wal_bytes=%d log_frames=%d checkpointed_frames=%d busy=%d consecutive_runs=%d",
			walBytes, passive.logFrames, passive.checkpointedFrames, passive.busy, w.consecutive,
		)
	}
	if truncate {
		if _, err := executor.TruncateCheckpoint(ctx); err != nil {
			w.recordFailure(ctx)
		}
	}
}

func (w *walWatchdog) evaluate(walBytes int64, passive walCheckpointResult) (warn, truncate bool) {
	valid := passive.busy == 0 && passive.logFrames >= 0 && passive.checkpointedFrames >= 0
	pinned := valid && walBytes > w.config.warnBytes && passive.checkpointedFrames < passive.logFrames
	if pinned {
		w.consecutive++
		if w.consecutive >= w.config.pinnedRuns && !w.warned {
			w.warned = true
			warn = true
		}
	} else {
		w.consecutive = 0
		w.warned = false
	}
	// An empty or tiny WAL has nothing worth reclaiming; truncating it on every
	// tick would take an exclusive checkpoint on an otherwise idle database.
	truncate = valid && passive.checkpointedFrames == passive.logFrames && walBytes > defaultWALTruncateFloorBytes
	return warn, truncate
}

func (w *walWatchdog) recordFailure(ctx context.Context) {
	w.consecutive = 0
	w.warned = false
	if ctx.Err() != nil {
		return
	}
	w.logf("anx-core: WAL watchdog maintenance failed stage=wal_watchdog")
}

func (w *walWatchdog) reclaimIncremental(ctx context.Context) {
	if !w.config.incrementalVacuum.enabled || w.openVacuum == nil || ctx.Err() != nil {
		return
	}
	executor, err := w.openVacuum(ctx)
	if err != nil {
		w.logf("anx-core: SQLite incremental vacuum failed stage=incremental_vacuum")
		return
	}
	defer func() {
		_ = executor.Close(ctx)
	}()
	result, err := executor.Reclaim(ctx, w.config.incrementalVacuum)
	if err != nil {
		if ctx.Err() == nil {
			w.logf("anx-core: SQLite incremental vacuum failed stage=incremental_vacuum")
		}
		return
	}
	if result.reclaimed > 0 {
		w.logf(
			"anx-core: SQLite incremental vacuum stage=incremental_vacuum pages_reclaimed=%d freelist_before=%d freelist_after=%d page_count=%d",
			result.reclaimed, result.freelistBefore, result.stats.freelistCount, result.stats.pageCount,
		)
	}
}

func (w *walWatchdog) statWALFile() (int64, error) {
	info, err := os.Stat(w.walPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	return info.Size(), nil
}

type sqliteWALCheckpointExecutor struct {
	conn                *sql.Conn
	truncateBusyTimeout time.Duration
}

func (e *sqliteWALCheckpointExecutor) Passive(ctx context.Context) (walCheckpointResult, error) {
	var result walCheckpointResult
	err := e.conn.QueryRowContext(ctx, `PRAGMA wal_checkpoint(PASSIVE)`).Scan(
		&result.busy, &result.logFrames, &result.checkpointedFrames,
	)
	return result, err
}

func (e *sqliteWALCheckpointExecutor) TruncateCheckpoint(ctx context.Context) (walCheckpointResult, error) {
	restore, err := sqliteutil.SetConnectionBusyTimeout(ctx, e.conn, e.truncateBusyTimeout)
	if err != nil {
		return walCheckpointResult{}, err
	}
	defer restore()
	var result walCheckpointResult
	err = e.conn.QueryRowContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`).Scan(
		&result.busy, &result.logFrames, &result.checkpointedFrames,
	)
	return result, err
}

func (e *sqliteWALCheckpointExecutor) Close(ctx context.Context) error {
	if ctx.Err() != nil {
		_ = e.conn.Raw(func(any) error { return driver.ErrBadConn })
	}
	return e.conn.Close()
}
