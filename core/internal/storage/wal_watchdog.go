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
	defaultJournalSizeLimit       int64 = 64 * 1024 * 1024
)

type sqliteMaintenanceConfig struct {
	enabled             bool
	interval            time.Duration
	warnBytes           int64
	pinnedRuns          int
	truncateBusyTimeout time.Duration
	journalSizeLimit    int64
}

func loadSQLiteMaintenanceConfig() (sqliteMaintenanceConfig, error) {
	config := sqliteMaintenanceConfig{
		enabled:             true,
		interval:            defaultWALWatchdogInterval,
		warnBytes:           defaultWALWarnBytes,
		pinnedRuns:          defaultWALPinnedRuns,
		truncateBusyTimeout: defaultWALTruncateBusyTimeout,
		journalSizeLimit:    defaultJournalSizeLimit,
	}
	if value, ok := os.LookupEnv("ANX_SQLITE_WAL_WATCHDOG"); ok {
		parsed, err := strconv.ParseBool(strings.TrimSpace(value))
		if err != nil {
			return config, fmt.Errorf("invalid ANX_SQLITE_WAL_WATCHDOG: must be a boolean")
		}
		config.enabled = parsed
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
	if value, ok := os.LookupEnv("ANX_SQLITE_JOURNAL_SIZE_LIMIT_BYTES"); ok {
		parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err != nil || parsed <= 0 {
			return config, fmt.Errorf("invalid ANX_SQLITE_JOURNAL_SIZE_LIMIT_BYTES: must be a positive integer")
		}
		config.journalSizeLimit = parsed
	}
	return config, nil
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
	return watchdog
}

func (w *Workspace) startWALWatchdog(config sqliteMaintenanceConfig) {
	if !config.enabled {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	watchdog := newWALWatchdog(w.db, w.layout.DatabasePath+"-wal", config)
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
	truncate = valid && passive.checkpointedFrames == passive.logFrames
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
