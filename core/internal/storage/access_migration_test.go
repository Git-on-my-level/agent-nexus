package storage_test

import (
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/storage"
	"context"
	"testing"
)

func TestResourceAccessMigrationReconcilesPrivacy54(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	ws, err := storage.InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO resource_access_tombstones VALUES('card','retained-card','card:retained-card','owner')`,
		`DROP TABLE access_requests`,
		`DROP INDEX host_enrollments_pending_expiry`,
		`DELETE FROM schema_migrations WHERE version=55`,
	} {
		if _, err := ws.DB().Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := ws.Close(); err != nil {
		t.Fatal(err)
	}
	ws, err = storage.InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	var count int
	if err := ws.DB().QueryRow(`SELECT COUNT(*) FROM resource_access_tombstones WHERE id='retained-card'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("lost retained ownership: count=%d err=%v", count, err)
	}
	if err := ws.DB().QueryRow(`SELECT COUNT(*) FROM access_requests`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if err := ws.DB().QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version IN (54,55)`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("migration reconciliation: count=%d err=%v", count, err)
	}
}

func TestResourceAccessMigrationBackfillsAndMaintainsPayloadEdges(t *testing.T) {
	for _, history := range []string{"v53", "main-v54"} {
		t.Run(history, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			ws, err := storage.InitializeWorkspace(ctx, root)
			if err != nil {
				t.Fatal(err)
			}
			// Reconstruct the selected pre-privacy schema while retaining canonical rows.
			for _, table := range []string{"events", "agent_wakeups", "card_plans", "runs"} {
				for _, op := range []string{"insert", "update", "delete"} {
					if _, err = ws.DB().Exec(`DROP TRIGGER access_` + table + `_` + op); err != nil {
						t.Fatal(err)
					}
				}
			}
			s := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
			card, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "legacy private"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.PatchThread(ctx, "owner", card["thread_id"].(string), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
				t.Fatal(err)
			}
			e, err := s.AppendEvent(ctx, "owner", map[string]any{"type": "message_posted", "refs": []string{}, "payload": map[string]any{"subject_ref": card["ref"]}})
			if err != nil {
				t.Fatal(err)
			}
			if history == "v53" {
				for _, stmt := range []string{`DROP TABLE access_requests`, `DROP INDEX host_enrollments_pending_expiry`} {
					if _, err := ws.DB().Exec(stmt); err != nil {
						t.Fatal(err)
					}
				}
			}
			for _, stmt := range []string{`DROP TABLE resource_access_edges`, `DROP TABLE resource_access_tombstones`, `DROP INDEX idx_ref_edges_access_target`, `DROP INDEX idx_cards_access_thread`, `DROP INDEX idx_inbox_access_card`, `DROP INDEX idx_work_access_project`, `DROP INDEX idx_inbox_access_event`, `DELETE FROM schema_migrations WHERE version>=54`} {
				if history == "main-v54" && stmt == `DELETE FROM schema_migrations WHERE version>=54` {
					stmt = `DELETE FROM schema_migrations WHERE version=55`
				}
				if _, err = ws.DB().Exec(stmt); err != nil {
					t.Fatal(err)
				}
			}
			if err = ws.Close(); err != nil {
				t.Fatal(err)
			}
			ws, err = storage.InitializeWorkspace(ctx, root)
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Close()
			s = primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
			scoped := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "stranger"})
			if s.CanAccessResource(scoped, "event", e["id"].(string)) {
				t.Fatal("pre-migration payload leak")
			}
			// Raw SQL imports are covered, including updates and deletion of the index.
			if _, err = ws.DB().Exec(`INSERT INTO events(id,type,ts,actor_id,payload_json) VALUES('import','message_posted','now','owner',json_object('payload',json_object('related_refs',json_array(?))))`, card["ref"]); err != nil {
				t.Fatal(err)
			}
			if s.CanAccessResource(scoped, "event", "import") {
				t.Fatal("raw import payload leak")
			}
			if _, err = ws.DB().Exec(`UPDATE events SET payload_json='{}' WHERE id='import'`); err != nil {
				t.Fatal(err)
			}
			if !s.CanAccessResource(scoped, "event", "import") {
				t.Fatal("updated public import denied")
			}
			if _, err = ws.DB().Exec(`DELETE FROM events WHERE id=?`, e["id"]); err != nil {
				t.Fatal(err)
			}
			var n int
			if err = ws.DB().QueryRow(`SELECT COUNT(*) FROM resource_access_edges WHERE source_kind='event' AND source_id=?`, e["id"]).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n != 0 {
				t.Fatal("orphan ownership edges")
			}
		})
	}
}
