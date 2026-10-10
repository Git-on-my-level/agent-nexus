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
	ra, rb := strings.ReplaceAll(a, "resource_access_epoch", "receipt_access_epoch"), strings.ReplaceAll(b, "resource_access_epoch", "receipt_access_epoch")
	if PlanSQLHash(ra) != PlanSQLHash(rb) || PlanSQLHash(ra) == PlanSQLHash(a) {
		t.Fatal("receipt snapshot data must normalize without erasing its epoch table")
	}
	mixed := strings.Replace(a, "resource_access_epoch", "receipt_access_epoch", 1)
	if PlanSQLHash(mixed) != SQLHash(mixed) || PlanSQLHash(ra+" AND COALESCE((SELECT version FROM main.receipt_access_epoch WHERE singleton=1),-1)=999") != SQLHash(ra+" AND COALESCE((SELECT version FROM main.receipt_access_epoch WHERE singleton=1),-1)=999") {
		t.Fatal("mixed or additional receipt gates normalized")
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

func TestPlanClassifierIndexedAggregates(t *testing.T) {
	db, _ := workFixture(t)
	aggregates, err := AggregateFunctions(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		sql string
		bad bool
	}{
		{"SELECT SUM(LENGTH(id)) FROM runs WHERE id >= ''", true},
		{"SELECT SUM(LENGTH(id)) FROM runs WHERE id >= '' LIMIT 1", true},
		{"SELECT COUNT(*) FROM main.runs AS r WHERE r.id >= ''", true},
		{`SELECT TOTAL(LENGTH(id)),AVG(LENGTH(id)),GROUP_CONCAT(id) FROM "runs" r WHERE r.id < 'zzz'`, true},
		{"SELECT SUM(LENGTH(id)) OVER () FROM runs WHERE id >= '' LIMIT 1", true},
		{"SELECT SUM(LENGTH(id)) FROM (SELECT id FROM runs WHERE id >= '' LIMIT 8)", true},
		{"SELECT SUM(LENGTH(id)) FROM runs WHERE id=?", true},
		{"SELECT id FROM runs WHERE id >= '' LIMIT 1", false},
		{"SELECT id FROM runs WHERE id=?", false},
		{"SELECT 'SUM(id)' FROM runs WHERE id >= '' LIMIT 1", false},
		{"SELECT id FROM runs WHERE id >= '' /* SUM(id) */ LIMIT 1", false},
		{"SELECT id FROM runs WHERE id >= '' -- COUNT(*)\n LIMIT 1", false},
		{"SELECT MAX(id) FROM runs WHERE id >= ''", true},
		{"SELECT MAX(LENGTH(id)) FROM runs WHERE id >= ''", true},
		{"SELECT MIN(LENGTH(id)) FROM runs WHERE id >= ''", true},
		{"SELECT JSON_GROUP_ARRAY(id) FROM runs WHERE id >= ''", true},
		{"SELECT JSON_GROUP_OBJECT(id,id) FROM runs WHERE id >= ''", true},
		{"SELECT JSONB_GROUP_ARRAY(id) FROM runs WHERE id >= ''", true},
		{"SELECT JSONB_GROUP_OBJECT(id,id) FROM runs WHERE id >= ''", true},
		{"SELECT ROW_NUMBER() OVER (ORDER BY id) FROM runs WHERE id >= '' LIMIT 1", true},
		{"SELECT 'MAX(LENGTH(id)) JSON_GROUP_ARRAY(id)' FROM runs WHERE id >= '' LIMIT 1", false},
	} {
		s := Statement{SQL: tc.sql}
		if strings.Contains(tc.sql, "id=?") {
			s.Args = []any{"run-000001"}
		}
		plan, err := Explain(context.Background(), db, s)
		if err != nil {
			t.Fatal(err)
		}
		for _, functionMaps := range [][]map[string]bool{nil, {nil, aggregates}} {
			findings := Findings(tc.sql, plan, map[string]bool{"runs": true}, functionMaps...)
			if got := len(findings) > 0; got != tc.bad {
				t.Errorf("%s: findings=%v plan=%v want flagged=%v", tc.sql, findings, plan, tc.bad)
			}
		}
	}
}

func TestPlanClassifierIndexedEqualityAggregateIsNotAlwaysBounded(t *testing.T) {
	db, _ := workFixture(t)
	if _, err := db.Exec("CREATE TABLE entries(id TEXT, kind TEXT); CREATE INDEX by_kind ON entries(kind); INSERT INTO entries SELECT id,'all' FROM runs"); err != nil {
		t.Fatal(err)
	}
	for _, function := range []string{"COUNT(*)", "SUM(LENGTH(id))", "AVG(LENGTH(id))", "TOTAL(LENGTH(id))", "GROUP_CONCAT(id)"} {
		q := "SELECT " + function + " FROM entries WHERE kind='all' LIMIT 1"
		plan, err := Explain(context.Background(), db, Statement{SQL: q})
		if err != nil {
			t.Fatal(err)
		}
		if len(Findings(q, plan, map[string]bool{"entries": true})) == 0 {
			t.Fatalf("nonunique equality aggregate escaped: %s %v", q, plan)
		}
	}
}

func TestPlanClassifierAggregatesHiddenBehindViews(t *testing.T) {
	db, capture := workFixture(t)
	if _, err := db.Exec(`
CREATE VIEW "Aggregate View" AS SELECT JSON_GROUP_ARRAY(r.id) AS value FROM runs AS r WHERE r.id >= '';
CREATE VIEW nested_view AS SELECT value FROM "Aggregate View" AS inner_view;
CREATE VIEW point_view AS SELECT id FROM runs WHERE id='run-000001';
CREATE TEMP VIEW temporary_aggregate AS SELECT MAX(LENGTH(id)) AS value FROM main.runs WHERE id >= '';
ATTACH DATABASE ':memory:' AS other;
CREATE TABLE other.runs(id TEXT PRIMARY KEY);
INSERT INTO other.runs SELECT id FROM main.runs;
CREATE VIEW other.aggregate_view AS SELECT JSONB_GROUP_ARRAY(id) AS value FROM runs WHERE id >= '';
CREATE VIEW other.nested_view AS SELECT value FROM aggregate_view;
`); err != nil {
		t.Fatal(err)
	}
	views, err := ViewDefinitions(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	capture.Start()
	var value string
	if err := db.QueryRow(`SELECT value FROM main."Aggregate View"`).Scan(&value); err != nil {
		t.Fatal(err)
	}
	_, queries, rows := capture.Stop()
	t.Logf("hidden indexed JSON aggregate: work=%+v queries=%d rows=%d", capture.Work(), queries, rows)
	if queries != 1 || rows != 1 || strings.Count(value, "run-") != 4096 || capture.Work().VMSteps < 4096 || capture.Work().FullScanSteps != 0 {
		t.Fatalf("view fixture did not exercise hidden indexed aggregation: work=%+v queries=%d rows=%d", capture.Work(), queries, rows)
	}
	if err := capture.WorkError(); err != nil {
		t.Fatal(err)
	}
	aggregates, err := AggregateFunctions(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		sql string
		bad bool
	}{
		{`SELECT value FROM "Aggregate View"`, true},
		{`SELECT value FROM (("Aggregate View"))`, true},
		{`SELECT v.value FROM main."Aggregate View" AS v LIMIT 1`, true},
		{`SELECT value FROM NESTED_VIEW AS outer_view`, true},
		{`SELECT value FROM main.nested_view AS outer_view`, true},
		{`SELECT value FROM temporary_aggregate`, true},
		{`SELECT value FROM temp.temporary_aggregate`, true},
		{`SELECT value FROM other.nested_view`, true},
		{`SELECT value FROM "other"."aggregate_view" AS a`, true},
		{`SELECT a.value FROM (SELECT 1) b, "Aggregate View" a`, true},
		{`SELECT id FROM point_view`, false},
		{`SELECT id AS "Aggregate View" FROM runs WHERE id='run-000001'`, false},
		{`SELECT 'Aggregate View' FROM runs WHERE id='run-000001'`, false},
	} {
		plan, err := Explain(context.Background(), db, Statement{SQL: tc.sql})
		if err != nil {
			t.Fatal(err)
		}
		findings := FindingsWithViews(tc.sql, plan, map[string]bool{"runs": true}, views, nil, aggregates)
		if got := len(findings) > 0; got != tc.bad {
			t.Errorf("%s: findings=%v plan=%v want=%v", tc.sql, findings, plan, tc.bad)
		}
	}
	// Root unqualified names resolve temp first, but stored main-view relations
	// remain bound to main even when a temp view later shadows the same name.
	if _, err := db.Exec(`CREATE TEMP VIEW "Aggregate View" AS SELECT id AS value FROM main.runs WHERE id='run-000001'`); err != nil {
		t.Fatal(err)
	}
	views, err = ViewDefinitions(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		sql string
		bad bool
	}{{`SELECT value FROM "Aggregate View"`, false}, {`SELECT value FROM main.nested_view`, true}} {
		plan, err := Explain(context.Background(), db, Statement{SQL: tc.sql})
		if err != nil {
			t.Fatal(err)
		}
		findings := FindingsWithViews(tc.sql, plan, map[string]bool{"runs": true}, views, nil, aggregates)
		if got := len(findings) > 0; got != tc.bad {
			t.Errorf("shadowed %s findings=%v want=%v", tc.sql, findings, tc.bad)
		}
	}
}

func TestViewAnalysisBoundsCyclesAndNormalizesNames(t *testing.T) {
	views := map[string]string{"MAIN.First": `CREATE VIEW first AS SELECT value FROM "SECOND"`, "main.second": `CREATE VIEW second AS SELECT SUM(id) AS value FROM FIRST`}
	q := viewAnalysisSQL(`SELECT value FROM main."FIRST"`, views)
	if strings.Count(q, "CREATE VIEW") != 2 || !strings.Contains(q, "SUM(id)") {
		t.Fatalf("recursive expansion incorrect: %s", q)
	}
}

func TestPlanClassifierViewFallbackAcrossSchemas(t *testing.T) {
	db, capture := workFixture(t)
	if _, err := db.Exec(`
CREATE VIEW inner_aggregate AS SELECT JSON_GROUP_ARRAY(r.id) AS value FROM runs r WHERE r.id >= '';
CREATE TEMP VIEW outer_temp AS SELECT value FROM inner_aggregate;
ATTACH DATABASE ':memory:' AS other;
CREATE TABLE other.runs(id TEXT PRIMARY KEY);
INSERT INTO other.runs SELECT id FROM main.runs;
CREATE VIEW other.attached_aggregate AS SELECT JSON_GROUP_ARRAY(id) AS value FROM runs WHERE id >= '';
CREATE TEMP VIEW outer_attached AS SELECT value FROM attached_aggregate;
`); err != nil {
		t.Fatal(err)
	}
	views, err := ViewDefinitions(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	aggregates, err := AggregateFunctions(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`SELECT value FROM outer_temp`, `SELECT value FROM temp.outer_temp`, `SELECT value FROM attached_aggregate`, `SELECT value FROM outer_attached`} {
		plan, err := Explain(context.Background(), db, Statement{SQL: q})
		if err != nil {
			t.Fatal(err)
		}
		findings := FindingsWithViews(q, plan, map[string]bool{"runs": true}, views, nil, aggregates)
		if len(findings) == 0 {
			t.Fatalf("fallback hidden aggregate escaped: %s plan=%v", q, plan)
		}
		capture.Start()
		var value string
		if err := db.QueryRow(q).Scan(&value); err != nil {
			t.Fatal(err)
		}
		_, queries, rows := capture.Stop()
		t.Logf("%s work=%+v rows=%d findings=%v", q, capture.Work(), rows, findings)
		if queries != 1 || rows != 1 || strings.Count(value, "run-") != 4096 || capture.Work().VMSteps < 4096 || capture.Work().FullScanSteps != 0 {
			t.Fatalf("fallback fixture lost hidden work: %s %+v", q, capture.Work())
		}
		if err := capture.WorkError(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestViewAnalysisConservativelyIncludesAmbiguousAttachedNames(t *testing.T) {
	views := map[string]string{"z.safe": `CREATE VIEW safe AS SELECT id FROM runs WHERE id='one'`, "a.safe": `CREATE VIEW safe AS SELECT JSON_GROUP_ARRAY(id) FROM runs WHERE id>=''`}
	q := `SELECT * FROM safe`
	first := viewAnalysisSQL(q, views)
	if strings.Count(first, "CREATE VIEW") != 2 || !strings.Contains(first, "JSON_GROUP_ARRAY") {
		t.Fatalf("ambiguous attached aggregate lost: %s", first)
	}
	for i := 0; i < 10; i++ {
		if next := viewAnalysisSQL(q, views); next != first {
			t.Fatalf("map order changed expansion: %s / %s", first, next)
		}
	}
}

func TestPlanClassifierAttachedRelationsAndDottedAliases(t *testing.T) {
	db, capture := workFixture(t)
	if _, err := db.Exec(`
ATTACH DATABASE ':memory:' AS other;
CREATE TABLE other.runs(id TEXT PRIMARY KEY);
INSERT INTO other.runs SELECT id FROM main.runs;
CREATE TEMP VIEW explicit_attached AS SELECT SUM(LENGTH(id)) AS value FROM other.runs WHERE id>='';
`); err != nil {
		t.Fatal(err)
	}
	views, err := ViewDefinitions(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	aggregates, err := AggregateFunctions(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		sql string
		bad bool
	}{
		{`SELECT SUM(LENGTH(id)) FROM other.runs WHERE id>=''`, true},
		{`SELECT SUM(LENGTH(id)) FROM "other"."runs" WHERE id>=''`, true},
		{`SELECT SUM(LENGTH(id)) FROM other.runs AS "main.literal.dot" WHERE id>=''`, true},
		{`SELECT SUM(LENGTH(id)) FROM other.runs AS "temp.alias with spaces" WHERE id>=''`, true},
		{`SELECT value FROM explicit_attached`, true},
		{`SELECT id FROM other.runs WHERE id='run-000001'`, false},
	} {
		plan, err := Explain(context.Background(), db, Statement{SQL: tc.sql})
		if err != nil {
			t.Fatal(err)
		}
		findings := FindingsWithViews(tc.sql, plan, map[string]bool{"runs": true}, views, nil, aggregates)
		if got := len(findings) > 0; got != tc.bad {
			t.Errorf("%s findings=%v plan=%v want=%v", tc.sql, findings, plan, tc.bad)
		}
	}
	capture.Start()
	var sum int
	if err := db.QueryRow(`SELECT SUM(LENGTH(id)) FROM other.runs WHERE id>=''`).Scan(&sum); err != nil {
		t.Fatal(err)
	}
	_, queries, rows := capture.Stop()
	if queries != 1 || rows != 1 || sum != 40960 || capture.Work().VMSteps != 16394 {
		t.Fatalf("attached fixture/count drift: sum=%d queries=%d rows=%d work=%+v", sum, queries, rows, capture.Work())
	}
	if err := capture.WorkError(); err != nil {
		t.Fatal(err)
	}
}

func TestViewPlanFingerprintBindsCurrentDefinitions(t *testing.T) {
	db, _ := workFixture(t)
	create := func(bound string) {
		t.Helper()
		if _, err := db.Exec("DROP VIEW IF EXISTS bounded_aggregate"); err != nil {
			t.Fatal(err)
		}
		// Definitions are test literals; no request input is interpolated.
		q := `CREATE VIEW bounded_aggregate AS SELECT JSON_GROUP_ARRAY(id) AS value FROM runs WHERE id>='` + bound + `'`
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	create("run-004094")
	q := `SELECT value FROM bounded_aggregate`
	oldPlan, err := Explain(context.Background(), db, Statement{SQL: q})
	if err != nil {
		t.Fatal(err)
	}
	oldViews, err := ViewDefinitions(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	oldHash := PlanSQLHashWithViews(q, oldViews)
	if oldHash == PlanSQLHash(q) {
		t.Fatal("view body not included in exception fingerprint")
	}
	if oldHash != PlanSQLHash(AnalysisSQL(q, oldViews)) {
		t.Fatal("analysis evidence cannot reproduce hash")
	}
	create("")
	newPlan, err := Explain(context.Background(), db, Statement{SQL: q})
	if err != nil {
		t.Fatal(err)
	}
	newViews, err := ViewDefinitions(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	if PlanHash(oldPlan) != PlanHash(newPlan) {
		t.Fatalf("fixture should preserve identical plan: %v / %v", oldPlan, newPlan)
	}
	if oldHash == PlanSQLHashWithViews(q, newViews) {
		t.Fatal("unbounded view inherited old bounded exception")
	}
	newViews["main.unrelated"] = `CREATE VIEW unrelated AS SELECT SUM(LENGTH(id)) FROM runs WHERE id>=''`
	if PlanSQLHashWithViews(q, newViews) != PlanSQLHashWithViews(q, map[string]string{"main.bounded_aggregate": newViews["main.bounded_aggregate"]}) {
		t.Fatal("unrelated view changed statement identity")
	}
	for _, plain := range []string{"SELECT id FROM runs WHERE id=?", "SELECT 1", "SELECT 'bounded_aggregate'", "SELECT id AS bounded_aggregate FROM runs WHERE id=?"} {
		if PlanSQLHashWithViews(plain, newViews) != PlanSQLHash(plain) {
			t.Fatalf("no-view hash changed: %s", plain)
		}
	}
}

func TestViewPlanFingerprintFollowsSchemaResolutionAndRecursion(t *testing.T) {
	q := `SELECT value FROM outer_temp`
	views := map[string]string{
		"temp.outer_temp":  `CREATE TEMP VIEW outer_temp AS SELECT value FROM inner_view`,
		"main.inner_view":  `CREATE VIEW inner_view AS SELECT JSON_GROUP_ARRAY(id) AS value FROM runs WHERE id>='run-004094'`,
		"other.inner_view": `CREATE VIEW inner_view AS SELECT id AS value FROM runs WHERE id='run-000001'`,
	}
	first := PlanSQLHashWithViews(q, views)
	views["other.inner_view"] = `CREATE VIEW inner_view AS SELECT SUM(LENGTH(id)) AS value FROM runs`
	if PlanSQLHashWithViews(q, views) != first {
		t.Fatal("shadowed attached definition changed resolved temp/main fingerprint")
	}
	views["main.inner_view"] = `CREATE VIEW inner_view AS SELECT JSON_GROUP_ARRAY(id) AS value FROM runs WHERE id>=''`
	if PlanSQLHashWithViews(q, views) == first {
		t.Fatal("nested main view change did not alter temp outer identity")
	}
	for i := 0; i < 10; i++ {
		if PlanSQLHashWithViews(q, views) != PlanSQLHash(AnalysisSQL(q, views)) {
			t.Fatal("view hashing order unstable")
		}
	}
}

func TestPointPlanHashNormalizesOnlyCompilerEpoch(t *testing.T) {
	prefix := "WITH RECURSIVE _anx_point_snapshot(token) AS MATERIALIZED (SELECT ? WHERE COALESCE((SELECT version FROM main.resource_access_epoch WHERE singleton=1),-1)=123), cards AS (SELECT * FROM main.cards) "
	q := prefix + "SELECT * FROM cards WHERE version=123 AND title='epoch=123'"
	if PlanSQLHash(q) != SQLHash(q) {
		t.Fatal("changed existing baseline fingerprints")
	}
	if PointPlanSQLHash(q) != PointPlanSQLHash(strings.Replace(q, "-1)=123", "-1)=456", 1)) {
		t.Fatal("compiler epoch changed shape")
	}
	for _, changed := range []string{strings.Replace(q, "version=123", "version=456", 1), strings.Replace(q, "epoch=123", "epoch=456", 1), strings.Replace(q, "SELECT ? WHERE", "SELECT 1 WHERE", 1)} {
		if PointPlanSQLHash(q) == PointPlanSQLHash(changed) {
			t.Fatal("business/unsupported compiler shape normalized")
		}
	}
}
