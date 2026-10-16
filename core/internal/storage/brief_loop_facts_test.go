package storage

import (
	"context"
	"strings"
	"testing"

	"agent-nexus-core/internal/primitives"
)

func TestBriefLoopFactsUpgradeAndReceiptMutations(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db := mainHistoryDatabase(t, root, 99)
	for _, q := range []string{
		`INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES('t','now','owner','{}')`,
		`INSERT INTO cards(id,title,summary,column_key,thread_id,created_at,created_by,updated_at,updated_by) VALUES('c','Loop','','todo','t','now','owner','now','owner'),('d','Other','','todo','t','now','owner','now','owner')`,
		`INSERT INTO events(id,type,ts,actor_id,thread_id,refs_json,payload_json) VALUES('r','run_recorded','2026-10-16T00:00:00Z','owner','t','["card:c"]','{"subject_ref":"card:c","payload":{"run_id":"one","outcome":"measured"}}')`,
		`DELETE FROM brief_dirty`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	ws, err := InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	db = ws.DB()
	check := func(q string, want int) {
		t.Helper()
		var got int
		if err := db.QueryRow(q).Scan(&got); err != nil || got != want {
			t.Fatalf("%s got=%d want=%d err=%v", q, got, want, err)
		}
	}
	check(`SELECT count(*) FROM resource_access_edges WHERE source_kind='brief_loop' AND source_id='loop-facts/c' AND target_ref='event:r'`, 1)
	check(`SELECT count(*) FROM brief_dirty WHERE card_id IN ('c','d')`, 2)
	// Re-running the installer is safe on a populated workspace.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = installBriefLoopFacts(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []struct {
		sql      string
		old, new int
	}{
		{`UPDATE events SET payload_json=json_set(payload_json,'$.subject_ref','card:d') WHERE id='r'`, 0, 1},
		{`UPDATE events SET type='card_updated' WHERE id='r'`, 0, 0},
		{`UPDATE events SET type='run_recorded' WHERE id='r'`, 0, 1},
		{`DELETE FROM events WHERE id='r'`, 0, 0},
	} {
		if _, err = db.Exec(`DELETE FROM brief_dirty`); err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(mutation.sql); err != nil {
			t.Fatal(err)
		}
		check(`SELECT count(*) FROM resource_access_edges WHERE source_kind='brief_loop' AND source_id='loop-facts/c'`, mutation.old)
		check(`SELECT count(*) FROM resource_access_edges WHERE source_kind='brief_loop' AND source_id='loop-facts/d'`, mutation.new)
		check(`SELECT count(*) FROM brief_dirty WHERE card_id='d'`, 1)
	}
}

func TestIncumbentLoopFactsUpgradeInvalidatesOldCaches(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db := mainHistoryDatabase(t, root, 100)
	if _, err := db.Exec(`INSERT INTO brief_initiatives(card_id,computed_at,loop_facts_json,loop_valid_from,loop_valid_until) VALUES('old-loop','now','{"best_before":99,"best_after":100}','2026-01-01T00:00:00Z','2099-01-01T00:00:00Z'); DELETE FROM brief_dirty`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	ws, err := InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err = ws.DB().QueryRow(`SELECT COUNT(*) FROM brief_dirty WHERE card_id='old-loop'`).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if _, err = ws.DB().Exec(`DELETE FROM brief_dirty`); err != nil {
		t.Fatal(err)
	}
	if err = ws.Close(); err != nil {
		t.Fatal(err)
	}
	ws, err = InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	if err = ws.DB().QueryRow(`SELECT COUNT(*) FROM brief_dirty WHERE card_id='old-loop'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("cache requeued again on restart", count, err)
	}
}

func TestLoopAdmissionUpgradeAndMutations(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db := mainHistoryDatabase(t, root, 101)
	for _, query := range []string{
		`INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES('t','now','owner','{}')`,
		`INSERT INTO cards(id,title,summary,column_key,thread_id,created_at,created_by,updated_at,updated_by) VALUES('c','Loop','','todo','t','now','owner','now','owner'),('d','Other','','todo','t','now','owner','now','owner')`,
		`INSERT INTO work_metadata(card_id,metadata_json,updated_at,updated_by) VALUES('c','{"goal":{"metric":"score","direction":"up"}}','now','owner')`,
		`INSERT INTO events(id,type,ts,actor_id,thread_id,refs_json,payload_json) VALUES('r','run_recorded','2026-10-16T00:00:00Z','owner','t','["card:c"]','{"subject_ref":"card:c","payload":{"run_id":"one","outcome":"measured"}}')`,
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	ws, err := InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	db = ws.DB()
	check := func(id string, annotated int, last string) {
		t.Helper()
		var got int
		var at string
		if err := db.QueryRow(`SELECT loop_annotated,COALESCE(loop_last_run_at,'') FROM brief_initiatives WHERE card_id=?`, id).Scan(&got, &at); err != nil || got != annotated || at != last {
			t.Fatal(id, got, at, err)
		}
	}
	at := "2026-10-16T00:00:00Z"
	check("c", 1, at)
	check("d", 0, "")
	for _, query := range []string{
		`UPDATE work_metadata SET metadata_json='{}' WHERE card_id='c'`,
		`UPDATE events SET payload_json=json_set(payload_json,'$.subject_ref','card:d') WHERE id='r'`,
	} {
		if _, err = db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	check("c", 0, "")
	check("d", 0, at)
	for _, query := range []string{
		`UPDATE events SET archived_at='now' WHERE id='r'`,
		`UPDATE events SET archived_at=NULL WHERE id='r'`,
		`UPDATE events SET type='card_updated' WHERE id='r'`,
		`UPDATE events SET type='run_recorded' WHERE id='r'`,
		`DELETE FROM events WHERE id='r'`,
	} {
		if _, err = db.Exec(query); err != nil {
			t.Fatal(err)
		}
		want := ""
		if query == `UPDATE events SET archived_at=NULL WHERE id='r'` || query == `UPDATE events SET type='run_recorded' WHERE id='r'` {
			want = at
		}
		check("d", 0, want)
	}
	if _, err = db.Exec(`UPDATE work_metadata SET metadata_json='{"cadence":{"every_seconds":60}}' WHERE card_id='c'`); err != nil {
		t.Fatal(err)
	}
	check("c", 1, "")
	if _, err = db.Exec(`DELETE FROM work_metadata WHERE card_id='c'`); err != nil {
		t.Fatal(err)
	}
	check("c", 0, "")
}

func TestLoopAdmissionRecencyUpgradeAndMetadataWrites(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db := mainHistoryDatabase(t, root, 102)
	for _, query := range []string{
		`INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES('t','now','owner','{}')`,
		`INSERT INTO cards(id,title,summary,column_key,thread_id,created_at,created_by,updated_at,updated_by) VALUES('c','Loop','','todo','t','now','owner','now','owner')`,
		`INSERT INTO work_metadata(card_id,metadata_json,updated_at,updated_by) VALUES('c','{"goal":{"metric":"score","direction":"up"}}','2026-10-15T10:00:00Z','owner')`,
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	ws, err := InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	db = ws.DB()
	check := func(want string) {
		t.Helper()
		var at string
		if err := db.QueryRow(`SELECT loop_annotated_at FROM brief_initiatives WHERE card_id='c'`).Scan(&at); err != nil || at != want {
			t.Fatal(at, want, err)
		}
	}
	check("2026-10-15T10:00:00Z")
	if _, err = db.Exec(`UPDATE work_metadata SET updated_at='2026-10-16T10:00:00Z' WHERE card_id='c'`); err != nil {
		t.Fatal(err)
	}
	check("2026-10-16T10:00:00Z")
	if _, err = db.Exec(`DELETE FROM work_metadata WHERE card_id='c'`); err != nil {
		t.Fatal(err)
	}
	check("")
	if _, err = db.Exec(`INSERT INTO work_metadata(card_id,metadata_json,updated_at,updated_by) VALUES('c','{"cadence":{"every_seconds":60}}','2026-10-16T11:00:00Z','owner')`); err != nil {
		t.Fatal(err)
	}
	check("2026-10-16T11:00:00Z")
}

func TestUpgradeFrom103UpdatesAgentSummaryViewAndBriefAskIndex(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db := mainHistoryDatabase(t, root, 103)
	for _, query := range []string{
		`INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES('t','2026-10-16T00:00:00Z','owner','{}')`,
		`INSERT INTO cards(id,handle,board_id,rank,title,summary,column_key,assignee,thread_id,created_at,created_by,updated_at,updated_by) VALUES('card-old','card-old','','','Old','','in_progress','agent-1','t','2026-10-16T00:00:00Z','owner','2026-10-16T00:01:00Z','owner'),('card-new','card-new','','','New','','in_progress','agent-1','t','2026-10-16T00:00:00Z','owner','2026-10-16T00:02:00Z','owner'),('card-done','card-done','','','Done','','done','other-agent','t','2026-10-16T00:00:00Z','owner','2026-10-16T00:03:00Z','owner')`,
		`INSERT INTO events(id,type,ts,actor_id,thread_id,refs_json,payload_json) VALUES('ask-stale','human_attention_requested','2026-10-16T00:00:00Z','agent-1','t','["thread:t"]','{"payload":{"kind":"ask","title":"Stale blocker","requester_actor_id":"agent-1","subject_ref":"card:card-old","blocks":["card:card-done"],"priority":"low"}}')`,
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	// Pre-106 schema has no updated_at column in agent_summary_card_positions and counts the completed blocker in blocks_count.
	if _, err := db.Query(`SELECT updated_at FROM agent_summary_card_positions LIMIT 1`); err == nil {
		t.Fatal("expected pre-106 agent_summary_card_positions to lack updated_at")
	}
	var preBlocksCount int
	if err := db.QueryRow(`SELECT blocks_count FROM brief_asks WHERE ask_id='ask-stale'`).Scan(&preBlocksCount); err != nil || preBlocksCount != 1 {
		t.Fatalf("pre-106 blocks_count = %d, err = %v", preBlocksCount, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	ws, err := InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	db = ws.DB()

	store := primitives.NewTestStore(db, ws.Layout().ArtifactContentDir)
	summaryCards, _, err := store.AgentSummaryCards(ctx, "actor:agent-1", nil)
	if err != nil || len(summaryCards) != 2 || summaryCards[0]["id"] != "card-new" || summaryCards[1]["id"] != "card-old" {
		t.Fatalf("AgentSummaryCards after upgrade = %#v err=%v", summaryCards, err)
	}
	var postBlocksCount int
	if err := db.QueryRow(`SELECT blocks_count FROM brief_asks WHERE ask_id='ask-stale'`).Scan(&postBlocksCount); err != nil || postBlocksCount != 1 {
		t.Fatalf("stored admission hint for completed blocker = %d err=%v, want 1 (effective count is read-time)", postBlocksCount, err)
	}

	rows, err := db.Query(`EXPLAIN QUERY PLAN SELECT ask_id FROM brief_asks a WHERE a.state='open' AND a.decide_by IS NULL ORDER BY a.blocks_count DESC, CASE lower(trim(a.priority)) WHEN 'critical' THEN 0 WHEN 'p0' THEN 0 WHEN 'urgent' THEN 0 WHEN 'high' THEN 1 WHEN 'p1' THEN 1 WHEN 'medium' THEN 2 WHEN 'p2' THEN 2 WHEN 'normal' THEN 2 WHEN 'low' THEN 3 WHEN 'p3' THEN 3 ELSE 4 END, a.created_at, a.ask_id LIMIT 60`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	usedIndex := false
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(detail, "USE TEMP B-TREE FOR ORDER BY") {
			t.Fatalf("upgraded brief_asks query required temp b-tree sort: %s", detail)
		}
		if strings.Contains(detail, "idx_brief_asks_open_page") {
			usedIndex = true
		}
	}
	if !usedIndex {
		t.Fatal("expected idx_brief_asks_open_page to be used after upgrade")
	}

	// Re-running migration 106 helper is idempotent.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := upgradeAgentSummaryAndBriefAskIndexes(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestUpgrade106SeriesCapacityBackfill(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db := mainHistoryDatabase(t, root, 105)
	_, err := db.Exec(`INSERT INTO series_adapters(name,description,agent_id,host_id,expected_interval,created_at) VALUES('active','Active','owner','host',60,'2026-10-16T00:00:00Z');
 INSERT INTO series_definitions(name,adapter,kind,unit) VALUES('historical','active','gauge','items'),('raw','active','gauge','items');
 INSERT INTO series_labels(series,labels) VALUES('historical','{}'),('raw','{}');
 INSERT INTO series_daily(series,labels,day,n,last_ts,last_value) VALUES('historical','{}',0,1,123,1);
 INSERT INTO series_points(series,labels,ts,value,received_day) VALUES('raw','{}',456,1,0);`)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	ws, err := InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	db = ws.DB()
	for i := 0; i < 2; i++ {
		var n int
		if err = db.QueryRow(`SELECT definition_count FROM series_adapters WHERE name='active'`).Scan(&n); err != nil || n != 2 {
			t.Fatal(n, err)
		}
		for name, want := range map[string]int64{"historical": 123, "raw": 456} {
			var got int64
			if err = db.QueryRow(`SELECT last_point_ts FROM series_labels WHERE series=?`, name).Scan(&got); err != nil || got != want {
				t.Fatal(name, got, err)
			}
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err = upgradeAgentSummaryAndBriefAskIndexes(ctx, tx); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	// Definition edits keep the hint exact, including adapter moves and deletes.
	if _, err = db.Exec(`INSERT INTO series_adapters(name,description,agent_id,host_id,expected_interval,created_at) VALUES('destination','Destination','owner','host',60,'2026-10-16T00:00:00Z');
 UPDATE series_definitions SET adapter='destination' WHERE name='raw';
 DELETE FROM series_definitions WHERE name='historical';`); err != nil {
		t.Fatal(err)
	}
	var a, b int
	if err = db.QueryRow(`SELECT (SELECT definition_count FROM series_adapters WHERE name='active'),(SELECT definition_count FROM series_adapters WHERE name='destination')`).Scan(&a, &b); err != nil || a != 0 || b != 1 {
		t.Fatal(a, b, err)
	}
}
