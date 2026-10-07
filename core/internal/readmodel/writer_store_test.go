package readmodel_test

import (
	"agent-nexus-core/internal/readmodel"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"path/filepath"
	"strings"
	"testing"

	"agent-nexus-core/internal/resourceaccess"
	"agent-nexus-core/internal/scopedrepo"
	"agent-nexus-core/internal/scopes"
)

type txAdapter struct {
	tx interface {
		ExecContext(context.Context, string, ...any) (sql.Result, error)
		QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	}
	fail  string
	calls int
}

func (a *txAdapter) Exec(ctx context.Context, q string, args ...any) (int64, error) {
	a.calls++
	if q == a.fail {
		return 0, errors.New("injected transaction fault")
	}
	if "zero:"+q == a.fail {
		return 0, nil
	}
	result, err := a.tx.ExecContext(ctx, q, args...)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
func (a *txAdapter) Query(ctx context.Context, q string, args ...any) (readmodel.Rows, error) {
	a.calls++
	return a.tx.QueryContext(ctx, q, args...)
}

type testHook func(context.Context, scopedrepo.MutationTx, scopes.CanonicalMutation) error

func (h testHook) ApplyCanonical(ctx context.Context, tx scopedrepo.MutationTx, m scopes.CanonicalMutation) error {
	return h(ctx, tx, m)
}
func testCapture(c scopes.Change, p scopes.Projection) (readmodel.Entry, json.RawMessage, error) {
	data, err := json.Marshal(map[string]string{"status": p.Status})
	return readmodel.Entry{Family: c.Family, Audience: c.Audience, Sort: p.Timestamp, Buckets: []string{p.Status}}, data, err
}
func TestWriterCanonicalHookSourceFeedPayloadCounterAtomicity(t *testing.T) {
	for _, fault := range []string{"", readmodel.InsertFeed, readmodel.InsertPayload, readmodel.IncrementCounter, readmodel.DeleteFeed, readmodel.DeletePayload, readmodel.DecrementCounter, "zero:" + readmodel.DeletePayload, "zero:" + readmodel.DecrementCounter} {
		t.Run(fault, func(t *testing.T) {
			db, _, request, _ := adapterFixture(t, 1, 1)
			ctx := context.Background()
			scope := request.ScopeIDs[0]
			if _, err := db.Exec(`CREATE TABLE test_source(id TEXT PRIMARY KEY,version INTEGER,state TEXT);INSERT INTO test_source VALUES('opaque',1,'open')`); err != nil {
				t.Fatal(err)
			}
			old := &readmodel.Projection{ScopeID: scope, Generation: 1, RID: 1, Version: 1, Entries: []readmodel.Entry{{Family: "inbox", Audience: "reader", Sort: 1, Buckets: []string{"open"}}}}
			tx, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			if err = readmodel.ApplyProjection(ctx, &txAdapter{tx: tx}, nil, old, map[readmodel.Stream]json.RawMessage{{Scope: scope, Family: "inbox", Audience: "reader"}: json.RawMessage(`{"status":"open"}`)}, false); err != nil {
				t.Fatal(err)
			}
			if err = tx.Commit(); err != nil {
				t.Fatal(err)
			}
			source, err := resourceaccess.NewDB(db).BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer source.Rollback()
			var version int64
			var state string
			if err = source.QueryRowContext(ctx, `SELECT version,state FROM test_source WHERE id=?`, "opaque").Scan(&version, &state); err != nil {
				t.Fatal(err)
			}
			if _, err = source.ExecContext(ctx, `UPDATE test_source SET version=2,state='answered' WHERE id=? AND version=?`, "opaque", version); err != nil {
				t.Fatal(err)
			}
			m := scopes.CanonicalMutation{Identity: scopes.ResourceIdentity{ScopeID: scope, Kind: "card", ResourceID: "opaque", RID: 1, CanonicalID: "opaque", CanonicalVersion: 2}, PreviousVersion: version, Changes: []scopes.Change{{ScopeID: scope, Kind: "card", ResourceID: "opaque", CanonicalVersion: 2, Family: "inbox", Audience: "reader", Before: &scopes.Projection{Status: state, Timestamp: 1}, After: &scopes.Projection{Status: "answered", Timestamp: 2}}}}
			calls := 0
			err = scopedrepo.ApplyCanonicalHooks(ctx, source, m, testHook(func(ctx context.Context, cap scopedrepo.MutationTx, m scopes.CanonicalMutation) error {
				old, next, payloads, err := readmodel.CaptureCanonical(m, 1, testCapture)
				if err != nil {
					return err
				}
				adapter := &txAdapter{tx: cap, fail: fault}
				err = readmodel.ApplyProjection(ctx, adapter, old, next, payloads, false)
				calls = adapter.calls
				return err
			}))
			if fault == "" {
				if err != nil {
					t.Fatal(err)
				}
				if err = source.Commit(); err != nil {
					t.Fatal(err)
				}
			} else {
				if err == nil {
					t.Fatal("missing fault")
				}
				if !errors.Is(source.Commit(), sql.ErrTxDone) {
					t.Fatal("source left committable")
				}
			}
			if calls > 24 {
				t.Fatal("write fanout", calls)
			}
			var actual string
			var actualVersion int64
			if err = db.QueryRow(`SELECT state,version FROM test_source WHERE id='opaque'`).Scan(&actual, &actualVersion); err != nil {
				t.Fatal(err)
			}
			want := "open"
			wantVersion := int64(1)
			if fault == "" {
				want = "answered"
				wantVersion = 2
			}
			if actual != want || actualVersion != wantVersion {
				t.Fatal(actual, actualVersion)
			}
			var payload string
			var feedVersion int64
			var open, answered int64
			if err = db.QueryRow(`SELECT version FROM scope_feed WHERE family='inbox'`).Scan(&feedVersion); err != nil {
				t.Fatal(err)
			}
			if err = db.QueryRow(`SELECT data FROM scope_feed_payloads WHERE family='inbox'`).Scan(&payload); err != nil {
				t.Fatal(err)
			}
			if err = db.QueryRow(`SELECT COALESCE(SUM(CASE bucket WHEN 'open' THEN value ELSE 0 END),0),COALESCE(SUM(CASE bucket WHEN 'answered' THEN value ELSE 0 END),0) FROM scope_counters WHERE family='inbox'`).Scan(&open, &answered); err != nil {
				t.Fatal(err)
			}
			if feedVersion != wantVersion || !strings.Contains(payload, want) || fault == "" && (open != 0 || answered != 1) || fault != "" && (open != 1 || answered != 0) {
				t.Fatal(feedVersion, payload, open, answered)
			}
		})
	}
}
func TestWriterRejectsCorruptOldProjectionAndOverflow(t *testing.T) {
	db, _, request, _ := adapterFixture(t, 1, 1)
	ctx := context.Background()
	p := &readmodel.Projection{ScopeID: request.ScopeIDs[0], Generation: 1, RID: 999, Version: 1, Entries: []readmodel.Entry{{Family: "work", Audience: "reader", Buckets: []string{"total"}}}}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	a := &txAdapter{tx: tx}
	if err = readmodel.ApplyProjection(ctx, a, p, nil, nil, false); !errors.Is(err, readmodel.ErrProjection) {
		t.Fatal(err)
	}
	tx.Rollback()
	tx, err = db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	a = &txAdapter{tx: tx}
	payloads := map[readmodel.Stream]json.RawMessage{{Scope: p.ScopeID, Family: "work", Audience: "reader"}: json.RawMessage(`{}`)}
	if _, err = tx.Exec(readmodel.IncrementCounter, p.ScopeID, 1, "work", "reader", "total", int64(math.MaxInt64)); err != nil {
		t.Fatal(err)
	}
	if err = readmodel.ApplyProjection(ctx, a, nil, p, payloads, false); err == nil {
		t.Fatal("overflow accepted")
	}
	tx.Rollback()
	tx, err = db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	a = &txAdapter{tx: tx}
	payloads[readmodel.Stream{Scope: p.ScopeID, Family: "work", Audience: "reader"}] = json.RawMessage(`bad JSON`)
	if err = readmodel.ApplyProjection(ctx, a, nil, p, payloads, false); !errors.Is(err, readmodel.ErrProjection) || a.calls != 0 {
		t.Fatal("late validation", err, a.calls)
	}
}

func TestDurableLifecycleTenThousandFencesRestartAndReceipt(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "durable.db")
	db := openFixture(t, path)
	defer func() { db.Close() }()
	repo := scopedrepo.New(db)
	if err := repo.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	if err := repo.InitializeFeedSchema(ctx); err != nil {
		t.Fatal(err)
	}
	_, err := db.Exec(readmodel.LifecycleSchemaProposal + `CREATE TABLE resource_access_epoch(singleton INTEGER PRIMARY KEY,version INTEGER);INSERT INTO resource_access_epoch VALUES(1,7);
 CREATE TABLE test_tree(rid INTEGER PRIMARY KEY,parent INTEGER NOT NULL,hidden INTEGER NOT NULL);
 INSERT INTO scope_domains VALUES('scope','active',1);INSERT INTO test_tree VALUES(1,0,1),(2,0,0);
 WITH RECURSIVE children(n) AS(SELECT 3 UNION ALL SELECT n+1 FROM children WHERE n<10002) INSERT INTO test_tree SELECT n,1,0 FROM children;`)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err = readmodel.BeginLifecycle(ctx, &txAdapter{tx: tx}, "scope", 1); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	run := func(fault string) (readmodel.StepResult, error) {
		tx, err := db.Begin()
		if err != nil {
			return readmodel.StepResult{}, err
		}
		defer tx.Rollback()
		a := &txAdapter{tx: tx, fail: fault}
		loader := func(ctx context.Context, j readmodel.Job, limit int) ([]readmodel.RebuildRecord, error) {
			rows, err := tx.QueryContext(ctx, `SELECT r.rid,r.parent,r.hidden,COALESCE(p.hidden,0) FROM test_tree r LEFT JOIN test_tree p ON p.rid=r.parent WHERE r.rid>? ORDER BY r.rid LIMIT ?`, j.Cursor, limit)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			var records []readmodel.RebuildRecord
			for rows.Next() {
				var rid, parent int64
				var hidden, parentHidden int
				if err = rows.Scan(&rid, &parent, &hidden, &parentHidden); err != nil {
					return nil, err
				}
				row := readmodel.LifecycleRow{RID: rid, ScopeID: j.ScopeID}
				if parent != 0 {
					row.Ancestors = []readmodel.Ancestor{{ScopeID: j.ScopeID, RID: parent}}
				}
				payloads := map[readmodel.Stream]json.RawMessage{}
				if hidden|parentHidden == 0 {
					row.After = &readmodel.Projection{ScopeID: j.ScopeID, Generation: j.Generation, RID: rid, Version: 1, Entries: []readmodel.Entry{{Family: "work", Audience: "reader", Sort: rid, Buckets: []string{"live"}}}}
					payloads[readmodel.Stream{Scope: j.ScopeID, Family: "work", Audience: "reader"}] = json.RawMessage(`{}`)
				}
				records = append(records, readmodel.RebuildRecord{Row: row, Payloads: payloads})
			}
			return records, rows.Err()
		}
		r, err := readmodel.Step(ctx, readmodel.NewDurableLifecycle(a, "scope", loader))
		if err != nil {
			return r, err
		}
		return r, tx.Commit()
	}
	if _, err = run(readmodel.CheckpointJob); err == nil {
		t.Fatal("missing checkpoint fault")
	}
	var cursor int64
	if err = db.QueryRow(`SELECT cursor FROM scope_feed_jobs`).Scan(&cursor); err != nil || cursor != 0 {
		t.Fatal(cursor, err)
	}
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM scope_feed`).Scan(&count); err != nil || count != 0 {
		t.Fatal("partial projections survived", count, err)
	}
	examined := 0
	for examined < 10002 {
		r, err := run("")
		if err != nil {
			t.Fatal(err)
		}
		if r.Complete || r.Examined < 1 || r.Examined > 64 {
			t.Fatal(r)
		}
		examined += r.Examined
		if examined == 64*30 {
			db.Close()
			db = openFixture(t, path)
		}
		var state string
		if err = db.QueryRow(`SELECT state FROM scope_domains`).Scan(&state); err != nil || state != "transitioning" {
			t.Fatal(state, err)
		}
	}
	// Empty seek alone is insufficient: a verified current receipt is mandatory.
	if _, err = run(""); !errors.Is(err, readmodel.ErrProjection) {
		t.Fatal("uncertified activation", err)
	}
	if _, err = db.Exec(`INSERT INTO scope_feed_generations VALUES('scope',3,1,1,1,1,6)`); err != nil {
		t.Fatal(err)
	}
	if _, err = run(""); !errors.Is(err, readmodel.ErrProjection) {
		t.Fatal("stale epoch activation", err)
	}
	if _, err = db.Exec(`UPDATE scope_feed_generations SET legacy_auth_epoch=7`); err != nil {
		t.Fatal(err)
	}
	if _, err = run(readmodel.DeleteJob); err == nil {
		t.Fatal("missing activation fault")
	}
	var state string
	var generation int64
	if err = db.QueryRow(`SELECT state,generation FROM scope_domains`).Scan(&state, &generation); err != nil || state != "transitioning" || generation != 2 {
		t.Fatal("activation escaped rollback", state, generation, err)
	}
	r, err := run("")
	if err != nil || !r.Complete {
		t.Fatal(r, err)
	}
	if err = db.QueryRow(`SELECT state,generation FROM scope_domains`).Scan(&state, &generation); err != nil || state != "active" || generation != 3 {
		t.Fatal(state, generation, err)
	}
	if err = db.QueryRow(`SELECT value FROM scope_counters WHERE bucket='live'`).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	// Stale worker cannot checkpoint against a superseding domain fence.
	tx, err = db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	a := &txAdapter{tx: tx}
	if err = readmodel.BeginLifecycle(ctx, a, "scope", 3); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`UPDATE scope_domains SET generation=6`); err != nil {
		t.Fatal(err)
	}
	if err = exact(ctx, a, readmodel.CheckpointJob, 64, "scope", 5, 4, 0, "scope", 4); !errors.Is(err, readmodel.ErrProjection) {
		t.Fatal("stale fence", err)
	}
	tx.Rollback()
}
