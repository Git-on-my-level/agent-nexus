package storage_test

import (
	"context"
	"strings"
	"testing"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/storage"
)

func TestBriefAgentOwnershipMigrationAndSessionLifecycle(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	ws, err := storage.InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { ws.Close() }()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, e := ws.DB().ExecContext(ctx, query, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec(`INSERT INTO agents(id,username,actor_id,created_at,updated_at,metadata_json) VALUES('ownership-agent','ownership-agent','reader','2026-01-01','2026-01-01','{}')`)
	store := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	card, err := store.CreateWork(ctx, "reader", "", map[string]any{"title": "Ownership"})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"open", "closed"} {
		exec(`INSERT INTO agent_sessions(id,agent_id,actor_id,provider,host_scope,native_session_id,native_session_id_kind,capabilities_json,activity,sequence,request_json,created_at,last_seen_at,expires_at) VALUES(?,'ownership-agent','actor:reader','custom','local',?,'opaque','{}',?,1,'{}','2026-01-01','2026-01-01','2027-01-01')`, id, id, map[string]string{"open": "active", "closed": "closed"}[id])
		exec(`INSERT INTO work_participants(id,card_id,session_id,activity,sequence,request_json,created_at,last_seen_at,expires_at) VALUES(?,?,?,'active',1,'{}','2026-01-01','2026-01-01','2027-01-01')`, id, card["id"], id)
	}
	// Reconstruct the pre-105 participant schema with existing open and closed
	// sessions. Upgrade must backfill once, preserve activity, and be repeatable.
	for _, query := range []string{
		`DROP TRIGGER work_participants_session_active_insert`,
		`DROP TRIGGER work_participants_session_active_update`,
		`DROP TRIGGER agent_sessions_participants_update`,
		`DROP INDEX idx_work_participants_actor_card_active`,
		`DROP INDEX idx_work_participants_session`,
		`ALTER TABLE work_participants DROP COLUMN session_active`,
		`UPDATE work_participants SET actor_id=''`,
	} {
		exec(query)
	}
	check := func(id, actor string, active int) {
		t.Helper()
		var gotActor, activity string
		var gotActive int
		if e := ws.DB().QueryRowContext(ctx, `SELECT actor_id,session_active,activity FROM work_participants WHERE id=?`, id).Scan(&gotActor, &gotActive, &activity); e != nil {
			t.Fatal(e)
		}
		if gotActor != actor || gotActive != active || activity != "active" {
			t.Fatalf("%s: actor=%s session_active=%d activity=%s", id, gotActor, gotActive, activity)
		}
	}
	for i := 0; i < 2; i++ {
		exec(`DELETE FROM schema_migrations WHERE version=105`)
		if err = ws.Close(); err != nil {
			t.Fatal(err)
		}
		ws, err = storage.InitializeWorkspace(ctx, root)
		if err != nil {
			t.Fatal(err)
		}
		check("open", "reader", 1)
		check("closed", "reader", 0)
	}
	exec(`UPDATE agent_sessions SET activity='closed' WHERE id='open'`)
	check("open", "reader", 0)
	exec(`UPDATE agent_sessions SET activity='idle',actor_id='actor:renamed' WHERE id='open'`)
	check("open", "renamed", 1)
	// Rebinding an existing participant to a closed session refreshes both hints.
	exec(`DELETE FROM work_participants WHERE id='closed'`)
	exec(`UPDATE work_participants SET session_id='closed' WHERE id='open'`)
	check("open", "reader", 0)
	exec(`UPDATE agent_sessions SET activity='active' WHERE id='closed'`)
	check("open", "reader", 1)
	exec(`INSERT INTO work_participants(id,card_id,session_id,activity,sequence,request_json,created_at,last_seen_at,expires_at) VALUES('inserted',?,'open','active',1,'{}','2026-01-01','2026-01-01','2027-01-01')`, card["id"])
	check("inserted", "renamed", 1)
	rows, err := ws.DB().QueryContext(ctx, `EXPLAIN QUERY PLAN UPDATE work_participants SET session_active=0 WHERE session_id='closed'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(plan, "\n"), "idx_work_participants_session") {
		t.Fatalf("session maintenance is not indexed: %v", plan)
	}
}
