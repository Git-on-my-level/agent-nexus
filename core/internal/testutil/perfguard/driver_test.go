package perfguard

import (
	"context"
	"database/sql"
	"math"
	"strings"
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

func TestPlanFingerprintOnlyNormalizesSnapshotData(t *testing.T) {
	prefix := "WITH RECURSIVE _anx_fresh_denied(kind,id) AS (SELECT 1 WHERE COALESCE((SELECT version FROM main.resource_access_epoch WHERE singleton=1),-1)<>"
	makeSQL := func(epoch string) string {
		return prefix + epoch + ") , cached AS (SELECT 1 WHERE COALESCE((SELECT version FROM main.resource_access_epoch WHERE singleton=1),-1)=" + epoch + ") SELECT * FROM events WHERE type='business'"
	}
	a, b := makeSQL("123"), makeSQL("456")
	if PlanSQLHash(a) != PlanSQLHash(b) {
		t.Fatal("epoch data changed query fingerprint")
	}
	for _, changed := range []string{strings.Replace(a, "business", "other", 1), a + " AND id=?", a + " -- epoch 456", a + " /* epoch 456 */"} {
		if PlanSQLHash(a) == PlanSQLHash(changed) {
			t.Fatal("structural or literal change escaped fingerprint")
		}
	}
	literal := a + " AND type='COALESCE((SELECT version FROM main.resource_access_epoch WHERE singleton=1),-1)=123'"
	if PlanSQLHash(literal) == PlanSQLHash(strings.Replace(literal, "=123'", "=456'", 1)) {
		t.Fatal("quoted business value normalized")
	}
	extra := a + " AND COALESCE((SELECT version FROM main.resource_access_epoch WHERE singleton=1),-1)=999"
	if PlanSQLHash(extra) != SQLHash(extra) {
		t.Fatal("noncompiler business epoch predicate normalized")
	}
	plain := strings.TrimPrefix(a, "WITH RECURSIVE _anx_fresh_denied(kind,id) AS (")
	if PlanSQLHash(plain) != SQLHash(plain) {
		t.Fatal("unscoped query normalized")
	}
}

func TestPlanClassifierSchemaQualifiedAggregate(t *testing.T) {
	db, _, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec("CREATE TABLE events(id INTEGER PRIMARY KEY, body TEXT)"); err != nil {
		t.Fatal(err)
	}
	q := "WITH e AS (SELECT count(*) FROM main.events WHERE length(body)>0) SELECT * FROM e"
	details, err := Explain(context.Background(), db, Statement{SQL: q})
	if err != nil {
		t.Fatal(err)
	}
	if len(Findings(q, details, map[string]bool{"events": true})) == 0 {
		t.Fatalf("schema-qualified scan escaped gate: %v", details)
	}
}

func TestPlanFingerprintRetainsDuplicateScans(t *testing.T) {
	a := []string{"SCAN e", "SEARCH p USING INDEX primary (id=?)"}
	if PlanHash(a) == PlanHash(append(a, "SCAN e")) {
		t.Fatal("additional scan inherited existing plan exception")
	}
	if PlanHash(a) == PlanHash([]string{"SEARCH e USING INDEX primary (id=?)", a[1]}) {
		t.Fatal("changed scan plan fingerprint ignored")
	}
}

func TestPlanMemoRetainsLikeArgumentVariants(t *testing.T) {
	q := "SELECT COUNT(*) FROM events WHERE payload_json LIKE ?"
	a := Statement{SQL: q, Args: []any{"synthetic-1%"}}
	b := Statement{SQL: q, Args: []any{"%1%"}}
	if PlanMemoKey(a) == PlanMemoKey(b) {
		t.Fatal("prefix lookup and wildcard scan share a plan memo")
	}
	for _, q := range []string{"SELECT COUNT(*) FROM events WHERE payload_json LIKE(?)", "SELECT COUNT(*) FROM events WHERE payload_json LIKE (?)", "SELECT COUNT(*) FROM events WHERE payload_json GLOB ( (?) )"} {
		a.SQL = q
		b.SQL = q
		if PlanMemoKey(a) == PlanMemoKey(b) {
			t.Fatal("parenthesized pattern variants share a plan memo")
		}
	}
	for _, q := range []string{"SELECT * FROM events WHERE id=?", "SELECT * FROM events WHERE id=? AND type='LIKE ?'", "SELECT * FROM events WHERE id=? /* LIKE ? */", "SELECT * FROM events WHERE id=? -- GLOB ?"} {
		a.SQL = q
		b.SQL = q
		if PlanMemoKey(a) == PlanMemoKey(b) {
			t.Fatal("STAT4 argument variants incorrectly share a plan memo")
		}
	}
}

func TestCaptureCountsEveryRepeatedExecutionWithoutRetainingDuplicatePlans(t *testing.T) {
	db, c, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec("CREATE TABLE events(id INTEGER PRIMARY KEY,payload_json TEXT); INSERT INTO events VALUES(1,'synthetic-1')"); err != nil {
		t.Fatal(err)
	}
	c.Start()
	for i := 0; i < 5; i++ {
		var n int
		if err = db.QueryRow("SELECT COUNT(*) FROM events WHERE id=?", 1).Scan(&n); err != nil {
			t.Fatal(err)
		}
	}
	for _, pattern := range []string{"synthetic-1%", "%1%"} {
		var n int
		if err = db.QueryRow("SELECT COUNT(*) FROM events WHERE payload_json LIKE(?)", pattern).Scan(&n); err != nil {
			t.Fatal(err)
		}
	}
	statements, queries, rows := c.Stop()
	if queries != 7 || rows != 7 || len(statements) != 3 {
		t.Fatalf("lost execution counts or pattern variants: statements=%d queries=%d rows=%d", len(statements), queries, rows)
	}
}

func TestPlanMemoPreservesArgumentTypes(t *testing.T) {
	a := Statement{SQL: "SELECT * FROM events WHERE payload_json LIKE ?", Args: []any{[]byte("a")}}
	b := Statement{SQL: a.SQL, Args: []any{"YQ=="}}
	if PlanMemoKey(a) == PlanMemoKey(b) {
		t.Fatal("SQLite blob and text patterns collided through JSON base64 encoding")
	}
}

func TestCapturePreservesNamedArgumentBindingsForExplain(t *testing.T) {
	db, c, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec("CREATE TABLE events(id INTEGER PRIMARY KEY,payload_json TEXT);INSERT INTO events VALUES(1,'synthetic-1')"); err != nil {
		t.Fatal(err)
	}
	c.Start()
	var n int
	q := "SELECT COUNT(*) FROM events WHERE payload_json LIKE @pattern AND id=@id"
	if err = db.QueryRow(q, sql.Named("id", 1), sql.Named("pattern", "%1%")).Scan(&n); err != nil {
		t.Fatal(err)
	}
	statements, _, _ := c.Stop()
	if n != 1 || statements[0].Args[0].(sql.NamedArg).Name != "id" || statements[0].Args[1].(sql.NamedArg).Name != "pattern" {
		t.Fatalf("named bindings lost: %#v", statements)
	}
	if _, err = Explain(context.Background(), db, statements[0]); err != nil {
		t.Fatal(err)
	}
	a := []any{sql.Named("pattern", []byte("a"))}
	b := []any{sql.Named("pattern", "YQ==")}
	if ArgsHash(a) == ArgsHash(b) {
		t.Fatal("named blob/text argument types collided")
	}
}

func TestScaleCorpusMinimum(t *testing.T) {
	if Rows < 4096 || PMRows < 1024 {
		t.Fatal("scale corpus no longer exercises thousands of canonical records and each PM family")
	}
}

func TestPlanMemoPreservesRawDriverValueBytes(t *testing.T) {
	for _, pair := range [][2]any{{string([]byte{0xff}), string([]byte{0xfe})}, {math.Inf(1), math.Inf(-1)}, {[]byte(nil), []byte{}}, {int64(1), float64(1)}} {
		if ArgsHash([]any{pair[0]}) == ArgsHash([]any{pair[1]}) {
			t.Fatalf("distinct driver values collided: %T %T", pair[0], pair[1])
		}
	}
}

func TestCapturePreservesEmptyAndNullBlobs(t *testing.T) {
	db, c, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, named := range []bool{false, true} {
		c.Start()
		q := "SELECT typeof(?),length(?)"
		if named {
			q = "SELECT typeof(@blob),length(@blob)"
		}
		for _, blob := range [][]byte{nil, {}} {
			args := []any{blob, blob}
			if named {
				args = []any{sql.Named("blob", blob)}
			}
			var kind string
			var length sql.NullInt64
			if err = db.QueryRow(q, args...).Scan(&kind, &length); err != nil {
				t.Fatal(err)
			}
		}
		statements, queries, rows := c.Stop()
		if queries != 2 || rows != 2 || len(statements) != 2 || PlanMemoKey(statements[0]) == PlanMemoKey(statements[1]) {
			t.Fatalf("lost blob binding variants: %#v", statements)
		}
		for i, statement := range statements {
			var kind string
			var length sql.NullInt64
			if err = db.QueryRow(statement.SQL, statement.Args...).Scan(&kind, &length); err != nil {
				t.Fatal(err)
			}
			if (i == 0 && (kind != "null" || length.Valid)) || (i == 1 && (kind != "blob" || !length.Valid || length.Int64 != 0)) {
				t.Fatalf("capture changed binding: named=%v kind=%s length=%v", named, kind, length)
			}
		}
	}
}
