package storage

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func openCompactTestDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestCompactSQLiteOfflineSetsIncrementalAutoVacuumAndPreservesRelations(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "state.sqlite")
	destination := filepath.Join(dir, "state.compact.sqlite")
	db := openCompactTestDB(t, source)
	for _, q := range []string{
		`PRAGMA page_size=4096`,
		`PRAGMA auto_vacuum=NONE`,
		`CREATE TABLE items(id INTEGER PRIMARY KEY, body BLOB)`,
		`CREATE TABLE pm_records(kind TEXT, id TEXT)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 200; i++ {
		if _, err := tx.Exec(`INSERT INTO items(id, body) VALUES(?, ?)`, i, strings.Repeat("x", 256)); err != nil {
			_ = tx.Rollback()
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO pm_records(kind, id) VALUES('card','c1')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	result, err := CompactSQLiteOffline(context.Background(), source, destination, CompactOptions{ReserveBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result.AutoVacuum != 2 {
		t.Fatalf("result auto_vacuum = %d, want 2", result.AutoVacuum)
	}
	if result.PageSize != 4096 {
		t.Fatalf("result page_size = %d, want 4096", result.PageSize)
	}
	if result.DestinationBytes <= 0 || result.SourceBytes <= 0 {
		t.Fatalf("unexpected sizes: %+v", result)
	}
	if result.SavedBytes != result.SourceBytes-result.DestinationBytes {
		t.Fatalf("saved bytes = %d", result.SavedBytes)
	}
	if result.WALBytes != 0 {
		t.Fatalf("result WAL bytes = %d, want 0 for a checkpointed source", result.WALBytes)
	}

	output := openCompactTestDB(t, destination)
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
	if items != 200 || pm != 1 {
		t.Fatalf("output rows items=%d pm=%d", items, pm)
	}

	sourceDB := openCompactTestDB(t, source)
	if err := sourceDB.QueryRow(`PRAGMA auto_vacuum`).Scan(&autoVacuum); err != nil {
		t.Fatal(err)
	}
	if autoVacuum != 0 {
		t.Fatalf("source auto_vacuum changed to %d", autoVacuum)
	}
	if _, err := sourceDB.Exec(`ATTACH DATABASE ? AS compact_output`, destination); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"items", "pm_records"} {
		for _, q := range []string{
			`SELECT EXISTS(SELECT * FROM main.` + table + ` EXCEPT SELECT * FROM compact_output.` + table + `)`,
			`SELECT EXISTS(SELECT * FROM compact_output.` + table + ` EXCEPT SELECT * FROM main.` + table + `)`,
		} {
			var mismatch bool
			if err := sourceDB.QueryRow(q).Scan(&mismatch); err != nil {
				t.Fatal(err)
			}
			if mismatch {
				t.Fatalf("compacted relation %s differs from source", table)
			}
		}
	}
}

func TestCompactSQLiteOfflineRefusesInsufficientFreeSpace(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "state.sqlite")
	destination := filepath.Join(dir, "state.compact.sqlite")
	db := openCompactTestDB(t, source)
	if _, err := db.Exec(`CREATE TABLE items(id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	_, err := CompactSQLiteOffline(context.Background(), source, destination, CompactOptions{ReserveBytes: math.MaxInt64 / 2})
	if err == nil || !strings.Contains(err.Error(), "insufficient free space") {
		t.Fatalf("expected free-space refusal, got %v", err)
	}
	if _, statErr := os.Stat(destination); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("refused compaction created a destination: %v", statErr)
	}
}

func TestCompactSQLiteOfflineRejectsInvalidReserve(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "state.sqlite")
	db := openCompactTestDB(t, source)
	if _, err := db.Exec(`CREATE TABLE items(id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		reserve int64
		want    string
	}{
		{name: "negative", reserve: -1, want: "reserve bytes must not be negative"},
		{name: "overflow", reserve: math.MaxInt64, want: "free-space estimate overflows"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			destination := filepath.Join(dir, tc.name+".sqlite")
			_, err := CompactSQLiteOffline(context.Background(), source, destination, CompactOptions{ReserveBytes: tc.reserve})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q, got %v", tc.want, err)
			}
			if _, statErr := os.Stat(destination); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("rejected compaction created a destination: %v", statErr)
			}
		})
	}
}

func TestCompactSQLiteOfflineRefusesSamePathAndExistingDestination(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "state.sqlite")
	db := openCompactTestDB(t, source)
	if _, err := db.Exec(`CREATE TABLE items(id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := CompactSQLiteOffline(context.Background(), source, source, CompactOptions{ReserveBytes: 1}); err == nil {
		t.Fatal("accepted the source as destination")
	}
	destination := filepath.Join(dir, "existing.sqlite")
	if err := os.WriteFile(destination, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := CompactSQLiteOffline(context.Background(), source, destination, CompactOptions{ReserveBytes: 1}); err == nil {
		t.Fatal("overwrote an existing destination")
	}
	body, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "keep" {
		t.Fatalf("existing destination changed: %q", body)
	}
}

func TestCompactSQLiteOfflineRemovesFailedOutput(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "state.sqlite")
	destination := filepath.Join(dir, "state.compact.sqlite")
	db := openCompactTestDB(t, source)
	// A WITHOUT ROWID pm_records table makes the PM cursor-identity check fail
	// after VACUUM INTO has already produced the output.
	if _, err := db.Exec(`CREATE TABLE pm_records(kind TEXT, id TEXT, PRIMARY KEY(kind,id)) WITHOUT ROWID`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := CompactSQLiteOffline(context.Background(), source, destination, CompactOptions{ReserveBytes: 1}); err == nil {
		t.Fatal("expected PM identity check to fail")
	}
	if _, err := os.Stat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed compaction retained output: %v", err)
	}

	// Cleanup must detach and close the output before removing it, leaving the
	// destination free for a corrected retry rather than a stale partial file.
	repair := openCompactTestDB(t, source)
	if _, err := repair.Exec(`DROP TABLE pm_records`); err != nil {
		t.Fatal(err)
	}
	if _, err := repair.Exec(`CREATE TABLE pm_records(kind TEXT, id TEXT)`); err != nil {
		t.Fatal(err)
	}
	if err := repair.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := CompactSQLiteOffline(context.Background(), source, destination, CompactOptions{ReserveBytes: 1}); err != nil {
		t.Fatalf("retry after failed cleanup: %v", err)
	}
}
