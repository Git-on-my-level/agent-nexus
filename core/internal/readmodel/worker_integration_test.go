package readmodel_test

import (
	"agent-nexus-core/internal/readmodel"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"agent-nexus-core/internal/scopedrepo"
	"agent-nexus-core/internal/scopemigrate"
	"agent-nexus-core/internal/scopes"
	"agent-nexus-core/internal/storage"
)

type completedMetadataSource struct{}

func (completedMetadataSource) Epoch(context.Context, *sql.Tx) (int64, error) { return 1, nil }
func (completedMetadataSource) Page(context.Context, *sql.Tx, string, int, int) ([]scopemigrate.Record, bool, error) {
	return nil, true, nil
}

// Candidate A/D adapter. Runner owns transaction lifetime and its unexpired
// lease; B additionally checks the scope fence at checkpoint/publication.
type workerLifecycleBridge struct {
	scope    scopes.ID
	fault    string
	examined int
	expire   bool
}

func (b *workerLifecycleBridge) Step(ctx context.Context, tx *sql.Tx, limit int, token int64) (bool, error) {
	if limit != readmodel.MaxLifecycleChunk || token < 1 {
		return false, readmodel.ErrBudget
	}
	loader := func(ctx context.Context, j readmodel.Job, limit int) ([]readmodel.RebuildRecord, error) {
		rows, err := tx.QueryContext(ctx, `SELECT c.rid,c.parent,c.hidden,COALESCE(p.hidden,0) FROM worker_tree c LEFT JOIN worker_tree p ON p.rid=c.parent WHERE c.rid>? ORDER BY c.rid LIMIT ?`, j.Cursor, limit)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []readmodel.RebuildRecord
		for rows.Next() {
			var rid, parent int64
			var hidden, parentHidden int
			if err := rows.Scan(&rid, &parent, &hidden, &parentHidden); err != nil {
				return nil, err
			}
			row := readmodel.LifecycleRow{RID: rid, ScopeID: j.ScopeID}
			if parent != 0 {
				row.Ancestors = []readmodel.Ancestor{{ScopeID: j.ScopeID, RID: parent}}
			}
			payloads := map[readmodel.Stream]json.RawMessage{}
			if hidden|parentHidden == 0 {
				row.After = &readmodel.Projection{ScopeID: j.ScopeID, Generation: j.Generation, RID: rid, Version: 1, Entries: []readmodel.Entry{{Family: "work", Audience: "all", Sort: rid, Buckets: []string{"live"}}}}
				payloads[readmodel.Stream{Scope: j.ScopeID, Family: "work", Audience: "all"}] = json.RawMessage(`{"title":"independent live root"}`)
			}
			out = append(out, readmodel.RebuildRecord{Row: row, Payloads: payloads})
		}
		return out, rows.Err()
	}
	result, err := readmodel.Step(ctx, readmodel.NewDurableLifecycle(&txAdapter{tx: tx, fail: b.fault}, b.scope, loader))
	if err != nil {
		return false, err
	}
	b.examined = result.Examined
	if b.expire {
		// Simulate lease loss after B has staged/checkpointed its slice. D's
		// final fenced save must roll back B's writes as well as this change.
		if _, err := tx.ExecContext(ctx, `UPDATE scope_migration_jobs SET lease_until=0 WHERE job='b-lifecycle'`); err != nil {
			return false, err
		}
	}
	return result.Complete, nil
}

func TestDurableWorkerLifecycleTenThousandAndLeaseRollback(t *testing.T) {
	if testing.Short() {
		t.Skip("durable worker/storage integration")
	}
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "worker.db")
	db := openFixture(t, path)
	defer func() { db.Close() }()
	r := scopedrepo.New(db)
	if err := r.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	if err := r.InitializeFeedSchema(ctx); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err = storage.InstallScopeMigration(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(readmodel.LifecycleSchemaProposal + `CREATE TABLE resource_access_epoch(singleton INTEGER PRIMARY KEY,version INTEGER); INSERT INTO resource_access_epoch VALUES(1,7);
CREATE TABLE worker_tree(rid INTEGER PRIMARY KEY,parent INTEGER,hidden INTEGER);
INSERT INTO worker_tree VALUES(1,0,1),(2,0,0);
WITH RECURSIVE seq(n) AS(SELECT 3 UNION ALL SELECT n+1 FROM seq WHERE n<10002) INSERT INTO worker_tree SELECT n,1,0 FROM seq;
INSERT INTO scope_domains VALUES('scope','active',1);
INSERT INTO scope_resources VALUES('scope','card','opaque-root','canonical-root',1),('scope','card','opaque-live','canonical-live',1);`); err != nil {
		t.Fatal(err)
	}
	if err = readmodel.BeginLifecycle(ctx, &txAdapter{tx: tx}, "scope", 1); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	b := &workerLifecycleBridge{scope: "scope"}
	runner := &scopemigrate.Runner{DB: db, Source: completedMetadataSource{}, Job: "b-lifecycle", Owner: "owner-1", SealedScope: "sealed", LeaseDuration: time.Minute, MaxBytes: 4096, Rebuilder: b}
	token, err := runner.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	checkCursor := func(want int64) {
		t.Helper()
		var got int64
		if err := db.QueryRow(`SELECT cursor FROM scope_feed_jobs`).Scan(&got); err != nil || got != want {
			t.Fatal("checkpoint", got, want, err)
		}
	}
	checkNoStagedWrites := func() {
		t.Helper()
		var count int
		if err := db.QueryRow(`SELECT (SELECT count(*) FROM scope_feed)+(SELECT count(*) FROM scope_feed_payloads)+(SELECT count(*) FROM scope_counters)`).Scan(&count); err != nil || count != 0 {
			t.Fatal("partial staged writes survived", count, err)
		}
	}
	b.fault = readmodel.CheckpointJob
	if _, err = runner.LifecycleStep(ctx, token); err == nil {
		t.Fatal("checkpoint fault ignored")
	}
	checkCursor(0)
	checkNoStagedWrites()
	b.fault = ""
	b.expire = true
	if _, err = runner.LifecycleStep(ctx, token); !errors.Is(err, scopemigrate.ErrLeaseLost) {
		t.Fatal("late lease loss", err)
	}
	checkCursor(0)
	checkNoStagedWrites()
	b.expire = false
	if _, err = db.Exec(`UPDATE scope_migration_jobs SET lease_until=0 WHERE job='b-lifecycle'`); err != nil {
		t.Fatal(err)
	}
	if _, err = runner.LifecycleStep(ctx, token); !errors.Is(err, scopemigrate.ErrLeaseLost) {
		t.Fatal("expired lease", err)
	}
	checkCursor(0)
	token, err = runner.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	examined := 0
	for examined < 10002 {
		done, err := runner.LifecycleStep(ctx, token)
		if err != nil || done || b.examined < 1 || b.examined > 64 {
			t.Fatal("bounded slice", done, b.examined, err)
		}
		examined += b.examined
		if examined == 64 {
			checkCursor(64)
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			db = openFixture(t, path)
			// sql.Open is lazy. Initialize the restored physical connection
			// before D starts its 50 ms transaction window, as its contention
			// tests do; reconnect setup is not lifecycle chunk work.
			if err := db.PingContext(ctx); err != nil {
				t.Fatal(err)
			}
			runner.DB = db
		}
	}
	checkCursor(10002)
	if _, err = runner.LifecycleStep(ctx, token); err == nil {
		t.Fatal("activated without trusted receipts")
	}
	checkCursor(10002)
	// Synthetic certificates test publication mechanics only. Production has no
	// receipt writer until A proves all-family legacy parity and current epoch.
	if _, err = db.Exec(`INSERT INTO scope_feed_generations VALUES('scope',3,1,1,1,1,7)`); err != nil {
		t.Fatal(err)
	}
	done, err := runner.LifecycleStep(ctx, token)
	if err != nil || !done {
		t.Fatal("activation", done, err)
	}
	var generation int64
	var state string
	if err = db.QueryRow(`SELECT generation,state FROM scope_domains WHERE id='scope'`).Scan(&generation, &state); err != nil || generation != 3 || state != "active" {
		t.Fatal(state, generation, err)
	}
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM scope_feed`).Scan(&count); err != nil || count != 1 {
		t.Fatal("archived descendants staged", count, err)
	}
	if err = db.QueryRow(`SELECT value FROM scope_counters WHERE bucket='live'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("live counter", count, err)
	}
}
