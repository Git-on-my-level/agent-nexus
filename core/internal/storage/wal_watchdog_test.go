package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

var sqliteMaintenanceEnv = []string{
	"ANX_SQLITE_WAL_WATCHDOG",
	"ANX_SQLITE_WAL_WATCHDOG_INTERVAL",
	"ANX_SQLITE_WAL_WARN_BYTES",
	"ANX_SQLITE_WAL_PINNED_RUNS",
	"ANX_SQLITE_WAL_TRUNCATE_BUSY_TIMEOUT",
	"ANX_SQLITE_JOURNAL_SIZE_LIMIT_BYTES",
	"ANX_SQLITE_INCREMENTAL_VACUUM",
	"ANX_SQLITE_INCREMENTAL_VACUUM_MIN_FREELIST_BYTES",
	"ANX_SQLITE_INCREMENTAL_VACUUM_MIN_FREELIST_PERCENT",
	"ANX_SQLITE_INCREMENTAL_VACUUM_PAGES",
	"ANX_SQLITE_INCREMENTAL_VACUUM_BUSY_TIMEOUT",
}

func clearSQLiteMaintenanceEnv(t *testing.T) {
	t.Helper()
	for _, name := range sqliteMaintenanceEnv {
		name := name
		value, ok := os.LookupEnv(name)
		if ok {
			t.Cleanup(func() { _ = os.Setenv(name, value) })
		} else {
			t.Cleanup(func() { _ = os.Unsetenv(name) })
		}
		_ = os.Unsetenv(name)
	}
}

func TestSQLiteMaintenanceConfigDefaults(t *testing.T) {
	clearSQLiteMaintenanceEnv(t)
	config, err := loadSQLiteMaintenanceConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !config.enabled {
		t.Fatal("watchdog should be enabled by default")
	}
	if config.interval != 5*time.Minute {
		t.Fatalf("interval = %v", config.interval)
	}
	if config.warnBytes != 512*1024*1024 {
		t.Fatalf("warn bytes = %d", config.warnBytes)
	}
	if config.pinnedRuns != 3 {
		t.Fatalf("pinned runs = %d", config.pinnedRuns)
	}
	if config.truncateBusyTimeout != 250*time.Millisecond {
		t.Fatalf("truncate busy timeout = %v", config.truncateBusyTimeout)
	}
	if config.journalSizeLimit != 64*1024*1024 {
		t.Fatalf("journal size limit = %d", config.journalSizeLimit)
	}
	if !config.incrementalVacuum.enabled {
		t.Fatal("incremental vacuum should be enabled by default")
	}
	if config.incrementalVacuum.minFreelistBytes != 64*1024*1024 {
		t.Fatalf("incremental min freelist bytes = %d", config.incrementalVacuum.minFreelistBytes)
	}
	if config.incrementalVacuum.minFreelistPercent != 10 {
		t.Fatalf("incremental min freelist percent = %d", config.incrementalVacuum.minFreelistPercent)
	}
	if config.incrementalVacuum.pages != 2048 {
		t.Fatalf("incremental pages = %d", config.incrementalVacuum.pages)
	}
	if config.incrementalVacuum.busyTimeout != 250*time.Millisecond {
		t.Fatalf("incremental busy timeout = %v", config.incrementalVacuum.busyTimeout)
	}
}

func TestSQLiteMaintenanceConfigOverrides(t *testing.T) {
	clearSQLiteMaintenanceEnv(t)
	t.Setenv("ANX_SQLITE_WAL_WATCHDOG", "true")
	t.Setenv("ANX_SQLITE_WAL_WATCHDOG_INTERVAL", "2m")
	t.Setenv("ANX_SQLITE_WAL_WARN_BYTES", "1234")
	t.Setenv("ANX_SQLITE_WAL_PINNED_RUNS", "7")
	t.Setenv("ANX_SQLITE_WAL_TRUNCATE_BUSY_TIMEOUT", "75ms")
	t.Setenv("ANX_SQLITE_JOURNAL_SIZE_LIMIT_BYTES", "5678")
	t.Setenv("ANX_SQLITE_INCREMENTAL_VACUUM", "true")
	t.Setenv("ANX_SQLITE_INCREMENTAL_VACUUM_MIN_FREELIST_BYTES", "123456")
	t.Setenv("ANX_SQLITE_INCREMENTAL_VACUUM_MIN_FREELIST_PERCENT", "25")
	t.Setenv("ANX_SQLITE_INCREMENTAL_VACUUM_PAGES", "512")
	t.Setenv("ANX_SQLITE_INCREMENTAL_VACUUM_BUSY_TIMEOUT", "50ms")
	config, err := loadSQLiteMaintenanceConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !config.enabled {
		t.Fatal("watchdog should be enabled")
	}
	if config.interval != 2*time.Minute || config.warnBytes != 1234 || config.pinnedRuns != 7 ||
		config.truncateBusyTimeout != 75*time.Millisecond || config.journalSizeLimit != 5678 {
		t.Fatalf("unexpected config: %#v", config)
	}
	if !config.incrementalVacuum.enabled ||
		config.incrementalVacuum.minFreelistBytes != 123456 ||
		config.incrementalVacuum.minFreelistPercent != 25 ||
		config.incrementalVacuum.pages != 512 ||
		config.incrementalVacuum.busyTimeout != 50*time.Millisecond {
		t.Fatalf("unexpected incremental vacuum config: %#v", config.incrementalVacuum)
	}
}

func TestSQLiteMaintenanceConfigDisabledSkipsTuning(t *testing.T) {
	t.Run("journal limit still applies", func(t *testing.T) {
		clearSQLiteMaintenanceEnv(t)
		t.Setenv("ANX_SQLITE_WAL_WATCHDOG", "false")
		t.Setenv("ANX_SQLITE_WAL_WATCHDOG_INTERVAL", "not-a-duration")
		t.Setenv("ANX_SQLITE_WAL_WARN_BYTES", "not-a-number")
		t.Setenv("ANX_SQLITE_INCREMENTAL_VACUUM", "not-a-bool")
		t.Setenv("ANX_SQLITE_INCREMENTAL_VACUUM_PAGES", "not-a-number")
		t.Setenv("ANX_SQLITE_JOURNAL_SIZE_LIMIT_BYTES", "5678")
		config, err := loadSQLiteMaintenanceConfig()
		if err != nil {
			t.Fatalf("disabled watchdog rejected tuning values: %v", err)
		}
		if config.enabled {
			t.Fatal("watchdog should be disabled")
		}
		if config.interval != defaultWALWatchdogInterval || config.warnBytes != defaultWALWarnBytes {
			t.Fatalf("disabled watchdog parsed tuning values: %#v", config)
		}
		if config.journalSizeLimit != 5678 {
			t.Fatalf("journal size limit = %d, want 5678", config.journalSizeLimit)
		}
	})

	t.Run("invalid journal limit still rejected", func(t *testing.T) {
		clearSQLiteMaintenanceEnv(t)
		t.Setenv("ANX_SQLITE_WAL_WATCHDOG", "false")
		t.Setenv("ANX_SQLITE_JOURNAL_SIZE_LIMIT_BYTES", "0")
		if _, err := loadSQLiteMaintenanceConfig(); err == nil {
			t.Fatal("disabled watchdog accepted an invalid journal size limit")
		}
	})
}

func TestSQLiteMaintenanceConfigDisabledIncrementalVacuumSkipsTuning(t *testing.T) {
	clearSQLiteMaintenanceEnv(t)
	t.Setenv("ANX_SQLITE_INCREMENTAL_VACUUM", "false")
	t.Setenv("ANX_SQLITE_INCREMENTAL_VACUUM_MIN_FREELIST_BYTES", "not-a-number")
	t.Setenv("ANX_SQLITE_INCREMENTAL_VACUUM_MIN_FREELIST_PERCENT", "not-a-number")
	t.Setenv("ANX_SQLITE_INCREMENTAL_VACUUM_PAGES", "not-a-number")
	t.Setenv("ANX_SQLITE_INCREMENTAL_VACUUM_BUSY_TIMEOUT", "not-a-duration")
	config, err := loadSQLiteMaintenanceConfig()
	if err != nil {
		t.Fatalf("disabled incremental vacuum rejected tuning values: %v", err)
	}
	if config.incrementalVacuum.enabled {
		t.Fatal("incremental vacuum should be disabled")
	}
	if config.incrementalVacuum.minFreelistBytes != defaultIncrementalVacuumMinFreelistBytes ||
		config.incrementalVacuum.minFreelistPercent != defaultIncrementalVacuumMinFreelistPercent ||
		config.incrementalVacuum.pages != defaultIncrementalVacuumPages ||
		config.incrementalVacuum.busyTimeout != defaultIncrementalVacuumBusyTimeout {
		t.Fatalf("disabled incremental vacuum parsed tuning values: %#v", config.incrementalVacuum)
	}
}

func TestSQLiteMaintenanceConfigRejectsInvalidValues(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
	}{
		{name: "ANX_SQLITE_WAL_WATCHDOG", value: "not-a-bool"},
		{name: "ANX_SQLITE_WAL_WATCHDOG_INTERVAL", value: "0s"},
		{name: "ANX_SQLITE_WAL_WATCHDOG_INTERVAL", value: "-1s"},
		{name: "ANX_SQLITE_WAL_WATCHDOG_INTERVAL", value: "nope"},
		{name: "ANX_SQLITE_WAL_WARN_BYTES", value: "0"},
		{name: "ANX_SQLITE_WAL_WARN_BYTES", value: "-1"},
		{name: "ANX_SQLITE_WAL_WARN_BYTES", value: "nope"},
		{name: "ANX_SQLITE_WAL_PINNED_RUNS", value: "0"},
		{name: "ANX_SQLITE_WAL_PINNED_RUNS", value: "-1"},
		{name: "ANX_SQLITE_WAL_PINNED_RUNS", value: "nope"},
		{name: "ANX_SQLITE_WAL_TRUNCATE_BUSY_TIMEOUT", value: "0s"},
		{name: "ANX_SQLITE_WAL_TRUNCATE_BUSY_TIMEOUT", value: "-1ms"},
		{name: "ANX_SQLITE_WAL_TRUNCATE_BUSY_TIMEOUT", value: "nope"},
		{name: "ANX_SQLITE_JOURNAL_SIZE_LIMIT_BYTES", value: "0"},
		{name: "ANX_SQLITE_JOURNAL_SIZE_LIMIT_BYTES", value: "-1"},
		{name: "ANX_SQLITE_JOURNAL_SIZE_LIMIT_BYTES", value: "nope"},
		{name: "ANX_SQLITE_INCREMENTAL_VACUUM", value: "not-a-bool"},
		{name: "ANX_SQLITE_INCREMENTAL_VACUUM_MIN_FREELIST_BYTES", value: "0"},
		{name: "ANX_SQLITE_INCREMENTAL_VACUUM_MIN_FREELIST_BYTES", value: "-1"},
		{name: "ANX_SQLITE_INCREMENTAL_VACUUM_MIN_FREELIST_BYTES", value: "nope"},
		{name: "ANX_SQLITE_INCREMENTAL_VACUUM_MIN_FREELIST_PERCENT", value: "-1"},
		{name: "ANX_SQLITE_INCREMENTAL_VACUUM_MIN_FREELIST_PERCENT", value: "101"},
		{name: "ANX_SQLITE_INCREMENTAL_VACUUM_MIN_FREELIST_PERCENT", value: "nope"},
		{name: "ANX_SQLITE_INCREMENTAL_VACUUM_PAGES", value: "0"},
		{name: "ANX_SQLITE_INCREMENTAL_VACUUM_PAGES", value: "-1"},
		{name: "ANX_SQLITE_INCREMENTAL_VACUUM_PAGES", value: "nope"},
		{name: "ANX_SQLITE_INCREMENTAL_VACUUM_BUSY_TIMEOUT", value: "0s"},
		{name: "ANX_SQLITE_INCREMENTAL_VACUUM_BUSY_TIMEOUT", value: "-1ms"},
		{name: "ANX_SQLITE_INCREMENTAL_VACUUM_BUSY_TIMEOUT", value: "nope"},
	} {
		t.Run(tc.name+"="+tc.value, func(t *testing.T) {
			clearSQLiteMaintenanceEnv(t)
			t.Setenv(tc.name, tc.value)
			if _, err := loadSQLiteMaintenanceConfig(); err == nil {
				t.Fatalf("expected %s=%q to be rejected", tc.name, tc.value)
			}
		})
	}
}

func TestSQLiteDSNJournalSizeLimitAndPragmas(t *testing.T) {
	clearSQLiteMaintenanceEnv(t)
	ws, err := InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	var limit int64
	if err := ws.DB().QueryRow(`PRAGMA journal_size_limit`).Scan(&limit); err != nil {
		t.Fatal(err)
	}
	if limit != defaultJournalSizeLimit {
		t.Fatalf("journal_size_limit = %d, want %d", limit, defaultJournalSizeLimit)
	}
	var busy int
	if err := ws.DB().QueryRow(`PRAGMA busy_timeout`).Scan(&busy); err != nil {
		t.Fatal(err)
	}
	if busy != 20000 {
		t.Fatalf("busy_timeout = %d, want 20000", busy)
	}
	var mode string
	if err := ws.DB().QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(mode, "wal") {
		t.Fatalf("journal_mode = %q, want WAL", mode)
	}
	if dsn := sqliteDSN("state.sqlite"); !strings.Contains(dsn, "_txlock=immediate") {
		t.Fatalf("DSN lost _txlock=immediate: %s", dsn)
	}
}

func TestSQLiteDSNConfiguredJournalSizeLimit(t *testing.T) {
	clearSQLiteMaintenanceEnv(t)
	t.Setenv("ANX_SQLITE_JOURNAL_SIZE_LIMIT_BYTES", "123456")
	ws, err := InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	var limit int64
	if err := ws.DB().QueryRow(`PRAGMA journal_size_limit`).Scan(&limit); err != nil {
		t.Fatal(err)
	}
	if limit != 123456 {
		t.Fatalf("journal_size_limit = %d, want 123456", limit)
	}
}

type fakeWALExecutor struct {
	passive       walCheckpointResult
	passiveErr    error
	truncate      walCheckpointResult
	truncateErr   error
	truncateCalls int
	closed        bool
}

func (f *fakeWALExecutor) Passive(context.Context) (walCheckpointResult, error) {
	return f.passive, f.passiveErr
}

func (f *fakeWALExecutor) TruncateCheckpoint(context.Context) (walCheckpointResult, error) {
	f.truncateCalls++
	return f.truncate, f.truncateErr
}

func (f *fakeWALExecutor) Close(context.Context) error {
	f.closed = true
	return nil
}

type fakeIncrementalVacuumExecutor struct {
	result incrementalVacuumResult
	err    error
	calls  int
	closed bool
}

func (f *fakeIncrementalVacuumExecutor) Reclaim(context.Context, incrementalVacuumConfig) (incrementalVacuumResult, error) {
	f.calls++
	return f.result, f.err
}

func (f *fakeIncrementalVacuumExecutor) Close(context.Context) error {
	f.closed = true
	return nil
}

func TestWALWatchdogIncrementalVacuumRunsAndStops(t *testing.T) {
	config := sqliteMaintenanceConfig{
		enabled:             true,
		interval:            10 * time.Millisecond,
		warnBytes:           defaultWALWarnBytes,
		pinnedRuns:          defaultWALPinnedRuns,
		truncateBusyTimeout: defaultWALTruncateBusyTimeout,
		incrementalVacuum: incrementalVacuumConfig{
			enabled:            true,
			minFreelistBytes:   1,
			minFreelistPercent: 0,
			pages:              2048,
			busyTimeout:        250 * time.Millisecond,
		},
	}
	fake := &fakeIncrementalVacuumExecutor{
		result: incrementalVacuumResult{
			reclaimed: 12,
			stats:     incrementalVacuumStats{freelistCount: 100, pageCount: 1000},
		},
	}
	var logs []string
	watchdog := &walWatchdog{
		config:  config,
		statWAL: func() (int64, error) { return 0, nil },
		openExecutor: func(context.Context) (walCheckpointExecutor, error) {
			return &fakeWALExecutor{}, nil
		},
		openVacuum: func(context.Context) (incrementalVacuumExecutor, error) {
			return fake, nil
		},
		logf: func(format string, args ...any) {
			logs = append(logs, fmt.Sprintf(format, args...))
		},
	}
	watchdog.tick(context.Background())
	if fake.calls != 1 || !fake.closed {
		t.Fatalf("incremental vacuum calls=%d closed=%v", fake.calls, fake.closed)
	}
	if len(logs) != 1 || !strings.Contains(logs[0], "pages_reclaimed=12") {
		t.Fatalf("missing reclaim log: %v", logs)
	}

	ctx, cancel := context.WithCancel(context.Background())
	watchdog.done = make(chan struct{})
	go watchdog.run(ctx)
	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case <-watchdog.done:
	case <-time.After(time.Second):
		t.Fatal("watchdog did not stop after cancellation")
	}
}

func TestWALWatchdogStateMachine(t *testing.T) {
	config := sqliteMaintenanceConfig{
		enabled:             true,
		interval:            time.Minute,
		warnBytes:           100,
		pinnedRuns:          3,
		truncateBusyTimeout: 250 * time.Millisecond,
		journalSizeLimit:    defaultJournalSizeLimit,
	}
	var logs []string
	fake := &fakeWALExecutor{}
	walBytes := int64(200)
	watchdog := &walWatchdog{
		config:  config,
		statWAL: func() (int64, error) { return walBytes, nil },
		openExecutor: func(context.Context) (walCheckpointExecutor, error) {
			return fake, nil
		},
		logf: func(format string, args ...any) {
			logs = append(logs, fmt.Sprintf(format, args...))
		},
	}
	tick := func() { watchdog.tick(context.Background()) }

	// Below threshold is never a pinned run.
	walBytes = 50
	fake.passive = walCheckpointResult{busy: 0, logFrames: 10, checkpointedFrames: 5}
	tick()
	if len(logs) != 0 || fake.truncateCalls != 0 {
		t.Fatalf("below threshold logged or truncated: logs=%v truncates=%d", logs, fake.truncateCalls)
	}

	// Incomplete above threshold on runs 1 and 2 stays quiet.
	walBytes = 200
	tick()
	tick()
	if len(logs) != 0 {
		t.Fatalf("warned before the third run: %v", logs)
	}
	if fake.truncateCalls != 0 {
		t.Fatalf("truncated while incomplete: %d", fake.truncateCalls)
	}

	// Run 3 warns exactly once with numeric fields and no content.
	tick()
	if len(logs) != 1 {
		t.Fatalf("expected one warning, got %v", logs)
	}
	for _, want := range []string{
		"stage=wal_pinned", "wal_bytes=200", "log_frames=10",
		"checkpointed_frames=5", "busy=0", "consecutive_runs=3",
	} {
		if !strings.Contains(logs[0], want) {
			t.Errorf("warning missing %q: %s", want, logs[0])
		}
	}
	if strings.Contains(logs[0], "/") || strings.Contains(logs[0], "thread") {
		t.Fatalf("warning leaked content or path: %s", logs[0])
	}

	// Run 4 does not repeat the warning.
	tick()
	if len(logs) != 1 {
		t.Fatalf("warning repeated: %v", logs)
	}

	// A complete PASSIVE run above the truncate floor truncates and resets the episode.
	walBytes = defaultWALTruncateFloorBytes + 1
	fake.passive = walCheckpointResult{busy: 0, logFrames: 10, checkpointedFrames: 10}
	tick()
	if fake.truncateCalls != 1 {
		t.Fatalf("complete run did not truncate: %d", fake.truncateCalls)
	}

	// A later episode can warn once again.
	walBytes = 200
	fake.passive = walCheckpointResult{busy: 0, logFrames: 10, checkpointedFrames: 5}
	tick()
	tick()
	tick()
	if len(logs) != 2 {
		t.Fatalf("later episode did not warn once: %v", logs)
	}

	// Busy and invalid frame results are not pinned runs and do not warn.
	fake.passive = walCheckpointResult{busy: 1, logFrames: -1, checkpointedFrames: -1}
	tick()
	fake.passive = walCheckpointResult{busy: 0, logFrames: -1, checkpointedFrames: -1}
	tick()
	if len(logs) != 2 {
		t.Fatalf("busy/invalid result warned: %v", logs)
	}

	// A checkpoint failure logs one bounded maintenance outcome and resets.
	fake.passiveErr = errors.New("private path /secret")
	tick()
	if len(logs) != 3 || !strings.Contains(logs[2], "stage=wal_watchdog") {
		t.Fatalf("failure outcome missing: %v", logs)
	}
	if strings.Contains(logs[2], "/secret") {
		t.Fatalf("failure outcome leaked error text: %s", logs[2])
	}
}

func TestWALWatchdogEvaluateTruncateFloor(t *testing.T) {
	for _, tc := range []struct {
		name     string
		walBytes int64
		passive  walCheckpointResult
		truncate bool
	}{
		{
			name:     "empty WAL complete",
			walBytes: 0,
			passive:  walCheckpointResult{busy: 0, logFrames: 0, checkpointedFrames: 0},
		},
		{
			name:     "tiny WAL complete",
			walBytes: defaultWALTruncateFloorBytes,
			passive:  walCheckpointResult{busy: 0, logFrames: 10, checkpointedFrames: 10},
		},
		{
			name:     "big WAL complete",
			walBytes: defaultWALTruncateFloorBytes + 1,
			passive:  walCheckpointResult{busy: 0, logFrames: 10, checkpointedFrames: 10},
			truncate: true,
		},
		{
			name:     "big WAL incomplete",
			walBytes: defaultWALTruncateFloorBytes + 1,
			passive:  walCheckpointResult{busy: 0, logFrames: 10, checkpointedFrames: 5},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			watchdog := &walWatchdog{config: sqliteMaintenanceConfig{warnBytes: defaultWALWarnBytes, pinnedRuns: defaultWALPinnedRuns}}
			_, truncate := watchdog.evaluate(tc.walBytes, tc.passive)
			if truncate != tc.truncate {
				t.Fatalf("truncate = %v, want %v", truncate, tc.truncate)
			}
		})
	}
}

func TestWALWatchdogPinnedReaderSkipsTruncate(t *testing.T) {
	clearSQLiteMaintenanceEnv(t)
	t.Setenv("ANX_SQLITE_WAL_WATCHDOG", "false")
	ws, err := InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	ctx := context.Background()
	if _, err := ws.DB().ExecContext(ctx, `INSERT INTO actors(id,display_name,tags_json,created_at,metadata_json) VALUES('pinned-1','Pinned','[]','2026-11-11T00:00:00Z','{}')`); err != nil {
		t.Fatal(err)
	}
	tx, err := ws.DB().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM actors`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.DB().ExecContext(ctx, `INSERT INTO actors(id,display_name,tags_json,created_at,metadata_json) VALUES('pinned-2','Pinned','[]','2026-11-11T00:00:00Z','{}')`); err != nil {
		t.Fatal(err)
	}
	conn, err := ws.DB().Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var busy, logFrames, checkpointed int64
	if err := conn.QueryRowContext(ctx, `PRAGMA wal_checkpoint(PASSIVE)`).Scan(&busy, &logFrames, &checkpointed); err != nil {
		t.Fatal(err)
	}
	if busy != 0 || checkpointed >= logFrames {
		t.Fatalf("expected an incomplete PASSIVE checkpoint, got busy=%d log=%d checkpointed=%d", busy, logFrames, checkpointed)
	}

	fake := &fakeWALExecutor{passive: walCheckpointResult{busy: busy, logFrames: logFrames, checkpointedFrames: checkpointed}}
	watchdog := &walWatchdog{
		config:  sqliteMaintenanceConfig{enabled: true, warnBytes: 1, pinnedRuns: 3, truncateBusyTimeout: 250 * time.Millisecond},
		statWAL: func() (int64, error) { return 1 << 30, nil },
		openExecutor: func(context.Context) (walCheckpointExecutor, error) {
			return fake, nil
		},
		logf: func(string, ...any) {},
	}
	watchdog.tick(ctx)
	if fake.truncateCalls != 0 {
		t.Fatalf("TRUNCATE attempted while PASSIVE was incomplete: %d", fake.truncateCalls)
	}
}

func TestWALWatchdogCompletedWALTruncatesAndRestoresTimeout(t *testing.T) {
	clearSQLiteMaintenanceEnv(t)
	t.Setenv("ANX_SQLITE_WAL_WATCHDOG", "false")
	ws, err := InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	ctx := context.Background()
	if _, err := ws.DB().ExecContext(ctx, `INSERT INTO actors(id,display_name,tags_json,created_at,metadata_json) VALUES('complete-1','Complete','[]','2026-11-11T00:00:00Z','{}')`); err != nil {
		t.Fatal(err)
	}
	conn, err := ws.DB().Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	executor := &sqliteWALCheckpointExecutor{conn: conn, truncateBusyTimeout: 250 * time.Millisecond}
	passive, err := executor.Passive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if passive.busy != 0 || passive.checkpointedFrames != passive.logFrames {
		t.Fatalf("PASSIVE was not complete: %#v", passive)
	}
	truncated, err := executor.TruncateCheckpoint(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if truncated.busy != 0 {
		t.Fatalf("TRUNCATE busy=%d", truncated.busy)
	}
	var timeout int
	if err := conn.QueryRowContext(ctx, `PRAGMA busy_timeout`).Scan(&timeout); err != nil {
		t.Fatal(err)
	}
	if timeout != 20000 {
		t.Fatalf("busy_timeout after TRUNCATE = %d, want 20000", timeout)
	}
	if err := executor.Close(ctx); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(ws.Layout().DatabasePath + "-wal")
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err == nil && info.Size() != 0 {
		t.Fatalf("WAL was not truncated: %d bytes", info.Size())
	}
}

func TestWALWatchdogTruncateBusyIsSkipped(t *testing.T) {
	clearSQLiteMaintenanceEnv(t)
	t.Setenv("ANX_SQLITE_WAL_WATCHDOG", "false")
	ws, err := InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	ctx := context.Background()
	if _, err := ws.DB().ExecContext(ctx, `INSERT INTO actors(id,display_name,tags_json,created_at,metadata_json) VALUES('busy-1','Busy','[]','2026-11-11T00:00:00Z','{}')`); err != nil {
		t.Fatal(err)
	}
	writer, err := ws.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Rollback()
	if _, err := writer.ExecContext(ctx, `INSERT INTO actors(id,display_name,tags_json,created_at,metadata_json) VALUES('busy-2','Busy','[]','2026-11-11T00:00:00Z','{}')`); err != nil {
		t.Fatal(err)
	}
	conn, err := ws.DB().Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	executor := &sqliteWALCheckpointExecutor{conn: conn, truncateBusyTimeout: 250 * time.Millisecond}
	start := time.Now()
	result, err := executor.TruncateCheckpoint(ctx)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("busy TRUNCATE returned an error: %v", err)
	}
	if result.busy == 0 {
		t.Fatalf("expected a busy TRUNCATE result, got %#v", result)
	}
	if elapsed < 200*time.Millisecond || elapsed > 2*time.Second {
		t.Fatalf("busy TRUNCATE took %v, want the short configured timeout", elapsed)
	}
}

func TestWALWatchdogDisabledStartsNoWorker(t *testing.T) {
	clearSQLiteMaintenanceEnv(t)
	t.Setenv("ANX_SQLITE_WAL_WATCHDOG", "false")
	ws, err := InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	if ws.walWatchdogCancel != nil || ws.walWatchdogDone != nil {
		t.Fatal("disabled watchdog started a worker")
	}
}

func TestWALWatchdogLifecycleStopsOnClose(t *testing.T) {
	clearSQLiteMaintenanceEnv(t)
	t.Setenv("ANX_SQLITE_WAL_WATCHDOG_INTERVAL", "10ms")
	ws, err := InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	start := time.Now()
	if err := ws.Close(); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Close took %v waiting for the watchdog", elapsed)
	}
	if err := ws.Close(); err != nil {
		t.Fatalf("repeated Close: %v", err)
	}
	if err := ws.Ping(context.Background()); err == nil {
		t.Fatal("Ping succeeded after Close")
	}
}
