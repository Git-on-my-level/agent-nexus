package scopedrepo_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"agent-nexus-core/internal/scopedrepo"
	"agent-nexus-core/internal/scopes"
)

func legacyFixture(t *testing.T) *sql.DB {
	t.Helper()
	db, _ := fixture(t)
	must(t, exec(db, `INSERT INTO scope_domains VALUES('lost','inaccessible',1),('other-lost','inaccessible',1); CREATE TABLE migration_checkpoint(cursor INTEGER NOT NULL); INSERT INTO migration_checkpoint VALUES(0)`))
	return db
}

func legacyCheckpointTx(t *testing.T, db *sql.DB) *sql.Tx {
	t.Helper()
	tx, err := db.Begin()
	must(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	_, err = tx.Exec(`UPDATE migration_checkpoint SET cursor=1`)
	must(t, err)
	return tx
}

func assertLegacyCounts(t *testing.T, db *sql.DB, checkpoint, resources int) {
	t.Helper()
	var got int
	must(t, db.QueryRow(`SELECT cursor FROM migration_checkpoint`).Scan(&got))
	if got != checkpoint {
		t.Fatalf("checkpoint = %d, want %d", got, checkpoint)
	}
	for _, table := range []string{"scope_resources", "scope_resource_rids"} {
		must(t, db.QueryRow("SELECT count(*) FROM "+table).Scan(&got))
		if got != resources {
			t.Fatalf("%s count = %d, want %d", table, got, resources)
		}
	}
}

func TestLegacyBatchAtomicCheckpointAndReplay(t *testing.T) {
	db := legacyFixture(t)
	ctx := context.Background()
	records := []scopedrepo.LegacyRecord{{Kind: "doc", CanonicalID: "old document:α", CanonicalVersion: 7}, {Kind: "board", CanonicalID: "old-board", CanonicalVersion: 2}}
	tx := legacyCheckpointTx(t, db)
	first, err := scopedrepo.RegisterLegacyBatch(ctx, tx, "lost", records)
	must(t, err)
	if len(first) != len(records) {
		t.Fatal(first)
	}
	for i, identity := range first {
		if identity.ScopeID != "lost" || identity.Kind != records[i].Kind || identity.CanonicalID != records[i].CanonicalID || identity.CanonicalVersion != records[i].CanonicalVersion || identity.RID < 1 || len(identity.ResourceID) != 32 || strings.Trim(identity.ResourceID, "0123456789abcdef") != "" {
			t.Fatal("invalid identity", identity)
		}
	}
	if first[0].RID == first[1].RID || first[0].ResourceID == first[1].ResourceID {
		t.Fatal("identities not distinct", first)
	}
	// Success must leave the caller's transaction open, allowing the same batch
	// and checkpoint to be committed as one unit.
	again, err := scopedrepo.RegisterLegacyBatch(ctx, tx, "lost", records)
	must(t, err)
	if !reflect.DeepEqual(first, again) {
		t.Fatal("same-transaction replay changed identity", first, again)
	}
	must(t, tx.Commit())
	assertLegacyCounts(t, db, 1, 2)
	tx = legacyCheckpointTx(t, db)
	again, err = scopedrepo.RegisterLegacyBatch(ctx, tx, "lost", records)
	must(t, err)
	if !reflect.DeepEqual(first, again) {
		t.Fatal("committed replay changed identity", first, again)
	}
	must(t, tx.Commit())
	// Registration never creates a caller-controlled alias or replay handle.
	for _, table := range []string{"scope_aliases", "scope_replays"} {
		var n int
		must(t, db.QueryRow("SELECT count(*) FROM "+table).Scan(&n))
		if n != 0 {
			t.Fatal(table, n)
		}
	}
}

func TestLegacyBatchCallerRollback(t *testing.T) {
	db := legacyFixture(t)
	tx := legacyCheckpointTx(t, db)
	_, err := scopedrepo.RegisterLegacyBatch(context.Background(), tx, "lost", []scopedrepo.LegacyRecord{{Kind: "doc", CanonicalID: "old", CanonicalVersion: 1}})
	must(t, err)
	must(t, tx.Rollback())
	assertLegacyCounts(t, db, 0, 0)
}

func TestLegacyBatchConflictsAbortIgnoredError(t *testing.T) {
	for _, conflict := range []string{"version", "scope", "duplicate-version", "insert-failure"} {
		t.Run(conflict, func(t *testing.T) {
			db := legacyFixture(t)
			ctx := context.Background()
			record := scopedrepo.LegacyRecord{Kind: "doc", CanonicalID: "old", CanonicalVersion: 4}
			tx := legacyCheckpointTx(t, db)
			original, err := scopedrepo.RegisterLegacyBatch(ctx, tx, "lost", []scopedrepo.LegacyRecord{record})
			must(t, err)
			must(t, tx.Commit())
			must(t, exec(db, `UPDATE migration_checkpoint SET cursor=0`))
			sink := scopes.ID("lost")
			batch := []scopedrepo.LegacyRecord{{Kind: "doc", CanonicalID: "new", CanonicalVersion: 1}, record}
			switch conflict {
			case "version":
				batch[1].CanonicalVersion++
			case "scope":
				sink = "other-lost"
			case "duplicate-version":
				batch = append(batch, scopedrepo.LegacyRecord{Kind: "doc", CanonicalID: "new", CanonicalVersion: 2})
			case "insert-failure":
				must(t, exec(db, `CREATE TRIGGER reject_legacy_insert BEFORE INSERT ON scope_resources WHEN NEW.canonical_id='fails' BEGIN SELECT RAISE(ABORT,'rejected'); END`))
				batch = append(batch, scopedrepo.LegacyRecord{Kind: "doc", CanonicalID: "fails", CanonicalVersion: 1})
			}
			tx = legacyCheckpointTx(t, db)
			out, err := scopedrepo.RegisterLegacyBatch(ctx, tx, sink, batch)
			if err == nil || out != nil {
				t.Fatal("conflicting batch accepted", out, err)
			}
			if err := tx.Commit(); !errors.Is(err, sql.ErrTxDone) {
				t.Fatal("ignored error could commit", err)
			}
			assertLegacyCounts(t, db, 0, 1)
			tx = legacyCheckpointTx(t, db)
			after, err := scopedrepo.RegisterLegacyBatch(ctx, tx, "lost", []scopedrepo.LegacyRecord{record})
			must(t, err)
			if !reflect.DeepEqual(original, after) {
				t.Fatal("conflict rebound identity", original, after)
			}
			must(t, tx.Rollback())
		})
	}
}

func TestLegacyBatchVerifiesNoGrantsSink(t *testing.T) {
	for _, target := range []string{"missing", "public", "private", "transitioning", "rogue"} {
		t.Run(target, func(t *testing.T) {
			db := legacyFixture(t)
			must(t, exec(db, `INSERT INTO scope_domains VALUES('transitioning','transitioning',1),('rogue','inaccessible',1)`))
			// Simulate historical corruption: checking state alone is insufficient.
			must(t, exec(db, `DROP TRIGGER scope_no_grants_insert; INSERT INTO scope_memberships VALUES('rogue-reader','rogue','reader',1)`))
			tx := legacyCheckpointTx(t, db)
			out, err := scopedrepo.RegisterLegacyBatch(context.Background(), tx, scopes.ID(target), []scopedrepo.LegacyRecord{{Kind: "doc", CanonicalID: "old", CanonicalVersion: 1}})
			if !errors.Is(err, scopes.ErrDenied) || out != nil {
				t.Fatal("unverified sink accepted", out, err)
			}
			if err := tx.Commit(); !errors.Is(err, sql.ErrTxDone) {
				t.Fatal("invalid sink did not abort", err)
			}
			assertLegacyCounts(t, db, 0, 0)
		})
	}
}

func TestLegacyBatchInputValidationBeforeDatabaseWork(t *testing.T) {
	valid := scopedrepo.LegacyRecord{Kind: "doc", CanonicalID: "old", CanonicalVersion: 1}
	cases := []struct {
		name string
		sink scopes.ID
		rows []scopedrepo.LegacyRecord
	}{
		{"empty-batch", "lost", nil},
		{"over-64", "lost", make([]scopedrepo.LegacyRecord, 65)},
		{"empty-sink", "", []scopedrepo.LegacyRecord{valid}},
		{"large-sink", scopes.ID(strings.Repeat("s", 257)), []scopedrepo.LegacyRecord{valid}},
		{"nul-sink", "lost\x00", []scopedrepo.LegacyRecord{valid}},
	}
	for _, field := range []string{"kind", "canonical"} {
		values := []string{"", strings.Repeat("a", 513)}
		if field == "kind" {
			values = append(values, strings.Repeat("k", 33), "nul\x00byte", "newline\n", string([]byte{0xff}))
		}
		for i, value := range values {
			r := valid
			if field == "kind" {
				r.Kind = value
			} else {
				r.CanonicalID = value
			}
			cases = append(cases, struct {
				name string
				sink scopes.ID
				rows []scopedrepo.LegacyRecord
			}{fmt.Sprintf("%s-case-%d", field, i), "lost", []scopedrepo.LegacyRecord{valid, r}})
		}
	}
	for _, version := range []int64{0, -1} {
		r := valid
		r.CanonicalVersion = version
		cases = append(cases, struct {
			name string
			sink scopes.ID
			rows []scopedrepo.LegacyRecord
		}{fmt.Sprintf("version-%d", version), "lost", []scopedrepo.LegacyRecord{r}})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := legacyFixture(t)
			// Missing authority table would cause a SQL error if validation ever
			// reached a query. The budget error proves rejection precedes SQL.
			must(t, exec(db, `ALTER TABLE scope_domains RENAME TO unavailable_domains`))
			tx := legacyCheckpointTx(t, db)
			out, err := scopedrepo.RegisterLegacyBatch(context.Background(), tx, tc.sink, tc.rows)
			if !errors.Is(err, scopes.ErrBudget) || out != nil {
				t.Fatal("input not rejected before database work", out, err)
			}
			if err := tx.Commit(); !errors.Is(err, sql.ErrTxDone) {
				t.Fatal("invalid input did not abort", err)
			}
			assertLegacyCounts(t, db, 0, 0)
		})
	}
}

func TestLegacyBatchPreservesBinaryCanonicalIdentity(t *testing.T) {
	db := legacyFixture(t)
	records := []scopedrepo.LegacyRecord{
		{Kind: "doc", CanonicalID: "old\x00hidden\xff\n", CanonicalVersion: 1},
		{Kind: "doc", CanonicalID: "old", CanonicalVersion: 1},
	}
	tx := legacyCheckpointTx(t, db)
	first, err := scopedrepo.RegisterLegacyBatch(context.Background(), tx, "lost", records)
	must(t, err)
	must(t, tx.Commit())
	if first[0].CanonicalID != records[0].CanonicalID || first[0].RID == first[1].RID {
		t.Fatal("legacy identity bytes changed", first)
	}
	tx = legacyCheckpointTx(t, db)
	again, err := scopedrepo.RegisterLegacyBatch(context.Background(), tx, "lost", records)
	must(t, err)
	if !reflect.DeepEqual(first, again) {
		t.Fatal("binary replay changed identity", first, again)
	}
	must(t, tx.Commit())
}

type legacyPanicContext struct{ context.Context }

func (legacyPanicContext) Done() <-chan struct{} { panic("legacy context panic") }

func TestLegacyBatchPanicAbortsTransaction(t *testing.T) {
	db := legacyFixture(t)
	tx := legacyCheckpointTx(t, db)
	func() {
		defer func() {
			if got := recover(); got != "legacy context panic" {
				t.Fatal("panic not preserved", got)
			}
		}()
		_, _ = scopedrepo.RegisterLegacyBatch(legacyPanicContext{context.Background()}, tx, "lost", []scopedrepo.LegacyRecord{{Kind: "doc", CanonicalID: "old", CanonicalVersion: 1}})
	}()
	if err := tx.Commit(); !errors.Is(err, sql.ErrTxDone) {
		t.Fatal("panic did not abort", err)
	}
	assertLegacyCounts(t, db, 0, 0)
}

func TestLegacyBatch64AndBoundedMetadata(t *testing.T) {
	db := legacyFixture(t)
	records := make([]scopedrepo.LegacyRecord, 64)
	for i := range records {
		records[i] = scopedrepo.LegacyRecord{Kind: strings.Repeat("k", 32), CanonicalID: fmt.Sprintf("%0512d", i), CanonicalVersion: 1}
	}
	tx := legacyCheckpointTx(t, db)
	out, err := scopedrepo.RegisterLegacyBatch(context.Background(), tx, "lost", records)
	must(t, err)
	if len(out) != 64 {
		t.Fatal(len(out))
	}
	must(t, tx.Commit())
	assertLegacyCounts(t, db, 1, 64)
}

func TestLegacyBatchQueriesUseExactIndexes(t *testing.T) {
	db := legacyFixture(t)
	for _, tc := range []struct {
		name string
		args []any
	}{{"legacy_sink", []any{"lost"}}, {"legacy_identity", []any{"doc", "old"}}} {
		t.Run(tc.name, func(t *testing.T) {
			query, err := os.ReadFile("queries/" + tc.name + ".sql")
			must(t, err)
			rows, err := db.Query("EXPLAIN QUERY PLAN "+string(query), tc.args...)
			must(t, err)
			defer rows.Close()
			probes := 0
			for rows.Next() {
				var id, parent, unused int
				var detail string
				must(t, rows.Scan(&id, &parent, &unused, &detail))
				if strings.Contains(detail, "SCAN") {
					t.Fatal("unbounded registration query", detail)
				}
				if strings.Contains(detail, "SEARCH") {
					probes++
				}
			}
			must(t, rows.Err())
			if probes != 2 {
				t.Fatal("expected two exact index probes", probes)
			}
		})
	}
}

func TestLegacyBatchRejectsCorruptIdentityMetadata(t *testing.T) {
	for _, corruption := range []string{"missing-rid", "oversized-opaque", "noninteger-version"} {
		t.Run(corruption, func(t *testing.T) {
			db := legacyFixture(t)
			switch corruption {
			case "missing-rid":
				must(t, exec(db, `DROP TRIGGER scope_resource_rid_allocate`))
				must(t, exec(db, `INSERT INTO scope_resources VALUES('lost','doc','opaque','old',1)`))
			case "oversized-opaque":
				must(t, exec(db, `INSERT INTO scope_resources VALUES('lost','doc',?,'old',1)`, strings.Repeat("x", 65537)))
			case "noninteger-version":
				must(t, exec(db, `INSERT INTO scope_resources VALUES('lost','doc','opaque','old',1.5)`))
			}
			tx := legacyCheckpointTx(t, db)
			out, err := scopedrepo.RegisterLegacyBatch(context.Background(), tx, "lost", []scopedrepo.LegacyRecord{{Kind: "doc", CanonicalID: "old", CanonicalVersion: 1}})
			if err == nil || out != nil {
				t.Fatal("corrupt identity accepted", out, err)
			}
			if err := tx.Commit(); !errors.Is(err, sql.ErrTxDone) {
				t.Fatal("corrupt identity did not abort", err)
			}
			var checkpoint, n int
			must(t, db.QueryRow(`SELECT cursor FROM migration_checkpoint`).Scan(&checkpoint))
			must(t, db.QueryRow(`SELECT count(*) FROM scope_resources`).Scan(&n))
			if checkpoint != 0 || n != 1 {
				t.Fatal("corrupt identity was rewritten", checkpoint, n)
			}
		})
	}
}

func TestLegacyBatchIgnoresHiddenAliases(t *testing.T) {
	db := legacyFixture(t)
	must(t, exec(db, `INSERT INTO scope_resources VALUES('private','doc','hidden-opaque','hidden-canonical',1); INSERT INTO scope_aliases VALUES('private','doc','legacy-name','hidden-opaque',0)`))
	tx := legacyCheckpointTx(t, db)
	identities, err := scopedrepo.RegisterLegacyBatch(context.Background(), tx, "lost", []scopedrepo.LegacyRecord{{Kind: "doc", CanonicalID: "legacy-name", CanonicalVersion: 1}})
	must(t, err)
	if identities[0].ResourceID == "hidden-opaque" || identities[0].CanonicalID != "legacy-name" || identities[0].ScopeID != "lost" {
		t.Fatal("hidden alias influenced registration", identities)
	}
	must(t, tx.Commit())
	var n int
	must(t, db.QueryRow(`SELECT count(*) FROM scope_aliases`).Scan(&n))
	if n != 1 {
		t.Fatal("registration altered aliases", n)
	}
}
