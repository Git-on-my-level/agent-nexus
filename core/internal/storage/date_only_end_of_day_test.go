package storage

import (
	"context"
	"testing"
)

func TestDateOnlyEndOfDayBackfillLeavesTimestamps(t *testing.T) {
	ws, err := InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	db := ws.DB()
	ctx := context.Background()
	if _, err = db.ExecContext(ctx, `INSERT INTO events(id,type,ts,actor_id,refs_json,payload_json) VALUES
		 ('date-ask','human_attention_requested','2026-10-01T00:00:00Z','a','[]','{"payload":{"by":"2026-10-15"}}'),
		 ('stamp-ask','human_attention_requested','2026-10-01T00:00:00Z','a','[]','{"payload":{"by":"2026-10-15T00:00:00Z"}}'),
		 ('date-note','message_posted','2026-10-01T00:00:00Z','a','[]','{"payload":{"next":{"at":"2026-10-15","what":"ship"}}}'),
		 ('stamp-note','message_posted','2026-10-01T00:00:00Z','a','[]','{"payload":{"next":{"at":"2026-10-15T00:00:00Z","what":"exact"}}}')`); err != nil {
		t.Fatal(err)
	}
	// Triggers store the current resolution. Put the date-only rows back at
	// midnight so the migration has the pre-fix facts to rewrite.
	if _, err = db.ExecContext(ctx, `UPDATE brief_asks SET decide_by='2026-10-15T00:00:00Z' WHERE ask_id='date-ask'`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `UPDATE ask_deadlines SET decide_by='2026-10-15T00:00:00Z', normalized=1 WHERE request_event_id IN ('date-ask','stamp-ask')`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO brief_notes(event_id,card_id,author,ts,next_at,next_what) VALUES
		 ('date-note','card-1','a','2026-10-01T00:00:00Z','2026-10-15T00:00:00Z','ship'),
		 ('stamp-note','card-1','a','2026-10-01T00:00:00Z','2026-10-15T00:00:00Z','exact')`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = installDateOnlyEndOfDay(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}

	var askBy, deadlineBy, noteAt string
	if err = db.QueryRow(`SELECT decide_by FROM brief_asks WHERE ask_id='date-ask'`).Scan(&askBy); err != nil || askBy != "2026-10-15T23:59:59Z" {
		t.Fatalf("brief date ask %q %v", askBy, err)
	}
	if err = db.QueryRow(`SELECT decide_by FROM ask_deadlines WHERE request_event_id='date-ask'`).Scan(&deadlineBy); err != nil || deadlineBy != "2026-10-15T23:59:59.000000000Z" {
		t.Fatalf("deadline date ask %q %v", deadlineBy, err)
	}
	if err = db.QueryRow(`SELECT next_at FROM brief_notes WHERE event_id='date-note'`).Scan(&noteAt); err != nil || noteAt != "2026-10-15T23:59:59Z" {
		t.Fatalf("date note %q %v", noteAt, err)
	}
	var dirty int
	if err = db.QueryRow(`SELECT COUNT(*) FROM brief_dirty WHERE card_id='card-1'`).Scan(&dirty); err != nil || dirty != 1 {
		t.Fatalf("dirty %d %v", dirty, err)
	}
	if err = db.QueryRow(`SELECT decide_by FROM brief_asks WHERE ask_id='stamp-ask'`).Scan(&askBy); err != nil || askBy != "2026-10-15T00:00:00Z" {
		t.Fatalf("timestamp ask moved: %q %v", askBy, err)
	}
	if err = db.QueryRow(`SELECT decide_by FROM ask_deadlines WHERE request_event_id='stamp-ask'`).Scan(&deadlineBy); err != nil || deadlineBy != "2026-10-15T00:00:00Z" {
		t.Fatalf("timestamp deadline moved: %q %v", deadlineBy, err)
	}
	if err = db.QueryRow(`SELECT next_at FROM brief_notes WHERE event_id='stamp-note'`).Scan(&noteAt); err != nil || noteAt != "2026-10-15T00:00:00Z" {
		t.Fatalf("timestamp note moved: %q %v", noteAt, err)
	}
}

func TestRepairBriefNoteExcerptsRefillsBlanks(t *testing.T) {
	ws, err := InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	db := ws.DB()
	ctx := context.Background()
	if _, err = db.ExecContext(ctx, `INSERT INTO events(id,type,ts,actor_id,refs_json,payload_json) VALUES
		 ('blank','message_posted','2026-10-01T00:00:00Z','a','[]','{"payload":{"text":"what happened"}}'),
		 ('stale','message_posted','2026-10-01T00:00:00Z','a','[]','{"payload":{"text":"note text","body":"old body"}}')`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO brief_notes(event_id,card_id,author,ts,excerpt) VALUES
		 ('blank','card-1','a','2026-10-01T00:00:00Z',''),
		 ('stale','card-1','a','2026-10-01T00:00:00Z','old body'),
		 ('orphan','card-1','a','2026-10-01T00:00:00Z','')`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = repairBriefNoteExcerpts(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{"blank": "what happened", "stale": "note text", "orphan": ""} {
		var got string
		if err = db.QueryRow(`SELECT excerpt FROM brief_notes WHERE event_id=?`, id).Scan(&got); err != nil || got != want {
			t.Fatalf("%s excerpt %q err %v, want %q", id, got, err, want)
		}
	}
}
