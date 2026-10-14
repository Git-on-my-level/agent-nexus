package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/replaypolicy"
)

func TestReplayReceiptCollectionBoundedAndIndexed(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`CREATE TABLE idempotency_replays(scope TEXT,actor_id TEXT,request_key TEXT,created_at TEXT, response_json TEXT,PRIMARY KEY(scope,actor_id,request_key)); CREATE INDEX idx_idempotency_replays_created_at ON idempotency_replays(created_at)`); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2030, 5, 6, 7, 8, 9, 0, time.UTC)
	cutoff := now.Add(-replaypolicy.Retention())
	for i := 0; i < replaypolicy.GCBatchSize()+3; i++ {
		if _, err = db.Exec(`INSERT INTO idempotency_replays VALUES('scope','actor',?,?,'private receipt')`, fmt.Sprint(i), cutoff.Add(-time.Second).Format("2006-01-02 15:04:05")); err != nil {
			t.Fatal(err)
		}
	}
	for _, key := range []string{"boundary", "fresh"} {
		at := cutoff
		if key == "fresh" {
			at = now
		}
		if _, err = db.Exec(`INSERT INTO idempotency_replays VALUES('scope','actor',?,?,'keep')`, key, at.Format("2006-01-02 15:04:05")); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := db.Query("EXPLAIN QUERY PLAN "+replayGCQuery, cutoff.Format("2006-01-02 15:04:05"), replaypolicy.GCBatchSize())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for rows.Next() {
		var a, b, c int
		var detail string
		if err := rows.Scan(&a, &b, &c, &detail); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(detail, "SCAN idempotency_replays") || strings.Contains(detail, "TEMP B-TREE") {
			t.Fatal(detail)
		}
		found = found || strings.Contains(detail, "idx_idempotency_replays_created_at")
	}
	rows.Close()
	if !found {
		t.Fatal("missing GC index")
	}
	for _, want := range []int64{int64(replaypolicy.GCBatchSize()), 3, 0} {
		got, err := CollectReplayReceipts(context.Background(), db, now)
		if err != nil || got != want {
			t.Fatalf("batch=%d want=%d err=%v", got, want, err)
		}
	}
	var kept int
	if err := db.QueryRow(`SELECT count(*) FROM idempotency_replays WHERE response_json='keep'`).Scan(&kept); err != nil || kept != 2 {
		t.Fatal(kept, err)
	}
	// The expired key may be reused only as a deliberately new intent. A fresh
	// receipt remains protected by the original composite unique key.
	if _, err := db.Exec(`INSERT INTO idempotency_replays VALUES('scope','actor','0',?,'new')`, now.Format("2006-01-02 15:04:05")); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO idempotency_replays VALUES('scope','actor','fresh',?,'duplicate')`, now.Format("2006-01-02 15:04:05")); err == nil {
		t.Fatal("fresh key reused")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() { RunReplayReceiptMaintenance(ctx, db, nil); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("maintenance ignored shutdown")
	}
}
