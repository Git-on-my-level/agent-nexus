package storage_test

import (
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/resourceaccess"
	"agent-nexus-core/internal/storage"
	"context"
	"database/sql"
	"strings"
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
		`DROP INDEX idx_work_access_project`,
		`CREATE INDEX idx_work_access_project ON work_metadata(json_extract(metadata_json,'$.project_ref'))`,
		`DROP INDEX idx_wakeups_access_trigger_event`,
		`DELETE FROM schema_migrations WHERE version>=55`,
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
	assertAccessIndexesUsed(t, ws.DB())
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

func TestResourceAccessMigrationReconcilesPrivacy55(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	ws, err := storage.InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	// Recreate the earlier PR's already-applied migration 55. It has event and
	// plan ownership edges, but no metadata edges or permanent wakeup index.
	for _, statement := range []string{
		`DROP TRIGGER access_work_metadata_insert`,
		`DROP TRIGGER access_work_metadata_update`,
		`DROP TRIGGER access_work_metadata_delete`,
		`DELETE FROM resource_access_edges WHERE source_kind='card'`,
		`DROP INDEX idx_wakeups_access_trigger_event`,
		`DROP INDEX idx_work_access_project`,
		`CREATE INDEX idx_work_access_project ON work_metadata(json_extract(metadata_json,'$.project_ref'))`,
		`DELETE FROM schema_migrations WHERE version=56`,
	} {
		if _, err = ws.DB().Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	s := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	private, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "private target"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "owner", private["thread_id"].(string), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	linked, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "linked work"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ws.DB().Exec(`UPDATE work_metadata SET metadata_json=json_set(metadata_json,'$.relations',json_array(json_object('kind','related','ref',?))) WHERE card_id=?`, private["ref"], linked["id"]); err != nil {
		t.Fatal(err)
	}
	if err = ws.Close(); err != nil {
		t.Fatal(err)
	}
	ws, err = storage.InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	assertAccessIndexesUsed(t, ws.DB())
	s = primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	scoped := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "stranger"})
	if s.CanAccessResource(scoped, "card", linked["id"].(string)) {
		t.Fatal("preview-55 metadata was not backfilled")
	}
	if !s.CanAccessResource(primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "owner"}), "card", linked["id"].(string)) {
		t.Fatal("owner lost upgraded work")
	}
	if _, err = ws.DB().Exec(`DELETE FROM work_metadata WHERE card_id=?`, linked["id"]); err != nil {
		t.Fatal(err)
	}
	if !s.CanAccessResource(scoped, "card", linked["id"].(string)) {
		t.Fatal("metadata deletion left stale ownership")
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
			for _, table := range []string{"events", "agent_wakeups", "card_plans", "work_metadata", "runs"} {
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
			linked, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "legacy linked work"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = ws.DB().Exec(`UPDATE work_metadata SET metadata_json=json_set(metadata_json,'$.plan',json_object('steps',json_array(json_object('ref',?,'title','legacy secret')))) WHERE card_id=?`, card["ref"], linked["id"]); err != nil {
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
			for _, stmt := range []string{`DROP TABLE resource_access_edges`, `DROP TABLE resource_access_tombstones`, `DROP INDEX idx_ref_edges_access_target`, `DROP INDEX idx_cards_access_thread`, `DROP INDEX idx_inbox_access_card`, `DROP INDEX idx_work_access_project`, `DROP INDEX idx_inbox_access_event`, `DROP INDEX idx_wakeups_access_trigger_event`, `DELETE FROM schema_migrations WHERE version>=54`} {
				if history == "main-v54" && stmt == `DELETE FROM schema_migrations WHERE version>=54` {
					stmt = `DELETE FROM schema_migrations WHERE version>=55`
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
			assertAccessIndexesUsed(t, ws.DB())
			s = primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
			scoped := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "stranger"})
			if s.CanAccessResource(scoped, "event", e["id"].(string)) {
				t.Fatal("pre-migration payload leak")
			}
			if s.CanAccessResource(scoped, "card", linked["id"].(string)) {
				t.Fatal("pre-migration work metadata leak")
			}
			if _, err = ws.DB().Exec(`UPDATE work_metadata SET metadata_json=json_remove(metadata_json,'$.plan') WHERE card_id=?`, linked["id"]); err != nil {
				t.Fatal(err)
			}
			if !s.CanAccessResource(scoped, "card", linked["id"].(string)) {
				t.Fatal("updated public work denied")
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

func assertAccessIndexesUsed(t *testing.T, db *sql.DB) {
	t.Helper()
	// Disable transient indexes: the ownership probes must use durable indexes
	// after both fresh installation and reconciliation of the privacy-54 preview.
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err = conn.ExecContext(context.Background(), `PRAGMA automatic_index=OFF`); err != nil {
		t.Fatal(err)
	}
	defer conn.ExecContext(context.Background(), `PRAGMA automatic_index=ON`)
	for index, query := range map[string]string{
		"idx_work_access_project":          `SELECT card_id FROM work_metadata WHERE ` + resourceaccess.ReferenceSQL("json_extract(metadata_json,'$.project_ref')") + `='topic:private' COLLATE NOCASE`,
		"idx_wakeups_access_trigger_event": `SELECT wakeup_id FROM agent_wakeups WHERE trigger_event_id='private-event'`,
	} {
		rows, err := conn.QueryContext(context.Background(), "EXPLAIN QUERY PLAN "+query)
		if err != nil {
			t.Fatal(err)
		}
		var details []string
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
				t.Fatal(err)
			}
			details = append(details, detail)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.Join(details, "\n"), "USING INDEX "+index) {
			t.Errorf("ownership probe does not use %s: %v", index, details)
		}
	}
}
