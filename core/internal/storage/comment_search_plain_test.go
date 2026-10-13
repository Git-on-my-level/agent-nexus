package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"modernc.org/sqlite"
)

func init() { sql.Register("comment-search-plain", &sqlite.Driver{}) }

// Install the production search triggers on canonical source tables, isolated
// from unrelated ownership triggers that have their own driver contract. The
// external driver has zero user-defined functions, including search functions.
func TestCommentSearchPlainConnectionCaptureAndCatchup(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "plain.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	commentSearchExec(t, db, `CREATE TABLE events(id TEXT PRIMARY KEY,type TEXT,ts TEXT,actor_id TEXT,thread_id TEXT,refs_json TEXT,payload_json TEXT,trashed_at TEXT,archived_at TEXT)`)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = installCommentSearch(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	plain, err := sql.Open("comment-search-plain", path)
	if err != nil {
		t.Fatal(err)
	}
	defer plain.Close()
	var count int
	if err = plain.QueryRow(`SELECT count(*) FROM pragma_function_list WHERE name LIKE 'anx_%'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("plain connection registered functions", count, err)
	}
	ws := &Workspace{db: db}
	check := func(term string, want int) {
		t.Helper()
		if got := commentSearchCount(t, db, `SELECT count(*) FROM comment_search_postings WHERE term=?`, term); got != want {
			t.Fatal(term, got, want)
		}
	}
	for _, q := range []string{
		`INSERT INTO events VALUES('plain','message_posted','now','owner','thread','[]','{"payload":{"text":"café original"}}',NULL,NULL)`,
		`UPDATE events SET payload_json='{"payload":{"text":"résumé replacement"}}' WHERE id='plain'`,
	} {
		commentSearchExec(t, plain, q)
		if n := commentSearchCount(t, db, `SELECT count(*) FROM comment_search_pending_heads`); n != 1 {
			t.Fatal("external source not marked pending", n)
		}
		check("original", 0)
		check("replacement", 0)
		drainCommentSearch(t, ws)
	}
	check("resume", 1)
	check("replacement", 1)
	check("cafe", 0)
	commentSearchExec(t, plain, `UPDATE events SET trashed_at='now' WHERE id='plain'`)
	check("replacement", 0)
	if n := commentSearchCount(t, db, `SELECT count(*) FROM comment_search_heads`); n != 0 {
		t.Fatal("trash retained head", n)
	}
	commentSearchExec(t, plain, `UPDATE events SET trashed_at=NULL WHERE id='plain'`)
	check("replacement", 0)
	drainCommentSearch(t, ws)
	check("replacement", 1)
	commentSearchExec(t, plain, `DELETE FROM events WHERE id='plain'`)
	check("replacement", 0)
	rows, err := db.Query(`SELECT name,sql FROM sqlite_master WHERE type='trigger' AND name LIKE 'comment_search_%'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var name, body string
		if err = rows.Scan(&name, &body); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(body, "anx_") {
			t.Fatal("search trigger requires application function", name)
		}
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestCommentSearchPreview83ExternalUpgrade(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "upgrade.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	commentSearchExec(t, db, `CREATE TABLE events(id TEXT PRIMARY KEY,type TEXT,ts TEXT,actor_id TEXT,thread_id TEXT,refs_json TEXT,payload_json TEXT,trashed_at TEXT,archived_at TEXT)`)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = installCommentSearch(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	commentSearchEvent(t, db, "legacy", "legacy café")
	ws := &Workspace{db: db}
	drainCommentSearch(t, ws)
	// Reconstruct the preview-83 schema and its function-bearing source update.
	commentSearchExec(t, db, `DROP TRIGGER comment_search_event_insert;DROP TRIGGER comment_search_event_update;DROP TRIGGER comment_search_raw_insert;DROP TRIGGER comment_search_raw_update;DROP VIEW comment_search_pending_heads;DROP INDEX comment_search_heads_pending;
 ALTER TABLE comment_search_heads DROP COLUMN raw_text;ALTER TABLE comment_search_heads DROP COLUMN raw_truncated;ALTER TABLE comment_search_heads DROP COLUMN ready;
 CREATE TRIGGER comment_search_event_update AFTER UPDATE ON events BEGIN UPDATE comment_search_heads SET projection_json=anx_comment_search_projection(CAST(json_extract(NEW.payload_json,'$.payload.text') AS BLOB)) WHERE event_id=NEW.id;END`)
	plain, err := sql.Open("comment-search-plain", path)
	if err != nil {
		t.Fatal(err)
	}
	defer plain.Close()
	if _, err = plain.Exec(`UPDATE events SET payload_json='{"payload":{"text":"new résumé"}}' WHERE id='legacy'`); err == nil {
		t.Fatal("preview fixture did not reproduce missing function")
	}
	tx, err = db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = upgradeExternalCommentSearch(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if n := commentSearchCount(t, db, `SELECT count(*) FROM comment_search_heads WHERE ready=1 AND length(raw_text)=0`); n != 1 {
		t.Fatal("upgrade rewrote historical source", n)
	}
	// Migration is a startup operation; use a fresh external connection to the
	// upgraded schema, just as the legacy CLI fixture does on every invocation.
	if err = plain.Close(); err != nil {
		t.Fatal(err)
	}
	plain, err = sql.Open("comment-search-plain", path)
	if err != nil {
		t.Fatal(err)
	}
	defer plain.Close()
	commentSearchExec(t, plain, `UPDATE events SET payload_json='{"payload":{"text":"new résumé"}}' WHERE id='legacy'`)
	if n := commentSearchCount(t, db, `SELECT count(*) FROM comment_search_pending_heads`); n != 1 {
		t.Fatal(n)
	}
	drainCommentSearch(t, ws)
	if n := commentSearchCount(t, db, `SELECT count(*) FROM comment_search_postings WHERE term='resume'`); n != 1 {
		t.Fatal("upgraded worker missed external write", n)
	}
}
