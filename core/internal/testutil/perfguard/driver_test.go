package perfguard

import (
	"context"
	"testing"
	"time"
)

func TestCaptureIncludesTransactionsPreparedAndRawReads(t *testing.T) {
	db, c, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if _, err := db.Exec("CREATE TABLE items(id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	c.Start()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tx.PrepareContext(ctx, "INSERT INTO items VALUES (?)")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stmt.ExecContext(ctx, 7); err != nil {
		t.Fatal(err)
	}
	stmt.Close()
	var id int
	if err := tx.QueryRowContext(ctx, "SELECT id FROM items WHERE id=?", 7).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT id FROM items").Scan(&id); err != nil {
		t.Fatal(err)
	}
	s, queries, rows := c.Stop()
	if len(s) != 3 || queries != 3 || rows != 2 || id != 7 {
		t.Fatalf("capture: statements=%d queries=%d rows=%d id=%d", len(s), queries, rows, id)
	}
	if len(s[0].Args) != 1 || s[0].Args[0] != int64(7) {
		t.Fatalf("lost prepared argument: %v", s[0].Args)
	}
}

func TestPlanClassifier(t *testing.T) {
	for _, tc := range []struct {
		sql  string
		plan []string
		bad  bool
	}{
		{"SELECT * FROM events e", []string{"SCAN e"}, true},
		{"SELECT * FROM events e WHERE id=?", []string{"SEARCH e USING INDEX primary (id=?)"}, false},
		{"SELECT * FROM events e", []string{"SCAN e USING INDEX idx"}, true},
		{"SELECT * FROM actors", []string{"SEARCH actors USING AUTOMATIC COVERING INDEX (id=?)"}, true},
		{"SELECT * FROM events e WHERE anx_resource_text_has_ref(e.payload_json,?)", []string{"SEARCH e USING INDEX idx (id=?)"}, true},
		{`SELECT * FROM "events" e`, []string{"SCAN e"}, true},
		{`SELECT * FROM [events] AS e`, []string{"SCAN e"}, true},
		{"SELECT * FROM main.`events` AS e", []string{"SCAN e"}, true},
		{`SELECT * FROM actors a, "events" e`, []string{"SCAN a", "SCAN e"}, true},
		{`SELECT * FROM events e WHERE expensive(e.payload_json)`, []string{"SEARCH e USING INDEX idx (id=?)"}, true},
		{`SELECT * FROM events e WHERE e.id=? ORDER BY expensive(e.payload_json)`, []string{"SEARCH e USING INDEX idx (id=?)"}, false},
		{`SELECT * FROM events e WHERE e.id=? AND e.type='expensive(payload_json)'`, []string{"SEARCH e USING INDEX idx (id=?)"}, false},
		{`SELECT SUM(id) FROM events AS "full scan"`, []string{"SCAN full scan"}, true},
		{`SELECT * FROM events e WHERE "order"=? AND expensive(e.payload_json)`, []string{"SEARCH e USING INDEX idx (id=?)"}, true},
		{`SELECT * FROM events e WHERE "group"=? AND expensive(e.payload_json)`, []string{"SEARCH e USING INDEX idx (id=?)"}, true},
		{"SELECT * FROM actors", []string{"SCAN actors"}, false},
	} {
		if got := len(Findings(tc.sql, tc.plan, map[string]bool{"events": true}, map[string]bool{"expensive": true})) > 0; got != tc.bad {
			t.Errorf("%s: flagged=%v", tc.sql, got)
		}
	}
}

func TestCaptureAllowsNextRequestAfterCancellation(t *testing.T) {
	db, _, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	var n int64
	err = db.QueryRowContext(ctx, "WITH RECURSIVE s(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM s WHERE n<1000000000) SELECT SUM(n) FROM s").Scan(&n)
	if err == nil {
		t.Fatal("unbounded query unexpectedly completed before cancellation")
	}
	if ctx.Err() != context.DeadlineExceeded {
		t.Fatalf("query failed before deadline: %v", err)
	}

	if err = db.QueryRow("SELECT 1").Scan(&n); err != nil {
		t.Fatalf("next request inherited canceled connection: %v", err)
	}
	if n != 1 {
		t.Fatal(n)
	}
}
