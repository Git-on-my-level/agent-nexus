package storage

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestScopeInboxMetadataMigrationResumes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	ws, err := InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ws.Close() }()
	for i := 0; i < 130; i++ {
		if _, err = ws.DB().Exec(`INSERT INTO derived_inbox_items(id,thread_id,category,trigger_at,generated_at,data_json) VALUES(?, 'source','ask','2026-10-07','2026-10-07','{}')`, fmt.Sprintf("row-%03d", i)); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []string{`DROP TRIGGER scope_inbox_live_insert`, `DROP TRIGGER scope_inbox_live_update`, `DROP TRIGGER scope_inbox_live_delete`, `DROP VIEW scope_inbox_live_positions`, `DROP TABLE scope_inbox_live`, `DROP TABLE scope_inbox_live_job`} {
		if _, err = ws.DB().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := ws.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = installScopeInboxLive(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = ws.DB().QueryRow(`SELECT count(*) FROM scope_inbox_live`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("startup backfilled %d %v", count, err)
	}
	done, err := ws.MaintainScopeInboxBatch(ctx)
	if err != nil || done {
		t.Fatal(done, err)
	}
	if err = ws.DB().QueryRow(`SELECT count(*) FROM scope_inbox_live`).Scan(&count); err != nil || count != 64 {
		t.Fatalf("slice %d %v", count, err)
	}
	if err = ws.Close(); err != nil {
		t.Fatal(err)
	}
	ws, err = InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`INSERT INTO derived_inbox_items(id,thread_id,category,trigger_at,generated_at,data_json) VALUES('a-behind-cursor','source','review','new','new','{}')`, `UPDATE derived_inbox_items SET thread_id='moved',trigger_at='edited' WHERE id='row-001'`, `DELETE FROM derived_inbox_items WHERE id='row-002'`} {
		if _, err = ws.DB().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	for !done {
		done, err = ws.MaintainScopeInboxBatch(ctx)
		if err != nil {
			t.Fatal(err)
		}
	}
	var wrong int
	err = ws.DB().QueryRow(`SELECT (SELECT count(*) FROM derived_inbox_items i LEFT JOIN scope_inbox_live f ON f.id=i.id WHERE f.id IS NULL OR f.scope_id<>i.thread_id OR f.trigger_at<>i.trigger_at) + (SELECT count(*) FROM scope_inbox_live f LEFT JOIN derived_inbox_items i ON i.id=f.id WHERE i.id IS NULL)`).Scan(&wrong)
	if err != nil || wrong != 0 {
		t.Fatalf("resumed index %d %v", wrong, err)
	}
}

func TestScopeInboxMaintenanceYieldsOnWriterContention(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	held, err := ws.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Rollback()
	started := time.Now()
	_, err = ws.MaintainScopeInboxBatch(ctx)
	if err == nil {
		t.Fatal("maintenance ignored active writer")
	}
	if time.Since(started) > 500*time.Millisecond {
		t.Fatalf("maintenance waited on writer: %s", time.Since(started))
	}
	_ = held.Rollback()
	var timeout int
	if err = ws.DB().QueryRow(`PRAGMA busy_timeout`).Scan(&timeout); err != nil || timeout != 20000 {
		t.Fatalf("serving timeout changed: %d %v", timeout, err)
	}
}

func TestScopeInboxOrderingPlansSeekBeforeLimit(t *testing.T) {
	t.Parallel()
	ws, err := InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	queries := []string{
		`SELECT id,category_rank,trigger_at FROM scope_inbox_live_positions ORDER BY category_rank,trigger_at DESC,id ASC LIMIT 29`,
		`SELECT id,category_rank,trigger_at FROM scope_inbox_live_positions WHERE category_rank=1 AND trigger_at='2026' AND id>'cursor' ORDER BY category_rank,trigger_at DESC,id ASC LIMIT 29`,
		`SELECT id,category_rank,trigger_at FROM scope_inbox_live_positions WHERE category_rank=1 AND trigger_at<'2026' ORDER BY category_rank,trigger_at DESC,id ASC LIMIT 29`,
		`SELECT id,category_rank,trigger_at FROM scope_inbox_live_positions WHERE category_rank>1 ORDER BY category_rank,trigger_at DESC,id ASC LIMIT 29`,
		`SELECT id,category_rank,trigger_at FROM scope_inbox_live_positions WHERE scope_id='thread' AND category_rank>1 ORDER BY category_rank,trigger_at DESC,id ASC LIMIT 29`,
	}
	for _, q := range queries {
		rows, err := ws.DB().Query(`EXPLAIN QUERY PLAN ` + q)
		if err != nil {
			t.Fatal(err)
		}
		var plans []string
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
				t.Fatal(err)
			}
			plans = append(plans, detail)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			t.Fatal(err)
		}
		plan := strings.Join(plans, "\n")
		if !strings.Contains(plan, "scope_inbox_live_") || strings.Contains(plan, "TEMP B-TREE") {
			t.Fatalf("unindexed/sorted candidate seek: %s\n%s", q, plan)
		}
	}
}
