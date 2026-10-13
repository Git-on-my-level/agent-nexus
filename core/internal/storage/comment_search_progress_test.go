package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/commentindex"
)

func TestCommentSearchPendingCursorUpgradePreservesCheckpoint(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	commentSearchExec(t, db, `CREATE TABLE comment_search_job(singleton INTEGER PRIMARY KEY,cursor TEXT,done INTEGER); INSERT INTO comment_search_job VALUES(1,'legacy-checkpoint',1)`)
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := 0; i < 2; i++ {
		if err = upgradeCommentSearchPendingCursor(context.Background(), tx); err != nil {
			t.Fatal(err)
		}
	}
	var cursor string
	var done bool
	var pending sql.NullString
	if err = tx.QueryRow(`SELECT cursor,done,pending_cursor FROM comment_search_job`).Scan(&cursor, &done, &pending); err != nil {
		t.Fatal(err)
	}
	if cursor != "legacy-checkpoint" || !done || pending.Valid {
		t.Fatal("upgrade changed checkpoint", cursor, done, pending)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestCommentSearchSlowWorstCasePreparation(t *testing.T) {
	ws, err := InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	var text strings.Builder
	for i := 0; i < 4096; i++ {
		fmt.Fprintf(&text, "term%04d ", i)
	}
	text.WriteString(strings.Repeat(" ", 65536-text.Len()))
	commentSearchEvent(t, ws.DB(), "worst", text.String())
	_, err = ws.maintainCommentSearchBatch(context.Background(), func(raw []byte, truncated bool) (string, error) {
		// Exceed the former 250 ms chunk budget, and prove another writer can commit
		// while preparation is running (including on a single-core machine).
		time.Sleep(350 * time.Millisecond)
		commentSearchExec(t, ws.DB(), `UPDATE comment_search_job SET done=1`)
		return commentindex.Prepare(raw, truncated)
	})
	if err != nil {
		t.Fatal(err)
	}
	if n := commentSearchCount(t, ws.DB(), `SELECT count(*) FROM comment_search_postings WHERE event_id='worst'`); n != 4096 {
		t.Fatal(n)
	}
	if n := commentSearchCount(t, ws.DB(), `SELECT count(*) FROM comment_search_pending_heads`); n != 0 {
		t.Fatal(n)
	}
}

func TestCommentSearchPreparedSourceChangeStaysPending(t *testing.T) {
	ws, err := InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	commentSearchEvent(t, ws.DB(), "changed", "oldword")
	_, err = ws.maintainCommentSearchBatch(context.Background(), func(raw []byte, truncated bool) (string, error) {
		commentSearchExec(t, ws.DB(), `UPDATE events SET payload_json='{"payload":{"text":"newword"}}' WHERE id='changed'`)
		return commentindex.Prepare(raw, truncated)
	})
	if err != nil {
		t.Fatal(err)
	}
	if n := commentSearchCount(t, ws.DB(), `SELECT count(*) FROM comment_search_heads WHERE ready=0`); n != 1 {
		t.Fatal("stale proposal certified", n)
	}
	drainCommentSearch(t, ws)
	if n := commentSearchCount(t, ws.DB(), `SELECT count(*) FROM comment_search_postings WHERE term='newword'`); n != 1 {
		t.Fatal(n)
	}
	if n := commentSearchCount(t, ws.DB(), `SELECT count(*) FROM comment_search_postings WHERE term='oldword'`); n != 0 {
		t.Fatal(n)
	}
}

func TestCommentSearchFailedRowRotatesAcrossReopen(t *testing.T) {
	root := t.TempDir()
	ws, err := InitializeWorkspace(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	commentSearchEvent(t, ws.DB(), "a-fail", "badword")
	commentSearchEvent(t, ws.DB(), "b-good", "goodword")
	commentSearchExec(t, ws.DB(), `CREATE TRIGGER permanent_failure BEFORE INSERT ON comment_search_postings WHEN NEW.event_id='a-fail' BEGIN SELECT RAISE(ABORT,'injected permanent failure'); END`)
	if _, err = ws.MaintainCommentSearchBatch(context.Background()); err == nil {
		t.Fatal("failure ignored")
	}
	if err = ws.Close(); err != nil {
		t.Fatal(err)
	}
	ws, err = InitializeWorkspace(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	if _, err = ws.MaintainCommentSearchBatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := commentSearchCount(t, ws.DB(), `SELECT count(*) FROM comment_search_postings WHERE term='goodword'`); n != 1 {
		t.Fatal("failed row starved successor", n)
	}
	if n := commentSearchCount(t, ws.DB(), `SELECT count(*) FROM comment_search_pending_heads`); n != 1 {
		t.Fatal("failed row was certified", n)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var reported error
	ws.RunCommentSearchMaintenance(ctx, func(err error) { reported = err; cancel() })
	if reported == nil || errors.Is(reported, context.Canceled) {
		t.Fatal("persistent failure not surfaced", reported)
	}
}
