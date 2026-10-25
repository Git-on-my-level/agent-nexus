package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"agent-nexus-core/internal/testsql"
	"agent-nexus-core/internal/testutil/perfguard"
)

func TestAdmissionMigrationCommitsAndResumesRootChunks(t *testing.T) {
	root := t.TempDir()
	db := mainHistoryDatabase(t, root, 115)
	for _, q := range []string{
		`INSERT INTO threads(id,updated_at,updated_by,body_json) WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i<3) SELECT 'root-'||i,'now','owner','{"pm_actor_id":"owner"}' FROM n`,
		`INSERT INTO cards(id,handle,title,summary,column_key,refs_json,created_at,created_by,updated_at,updated_by) WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i<63) SELECT 'shared-'||i,'shared-'||i,'Public','','ready','["thread:root-0","thread:root-1","thread:root-2","thread:root-3"]','now','owner','now','owner' FROM n`,
		`INSERT INTO resource_access_tombstones(kind,id,ref,owner) VALUES('thread','root-0','retained-root','second-owner')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	read, err := sql.Open("sqlite", sqliteDSN(NewLayout(root).DatabasePath))
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	write, counter := testsql.Open(sqliteDSN(NewLayout(root).DatabasePath))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	interrupted, durableRoots := false, 0
	var inspectErr error
	counter.BeforeQuery(func(q string) {
		if interrupted {
			return
		}
		var committed bool
		inspectErr = read.QueryRow(`SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name='resource_access_admission_reach')`).Scan(&committed)
		if inspectErr != nil || !committed {
			return
		}
		inspectErr = read.QueryRow(`SELECT count(*) FROM (SELECT DISTINCT root_kind,root_id FROM resource_access_admission_reach)`).Scan(&durableRoots)
		if inspectErr == nil && durableRoots > 0 {
			interrupted = true
			cancel() // Interrupt after observing the first independently committed root.
		}
	})
	var admission migration
	for _, m := range migrations {
		if m.Version == 116 {
			admission = m
		}
	}
	err = applyMigration(ctx, write, admission)
	write.Close()
	if !errors.Is(err, context.Canceled) || !interrupted || inspectErr != nil || durableRoots != 1 {
		t.Fatalf("interrupted=%v durable roots=%d inspection=%v migration=%v; want one committed root before cancellation", interrupted, durableRoots, inspectErr, err)
	}
	var recorded int
	if err := read.QueryRow(`SELECT count(*) FROM schema_migrations WHERE version=116`).Scan(&recorded); err != nil || recorded != 0 {
		t.Fatalf("incomplete migration recorded: count=%d err=%v", recorded, err)
	}
	if _, err := read.Exec(`CREATE TRIGGER test_completed_root_not_replayed BEFORE INSERT ON resource_access_admission_reach WHEN NEW.root_id='root-0' BEGIN SELECT RAISE(ABORT,'completed root replayed'); END`); err != nil {
		t.Fatal(err)
	}
	read.Close()
	ws, err := InitializeWorkspace(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	checkAdmissionSecurityParity(t, ws.DB(), "resumed root chunks")
	if err := ws.DB().QueryRow(`SELECT count(*) FROM sqlite_schema WHERE name='test_completed_root_not_replayed'`).Scan(&recorded); err != nil || recorded != 1 {
		t.Fatalf("resumption rebuilt committed state: count=%d err=%v", recorded, err)
	}
	if err := ws.DB().QueryRow(`SELECT count(*) FROM schema_migrations WHERE version=116`).Scan(&recorded); err != nil || recorded != 1 {
		t.Fatalf("completed migration not recorded: count=%d err=%v", recorded, err)
	}
}

func TestAdmissionMigrationRootChunksScale(t *testing.T) {
	if testing.Short() {
		t.Skip("released-schema 4096-card dense migration fixture")
	}
	const roots, shared = 512, 512
	root := t.TempDir()
	fixture := mainHistoryDatabase(t, root, 115)
	for _, q := range []string{
		`INSERT INTO threads(id,updated_at,updated_by,body_json) WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i<511) SELECT printf('root-%03d',i),'now','owner',json_object('pm_actor_id',CASE WHEN i%2=0 THEN 'owner' ELSE 'second-owner' END) FROM n`,
		`INSERT INTO cards(id,handle,title,summary,column_key,refs_json,archived_at,created_at,created_by,updated_at,updated_by) WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i<4095) SELECT CASE WHEN i=0 THEN 'root-hub' ELSE printf('shared-%04d',i) END,CASE WHEN i=0 THEN 'root-hub' ELSE printf('shared-%04d',i) END,'Public','','ready',CASE WHEN i=0 THEN (SELECT json_group_array('thread:'||id) FROM threads) WHEN i<=512 THEN '["card:root-hub"]' ELSE '[]' END,CASE WHEN i<=512 THEN 'now' END,'now','owner','now','owner' FROM n`,
	} {
		if _, err := fixture.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	fixture.Close()
	db, capture, err := perfguard.Open(sqliteDSN(NewLayout(root).DatabasePath))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	var changes []int64
	var times []time.Time
	var observerErr error
	capture.BeforeBeginConn(func(ctx context.Context, conn driver.Conn, _ driver.TxOptions) {
		rows, err := conn.(driver.QueryerContext).QueryContext(ctx, `SELECT total_changes()`, nil)
		if err != nil {
			observerErr = err
			return
		}
		defer rows.Close()
		value := make([]driver.Value, 1)
		if err := rows.Next(value); err != nil {
			observerErr = err
			return
		}
		changes = append(changes, value[0].(int64))
		times = append(times, time.Now())
	})
	var admission migration
	for _, m := range migrations {
		if m.Version == 116 {
			admission = m
		}
	}
	start := time.Now()
	if err := applyMigration(context.Background(), db, admission); err != nil {
		t.Fatal(err)
	}
	if observerErr != nil {
		t.Fatal(observerErr)
	}
	if len(changes) != roots+2 {
		t.Fatalf("transactions=%d want setup + %d roots + completion", len(changes), roots)
	}
	var maxChanges int64
	var maxElapsed time.Duration
	for i := 2; i < len(changes); i++ {
		delta := changes[i] - changes[i-1]
		maxChanges = max(maxChanges, delta)
		maxElapsed = max(maxElapsed, times[i].Sub(times[i-1]))
		// At most one reach, owner and lifecycle write for each vertex in
		// this root's three ledgers, plus its durable cursor update.
		if delta > 3*3*(shared+2)+1 {
			t.Fatalf("one root rewrote unrelated memberships: changes=%d", delta)
		}
	}
	t.Logf("cards=4096 roots=%d shared=%d transactions=%d setup_writes=%d max_root_writes=%d max_root_elapsed=%s elapsed=%s", roots, shared, len(changes), changes[1]-changes[0], maxChanges, maxElapsed, time.Since(start))
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	ws, err := InitializeWorkspace(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	checkAdmissionSecurityParity(t, ws.DB(), "dense chunked migration")
}
