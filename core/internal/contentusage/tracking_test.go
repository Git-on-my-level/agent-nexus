package contentusage

import (
	"context"
	"database/sql"
	_ "modernc.org/sqlite"
	"testing"
)

func TestTrackingCountsCanonicalChangesAndRollsBack(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx := context.Background()
	if _, err = db.Exec(`CREATE TABLE events(id TEXT PRIMARY KEY,payload_json TEXT,search_text TEXT)`); err != nil {
		t.Fatal(err)
	}
	if err = InstallTable(ctx, db, "events"); err != nil {
		t.Fatal(err)
	}
	version := func() int {
		t.Helper()
		var n int
		if err := db.QueryRow(`SELECT revision FROM background_usage_revision`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	for _, q := range []string{`INSERT INTO events VALUES('a','first','derived')`, `UPDATE events SET payload_json='second' WHERE id='a'`, `UPDATE events SET search_text='new derived' WHERE id='a'`, `UPDATE events SET payload_json=payload_json WHERE id='a'`} {
		if _, err = db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if n := version(); n != 2 {
		t.Fatalf("expected two canonical changes, got %d", n)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`DELETE FROM events`); err != nil {
		t.Fatal(err)
	}
	tx.Rollback()
	if n := version(); n != 2 {
		t.Fatalf("rolled-back deletion changed revision: %d", n)
	}
	// Reinstallation must pick up new billable columns on future upgrades.
	if _, err = db.Exec(`ALTER TABLE events ADD COLUMN summary TEXT`); err != nil {
		t.Fatal(err)
	}
	if err = InstallTable(ctx, db, "events"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE events SET summary='new canonical field'`); err != nil {
		t.Fatal(err)
	}
	if n := version(); n != 3 {
		t.Fatalf("new canonical field was not tracked: %d", n)
	}
	if _, err = db.Exec(`DELETE FROM events`); err != nil {
		t.Fatal(err)
	}
	if n := version(); n != 4 {
		t.Fatal(n)
	}
}
