package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func compactTestSource(t *testing.T, path string, wal bool) *sql.DB {
	t.Helper()
	db := openCompactTestDB(t, path)
	db.SetMaxOpenConns(1)
	pragmas := []string{`PRAGMA page_size=4096`, `PRAGMA auto_vacuum=NONE`}
	if wal {
		pragmas = append(pragmas, `PRAGMA journal_mode=WAL`)
	}
	for _, q := range append(pragmas,
		`CREATE TABLE items(id INTEGER PRIMARY KEY, body BLOB)`,
		`CREATE TABLE pm_records(kind TEXT, id TEXT)`,
		`CREATE INDEX items_body ON items(body)`,
	) {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		if _, err := tx.Exec(`INSERT INTO items(id, body) VALUES(?, ?)`, i, strings.Repeat("x", 128)); err != nil {
			_ = tx.Rollback()
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"c1", "c2"} {
		if _, err := db.Exec(`INSERT INTO pm_records(kind, id) VALUES('card', ?)`, id); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func compactFileHash(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func compactSchemaSnapshot(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`SELECT type, name, tbl_name, coalesce(sql, '') FROM sqlite_schema ORDER BY type, name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var typ, name, tblName, sqlText string
		if err := rows.Scan(&typ, &name, &tblName, &sqlText); err != nil {
			t.Fatal(err)
		}
		result = append(result, typ+"\x00"+name+"\x00"+tblName+"\x00"+sqlText)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}

func compactPMRows(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`SELECT rowid, kind, id FROM pm_records ORDER BY rowid`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var rowid int64
		var kind, id string
		if err := rows.Scan(&rowid, &kind, &id); err != nil {
			t.Fatal(err)
		}
		result = append(result, strconv.FormatInt(rowid, 10)+"\x00"+kind+"\x00"+id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestCompactSQLiteOfflineQuickVerify(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "state.sqlite")
	destination := filepath.Join(dir, "state.compact.sqlite")
	db := compactTestSource(t, source, true)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	beforeInfo, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	beforeHash := compactFileHash(t, source)

	result, err := CompactSQLiteOffline(context.Background(), source, destination, CompactOptions{
		ReserveBytes: 1,
		Verify:       CompactVerifyQuick,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Verified || result.DryRun {
		t.Fatalf("unexpected result flags: %+v", result)
	}
	if result.AutoVacuum != 2 || result.PageSize != 4096 {
		t.Fatalf("result auto_vacuum=%d page_size=%d, want 2/4096", result.AutoVacuum, result.PageSize)
	}
	afterInfo, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	if afterInfo.Size() != beforeInfo.Size() || compactFileHash(t, source) != beforeHash {
		t.Fatal("quick compaction modified the source")
	}

	output := openCompactTestDB(t, destination)
	defer output.Close()
	var check string
	if err := output.QueryRow(`PRAGMA quick_check`).Scan(&check); err != nil {
		t.Fatal(err)
	}
	if check != "ok" {
		t.Fatalf("output quick_check = %q", check)
	}
	var autoVacuum, pageSize int
	if err := output.QueryRow(`PRAGMA auto_vacuum`).Scan(&autoVacuum); err != nil {
		t.Fatal(err)
	}
	if err := output.QueryRow(`PRAGMA page_size`).Scan(&pageSize); err != nil {
		t.Fatal(err)
	}
	if autoVacuum != 2 || pageSize != 4096 {
		t.Fatalf("output auto_vacuum=%d page_size=%d, want 2/4096", autoVacuum, pageSize)
	}
	var items, pm int
	if err := output.QueryRow(`SELECT count(*) FROM items`).Scan(&items); err != nil {
		t.Fatal(err)
	}
	if err := output.QueryRow(`SELECT count(*) FROM pm_records`).Scan(&pm); err != nil {
		t.Fatal(err)
	}
	if items != 100 || pm != 2 {
		t.Fatalf("output rows items=%d pm=%d", items, pm)
	}

	sourceDB := openCompactTestDB(t, source)
	defer sourceDB.Close()
	if got, want := compactSchemaSnapshot(t, output), compactSchemaSnapshot(t, sourceDB); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("output schema differs from source:\n%v\n%v", got, want)
	}
	if got, want := compactPMRows(t, output), compactPMRows(t, sourceDB); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("output PM rows differ from source: %v vs %v", got, want)
	}
}

func TestCompactSQLiteOfflineQuickVerifyDetectsCountMismatch(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "state.sqlite")
	destination := filepath.Join(dir, "state.compact.sqlite")
	db := compactTestSource(t, source, false)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	_, err := CompactSQLiteOffline(context.Background(), source, destination, CompactOptions{
		ReserveBytes: 1,
		Verify:       CompactVerifyQuick,
		testAfterVacuumInto: func(destination string) error {
			output, err := sql.Open("sqlite", "file:"+destination)
			if err != nil {
				return err
			}
			defer output.Close()
			_, err = output.Exec(`DELETE FROM items WHERE id=0`)
			return err
		},
	})
	if err == nil || !strings.Contains(err.Error(), "differs from source") {
		t.Fatalf("expected count mismatch, got %v", err)
	}
	if _, statErr := os.Stat(destination); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("failed quick compaction retained output: %v", statErr)
	}
}

func TestCompactSQLiteOfflineQuickVerifyDetectsSchemaMismatch(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "state.sqlite")
	destination := filepath.Join(dir, "state.compact.sqlite")
	db := compactTestSource(t, source, false)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	_, err := CompactSQLiteOffline(context.Background(), source, destination, CompactOptions{
		ReserveBytes: 1,
		Verify:       CompactVerifyQuick,
		testAfterVacuumInto: func(destination string) error {
			output, err := sql.Open("sqlite", "file:"+destination)
			if err != nil {
				return err
			}
			defer output.Close()
			_, err = output.Exec(`CREATE TABLE injected(id INTEGER PRIMARY KEY)`)
			return err
		},
	})
	if err == nil || !strings.Contains(err.Error(), "schema differs") {
		t.Fatalf("expected schema mismatch, got %v", err)
	}
	if _, statErr := os.Stat(destination); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("failed quick compaction retained output: %v", statErr)
	}
}

func TestCompactSQLiteOfflineQuickVerifyDetectsPMMismatch(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "state.sqlite")
	destination := filepath.Join(dir, "state.compact.sqlite")
	db := compactTestSource(t, source, false)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	_, err := CompactSQLiteOffline(context.Background(), source, destination, CompactOptions{
		ReserveBytes: 1,
		Verify:       CompactVerifyQuick,
		testAfterVacuumInto: func(destination string) error {
			output, err := sql.Open("sqlite", "file:"+destination)
			if err != nil {
				return err
			}
			defer output.Close()
			if _, err = output.Exec(`DELETE FROM pm_records WHERE id='c1'`); err != nil {
				return err
			}
			_, err = output.Exec(`INSERT INTO pm_records(rowid, kind, id) VALUES(999, 'card', 'c1')`)
			return err
		},
	})
	if err == nil || !strings.Contains(err.Error(), "PM cursor identities") {
		t.Fatalf("expected PM identity mismatch, got %v", err)
	}
	if _, statErr := os.Stat(destination); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("failed quick compaction retained output: %v", statErr)
	}
}

func TestCompactSQLiteOfflineFullVerifyRunsExcept(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "state.sqlite")
	db := compactTestSource(t, source, false)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	mutate := func(destination string) error {
		output, err := sql.Open("sqlite", "file:"+destination)
		if err != nil {
			return err
		}
		defer output.Close()
		_, err = output.Exec(`UPDATE items SET body='changed' WHERE id=0`)
		return err
	}

	fullDestination := filepath.Join(dir, "state.full.sqlite")
	_, err := CompactSQLiteOffline(context.Background(), source, fullDestination, CompactOptions{
		ReserveBytes:        1,
		Verify:              CompactVerifyFull,
		testAfterVacuumInto: mutate,
	})
	if err == nil || !strings.Contains(err.Error(), "differs from source") {
		t.Fatalf("expected full EXCEPT mismatch, got %v", err)
	}
	if _, statErr := os.Stat(fullDestination); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("failed full compaction retained output: %v", statErr)
	}

	quickDestination := filepath.Join(dir, "state.quick.sqlite")
	if _, err := CompactSQLiteOffline(context.Background(), source, quickDestination, CompactOptions{
		ReserveBytes:        1,
		Verify:              CompactVerifyQuick,
		testAfterVacuumInto: mutate,
	}); err != nil {
		t.Fatalf("quick verification should skip EXCEPT: %v", err)
	}
}

func TestCompactSQLiteOfflineOnlineDryRun(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "state.sqlite")
	destination := filepath.Join(dir, "state.compact.sqlite")
	db := compactTestSource(t, source, true)
	if _, err := db.Exec(`INSERT INTO items(body) VALUES('committed')`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO items(body) VALUES('uncommitted')`); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	beforeInfo, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	beforeHash := compactFileHash(t, source)

	var phaseMu sync.Mutex
	seen := map[CompactPhase]bool{}
	result, err := CompactSQLiteOffline(context.Background(), source, destination, CompactOptions{
		ReserveBytes: 1,
		Verify:       CompactVerifyQuick,
		OnlineDryRun: true,
		PhaseObserver: func(phase CompactPhase, _ time.Duration) {
			phaseMu.Lock()
			seen[phase] = true
			phaseMu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, phase := range []CompactPhase{CompactPhaseVacuumInto, CompactPhaseCheck, CompactPhaseCounts, CompactPhasePMIdentity, CompactPhaseTotal} {
		if !seen[phase] {
			t.Fatalf("online dry run did not time phase %s (saw %v)", phase, seen)
		}
	}
	if seen[CompactPhaseExcept] {
		t.Fatal("online dry run must not run EXCEPT passes")
	}
	if !result.DryRun || result.Verified {
		t.Fatalf("unexpected dry-run result flags: %+v", result)
	}
	if result.AutoVacuum != 2 || result.PageSize != 4096 {
		t.Fatalf("dry-run auto_vacuum=%d page_size=%d, want 2/4096", result.AutoVacuum, result.PageSize)
	}
	afterInfo, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	if afterInfo.Size() != beforeInfo.Size() || compactFileHash(t, source) != beforeHash {
		t.Fatal("online dry run modified the source")
	}
	if _, err := os.Stat(filepath.Join(dir, ".scope-serving.lock")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("online dry run acquired the serving lease: %v", err)
	}

	output := openCompactTestDB(t, destination)
	var check string
	if err := output.QueryRow(`PRAGMA quick_check`).Scan(&check); err != nil {
		t.Fatal(err)
	}
	if check != "ok" {
		t.Fatalf("dry-run output quick_check = %q", check)
	}
	var rows int
	if err := output.QueryRow(`SELECT count(*) FROM items`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 101 {
		t.Fatalf("dry-run output rows = %d, want 101 committed rows", rows)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}

	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO items(body) VALUES('after')`); err != nil {
		t.Fatalf("source was not writable after the dry run: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCompactSQLiteOfflineOnlineDryRunRefusesFull(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "state.sqlite")
	destination := filepath.Join(dir, "state.compact.sqlite")
	db := compactTestSource(t, source, true)
	defer db.Close()
	_, err := CompactSQLiteOffline(context.Background(), source, destination, CompactOptions{
		ReserveBytes: 1,
		Verify:       CompactVerifyFull,
		OnlineDryRun: true,
	})
	if err == nil || !strings.Contains(err.Error(), "quick") {
		t.Fatalf("expected full dry-run refusal, got %v", err)
	}
	if _, statErr := os.Stat(destination); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("refused dry run created a destination: %v", statErr)
	}
}

func TestCompactSQLiteOfflinePhaseObserver(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "state.sqlite")
	destination := filepath.Join(dir, "state.compact.sqlite")
	db := compactTestSource(t, source, false)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var phases []CompactPhase
	observer := func(phase CompactPhase, elapsed time.Duration) {
		mu.Lock()
		defer mu.Unlock()
		phases = append(phases, phase)
	}
	if _, err := CompactSQLiteOffline(context.Background(), source, destination, CompactOptions{
		ReserveBytes:  1,
		Verify:        CompactVerifyQuick,
		PhaseObserver: observer,
	}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, want := range []CompactPhase{
		CompactPhasePreflight,
		CompactPhaseVacuumInto,
		CompactPhaseCheck,
		CompactPhaseSchemaSettings,
		CompactPhaseCounts,
		CompactPhasePMIdentity,
		CompactPhaseFSync,
		CompactPhaseTotal,
	} {
		found := false
		for _, phase := range phases {
			if phase == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("phase %q missing from %v", want, phases)
		}
	}
	for _, phase := range phases {
		if phase == CompactPhaseExcept {
			t.Fatalf("quick verification reported the EXCEPT phase: %v", phases)
		}
	}
}

func TestCompactSQLiteOfflinePhaseObserverReportsFailedPhase(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "state.sqlite")
	destination := filepath.Join(dir, "state.compact.sqlite")
	db := compactTestSource(t, source, false)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.Mutex
	var phases []CompactPhase
	observer := func(phase CompactPhase, elapsed time.Duration) {
		mu.Lock()
		defer mu.Unlock()
		phases = append(phases, phase)
	}
	_, err := CompactSQLiteOffline(ctx, source, destination, CompactOptions{
		ReserveBytes:  1,
		Verify:        CompactVerifyQuick,
		PhaseObserver: observer,
		testAfterVacuumInto: func(destination string) error {
			cancel()
			return nil
		},
	})
	if err == nil {
		t.Fatal("expected context cancellation")
	}
	mu.Lock()
	defer mu.Unlock()
	var sawVacuum, sawCheck, sawTotal bool
	for _, phase := range phases {
		switch phase {
		case CompactPhaseVacuumInto:
			sawVacuum = true
		case CompactPhaseCheck:
			sawCheck = true
		case CompactPhaseTotal:
			sawTotal = true
		}
	}
	if !sawVacuum || !sawCheck || !sawTotal {
		t.Fatalf("expected vacuum_into, failed check and total phases, got %v", phases)
	}
	if _, statErr := os.Stat(destination); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("canceled compaction retained output: %v", statErr)
	}
}

func TestSQLiteReadOnlyDSNKeepsModeAndBusyTimeout(t *testing.T) {
	dsn := sqliteReadOnlyDSN("/data/ws dir/state?.sqlite")
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse %q: %v", dsn, err)
	}
	if parsed.Path != "/data/ws dir/state?.sqlite" {
		t.Fatalf("path = %q", parsed.Path)
	}
	q := parsed.Query()
	if q.Get("mode") != "ro" {
		t.Fatalf("mode = %q, want ro (dsn %q)", q.Get("mode"), dsn)
	}
	if q.Get("_pragma") != "busy_timeout(20000)" {
		t.Fatalf("_pragma = %q (dsn %q)", q.Get("_pragma"), dsn)
	}
}
